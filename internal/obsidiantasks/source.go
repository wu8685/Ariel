package obsidiantasks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wu8685/Ariel/protocol"
	"gopkg.in/yaml.v3"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

type Options struct {
	VaultRoot     string
	DefaultHostID string
	Now           func() time.Time
	NewID         func() string
}

type Source struct {
	root, taskDir, storeID, statePath, defaultHostID string
	now                                              func() time.Time
	newID                                            func() string
	mu                                               sync.Mutex
	issued                                           map[string]issuedCommand
	leased                                           map[string]bool
	receipts                                         map[string]protocol.TaskReceipt
	handled                                          map[string]string
}

type issuedCommand struct {
	command protocol.TaskCommand
	path    string
}

type taskEntry struct {
	path, content, body, name, status, taskID, hostID, agentID, claimedAt, workspace, revision string
}

type deliveryJournal struct {
	Version int                       `json:"version"`
	Issued  map[string]journalCommand `json:"issued"`
	Handled map[string]string         `json:"handled,omitempty"`
}

type journalCommand struct {
	Command protocol.TaskCommand  `json:"command"`
	Path    string                `json:"path"`
	Receipt *protocol.TaskReceipt `json:"receipt,omitempty"`
}

func New(opts Options) (*Source, error) {
	if !filepath.IsAbs(opts.VaultRoot) {
		return nil, errors.New("vault root must be absolute")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(opts.VaultRoot))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, errors.New("vault root must be a directory")
	}
	taskDir := filepath.Join(root, "tasks", "list")
	if info, err = os.Stat(taskDir); err != nil || !info.IsDir() {
		return nil, errors.New("tasks/list must be a directory")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	newID := opts.NewID
	if newID == nil {
		newID = randomUUID
	}
	digest := sha256.Sum256([]byte(root))
	if opts.DefaultHostID != "" && !safeID.MatchString(opts.DefaultHostID) {
		return nil, errors.New("default host ID is invalid")
	}
	source := &Source{
		root: root, taskDir: taskDir, storeID: "obsidian-" + hex.EncodeToString(digest[:8]),
		statePath: filepath.Join(root, ".ariel", "tasks", "state.json"), defaultHostID: opts.DefaultHostID,
		now: now, newID: newID, issued: map[string]issuedCommand{}, leased: map[string]bool{},
		receipts: map[string]protocol.TaskReceipt{}, handled: map[string]string{},
	}
	if err := source.loadJournal(); err != nil {
		return nil, err
	}
	return source, nil
}

func randomUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func (s *Source) Ready() bool         { return s != nil && s.root != "" }
func (s *Source) TaskStoreID() string { return s.storeID }

func (s *Source) Call(_ context.Context, method string, params, output any) error {
	if !s.Ready() {
		return errors.New("OBSIDIAN_TASK_SOURCE_UNAVAILABLE")
	}
	var result any = map[string]any{}
	var err error
	switch method {
	case "task.command.claim":
		var commands []protocol.TaskCommand
		commands, err = s.claimCommands()
		result = map[string]any{"commands": commands}
	case "task.command.tracking":
		result = map[string]any{"commands": s.trackingCommands(), "nextCursor": ""}
	case "task.command.validate":
		result, err = s.validate(params)
	case "task.command.record":
		err = s.recordReceipt(params)
	case "task.fact.record":
		err = s.recordFact(params)
	case "task.list":
		result = map[string]any{"taskStoreId": s.storeID, "tasks": []any{}}
	default:
		err = errors.New("INVALID_ARGUMENT")
	}
	if err != nil || output == nil {
		return err
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return marshalErr
	}
	return json.Unmarshal(encoded, output)
}

func (s *Source) scan() ([]taskEntry, error) {
	entries := make([]taskEntry, 0)
	err := filepath.WalkDir(s.taskDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !within(s.taskDir, resolved) {
			return nil
		}
		if err := s.normalizeTask(resolved); err != nil {
			return nil
		}
		parsed, err := readTask(resolved)
		if err != nil {
			return nil
		}
		parsed.path = resolved
		entries = append(entries, parsed)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, err
}

func (s *Source) normalizeTask(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return err
	}
	values, err := frontmatterScalars(frontmatter)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(values["status"])
	if status != "TODO" && status != "WIP" && status != "DONE" {
		return errors.New("invalid task status")
	}
	nativeCard := !strings.Contains(body, "## 任务描述") || !strings.Contains(body, "## 成功条件（选填）") || !strings.Contains(body, "## 评论")
	updates := make([]frontmatterUpdate, 0, 8)
	filename := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if name := strings.TrimSpace(values["name"]); name == "" || (name == "未命名" && filename != name) {
		updates = append(updates, frontmatterUpdate{"name", yamlScalar(filename)})
	}
	today := s.now().Format("2006-01-02")
	if strings.TrimSpace(values["date_created"]) == "" {
		updates = append(updates, frontmatterUpdate{"date_created", today})
	}
	if strings.TrimSpace(values["date_modified"]) == "" {
		updates = append(updates, frontmatterUpdate{"date_modified", today})
	}
	if strings.TrimSpace(values["tags"]) == "" {
		updates = append(updates, frontmatterUpdate{"tags", "[task]"})
	}
	if strings.TrimSpace(values["task_id"]) == "" {
		id := s.newID()
		if id == "" || !safeID.MatchString(id) {
			return errors.New("generated task ID is invalid")
		}
		updates = append(updates, frontmatterUpdate{"task_id", "task-" + id})
	}
	if nativeCard && strings.TrimSpace(values["agent_host"]) == "" && s.defaultHostID != "" {
		updates = append(updates, frontmatterUpdate{"agent_host", s.defaultHostID})
	}
	for _, key := range []string{"agent_id", "claimed_at"} {
		if _, ok := values[key]; !ok {
			updates = append(updates, frontmatterUpdate{key, ""})
		}
	}
	changed := len(updates) > 0
	if changed {
		content, err = setFrontmatterUpdates(content, updates)
		if err != nil {
			return err
		}
	}
	if nativeCard {
		content = strings.TrimRight(content, "\n") + "\n\n## 任务描述\n\n\n\n## 成功条件（选填）\n\n- [ ]\n\n## 评论\n"
		changed = true
	}
	if !changed {
		return nil
	}
	return atomicWrite(path, []byte(content))
}

func frontmatterScalars(frontmatter string) (map[string]string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatter), &document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("invalid frontmatter")
	}
	values := map[string]string{}
	root := document.Content[0]
	for index := 0; index+1 < len(root.Content); index += 2 {
		key, value := root.Content[index].Value, root.Content[index+1]
		if value.Kind == yaml.ScalarNode {
			values[key] = strings.TrimSpace(value.Value)
		} else if key == "tags" && value.Kind == yaml.SequenceNode && len(value.Content) > 0 {
			values[key] = value.Content[0].Value
		}
	}
	return values, nil
}

type frontmatterUpdate struct{ key, value string }

func yamlScalar(value string) string {
	if value != "" && !strings.ContainsAny(value, "\r\n:#[]{}\"") {
		return value
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func setFrontmatterUpdates(content string, updates []frontmatterUpdate) (string, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return "", err
	}
	lines := strings.Split(frontmatter, "\n")
	for _, update := range updates {
		found := false
		for index, line := range lines {
			if strings.TrimSpace(strings.SplitN(line, ":", 2)[0]) == update.key && strings.Contains(line, ":") {
				lines[index] = update.key + ": " + update.value
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, update.key+": "+update.value)
		}
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body, nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *Source) claimCommands() ([]protocol.TaskCommand, error) {
	entries, err := s.scan()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneSupersededLocked(entries) {
		if err := s.persistJournalLocked(); err != nil {
			return nil, err
		}
	}
	commands := make([]protocol.TaskCommand, 0)
	for _, entry := range entries {
		if s.handled[entry.taskID] == entry.revision {
			continue
		}
		kind := ""
		nativeThreadID := ""
		switch {
		case entry.status == "TODO" && entry.hostID != "" && entry.agentID == "" && entry.claimedAt == "":
			kind = "claim"
		case entry.status == "WIP" && entry.hostID != "" && entry.agentID != "":
			kind, nativeThreadID = "work", entry.agentID
		default:
			continue
		}
		commandID := stableID("command", kind, entry.taskID, entry.revision)
		if s.leased[commandID] {
			continue
		}
		command := protocol.TaskCommand{
			TaskStoreID: s.storeID, CommandID: commandID, TaskID: entry.taskID,
			AttemptID: stableID("attempt", kind, entry.taskID, entry.revision), ExecutionHostID: entry.hostID,
			BindingGeneration: 1, TaskVersion: 1, Kind: kind, TaskRevision: entry.revision,
			NativeThreadID: nativeThreadID, ClientMessageID: stableID("message", kind, entry.taskID, entry.revision),
			LeaseToken: stableID("lease", kind, entry.taskID, entry.revision), CWD: entry.workspace,
		}
		if command.CWD == "" {
			command.CWD = s.root
		}
		if kind == "claim" {
			command.Text = claimPrompt(entry)
		} else {
			command.Text = workPrompt(entry)
		}
		if len(command.Text) > 49152 {
			continue
		}
		s.leased[commandID] = true
		s.issued[commandID] = issuedCommand{command: command, path: entry.path}
		if err := s.persistJournalLocked(); err != nil {
			delete(s.leased, commandID)
			delete(s.issued, commandID)
			return nil, err
		}
		commands = append(commands, command)
		if len(commands) == 8 {
			break
		}
	}
	return commands, nil
}

func (s *Source) pruneSupersededLocked(entries []taskEntry) bool {
	current := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.taskID != "" {
			current[entry.taskID] = entry.revision
		}
	}
	changed := false
	for commandID, issued := range s.issued {
		revision, exists := current[issued.command.TaskID]
		if exists && revision == issued.command.TaskRevision {
			continue
		}
		delete(s.issued, commandID)
		delete(s.leased, commandID)
		delete(s.receipts, commandID)
		changed = true
	}
	return changed
}

func (s *Source) trackingCommands() []protocol.TaskCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := make([]protocol.TaskCommand, 0)
	for commandID, receipt := range s.receipts {
		issued, ok := s.issued[commandID]
		if !ok || receipt.Outcome != "accepted" || receipt.NativeThreadID == "" || receipt.NativeTurnID == "" {
			continue
		}
		command := issued.command
		command.NativeThreadID = receipt.NativeThreadID
		command.NativeTurnID = receipt.NativeTurnID
		command.State = "accepted"
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].CommandID < commands[j].CommandID })
	if len(commands) > 8 {
		commands = commands[:8]
	}
	return commands
}

func stableID(prefix string, parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "-" + hex.EncodeToString(digest[:16])
}

func claimPrompt(entry taskEntry) string {
	return fmt.Sprintf("You are the Codex owner selected to claim Obsidian task %q. This is a claim-only turn: do not perform the task or mutate workspace state. You may complete mandatory read-only workspace initialization required by higher-priority instructions. Then reply with exactly this JSON and no Markdown or extra text: {\"task_action\":\"claim\",\"task_id\":%q,\"task_revision\":%q}", entry.name, entry.taskID, entry.revision)
}

func workPrompt(entry taskEntry) string {
	return fmt.Sprintf("You own Obsidian task %q. Complete the task below within the current permissions. When and only when its success conditions are satisfied, finish with exactly {\"task_action\":\"done\",\"task_id\":%q,\"task_revision\":%q}. If blocked, finish with exactly the same object using task_action \"blocked\".\n\n%s", entry.name, entry.taskID, entry.revision, strings.TrimSpace(entry.body))
}

func (s *Source) validate(params any) (map[string]any, error) {
	var request struct {
		CommandID  string `json:"commandId"`
		LeaseToken string `json:"leaseToken"`
		ReadOnly   bool   `json:"readOnly"`
	}
	if remarshal(params, &request) != nil {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	s.mu.Lock()
	issued, ok := s.issued[request.CommandID]
	s.mu.Unlock()
	if !ok || issued.command.LeaseToken != request.LeaseToken {
		return map[string]any{"valid": false}, nil
	}
	entry, err := readTask(issued.path)
	if err != nil || entry.revision != issued.command.TaskRevision || entry.taskID != issued.command.TaskID || entry.hostID != issued.command.ExecutionHostID {
		return map[string]any{"valid": false}, nil
	}
	valid := issued.command.Kind == "claim" && entry.status == "TODO" && entry.agentID == "" && entry.claimedAt == ""
	valid = valid || issued.command.Kind == "work" && entry.status == "WIP" && entry.agentID == issued.command.NativeThreadID
	if !valid {
		return map[string]any{"valid": false}, nil
	}
	command := issued.command
	if request.ReadOnly {
		s.mu.Lock()
		receipt, ok := s.receipts[request.CommandID]
		s.mu.Unlock()
		if !ok || receipt.Outcome != "accepted" || receipt.NativeThreadID == "" || receipt.NativeTurnID == "" {
			return map[string]any{"valid": false}, nil
		}
		command.NativeThreadID = receipt.NativeThreadID
		command.NativeTurnID = receipt.NativeTurnID
		command.State = "accepted"
	}
	return map[string]any{"valid": true, "command": command}, nil
}

func (s *Source) recordReceipt(params any) error {
	var receipt protocol.TaskReceipt
	if remarshal(params, &receipt) != nil {
		return errors.New("INVALID_ARGUMENT")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issued, ok := s.issued[receipt.CommandID]
	if !ok || issued.command.LeaseToken != receipt.LeaseToken {
		return nil
	}
	if receipt.Outcome == "accepted" && receipt.NativeThreadID != "" && receipt.NativeTurnID != "" {
		s.receipts[receipt.CommandID] = receipt
		return s.persistJournalLocked()
	}
	delete(s.leased, receipt.CommandID)
	delete(s.issued, receipt.CommandID)
	delete(s.receipts, receipt.CommandID)
	return s.persistJournalLocked()
}

func (s *Source) recordFact(params any) error {
	var fact protocol.TaskFact
	if remarshal(params, &fact) != nil {
		return errors.New("INVALID_ARGUMENT")
	}
	s.mu.Lock()
	issued, ok := s.issued[fact.CommandID]
	receipt := s.receipts[fact.CommandID]
	s.mu.Unlock()
	if !ok || fact.TaskID != issued.command.TaskID || fact.AttemptID != issued.command.AttemptID || fact.BindingGeneration != issued.command.BindingGeneration {
		return nil
	}
	if fact.Status == "completed" && fact.TaskRevision != issued.command.TaskRevision {
		return nil
	}
	if receipt.NativeThreadID == "" || receipt.NativeThreadID != fact.NativeThreadID || receipt.NativeTurnID != fact.NativeTurnID {
		return nil
	}
	var err error
	if issued.command.Kind == "claim" && fact.Status == "completed" && fact.TaskAction == "claim" {
		err = s.applyClaim(issued, fact.NativeThreadID)
	} else if issued.command.Kind == "work" && fact.Status == "completed" && (fact.TaskAction == "done" || fact.TaskAction == "blocked") {
		err = s.applyOutcome(issued, fact.TaskAction)
	}
	if err != nil {
		return err
	}
	if fact.Status == "completed" || fact.Status == "failed" {
		s.mu.Lock()
		if issued.command.Kind == "work" && fact.Status == "completed" && fact.TaskAction == "blocked" {
			if entry, readErr := readTask(issued.path); readErr == nil {
				s.handled[entry.taskID] = entry.revision
			}
		}
		delete(s.leased, fact.CommandID)
		delete(s.issued, fact.CommandID)
		delete(s.receipts, fact.CommandID)
		err = s.persistJournalLocked()
		s.mu.Unlock()
	}
	return err
}

func (s *Source) applyClaim(issued issuedCommand, threadID string) error {
	entry, err := readTask(issued.path)
	if err != nil {
		return err
	}
	if entry.revision != issued.command.TaskRevision || entry.status != "TODO" || entry.hostID != issued.command.ExecutionHostID || entry.agentID != "" || entry.claimedAt != "" || !safeID.MatchString(threadID) {
		return errors.New("STALE_TASK_AUTHORIZATION")
	}
	now := s.now()
	updated, err := setFrontmatter(entry.content, map[string]string{
		"status": "WIP", "agent_id": threadID, "claimed_at": now.Format(time.RFC3339), "date_modified": now.Format("2006-01-02"),
	})
	if err != nil {
		return err
	}
	updated = appendComment(updated, "claim-"+entry.taskID, now, "Codex "+threadID, "已在 Ariel 主机 `"+entry.hostID+"` 领取任务，状态已更新为 `WIP`。", "进展")
	return atomicWrite(issued.path, []byte(updated))
}

func (s *Source) applyOutcome(issued issuedCommand, action string) error {
	entry, err := readTask(issued.path)
	if err != nil {
		return err
	}
	if entry.revision != issued.command.TaskRevision || entry.status != "WIP" || entry.agentID != issued.command.NativeThreadID {
		return errors.New("STALE_TASK_AUTHORIZATION")
	}
	now := s.now()
	commentType, body, status := "阻塞", "Codex 报告任务受阻，保留 `WIP` 等待处理。", "WIP"
	if action == "done" {
		commentType, body, status = "进展", "Codex 已确认任务成功条件满足，状态已更新为 `DONE`。", "DONE"
	}
	updated, err := setFrontmatter(entry.content, map[string]string{"status": status, "date_modified": now.Format("2006-01-02")})
	if err != nil {
		return err
	}
	updated = appendComment(updated, action+"-"+entry.taskID+"-"+entry.revision[:12], now, "Codex "+entry.agentID, body, commentType)
	return atomicWrite(issued.path, []byte(updated))
}

func remarshal(input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func readTask(path string) (taskEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return taskEntry{}, err
	}
	content := string(data)
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil || !strings.Contains(body, "## 任务描述") || !strings.Contains(body, "## 评论") {
		return taskEntry{}, errors.New("invalid task document")
	}
	var document yaml.Node
	if yaml.Unmarshal([]byte(frontmatter), &document) != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return taskEntry{}, errors.New("invalid frontmatter")
	}
	root := document.Content[0]
	get := func(key string) string {
		for index := 0; index+1 < len(root.Content); index += 2 {
			if root.Content[index].Value == key && root.Content[index+1].Kind == yaml.ScalarNode {
				return strings.TrimSpace(root.Content[index+1].Value)
			}
		}
		return ""
	}
	entry := taskEntry{content: content, body: body, name: get("name"), status: get("status"), taskID: get("task_id"), hostID: get("agent_host"), agentID: get("agent_id"), claimedAt: get("claimed_at"), workspace: get("workspace")}
	if entry.name == "" || get("date_created") == "" || get("date_modified") == "" || (entry.status != "TODO" && entry.status != "WIP" && entry.status != "DONE") {
		return taskEntry{}, errors.New("invalid task metadata")
	}
	if entry.taskID != "" && !safeID.MatchString(entry.taskID) || entry.hostID != "" && !safeID.MatchString(entry.hostID) || entry.agentID != "" && !safeID.MatchString(entry.agentID) {
		return taskEntry{}, errors.New("invalid task identity")
	}
	if entry.workspace != "" {
		if !filepath.IsAbs(entry.workspace) {
			return taskEntry{}, errors.New("workspace must be absolute")
		}
		resolved, err := filepath.EvalSymlinks(filepath.Clean(entry.workspace))
		if err != nil {
			return taskEntry{}, errors.New("workspace is unavailable")
		}
		entry.workspace = resolved
	}
	digest := sha256.Sum256(data)
	entry.revision = hex.EncodeToString(digest[:])
	return entry, nil
}

func (s *Source) loadJournal() error {
	if info, err := os.Lstat(s.statePath); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return errors.New("task delivery journal path is unsafe")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("task delivery journal: %w", err)
	}
	data, err := os.ReadFile(s.statePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("task delivery journal: %w", err)
	}
	var journal deliveryJournal
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil || journal.Version != 1 {
		return errors.New("task delivery journal is corrupt or unsupported")
	}
	for id, item := range journal.Issued {
		if id == "" || item.Command.CommandID != id || !within(s.taskDir, item.Path) {
			return errors.New("task delivery journal contains an invalid command")
		}
		resolved, err := filepath.EvalSymlinks(item.Path)
		if os.IsNotExist(err) {
			// Obsidian Bases renames the just-created file after the source may
			// already have issued a claim. The old path can no longer authorize
			// that command, so dropping it is safer than replaying it.
			continue
		}
		if err != nil || resolved != item.Path || !within(s.taskDir, resolved) {
			return errors.New("task delivery journal contains an unsafe path")
		}
		s.issued[id] = issuedCommand{command: item.Command, path: item.Path}
		if item.Receipt != nil {
			s.receipts[id] = *item.Receipt
			s.leased[id] = true
		}
	}
	for taskID, revision := range journal.Handled {
		if safeID.MatchString(taskID) && len(revision) == 64 {
			s.handled[taskID] = revision
		}
	}
	return nil
}

func (s *Source) persistJournalLocked() error {
	journal := deliveryJournal{Version: 1, Issued: map[string]journalCommand{}, Handled: map[string]string{}}
	for id, issued := range s.issued {
		item := journalCommand{Command: issued.command, Path: issued.path}
		if receipt, ok := s.receipts[id]; ok {
			copy := receipt
			item.Receipt = &copy
		}
		journal.Issued[id] = item
	}
	for taskID, revision := range s.handled {
		journal.Handled[taskID] = revision
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.statePath)
	if err := s.ensureJournalDirectory(); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(append(data, '\n'))
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tempPath, s.statePath)
}

func (s *Source) ensureJournalDirectory() error {
	current := s.root
	for _, name := range []string{".ariel", "tasks"} {
		current = filepath.Join(current, name)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("task delivery journal directory is unsafe")
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func splitFrontmatter(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", errors.New("missing frontmatter")
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return "", "", errors.New("unterminated frontmatter")
	}
	end += 4
	return content[4:end], content[end+5:], nil
}

func setFrontmatter(content string, updates map[string]string) (string, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return "", err
	}
	lines := strings.Split(frontmatter, "\n")
	for key, value := range updates {
		if !safeFrontmatterValue(value) {
			return "", errors.New("unsafe frontmatter value")
		}
		found := false
		for index, line := range lines {
			if strings.HasPrefix(line, key+":") {
				lines[index] = key + ": " + value
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, key+": "+value)
		}
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body, nil
}

func safeFrontmatterValue(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\r\n#[]{}\"")
}

func appendComment(content, id string, now time.Time, author, body, kind string) string {
	marker := "<!-- task-comment-id: " + id + " -->"
	if strings.Contains(content, marker) {
		return content
	}
	return strings.TrimRight(content, "\n") + "\n\n" + marker + "\n### " + now.Format("2006-01-02 15:04") + "｜" + author + "\n\n" + body + "\n\n- 类型：" + kind + "\n"
}

func atomicWrite(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ariel-task-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(info.Mode().Perm()); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
