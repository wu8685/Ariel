package desktopagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/protocol"
)

// TaskBackend is a local business contract. It is never a second native owner.
type TaskBackend interface {
	Ready() bool
	TaskStoreID() string
	Call(context.Context, string, any, any) error
}

type taskRuntime struct {
	agent               *agent
	cfg                 Config
	backend             TaskBackend
	mu                  sync.Mutex
	persistMu           sync.Mutex
	manager             map[string]protocol.TaskFrame
	executor            map[string]protocol.TaskFrame
	receipts            map[string]protocol.TaskFrame
	facts               map[string]protocol.TaskFrame
	subscriptions       map[string]string
	lastStatus          map[string]string
	validated           map[string]bool
	lastEmission        map[string]time.Time
	executionReadyValue bool
	lastExecutionCheck  time.Time
	lastCapabilities    string
	seen                map[string]bool
	trackingCursor      string
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
}

func newTaskRuntime(a *agent, cfg Config) *taskRuntime {
	return &taskRuntime{agent: a, cfg: cfg, backend: cfg.TaskBackend, manager: map[string]protocol.TaskFrame{}, executor: map[string]protocol.TaskFrame{}, receipts: map[string]protocol.TaskFrame{}, facts: map[string]protocol.TaskFrame{}, subscriptions: map[string]string{}, lastStatus: map[string]string{}, validated: map[string]bool{}, lastEmission: map[string]time.Time{}, seen: map[string]bool{}}
}
func (t *taskRuntime) managementReady() bool {
	return t.cfg.TasksEnabled && t.backend != nil && t.backend.Ready() && t.backend.TaskStoreID() != ""
}
func (t *taskRuntime) executionReady(ctx context.Context) bool {
	ready := t.executionAuthorized() && t.cfg.TaskExecutionCheck != nil && t.cfg.TaskExecutionCheck(ctx, "", t.cfg.CWD) == nil
	t.mu.Lock()
	if len(t.seen) >= 4096 {
		ready = false
	}
	t.executionReadyValue = ready
	t.lastExecutionCheck = time.Now()
	t.mu.Unlock()
	return ready
}
func (t *taskRuntime) executionAuthorized() bool {
	return t.cfg.TasksEnabled && !t.cfg.TasksManagerOnly && t.agent.service != nil && len(t.cfg.TaskAllowedRoots) > 0 && len(t.cfg.TaskAllowedManagers) > 0
}
func (t *taskRuntime) start(ctx context.Context) {
	ctx, t.cancel = context.WithCancel(ctx)
	if !t.cfg.TasksEnabled {
		return
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		interval := t.cfg.TaskPollInterval
		if interval <= 0 {
			interval = 2 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			t.refreshCapabilities(ctx)
			if t.managementReady() {
				t.flush(ctx)
				t.acquire(ctx, "task.command.tracking", "task.track")
				t.claim(ctx)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (t *taskRuntime) refreshCapabilities(ctx context.Context) {
	if refreshable, ok := t.backend.(interface{ Refresh(context.Context) error }); ok {
		_ = refreshable.Refresh(ctx)
	}
	t.mu.Lock()
	needsCheck := time.Since(t.lastExecutionCheck) >= 30*time.Second
	t.mu.Unlock()
	if needsCheck {
		t.executionReady(ctx)
	}
	t.mu.Lock()
	executionReady := t.executionReadyValue
	t.mu.Unlock()
	update := protocol.TaskCapabilityFrame{Type: "task.capabilities", V: 1, ManagementReady: t.managementReady(), ExecutionReady: executionReady}
	if t.backend != nil {
		update.TaskStoreID = t.backend.TaskStoreID()
	}
	raw, _ := json.Marshal(update)
	t.mu.Lock()
	if t.lastCapabilities == string(raw) {
		t.mu.Unlock()
		return
	}
	t.lastCapabilities = string(raw)
	t.mu.Unlock()
	_ = t.agent.send(ctx, update)
}
func (t *taskRuntime) close() {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
	t.mu.Lock()
	subs := make([]string, 0, len(t.subscriptions))
	for _, sub := range t.subscriptions {
		subs = append(subs, sub)
	}
	t.mu.Unlock()
	if t.agent.service != nil {
		for _, sub := range subs {
			t.agent.service.Unsubscribe(sub)
		}
	}
}
func (t *taskRuntime) public(ctx context.Context, method string, params json.RawMessage) (map[string]any, error) {
	if !t.managementReady() {
		return nil, errors.New("CHILLING_UNAVAILABLE")
	}
	var scope struct {
		TaskStoreID string `json:"taskStoreId"`
	}
	if json.Unmarshal(params, &scope) != nil || scope.TaskStoreID != t.backend.TaskStoreID() {
		return nil, errors.New("UNAUTHORIZED")
	}
	var out map[string]any
	if err := t.backend.Call(ctx, method, json.RawMessage(params), &out); err != nil {
		return nil, err
	}
	body, err := json.Marshal(out)
	if err != nil || len(body) > protocol.MaxTaskBytes {
		return nil, errors.New("OVERLOADED")
	}
	return out, nil
}
func (t *taskRuntime) claim(ctx context.Context) {
	t.acquire(ctx, "task.command.claim", "task.dispatch")
}
func (t *taskRuntime) acquire(ctx context.Context, method, frameType string) {
	t.mu.Lock()
	full := len(t.manager) >= 64
	t.mu.Unlock()
	if full || ctx.Err() != nil {
		return
	}
	var response struct {
		Commands   []protocol.TaskCommand `json:"commands"`
		NextCursor string                 `json:"nextCursor"`
	}
	params := map[string]any{}
	if frameType == "task.track" {
		params["cursor"] = t.trackingCursor
	}
	if t.backend.Call(ctx, method, params, &response) != nil || len(response.Commands) > 8 {
		return
	}
	if frameType == "task.track" {
		t.trackingCursor = response.NextCursor
	}
	for _, command := range response.Commands {
		if command.TaskStoreID == "" {
			command.TaskStoreID = t.backend.TaskStoreID()
		}
		if command.TaskStoreID != t.backend.TaskStoreID() {
			continue
		}
		if frameType == "task.track" && (command.NativeThreadID == "" || command.NativeTurnID == "" || (command.State != "accepted" && command.State != "unknown")) {
			continue
		}
		f := protocol.TaskFrame{Type: frameType, V: 1, DispatchID: "dispatch-" + rand.Text(), ManagementDeviceID: t.agent.deviceID, TaskStoreID: t.backend.TaskStoreID(), ExecutionHostID: command.ExecutionHostID, Command: &command}
		body, _ := json.Marshal(f)
		if len(body) > protocol.MaxTaskBytes || protocol.Validate(body) != nil {
			continue
		}
		t.mu.Lock()
		duplicate := false
		for _, pending := range t.manager {
			if pending.Command.CommandID == command.CommandID {
				duplicate = true
				break
			}
		}
		if !duplicate && len(t.manager) < 64 {
			t.manager[f.DispatchID] = f
		} else {
			duplicate = true
		}
		t.mu.Unlock()
		if duplicate {
			continue
		}
		if t.agent.send(ctx, f) != nil {
			receipt := f.CloneAs("task.receipt")
			receipt.Receipt = &protocol.TaskReceipt{CommandID: command.CommandID, LeaseToken: command.LeaseToken, Outcome: "unknown", Reason: "OUTCOME_UNKNOWN"}
			t.saveReceipt(ctx, receipt)
		}
	}
}
func (t *taskRuntime) flush(ctx context.Context) {
	t.mu.Lock()
	receipts := make([]protocol.TaskFrame, 0, len(t.receipts))
	for _, f := range t.receipts {
		receipts = append(receipts, f)
	}
	facts := make([]protocol.TaskFrame, 0, len(t.facts))
	for _, f := range t.facts {
		facts = append(facts, f)
	}
	t.mu.Unlock()
	for _, f := range receipts {
		t.persistReceipt(ctx, f)
	}
	for _, f := range facts {
		t.persistFact(ctx, f)
	}
}
func (t *taskRuntime) saveReceipt(ctx context.Context, f protocol.TaskFrame) {
	if f.Receipt == nil || !t.cfg.TasksEnabled || t.backend == nil {
		return
	}
	t.mu.Lock()
	pending, ok := t.manager[f.DispatchID]
	if !ok || pending.Command.CommandID != f.Receipt.CommandID || pending.Command.LeaseToken != f.Receipt.LeaseToken {
		t.mu.Unlock()
		return
	}
	if pending.Type == "task.track" && (f.Receipt.Outcome == "busy" || f.Receipt.Outcome == "failed") {
		delete(t.manager, f.DispatchID)
		delete(t.receipts, f.DispatchID)
		t.mu.Unlock()
		return
	}
	if previous, ok := t.receipts[f.DispatchID]; ok && f.Receipt.Outcome == "unknown" && f.Receipt.NativeThreadID == "" && previous.Receipt.NativeThreadID != "" && previous.Receipt.NativeTurnID != "" {
		// Relay disconnection has no native payload. Do not erase a native
		// identity already observed while the business store was unavailable.
		receipt := *f.Receipt
		receipt.NativeThreadID = previous.Receipt.NativeThreadID
		receipt.NativeTurnID = previous.Receipt.NativeTurnID
		receipt.ClientMessageID = previous.Receipt.ClientMessageID
		f.Receipt = &receipt
	}
	t.receipts[f.DispatchID] = f
	t.mu.Unlock()
	t.persistReceipt(ctx, f)
}
func (t *taskRuntime) persistReceipt(ctx context.Context, f protocol.TaskFrame) {
	t.persistMu.Lock()
	defer t.persistMu.Unlock()
	if !t.managementReady() {
		return
	}
	t.mu.Lock()
	current, ok := t.receipts[f.DispatchID]
	stillCurrent := ok && current.Receipt != nil && *current.Receipt == *f.Receipt
	t.mu.Unlock()
	if !stillCurrent {
		return
	}
	if t.backend.Call(ctx, "task.command.record", f.Receipt, new(map[string]any)) == nil {
		t.mu.Lock()
		current, ok := t.receipts[f.DispatchID]
		if ok && current.Receipt != nil && *current.Receipt == *f.Receipt {
			delete(t.receipts, f.DispatchID)
			if f.Receipt.Outcome != "accepted" {
				delete(t.manager, f.DispatchID)
			}
		}
		t.mu.Unlock()
	}
}
func (t *taskRuntime) saveFact(ctx context.Context, f protocol.TaskFrame) {
	if f.Fact == nil || !t.cfg.TasksEnabled || t.backend == nil {
		return
	}
	t.mu.Lock()
	pending, ok := t.manager[f.DispatchID]
	if !ok || pending.Command.CommandID != f.Fact.CommandID || pending.Command.AttemptID != f.Fact.AttemptID || pending.Command.BindingGeneration != f.Fact.BindingGeneration {
		t.mu.Unlock()
		return
	}
	for id, previous := range t.facts {
		if previous.DispatchID != f.DispatchID {
			continue
		}
		if (previous.Fact.Status == "completed" || previous.Fact.Status == "failed") && f.Fact.Status != "completed" && f.Fact.Status != "failed" {
			t.mu.Unlock()
			return
		}
		delete(t.facts, id)
	}
	if len(t.facts) >= 128 {
		t.mu.Unlock()
		return
	}
	t.facts[f.Fact.EventID] = f
	t.mu.Unlock()
	t.persistFact(ctx, f)
}
func (t *taskRuntime) persistFact(ctx context.Context, f protocol.TaskFrame) {
	t.persistMu.Lock()
	defer t.persistMu.Unlock()
	if !t.managementReady() {
		return
	}
	t.mu.Lock()
	current, ok := t.facts[f.Fact.EventID]
	stillCurrent := ok && current.DispatchID == f.DispatchID
	t.mu.Unlock()
	if !stillCurrent {
		return
	}
	if t.backend.Call(ctx, "task.fact.record", f.Fact, new(map[string]any)) == nil {
		t.mu.Lock()
		if current, ok := t.facts[f.Fact.EventID]; ok && current.DispatchID == f.DispatchID {
			delete(t.facts, f.Fact.EventID)
		}
		if f.Fact.Status == "completed" || f.Fact.Status == "failed" {
			delete(t.manager, f.DispatchID)
		}
		t.mu.Unlock()
	}
}
func (t *taskRuntime) handle(ctx context.Context, raw []byte) {
	if !t.cfg.TasksEnabled || len(raw) > protocol.MaxTaskBytes {
		return
	}
	f, err := protocol.DecodeTaskFrame(raw)
	if err != nil {
		return
	}
	switch f.Type {
	case "task.dispatch", "task.track":
		if f.Command == nil || f.ExecutionHostID != t.agent.deviceID || !containsTaskString(t.cfg.TaskAllowedManagers, f.ManagementDeviceID) || f.Command.ExecutionHostID != t.agent.deviceID {
			return
		}
		t.mu.Lock()
		if len(t.executor) >= 64 || t.executor[f.DispatchID].Command != nil {
			t.mu.Unlock()
			return
		}
		t.executor[f.DispatchID] = f
		t.mu.Unlock()
		_ = t.agent.send(ctx, f.CloneAs("task.validate"))
	case "task.validate":
		if f.ManagementDeviceID != t.agent.deviceID || !t.managementReady() {
			return
		}
		t.mu.Lock()
		pending, ok := t.manager[f.DispatchID]
		t.mu.Unlock()
		if !ok {
			return
		}
		var result struct {
			Valid   bool                  `json:"valid"`
			Command *protocol.TaskCommand `json:"command"`
		}
		validation := map[string]any{"commandId": pending.Command.CommandID, "leaseToken": pending.Command.LeaseToken}
		if pending.Type == "task.track" {
			validation["readOnly"] = true
		}
		err := t.backend.Call(ctx, "task.command.validate", validation, &result)
		if result.Command != nil && result.Command.TaskStoreID == "" {
			result.Command.TaskStoreID = pending.TaskStoreID
		}
		valid := err == nil && result.Valid && result.Command != nil && reflect.DeepEqual(*result.Command, *pending.Command)
		reply := f.CloneAs("task.validated")
		reply.Valid = &valid
		if valid {
			reply.Command = result.Command
		}
		_ = t.agent.send(ctx, reply)
	case "task.validated":
		t.mu.Lock()
		pending, ok := t.executor[f.DispatchID]
		if t.validated[f.DispatchID] {
			t.mu.Unlock()
			return
		}
		if ok {
			t.validated[f.DispatchID] = true
		}
		t.mu.Unlock()
		if !ok {
			return
		}
		if f.Valid == nil || !*f.Valid || f.Command == nil || !reflect.DeepEqual(*pending.Command, *f.Command) {
			t.finish(ctx, pending, protocol.TaskReceipt{CommandID: pending.Command.CommandID, LeaseToken: pending.Command.LeaseToken, Outcome: "busy", Reason: "STALE_TASK_AUTHORIZATION"})
			return
		}
		t.execute(ctx, pending)
	case "task.receipt":
		if f.ManagementDeviceID == t.agent.deviceID {
			t.saveReceipt(ctx, f)
		}
	case "task.fact":
		if f.ManagementDeviceID == t.agent.deviceID {
			t.saveFact(ctx, f)
		}
	}
}
func containsTaskString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (t *taskRuntime) allowedDirectory(cwd string) bool {
	canonical, err := canonicalProjectDirectory(cwd)
	if err != nil {
		return false
	}
	for _, root := range t.cfg.TaskAllowedRoots {
		resolved, err := canonicalProjectDirectory(root)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(resolved, canonical)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
func (t *taskRuntime) execute(ctx context.Context, f protocol.TaskFrame) {
	c := *f.Command
	r := protocol.TaskReceipt{CommandID: c.CommandID, LeaseToken: c.LeaseToken, ClientMessageID: c.ClientMessageID}
	if t.agent.service == nil || !t.allowedDirectory(c.CWD) {
		r.Outcome = "failed"
		r.Reason = "TASK_EXECUTION_UNAVAILABLE"
		t.finish(ctx, f, r)
		return
	}
	if f.Type == "task.track" {
		if c.NativeThreadID == "" || c.NativeTurnID == "" {
			r.Outcome = "unknown"
			r.Reason = "NATIVE_STATE_UNCERTAIN"
			t.finish(ctx, f, r)
			return
		}
		t.track(ctx, f, c.NativeThreadID, c.NativeTurnID, true)
		return
	}
	checkErr := errors.New("OFFICIAL_PROVIDER_UNVERIFIED")
	if t.cfg.TaskExecutionCheck != nil {
		checkErr = t.cfg.TaskExecutionCheck(ctx, c.NativeThreadID, c.CWD)
	}
	if checkErr != nil {
		r.Outcome = "failed"
		r.Reason = taskExecutionFailureReason(checkErr)
		t.finish(ctx, f, r)
		return
	}
	key := c.CommandID + "\x00" + c.LeaseToken
	t.mu.Lock()
	if len(t.seen) >= 4096 && !t.seen[key] {
		t.mu.Unlock()
		r.Outcome = "busy"
		r.Reason = "OVERLOADED"
		t.finish(ctx, f, r)
		return
	}
	if t.seen[key] {
		t.mu.Unlock()
		r.Outcome = "unknown"
		r.Reason = "OUTCOME_UNKNOWN"
		t.finish(ctx, f, r)
		return
	}
	t.seen[key] = true
	t.mu.Unlock()
	var err error
	switch c.Kind {
	case "start", "claim":
		if c.NativeThreadID != "" {
			err = errors.New("INVALID_ARGUMENT")
			break
		}
		var thread map[string]any
		thread, r.NativeTurnID, err = t.agent.service.BootstrapThread(ctx, c.CWD, c.ClientMessageID, c.Text, nil)
		if thread != nil {
			r.NativeThreadID, _ = thread["threadId"].(string)
		}
	case "feedback", "work":
		if c.NativeThreadID == "" {
			err = errors.New("INVALID_ARGUMENT")
			break
		}
		r.NativeThreadID = c.NativeThreadID
		r.NativeTurnID, err = t.agent.service.Start(ctx, c.NativeThreadID, c.ClientMessageID, c.Text)
	case "check":
		r.NativeThreadID = c.NativeThreadID
		var thread map[string]any
		thread, err = t.agent.service.Read(ctx, c.NativeThreadID)
		if err == nil {
			r.NativeTurnID = lastTaskTurnID(thread)
		}
	default:
		err = errors.New("INVALID_ARGUMENT")
	}
	if err != nil {
		r.Reason = requestErrorCode("turn.start", err)
		r.Outcome = "failed"
		if r.Reason == "TURN_BUSY" {
			r.Outcome = "busy"
			t.mu.Lock()
			delete(t.seen, key)
			t.mu.Unlock()
		}
		var callErr *desktopipc.CallError
		if r.Reason == "OUTCOME_UNKNOWN" || errors.Is(err, appserver.ErrOutcomeUnknown) || (errors.As(err, &callErr) && callErr.Outcome == "unknown") {
			r.Outcome = "unknown"
			r.Reason = "OUTCOME_UNKNOWN"
		}
		t.finish(ctx, f, r)
		return
	}
	r.Outcome = "accepted"
	t.finish(ctx, f, r)
	if r.NativeThreadID != "" && r.NativeTurnID != "" {
		t.track(ctx, f, r.NativeThreadID, r.NativeTurnID, false)
	}
}
func taskExecutionFailureReason(err error) string {
	if err != nil && err.Error() == "NATIVE_COMPATIBILITY_UNVERIFIED" {
		return "NATIVE_COMPATIBILITY_UNVERIFIED"
	}
	return "OFFICIAL_PROVIDER_UNVERIFIED"
}
func lastTaskTurnID(thread map[string]any) string {
	turns, _ := thread["turns"].([]any)
	if len(turns) == 0 {
		return ""
	}
	last, _ := turns[len(turns)-1].(map[string]any)
	id, _ := last["turnId"].(string)
	return id
}
func (t *taskRuntime) finish(ctx context.Context, f protocol.TaskFrame, r protocol.TaskReceipt) {
	reply := f.CloneAs("task.receipt")
	reply.Receipt = &r
	_ = t.agent.send(ctx, reply)
	if r.Outcome != "accepted" {
		t.releaseTracking(f.DispatchID)
	}
}
func (t *taskRuntime) releaseTracking(dispatchID string) {
	t.mu.Lock()
	subID := t.subscriptions[dispatchID]
	delete(t.executor, dispatchID)
	delete(t.validated, dispatchID)
	delete(t.subscriptions, dispatchID)
	delete(t.lastStatus, dispatchID)
	delete(t.lastEmission, dispatchID)
	t.mu.Unlock()
	if subID != "" && t.agent.service != nil {
		// Native subscription callbacks run under the existing owner's lock.
		go t.agent.service.Unsubscribe(subID)
	}
}
func taskTurnObservation(thread map[string]any, turnID string) (string, map[string]any) {
	turns, _ := thread["turns"].([]any)
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		if turn["turnId"] != turnID {
			continue
		}
		switch turn["status"] {
		case "completed":
			return "completed", turn
		case "failed", "interrupted":
			return "failed", turn
		case "inProgress":
			if interactions, ok := thread["pendingInteractions"].([]any); ok && len(interactions) > 0 {
				return "waiting", turn
			}
			return "running", turn
		default:
			return "", nil
		}
	}
	return "", nil
}
func (t *taskRuntime) track(ctx context.Context, f protocol.TaskFrame, threadID, turnID string, recovering bool) {
	var subID string
	emit := func(thread map[string]any) {
		status, matched := taskTurnObservation(thread, turnID)
		if matched == nil {
			return
		}
		progress := taskSafeProgress(status, matched)
		fingerprint := status + "\x00" + progress
		t.mu.Lock()
		previous := t.lastStatus[f.DispatchID]
		if previous == fingerprint || (strings.HasPrefix(previous, status+"\x00") && time.Since(t.lastEmission[f.DispatchID]) < 10*time.Minute) {
			t.mu.Unlock()
			return
		}
		t.lastStatus[f.DispatchID] = fingerprint
		t.lastEmission[f.DispatchID] = time.Now()
		t.mu.Unlock()
		fact := f.CloneAs("task.fact")
		observation, _ := json.Marshal([]any{f.TaskStoreID, f.Command.CommandID, threadID, turnID, status, matched, thread["pendingInteractions"]})
		digest := sha256.Sum256(observation)
		fact.Fact = &protocol.TaskFact{EventID: "fact-" + hex.EncodeToString(digest[:]), CommandID: f.Command.CommandID, TaskID: f.Command.TaskID, AttemptID: f.Command.AttemptID, ExecutionHostID: t.agent.deviceID, BindingGeneration: f.Command.BindingGeneration, NativeThreadID: threadID, NativeTurnID: turnID, Status: status}
		fact.Fact.Progress = progress
		if status == "completed" && (f.Command.Kind == "claim" || f.Command.Kind == "work") {
			if intent, ok := taskIntentFromTurn(matched, f.Command.TaskID, f.Command.TaskRevision, f.Command.Kind); ok {
				fact.Fact.TaskAction = intent.Action
				fact.Fact.TaskRevision = intent.Revision
			}
		}
		if status == "completed" && taskTurnHasAssistantOutput(matched) {
			fact.Fact.FeedbackIDs = append([]string(nil), f.Command.FeedbackIDs...)
		}
		_ = t.agent.send(ctx, fact)
		if status == "completed" || status == "failed" {
			t.releaseTracking(f.DispatchID)
		}
	}
	uncertain := func() {
		t.finish(ctx, f, protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "unknown", NativeThreadID: threadID, NativeTurnID: turnID, ClientMessageID: f.Command.ClientMessageID, Reason: "NATIVE_STATE_UNCERTAIN"})
	}
	var snapshot map[string]any
	var activate func()
	var err error
	subID, _, snapshot, activate, err = t.agent.service.Subscribe(ctx, threadID, func(event map[string]any) {
		if event["event"] == "thread.error" {
			uncertain()
			return
		}
		if thread, ok := event["thread"].(map[string]any); ok {
			emit(thread)
		}
	})
	if err != nil {
		uncertain()
		return
	}
	if !taskSnapshotDirectoryMatches(snapshot, f.Command.CWD) {
		t.agent.service.Unsubscribe(subID)
		t.finish(ctx, f, protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "unknown", NativeThreadID: threadID, NativeTurnID: turnID, ClientMessageID: f.Command.ClientMessageID, Reason: "PATH_NOT_ALLOWED"})
		return
	}
	if recovering {
		exact := false
		turns, _ := snapshot["turns"].([]any)
		for _, raw := range turns {
			turn, _ := raw.(map[string]any)
			if turn["turnId"] != turnID {
				continue
			}
			exact = f.Command.State == "accepted"
			items, _ := turn["items"].([]any)
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item["role"] == "user" && item["clientMessageId"] == f.Command.ClientMessageID {
					exact = true
				}
			}
		}
		if !exact {
			t.agent.service.Unsubscribe(subID)
			t.finish(ctx, f, protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "unknown", NativeThreadID: threadID, NativeTurnID: turnID, ClientMessageID: f.Command.ClientMessageID, Reason: "NATIVE_STATE_UNCERTAIN"})
			return
		}
		t.finish(ctx, f, protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "accepted", NativeThreadID: threadID, NativeTurnID: turnID, ClientMessageID: f.Command.ClientMessageID})
	}
	t.mu.Lock()
	t.subscriptions[f.DispatchID] = subID
	t.mu.Unlock()
	emit(snapshot)
	activate()
}
func taskSnapshotDirectoryMatches(snapshot map[string]any, cwd string) bool {
	canonical, err := canonicalProjectDirectory(cwd)
	if err != nil {
		return false
	}
	native, ok := snapshot["cwd"].(string)
	return ok && filepath.Clean(native) == canonical
}
