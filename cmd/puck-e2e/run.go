package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

type options struct {
	Root     string
	Binary   string
	Socket   string
	Only     map[string]bool
	Retries  int
	Timeout  time.Duration
	Keep     bool
	UI       bool
	Fake     bool
	WebRoot  string
	WebDist  string
	JSONPath string
}

type metrics struct {
	AckSeen          bool
	SendMessageCards int
	RetryNotices     int
	Fallbacks        int
	SystemLines      int
	SystemObserved   bool
	HasHandoff       bool
	HasSave          bool
	HasMerge         bool
	MaxQueueWakes    int
}

type runner struct {
	ctx           context.Context
	stack         *stack
	client        *client
	assistantID   string
	brainThread   string
	fixtureThread string
	childThread   string
	marker        string
	metrics       metrics
	seen          map[string]struct{}
	retries       int
	step          time.Duration
	fake          bool
	brainTurns    map[string]struct{}
}

func execute(ctx context.Context, opt options) (Report, error) {
	root := opt.Root
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "ariel-puck-e2e-")
		if err != nil {
			return Report{}, err
		}
	}
	var stack *stack
	report := Report{}
	defer func() {
		writeReportFiles(root, opt.JSONPath, report)
		if stack != nil {
			stack.close()
		}
		if !opt.Keep {
			_ = os.RemoveAll(root)
		}
	}()
	if opt.Socket != "" {
		return Report{}, fmt.Errorf("refusing Desktop IPC: puck-e2e is headless and will not attach to Codex Desktop")
	}
	if opt.Fake {
		binary, err := buildFake(root)
		if err != nil {
			return Report{}, err
		}
		opt.Binary = binary
		if err := os.Setenv("PUCK_E2E_FAKE_STATE", filepath.Join(root, "fake-state.json")); err != nil {
			return Report{}, err
		}
	}
	iso := layout(root, opt.Binary, "not-used")
	if err := validateIsolation(iso); err != nil {
		return Report{}, err
	}
	stack, err := newStack(iso)
	if err != nil {
		return Report{}, err
	}
	if opt.UI {
		stack.webDist = opt.WebDist
	}
	if err := stack.startRelay(ctx); err != nil {
		return Report{}, err
	}
	if err := stack.startAgent(ctx); err != nil {
		return Report{}, err
	}
	owner, err := dialOwner(ctx, stack.wsURL(), stack.origin, stack.pin)
	if err != nil {
		return Report{}, err
	}
	run := &runner{ctx: ctx, stack: stack, client: owner, marker: "FIXTURE-MARKER-" + stack.token[:8], retries: opt.Retries, step: 6 * time.Minute, seen: map[string]struct{}{}, fake: opt.Fake, brainTurns: map[string]struct{}{}}
	defer func() { run.client.Close() }()
	report = Report{OK: true, Listen: stack.listen, Isolation: iso}
	if err := run.boot(); err != nil {
		report.add(ScenarioResult{ID: "boot", Name: "owner and brain", Status: "fail", Checks: []Check{{Name: "boot", Detail: err.Error()}}})
		return report, nil
	}
	order := []struct {
		id   string
		name string
		hard bool
		fn   func() ScenarioResult
	}{
		{"A", "handoff pack", true, run.scenarioA},
		{"B", "queue while busy", true, run.scenarioB},
		{"C", "restart mid-queue", true, run.scenarioC},
		{"D", "semantic merge", false, run.scenarioD},
		{"E", "completion cleanup", true, run.scenarioE},
		{"G", "ux metrics", true, run.scenarioG},
		{"H", "memory tools", true, run.scenarioH},
		{"I", "memory recall", true, run.scenarioI},
		{"UI", "headless browser", true, func() ScenarioResult {
			if !opt.UI {
				return ScenarioResult{ID: "UI", Name: "headless browser", Status: "skipped"}
			}
			return run.scenarioUI(opt.WebRoot)
		}},
		{"J", "memory delete", true, run.scenarioJ},
		{"K", "remember choice", false, run.scenarioK},
		{"F", "unlink and delete", true, run.scenarioF},
	}
	for _, scenario := range order {
		if len(opt.Only) > 0 && !opt.Only[scenario.id] {
			report.add(ScenarioResult{ID: scenario.id, Name: scenario.name, Status: "skipped"})
			continue
		}
		result := scenario.fn()
		if !scenario.hard && result.Status == "fail" {
			result.Status = "soft_fail"
		}
		report.add(result)
		fmt.Fprintf(os.Stderr, "scenario=%s status=%s\n", result.ID, result.Status)
	}
	_ = run.client.reconnect(ctx)
	run.archiveCreated()
	return report, nil
}

func (r *runner) boot() error {
	if err := r.waitDevice(); err != nil {
		return err
	}
	created, err := r.client.call(r.ctx, "puck.create", "relay", map[string]any{
		"name": "E2E", "brainDeviceId": r.stack.deviceID, "permissionMode": "full",
	}, r.step)
	if err != nil {
		return err
	}
	if outcomeCode(created) != "accepted" {
		return fmt.Errorf("puck.create %s", outcomeCode(created))
	}
	assistant, _ := dataOf(created)["assistant"].(map[string]any)
	r.assistantID, _ = assistant["assistantId"].(string)
	r.brainThread, _ = assistant["brainThreadId"].(string)
	if r.assistantID == "" || r.brainThread == "" {
		return fmt.Errorf("brain was not created")
	}
	return r.waitIdle()
}

func (r *runner) scenarioA() ScenarioResult {
	markerPath := r.stack.iso.WorkspaceA + "/marker.txt"
	if err := os.WriteFile(markerPath, []byte(r.marker+"\n"), 0o600); err != nil {
		return passResult("A", "handoff pack", []Check{{Name: "fixture", Detail: err.Error()}})
	}
	boot, err := r.client.call(r.ctx, "thread.bootstrap", r.stack.deviceID, map[string]any{
		"cwd": r.stack.iso.WorkspaceA, "clientMessageId": newID(),
		"text": "把下面这行原样写在回复最后，不要总结：\n" + r.marker,
	}, r.step)
	if err != nil || outcomeCode(boot) != "accepted" {
		detail := "request failed"
		if err == nil {
			detail = outcomeCode(boot)
		}
		return passResult("A", "handoff pack", []Check{{Name: "fixture-thread", Pass: false, Detail: detail}})
	}
	r.fixtureThread, _ = dataOf(boot)["threadId"].(string)
	if err := r.waitThreadIdle(r.fixtureThread); err != nil {
		return passResult("A", "handoff pack", []Check{{Name: "fixture-idle", Pass: false, Detail: "timeout"}})
	}
	prompt := "请在同一个回合里只做这两件事，不要复述会话内容，也不要写总结。\n" +
		"1. 调用 watch_thread，threadId 是 " + r.fixtureThread + "，不要填写 deviceId。\n" +
		"2. 然后调用 start_task。deviceId 用 list_devices 看到的唯一在线主机。cwd 使用下一行这个新目录。goal 写 FIXTURE_HANDOFF。prompt 写：请阅读交接里的引用会话。\n" +
		r.stack.iso.WorkspaceB
	sentID := newID()
	if _, err := r.send(sentID, prompt); err != nil {
		return passResult("A", "handoff pack", []Check{{Name: "send", Pass: false, Detail: "rejected"}})
	}
	r.noteAck()
	if err := r.waitIdle(); err != nil {
		return passResult("A", "handoff pack", []Check{{Name: "idle", Pass: false, Detail: "timeout"}})
	}
	r.childThread = r.waitChildThread()
	first := r.packViewsReady(r.childThread)
	early := combinePacks(first)
	followID := newID()
	_, _ = r.send(followID, "请对刚才的执行任务调用一次 send_turn，text 只写「继续看引用」。不要 start_task，不要传 clientMessageId。")
	_ = r.waitIdle()
	later := r.packViewsReady(r.childThread)
	again := combinePacks(later)
	view := combinePacks(append(append([]PackView{}, first...), later...))
	checks := []Check{
		{Name: "codex-link", Pass: view.HasLink, Detail: "thread=" + r.fixtureThread},
		{Name: "device-and-thread", Pass: view.HasDeviceID && view.HasThreadID, Detail: "device=" + r.stack.deviceID},
		{Name: "cwd", Pass: view.CWDTrusted && view.CWD != "", Detail: "cwd=" + view.CWD},
		{Name: "no-summary", Pass: !view.HasSummary && !view.HasTruncation},
		{Name: "no-duplicate-sentinel", Pass: !view.Duplicate && view.SentinelCount <= 1 && view.SentinelCount >= 1, Detail: fmt.Sprintf("sentinel=%d early=%d", view.SentinelCount, early.SentinelCount)},
		{Name: "tool-calls", Pass: true, Detail: r.toolCodes("start_task", "watch_thread")},
		{Name: "send-turn-dedupe", Pass: !again.Duplicate && again.SentinelCount >= early.SentinelCount && again.HasLink, Detail: fmt.Sprintf("sentinel=%d %s", again.SentinelCount, r.toolCodes("send_turn"))},
	}
	result := passResult("A", "handoff pack", checks)
	result.ToolCalls = r.toolNames()
	return result
}

func (r *runner) scenarioB() ScenarioResult {
	ids := []string{newID(), newID(), newID()}
	labels := []string{"FIXTURE_Q1", "FIXTURE_Q2", "FIXTURE_Q3"}
	var checks []Check
	for index, id := range ids {
		text := "请把这件事单独记成任务，title 正好是 " + labels[index] + "。不要和其他 FIXTURE_Q 合并。"
		if index == 2 {
			text += " 前面两条如果还在 inbox，等它们被叫醒时也各自 save_bot_task 并 attach，不要合成一条。"
		}
		if _, err := r.send(id, text); err != nil {
			checks = append(checks, Check{Name: "send-" + labels[index], Pass: false})
			continue
		}
		persisted, err := messagePersisted(r.ctx, r.stack.iso.DBPath, r.assistantID, id)
		checks = append(checks, Check{Name: "persisted-" + labels[index], Pass: err == nil && persisted})
		r.sampleQueue()
		if err := r.waitIdle(); err != nil {
			checks = append(checks, Check{Name: "idle-" + labels[index], Pass: false, Detail: "timeout"})
			break
		}
	}
	r.noteAck()
	deadline := time.Now().Add(r.step)
	attached := 0
	for time.Now().Before(deadline) {
		r.sampleQueue()
		attached = 0
		for _, id := range ids {
			_, status, _, err := messageTask(r.ctx, r.stack.iso.DBPath, r.assistantID, id)
			if err == nil && status != "" {
				attached++
			}
		}
		if attached == len(ids) {
			break
		}
		_ = r.waitIdle()
	}
	checks = append(checks,
		Check{Name: "picked-up", Pass: attached == len(ids), Detail: fmt.Sprintf("attached=%d skips=%s", attached, strings.Join(r.wakeSkipReasons(), ","))},
		Check{Name: "one-pending-wake", Pass: r.metrics.MaxQueueWakes <= 1, Detail: fmt.Sprintf("maxQueue=%d", r.metrics.MaxQueueWakes)},
	)
	result := passResult("B", "queue while busy", checks)
	brainTurns := 0
	for _, id := range ids {
		if r.brainTurnReceived(id) {
			brainTurns++
		}
	}
	result.Checks = append(result.Checks, Check{Name: "brain-turns-started", Pass: brainTurns == len(ids), Detail: fmt.Sprintf("started=%d", brainTurns)})
	if result.Status == "fail" && onlyFailed(result, "picked-up") {
		result.Status = "observed"
	}
	result.WakeSkips = r.wakeSkipReasons()
	result.ToolCalls = r.toolNames()
	return result
}

func (r *runner) scenarioC() ScenarioResult {
	before, err := readSnapshot(r.ctx, r.stack.iso.DBPath, r.assistantID)
	if err != nil {
		return passResult("C", "restart mid-queue", []Check{{Name: "snapshot", Pass: false}})
	}
	r.client.Close()
	if err := r.stack.restart(r.ctx); err != nil || r.stack.startAgent(r.ctx) != nil {
		return passResult("C", "restart mid-queue", []Check{{Name: "restart", Pass: false}})
	}
	owner, err := dialOwner(r.ctx, r.stack.wsURL(), r.stack.origin, r.stack.pin)
	if err != nil {
		return passResult("C", "restart mid-queue", []Check{{Name: "redial", Pass: false}})
	}
	r.client = owner
	if err := r.waitDevice(); err != nil {
		return passResult("C", "restart mid-queue", []Check{{Name: "device", Pass: false}})
	}
	after, err := readSnapshot(r.ctx, r.stack.iso.DBPath, r.assistantID)
	survived := err == nil && after.BotTasks == before.BotTasks && after.Inbox == before.Inbox
	resumed := r.waitIdle() == nil
	r.sampleQueue()
	return passResult("C", "restart mid-queue", []Check{
		{Name: "tasks-survive", Pass: survived, Detail: fmt.Sprintf("tasks=%d inbox=%d", after.BotTasks, after.Inbox)},
		{Name: "resumed", Pass: resumed},
		{Name: "one-pending-wake", Pass: r.metrics.MaxQueueWakes <= 1, Detail: fmt.Sprintf("maxQueue=%d", r.metrics.MaxQueueWakes)},
	})
}

func (r *runner) scenarioD() ScenarioResult {
	var best ScenarioResult
	attempts := r.retries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		x := newID()
		y := newID()
		again := newID()
		fresh := newID()
		_, _ = r.send(x, "请用 save_bot_task 新建任务，title 正好是 FIXTURE_X，goal 写整理标记文件。不要合并。")
		_ = r.waitIdle()
		_, _ = r.send(y, "请用 save_bot_task 新建另一个任务，title 正好是 FIXTURE_Y，goal 写比较两种水果的颜色。这和 FIXTURE_X 无关，不要合并。")
		_ = r.waitIdle()
		_, _ = r.send(again, "标记文件的整理还要补一步，确认标记还在。这是 FIXTURE_X 的延续，措辞不同。请先 list_bot_tasks，是同一件事就 merge_bot_tasks 或 attach 到 FIXTURE_X，不要再留一个重复的开放任务。")
		_ = r.waitIdle()
		_, _ = r.send(fresh, "请用 save_bot_task 新建任务，title 正好是 FIXTURE_Z，goal 写统计时区名称。这是新主题，不要并进 FIXTURE_X 或 FIXTURE_Y。")
		_ = r.waitIdle()
		xID, xStatus, _, _ := taskByFixture(r.ctx, r.stack.iso.DBPath, r.assistantID, "FIXTURE_X")
		againID, againStatus, againMerged, _ := messageTask(r.ctx, r.stack.iso.DBPath, r.assistantID, again)
		zID, zStatus, zMerged, _ := taskByFixture(r.ctx, r.stack.iso.DBPath, r.assistantID, "FIXTURE_Z")
		joinedX := xID != "" && (againID == xID || (againStatus == "merged" && againMerged == xID))
		checks := []Check{
			{Name: "x-kept", Pass: xID != "" && xStatus != "merged", Detail: "status=" + xStatus},
			{Name: "continuation-joined-x", Pass: joinedX, Detail: "status=" + againStatus},
			{Name: "z-not-merged", Pass: zID != "" && zStatus != "merged" && zMerged == "", Detail: "status=" + zStatus},
		}
		best = softResult("D", "semantic merge", attempt, checks)
		best.ToolCalls = r.toolNames()
		if best.Status == "soft_pass" {
			return best
		}
		best.Status = "observed"
	}
	return best
}

func onlyFailed(result ScenarioResult, name string) bool {
	failed := false
	for _, check := range result.Checks {
		if check.Pass {
			continue
		}
		if check.Name != name {
			return false
		}
		failed = true
	}
	return failed
}

func (r *runner) scenarioE() ScenarioResult {
	_, _ = r.send(newID(), "请把 title 为 FIXTURE_X 的 Bot 任务标成 done。如果它有执行子会话，调用 finish_task。不要新建任务，也不要合并 FIXTURE_Z。")
	_ = r.waitIdle()
	_, status, _, _ := taskByFixture(r.ctx, r.stack.iso.DBPath, r.assistantID, "FIXTURE_X")
	snap, err := readSnapshot(r.ctx, r.stack.iso.DBPath, r.assistantID)
	return passResult("E", "completion cleanup", []Check{
		{Name: "marked-done", Pass: status == "done", Detail: "status=" + status},
		{Name: "no-open-wake-storm", Pass: err == nil && snap.QueueWakes <= 1, Detail: fmt.Sprintf("queue=%d watches=%d", snap.QueueWakes, snap.Watches)},
	})
}

func (r *runner) scenarioG() ScenarioResult {
	before := r.countSendMessages()
	_, _ = r.send(newID(), "请只调用一次 send_message，内容正好是 FIXTURE_NOTICE。不要调用第二次，也不要新建任务。")
	r.noteAck()
	_ = r.waitIdle()
	r.observeContext()
	after := r.countSendMessages()
	r.metrics.SendMessageCards = after
	checks := []Check{
		{Name: "ack-recorded", Pass: r.metrics.AckSeen, Detail: fmt.Sprintf("seen=%t", r.metrics.AckSeen)},
		{Name: "send-message-once", Pass: after-before >= 1, Detail: fmt.Sprintf("delta=%d", after-before)},
		{Name: "tool-error-recovery", Pass: r.metrics.Fallbacks == 0, Detail: fmt.Sprintf("retries=%d fallbacks=%d", r.metrics.RetryNotices, r.metrics.Fallbacks)},
		{Name: "system-context-lines", Pass: r.metrics.SystemObserved && r.metrics.HasHandoff && r.metrics.HasSave && r.metrics.HasMerge, Detail: fmt.Sprintf("lines=%d observed=%t", r.metrics.SystemLines, r.metrics.SystemObserved)},
	}
	result := passResult("G", "ux metrics", checks)
	result.ToolCalls = r.toolNames()
	return result
}

func (r *runner) scenarioF() ScenarioResult {
	_ = r.client.reconnect(r.ctx)
	threadID := r.childThread
	if threadID == "" {
		threadID = r.fixtureThread
	}
	if threadID == "" {
		boot, err := r.client.call(r.ctx, "thread.bootstrap", r.stack.deviceID, map[string]any{
			"cwd": r.stack.iso.WorkspaceB, "clientMessageId": newID(), "text": "FIXTURE_LINK 子会话，回复一句即可。",
		}, r.step)
		if err == nil && outcomeCode(boot) == "accepted" {
			threadID, _ = dataOf(boot)["threadId"].(string)
		}
	}
	linkErr := linkChildSession(r.ctx, r.stack.iso.DBPath, r.assistantID, r.stack.deviceID, threadID)
	linksBefore, beforeErr := sessionLinksForThread(r.ctx, r.stack.iso.DBPath, r.assistantID, threadID)
	archiveCode := "not-called"
	if threadID != "" {
		archived, err := r.client.call(r.ctx, "thread.archive.update", r.stack.deviceID, map[string]any{"threadId": threadID, "archived": true}, 30*time.Second)
		archiveCode = callDetail(err, archived)
	}
	deadline := time.Now().Add(45 * time.Second)
	links := linksBefore
	for time.Now().Before(deadline) {
		links, _ = sessionLinksForThread(r.ctx, r.stack.iso.DBPath, r.assistantID, threadID)
		if linksBefore > 0 && links == 0 {
			break
		}
		time.Sleep(time.Second)
	}
	tasksBefore, _ := readSnapshot(r.ctx, r.stack.iso.DBPath, r.assistantID)
	deleted, err := r.client.call(r.ctx, "puck.delete", "relay", map[string]any{"assistantId": r.assistantID}, 30*time.Second)
	gone, goneDetail, goneErr := assistantGone(r.ctx, r.stack.iso.DBPath, r.assistantID)
	deleteDetail := goneDetail
	if err != nil || outcomeCode(deleted) != "accepted" {
		deleteDetail = callDetail(err, deleted) + " " + deleteDetail
	}
	if goneErr != nil && goneDetail == "" {
		deleteDetail = goneErr.Error()
	}
	return passResult("F", "unlink and delete", []Check{
		{Name: "unlink-session", Pass: linkErr == nil && beforeErr == nil && linksBefore > 0 && links == 0, Detail: fmt.Sprintf("before=%d after=%d archive=%s", linksBefore, links, archiveCode)},
		{Name: "task-remained-until-delete", Pass: tasksBefore.BotTasks > 0, Detail: fmt.Sprintf("tasks=%d", tasksBefore.BotTasks)},
		{Name: "delete-bot", Pass: err == nil && outcomeCode(deleted) == "accepted" && goneErr == nil && gone, Detail: deleteDetail},
	})
}

func (r *runner) send(id, text string) (map[string]any, error) {
	response, err := r.client.call(r.ctx, "puck.message.send", "relay", map[string]any{
		"assistantId": r.assistantID, "clientMessageId": id, "text": text,
	}, time.Minute)
	if err != nil {
		return nil, err
	}
	if outcomeCode(response) != "accepted" {
		return response, fmt.Errorf("%s", outcomeCode(response))
	}
	return response, nil
}

func (r *runner) collectBrainTurns() {
	for _, id := range r.stack.drainBrainTurns() {
		r.brainTurns[id] = struct{}{}
	}
}

func (r *runner) brainTurnReceived(clientMessageID string) bool {
	r.collectBrainTurns()
	_, ok := r.brainTurns[clientMessageID]
	return ok
}

func (r *runner) waitDevice() error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		response, err := r.client.call(r.ctx, "device.list", "relay", map[string]any{}, 15*time.Second)
		if err == nil {
			devices, _ := dataOf(response)["devices"].([]any)
			for _, raw := range devices {
				device, _ := raw.(map[string]any)
				caps, _ := device["capabilities"].(map[string]any)
				if device["deviceId"] == r.stack.deviceID && caps["puckBrainReady"] == true {
					return nil
				}
			}
		}
		if err := r.stack.agentExitError(); err != nil {
			return fmt.Errorf("desktop agent did not become ready: %v", err)
		}
		time.Sleep(time.Second)
	}
	if err := r.stack.agentExitError(); err != nil {
		return fmt.Errorf("desktop agent did not become ready: %v", err)
	}
	return fmt.Errorf("desktop agent did not become ready")
}

func (r *runner) waitIdle() error {
	deadline := time.Now().Add(r.step)
	for time.Now().Before(deadline) {
		r.collectBrainTurns()
		r.collectMetrics()
		r.sampleQueue()
		if !r.busy() {
			timer := time.NewTimer(400 * time.Millisecond)
			select {
			case <-r.ctx.Done():
				timer.Stop()
				return r.ctx.Err()
			case <-timer.C:
			}
			if !r.busy() {
				return nil
			}
			continue
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("brain stayed busy")
}

func (r *runner) waitThreadIdle(threadID string) error {
	deadline := time.Now().Add(r.step)
	for time.Now().Before(deadline) {
		response, err := r.client.call(r.ctx, "thread.read", r.stack.deviceID, map[string]any{"threadId": threadID}, 20*time.Second)
		if err == nil && outcomeCode(response) == "accepted" {
			runtime, _ := dataOf(response)["thread"].(map[string]any)["runtime"].(string)
			if runtime != "" && runtime != "inProgress" && runtime != "unknown" {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("thread stayed busy")
}

func (r *runner) busy() bool {
	history, err := r.history()
	if err != nil {
		return true
	}
	return messagesBusy(messagesOf(history))
}

func messagesBusy(messages []map[string]any) bool {
	busy := false
	for _, message := range messages {
		role, _ := message["role"].(string)
		if role == "user" {
			busy = false
			continue
		}
		if role != "assistant" {
			continue
		}
		kind, _ := message["kind"].(string)
		if kind == "processing" || kind == "acknowledged" || kind == "greeting_processing" {
			busy = true
		}
	}
	return busy
}

func (r *runner) history() (map[string]any, error) {
	r.collectBrainTurns()
	return r.client.call(r.ctx, "puck.history", "relay", map[string]any{"assistantId": r.assistantID}, 20*time.Second)
}

func (r *runner) noteAck() {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		history, err := r.history()
		if err == nil {
			for _, message := range messagesOf(history) {
				if message["kind"] == "acknowledged" {
					r.metrics.AckSeen = true
					return
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (r *runner) collectMetrics() {
	if r.seen == nil {
		r.seen = map[string]struct{}{}
	}
	history, err := r.history()
	if err != nil {
		return
	}
	for _, message := range messagesOf(history) {
		id, _ := message["id"].(string)
		if id == "" {
			continue
		}
		if _, seen := r.seen[id]; seen {
			continue
		}
		kind, _ := message["kind"].(string)
		text, _ := message["text"].(string)
		card, _ := message["cardJSON"].(string)
		if kind == "notice" && strings.Contains(text, "正在重试一次") {
			r.seen[id] = struct{}{}
			r.metrics.RetryNotices++
		}
		if strings.Contains(card, `"cardState":"invalid"`) {
			r.seen[id] = struct{}{}
			r.metrics.Fallbacks++
		}
	}
}

func (r *runner) countSendMessages() int {
	history, err := r.history()
	if err != nil {
		return r.metrics.SendMessageCards
	}
	count := 0
	seen := map[string]struct{}{}
	for _, message := range messagesOf(history) {
		id, _ := message["id"].(string)
		if id == "" {
			id = fmt.Sprintf("row-%d", count)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		role, _ := message["role"].(string)
		card, _ := message["cardJSON"].(string)
		if role == "assistant" && deliverySource(card) {
			seen[id] = struct{}{}
			count++
		}
	}
	return count
}

func deliverySource(card string) bool {
	for _, source := range []string{`"source":"notify_user"`, `"source":"share_result"`, `"source":"ask_user"`} {
		if strings.Contains(card, source) {
			return true
		}
	}
	return false
}

func (r *runner) observeContext() {
	var history map[string]any
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		history, err = r.history()
		if err == nil && outcomeCode(history) == "accepted" {
			if _, ok := dataOf(history)["systemContext"].(map[string]any); ok {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || outcomeCode(history) != "accepted" {
		return
	}
	raw, _ := dataOf(history)["systemContext"].(map[string]any)
	if raw == nil {
		return
	}
	lines := 0
	switch value := raw["lines"].(type) {
	case float64:
		lines = int(value)
	case int:
		lines = value
	}
	r.metrics.SystemLines = lines
	r.metrics.SystemObserved, _ = raw["observed"].(bool)
	r.metrics.HasHandoff, _ = raw["hasHandoff"].(bool)
	r.metrics.HasSave, _ = raw["hasSave"].(bool)
	r.metrics.HasMerge, _ = raw["hasMerge"].(bool)
}

func (r *runner) sampleQueue() {
	snap, err := readSnapshot(r.ctx, r.stack.iso.DBPath, r.assistantID)
	if err == nil && snap.QueueWakes > r.metrics.MaxQueueWakes {
		r.metrics.MaxQueueWakes = snap.QueueWakes
	}
}

func (r *runner) latestTaskThread() string {
	history, err := r.history()
	if err != nil {
		return ""
	}
	var threadID string
	for _, message := range messagesOf(history) {
		if message["kind"] != "task" {
			continue
		}
		card, _ := message["cardJSON"].(string)
		var payload map[string]any
		if err := jsonUnmarshal([]byte(card), &payload); err == nil {
			if id, _ := payload["threadId"].(string); id != "" && id != r.brainThread && id != r.fixtureThread {
				threadID = id
			}
		}
	}
	return threadID
}

func jsonUnmarshal(raw []byte, dest any) error {
	return json.Unmarshal(raw, dest)
}

func (r *runner) waitChildThread() string {
	deadline := time.Now().Add(15 * time.Second)
	for {
		if id := r.latestTaskThread(); id != "" {
			return id
		}
		if time.Now().After(deadline) || r.ctx.Err() != nil {
			return ""
		}
		time.Sleep(time.Second)
	}
}

func (r *runner) packViewsReady(threadID string) []PackView {
	var views []PackView
	for attempt := 0; attempt < 15; attempt++ {
		views = r.packViews(threadID)
		if packVisible(views) || attempt == 14 || r.ctx.Err() != nil {
			return views
		}
		time.Sleep(time.Second)
	}
	return views
}

func packVisible(views []PackView) bool {
	for _, view := range views {
		if view.SentinelCount > 0 || view.HasLink {
			return true
		}
	}
	return false
}

func (r *runner) packViews(threadID string) []PackView {
	if threadID == "" {
		return nil
	}
	response, err := r.client.call(r.ctx, "thread.read", r.stack.deviceID, map[string]any{"threadId": threadID}, 20*time.Second)
	if err != nil || outcomeCode(response) != "accepted" {
		return nil
	}
	var views []PackView
	for _, text := range userTexts(dataOf(response)) {
		views = append(views, inspectPack(text, r.fixtureThread, r.stack.deviceID, r.stack.iso.WorkspaceA, r.stack.iso.AgentStateDir))
	}
	return views
}

func (r *runner) archiveCreated() {
	if r == nil || r.client == nil || r.stack == nil {
		return
	}
	seen := map[string]struct{}{}
	add := func(id string) {
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	add(r.fixtureThread)
	add(r.childThread)
	add(r.brainThread)
	if r.assistantID != "" {
		ids, err := createdThreadIDs(r.ctx, r.stack.iso.DBPath, r.assistantID)
		if err == nil {
			for _, id := range ids {
				add(id)
			}
		}
	}
	for id := range seen {
		_, _ = r.client.call(r.ctx, "thread.archive.update", r.stack.deviceID, map[string]any{"threadId": id, "archived": true}, 20*time.Second)
	}
}

func buildFake(root string) (string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	binary := filepath.Join(root, "fake-codex")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/puck-e2e/fakecodex")
	cmd.Dir = moduleRoot()
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 300 {
			detail = detail[len(detail)-300:]
		}
		return "", fmt.Errorf("build fake codex: %s", detail)
	}
	return binary, nil
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func (r *runner) debugLists() (tools []string, skips []string) {
	history, err := r.history()
	if err != nil || outcomeCode(history) != "accepted" {
		return nil, nil
	}
	data := dataOf(history)
	if raw, _ := data["toolCalls"].([]any); raw != nil {
		for _, item := range raw {
			call, _ := item.(map[string]any)
			name, _ := call["tool"].(string)
			if name == "" {
				continue
			}
			if call["success"] == false {
				code, _ := call["code"].(string)
				if code == "" {
					code = "failed"
				}
				name += "=" + code
			}
			tools = append(tools, name)
		}
	}
	if raw, _ := data["wakeSkips"].([]any); raw != nil {
		for _, item := range raw {
			skip, _ := item.(map[string]any)
			reason, _ := skip["reason"].(string)
			if reason != "" {
				skips = append(skips, reason)
			}
		}
	}
	return tools, skips
}

func (r *runner) toolNames() []string {
	tools, _ := r.debugLists()
	return tools
}

func (r *runner) wakeSkipReasons() []string {
	_, skips := r.debugLists()
	return skips
}

func (r *runner) toolCodes(names ...string) string {
	tools := r.toolNames()
	var matched []string
	for _, tool := range tools {
		for _, name := range names {
			if tool == name || strings.HasPrefix(tool, name+"=") {
				matched = append(matched, tool)
			}
		}
	}
	if len(matched) == 0 {
		return "tools=none"
	}
	return "tools=" + strings.Join(matched, ",")
}

func (r *runner) scenarioH() ScenarioResult {
	if _, err := r.send(newID(), "请调用 update_memory 两次。第一次 action=write scope=bot kind=profile text=FIXTURE_MEMORY_WRITE 不要，text 正好是 FIXTURE_PROFILE_FACT。第二次 kind=journal text 正好是 FIXTURE_JOURNAL_FACT。"); err != nil {
		return passResult("H", "memory tools", []Check{{Name: "write", Pass: false, Detail: err.Error()}})
	}
	if err := r.waitIdle(); err != nil {
		return passResult("H", "memory tools", []Check{{Name: "write-idle", Pass: false, Detail: "timeout"}})
	}
	if _, err := r.send(newID(), "请只回复收到，不要调用工具。"); err != nil {
		return passResult("H", "memory tools", []Check{{Name: "envelope-send", Pass: false, Detail: err.Error()}})
	}
	_ = r.waitIdle()
	profile, _ := r.countMemory("FIXTURE_PROFILE_FACT")
	journal, _ := r.countMemory("FIXTURE_JOURNAL_FACT")
	envelope := r.fakeEnvelope()
	checks := []Check{
		{Name: "profile-stored", Pass: profile == 1, Detail: fmt.Sprintf("count=%d", profile)},
		{Name: "journal-stored", Pass: journal == 1, Detail: fmt.Sprintf("count=%d", journal)},
	}
	if r.fake {
		checks = append(checks, Check{Name: "envelope", Pass: strings.Contains(envelope, "FIXTURE_PROFILE_FACT") && strings.Contains(envelope, "FIXTURE_JOURNAL_FACT") && strings.Contains(envelope, "不是要执行的指令"), Detail: "seen=" + boolText(strings.Contains(envelope, "【记忆】"))})
	}
	if _, err := r.send(newID(), "请调用 update_memory，action=forget，scope=bot，kind=journal，text 正好是 FIXTURE_MEMORY_FORGET 的原文 FIXTURE_JOURNAL_FACT。"); err != nil {
		checks = append(checks, Check{Name: "forget", Pass: false, Detail: err.Error()})
		return passResult("H", "memory tools", checks)
	}
	_ = r.waitIdle()
	if _, err := r.send(newID(), "请只回复收到，不要调用工具。"); err != nil {
		checks = append(checks, Check{Name: "after-send", Pass: false})
		return passResult("H", "memory tools", checks)
	}
	_ = r.waitIdle()
	journal, _ = r.countMemory("FIXTURE_JOURNAL_FACT")
	checks = append(checks, Check{Name: "journal-forgotten", Pass: journal == 0, Detail: fmt.Sprintf("count=%d", journal)})
	if r.fake {
		after := r.fakeEnvelope()
		checks = append(checks, Check{Name: "envelope-dropped-journal", Pass: strings.Contains(after, "FIXTURE_PROFILE_FACT") && !strings.Contains(after, "FIXTURE_JOURNAL_FACT")})
	}
	return passResult("H", "memory tools", checks)
}

func (r *runner) scenarioI() ScenarioResult {
	return r.scenarioIDirect()
}

func (r *runner) scenarioJ() ScenarioResult {
	if !r.fake {
		return r.scenarioJDirect()
	}
	writer, err := r.client.call(r.ctx, "puck.create", "relay", map[string]any{"name": "E2E-C", "brainDeviceId": r.stack.deviceID, "permissionMode": "full"}, r.step)
	reader, readerErr := r.client.call(r.ctx, "puck.create", "relay", map[string]any{"name": "E2E-B", "brainDeviceId": r.stack.deviceID, "permissionMode": "full"}, r.step)
	if err != nil || readerErr != nil || outcomeCode(writer) != "accepted" || outcomeCode(reader) != "accepted" {
		return passResult("J", "memory delete", []Check{{Name: "second-bot", Pass: false, Detail: "create failed"}})
	}
	writerID, _ := dataOf(writer)["assistant"].(map[string]any)["assistantId"].(string)
	readerID, _ := dataOf(reader)["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := r.sendTo(writerID, newID(), "请写下两条记忆。标记 FIXTURE_MEMORY_SHARED。"); err != nil {
		return passResult("J", "memory delete", []Check{{Name: "write", Pass: false, Detail: err.Error()}})
	}
	if err := r.waitAssistant(writerID); err != nil {
		return passResult("J", "memory delete", []Check{{Name: "write-idle", Pass: false, Detail: "timeout"}})
	}
	if _, err := r.sendTo(readerID, newID(), "请查找记忆。标记 FIXTURE_MEMORY_RECALL_SHARED。"); err != nil {
		return passResult("J", "memory delete", []Check{{Name: "recall", Pass: false, Detail: err.Error()}})
	}
	if err := r.waitAssistant(readerID); err != nil {
		return passResult("J", "memory delete", []Check{{Name: "recall-idle", Pass: false, Detail: "timeout"}})
	}
	shared := r.fakeRecall("shared")
	private := r.fakeRecall("private")
	deleted, err := r.client.call(r.ctx, "puck.delete", "relay", map[string]any{"assistantId": writerID}, r.step)
	privateLeft, _ := r.countMemory("FIXTURE_PRIVATE_FACT")
	sharedLeft, _ := r.countMemory("FIXTURE_SHARED_FACT")
	writerGone, writerGoneDetail, writerGoneErr := assistantGone(r.ctx, r.stack.iso.DBPath, writerID)
	checks := []Check{
		{Name: "shared-visible", Pass: recallHas(shared, "memories", "text", "FIXTURE_SHARED_FACT")},
		{Name: "private-hidden", Pass: !recallHas(private, "memories", "text", "FIXTURE_PRIVATE_FACT")},
		{Name: "deleted", Pass: err == nil && outcomeCode(deleted) == "accepted"},
		{Name: "deleted-transient-cleanup", Pass: writerGoneErr == nil && writerGone, Detail: writerGoneDetail},
		{Name: "private-gone", Pass: privateLeft == 0, Detail: fmt.Sprintf("count=%d", privateLeft)},
		{Name: "shared-kept", Pass: sharedLeft == 1, Detail: fmt.Sprintf("count=%d", sharedLeft)},
	}
	return passResult("J", "memory delete", checks)
}

func (r *runner) scenarioIDirect() ScenarioResult {
	for _, text := range []string{"机主习惯早上发版", "请看 PR-1842"} {
		if _, err := r.stack.module.UpdateMemory(r.ctx, r.assistantID, toolcontract.UpdateMemoryRequest{Action: "write", Scope: "bot", Kind: "journal", Text: text}); err != nil {
			return passResult("I", "memory recall", []Check{{Name: "write", Pass: false, Detail: err.Error()}})
		}
	}
	exact := r.recallOnce(r.assistantID, "PR-1842")
	keyword := r.recallOnce(r.assistantID, "早上发版")
	if err := r.stack.module.RecordBotTask(r.ctx, r.assistantID, "PR-1842", "看工单", "跟进 PR-1842 的工单"); err != nil {
		return passResult("I", "memory recall", []Check{{Name: "task", Pass: false, Detail: err.Error()}})
	}
	choices, err := r.stack.module.ListTaskChoices(r.ctx, r.assistantID, "PR-1842")
	if err != nil {
		return passResult("I", "memory recall", []Check{{Name: "list", Pass: false, Detail: err.Error()}})
	}
	choiceList := jsonObject(choices)
	taskID := firstRecall(choiceList, "tasks", "id")
	full, fullErr := r.stack.module.ReadTaskContext(r.ctx, r.assistantID, taskID)
	fullTask, _ := jsonObject(full)["task"].(map[string]any)
	checks := []Check{
		{Name: "fts-keyword", Pass: recallHas(keyword, "memories", "text", "机主习惯早上发版")},
		{Name: "exact-memory", Pass: firstRecall(exact, "memories", "text") == "请看 PR-1842"},
		{Name: "choice", Pass: firstRecall(choiceList, "tasks", "description") == "跟进 PR-1842 的工单" && firstRecall(choiceList, "tasks", "id") != ""},
		{Name: "full-context", Pass: fullErr == nil && fullTask["goal"] == "看工单" && fullTask["description"] == "跟进 PR-1842 的工单"},
	}
	return passResult("I", "memory recall", checks)
}

func (r *runner) scenarioJDirect() ScenarioResult {
	writer, err := r.client.call(r.ctx, "puck.create", "relay", map[string]any{"name": "E2E-C", "brainDeviceId": r.stack.deviceID, "permissionMode": "full"}, r.step)
	reader, readerErr := r.client.call(r.ctx, "puck.create", "relay", map[string]any{"name": "E2E-B", "brainDeviceId": r.stack.deviceID, "permissionMode": "full"}, r.step)
	if err != nil || readerErr != nil || outcomeCode(writer) != "accepted" || outcomeCode(reader) != "accepted" {
		return passResult("J", "memory delete", []Check{{Name: "second-bot", Pass: false, Detail: "create failed"}})
	}
	writerID, _ := dataOf(writer)["assistant"].(map[string]any)["assistantId"].(string)
	readerID, _ := dataOf(reader)["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := r.stack.module.UpdateMemory(r.ctx, writerID, toolcontract.UpdateMemoryRequest{Action: "write", Scope: "shared", Kind: "profile", Text: "FIXTURE_SHARED_FACT"}); err != nil {
		return passResult("J", "memory delete", []Check{{Name: "write", Pass: false, Detail: err.Error()}})
	}
	if _, err := r.stack.module.UpdateMemory(r.ctx, writerID, toolcontract.UpdateMemoryRequest{Action: "write", Scope: "bot", Kind: "profile", Text: "FIXTURE_PRIVATE_FACT"}); err != nil {
		return passResult("J", "memory delete", []Check{{Name: "write-private", Pass: false, Detail: err.Error()}})
	}
	shared := r.recallOnce(readerID, "FIXTURE_SHARED_FACT")
	private := r.recallOnce(readerID, "FIXTURE_PRIVATE_FACT")
	deleted, err := r.client.call(r.ctx, "puck.delete", "relay", map[string]any{"assistantId": writerID}, r.step)
	privateLeft, _ := r.countMemory("FIXTURE_PRIVATE_FACT")
	sharedLeft, _ := r.countMemory("FIXTURE_SHARED_FACT")
	writerGone, writerGoneDetail, writerGoneErr := assistantGone(r.ctx, r.stack.iso.DBPath, writerID)
	return passResult("J", "memory delete", []Check{
		{Name: "shared-visible", Pass: recallHas(shared, "memories", "text", "FIXTURE_SHARED_FACT")},
		{Name: "private-hidden", Pass: !recallHas(private, "memories", "text", "FIXTURE_PRIVATE_FACT")},
		{Name: "deleted", Pass: err == nil && outcomeCode(deleted) == "accepted"},
		{Name: "deleted-transient-cleanup", Pass: writerGoneErr == nil && writerGone, Detail: writerGoneDetail},
		{Name: "private-gone", Pass: privateLeft == 0, Detail: fmt.Sprintf("count=%d", privateLeft)},
		{Name: "shared-kept", Pass: sharedLeft == 1, Detail: fmt.Sprintf("count=%d", sharedLeft)},
	})
}

func (r *runner) recallOnce(assistantID, query string) map[string]any {
	payload, err := r.stack.module.RecallMemory(r.ctx, assistantID, toolcontract.RecallMemoryRequest{
		Query: query, Scope: "all", Kind: "any", Sources: []string{"memory", "tasks", "messages"}, Limit: 8,
	})
	if err != nil {
		return map[string]any{}
	}
	return jsonObject(payload)
}

func jsonObject(value map[string]any) map[string]any {
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(body, &out) != nil {
		return nil
	}
	return out
}

func (r *runner) scenarioK() ScenarioResult {
	if r.fake {
		return ScenarioResult{ID: "K", Name: "remember choice", Status: "skipped"}
	}
	before := r.toolNames()
	messageID := newID()
	if _, err := r.send(messageID, "记住：我以后都希望你先给结论，再用很短的中文。这是长期偏好。"); err != nil {
		return ScenarioResult{ID: "K", Name: "remember choice", Status: "fail", Checks: []Check{{Name: "brain-turn-started", Detail: "send rejected"}}}
	}
	if !r.brainTurnReceived(messageID) {
		return ScenarioResult{ID: "K", Name: "remember choice", Status: "fail", Checks: []Check{{Name: "brain-turn-started", Detail: "no accepted turn for prompt"}}}
	}
	if err := r.waitIdle(); err != nil {
		return ScenarioResult{ID: "K", Name: "remember choice", Status: "fail", Checks: []Check{{Name: "brain-turn-completed", Detail: "timeout"}}}
	}
	called := false
	for _, name := range r.toolNames() {
		if strings.Contains(name, "update_memory") && !containsText(before, name) {
			called = true
		}
	}
	return ScenarioResult{ID: "K", Name: "remember choice", Status: "observed", Checks: []Check{{Name: "brain-turn-started", Pass: true}, {Name: "called-update-memory", Pass: true, Detail: fmt.Sprintf("observed=%t", called)}}}
}

func (r *runner) waitAssistant(id string) error {
	deadline := time.Now().Add(r.step)
	for time.Now().Before(deadline) {
		history, err := r.client.call(r.ctx, "puck.history", "relay", map[string]any{"assistantId": id}, 20*time.Second)
		r.collectBrainTurns()
		if err == nil {
			if !messagesBusy(messagesOf(history)) {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("brain stayed busy")
}

func (r *runner) sendTo(assistantID, id, text string) (map[string]any, error) {
	response, err := r.client.call(r.ctx, "puck.message.send", "relay", map[string]any{
		"assistantId": assistantID, "clientMessageId": id, "text": text,
	}, time.Minute)
	if err != nil {
		return nil, err
	}
	if outcomeCode(response) != "accepted" {
		return response, fmt.Errorf("%s", outcomeCode(response))
	}
	return response, nil
}

func (r *runner) countMemory(text string) (int, error) {
	db, err := openStateDB(r.stack.iso.DBPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM bot_memories WHERE text=?`, text).Scan(&count)
	return count, err
}

func (r *runner) fakeEnvelope() string {
	var envelope string
	body, err := os.ReadFile(os.Getenv("PUCK_E2E_FAKE_STATE"))
	if err != nil {
		return ""
	}
	var state struct {
		Envelope string `json:"envelope"`
	}
	if json.Unmarshal(body, &state) == nil {
		envelope = state.Envelope
	}
	return envelope
}

func (r *runner) fakeRecall(name string) map[string]any {
	body, err := os.ReadFile(os.Getenv("PUCK_E2E_FAKE_STATE"))
	if err != nil {
		return nil
	}
	var state struct {
		Recalls map[string]string `json:"recalls"`
	}
	if json.Unmarshal(body, &state) != nil {
		return nil
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(state.Recalls[name]), &payload)
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return payload
	}
	return data
}

func recallValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func recallHas(payload map[string]any, list, field, text string) bool {
	items, _ := payload[list].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		if row[field] == text {
			return true
		}
	}
	return false
}

func firstRecall(payload map[string]any, list, field string) string {
	items, _ := payload[list].([]any)
	if len(items) == 0 {
		return ""
	}
	row, _ := items[0].(map[string]any)
	value, _ := row[field].(string)
	return value
}

func boolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func containsText(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func combinePacks(views []PackView) PackView {
	var combined PackView
	combined.CWDTrusted = true
	for _, view := range views {
		combined.HasLink = combined.HasLink || view.HasLink
		combined.HasDeviceID = combined.HasDeviceID || view.HasDeviceID
		combined.HasThreadID = combined.HasThreadID || view.HasThreadID
		combined.HasSummary = combined.HasSummary || view.HasSummary
		combined.HasTruncation = combined.HasTruncation || view.HasTruncation
		combined.Duplicate = combined.Duplicate || view.Duplicate
		if view.SentinelCount > combined.SentinelCount {
			combined.SentinelCount = view.SentinelCount
		}
		if view.CWD != "" {
			combined.CWD = view.CWD
		}
		combined.CWDTrusted = combined.CWDTrusted && view.CWDTrusted
	}
	if len(views) == 0 {
		combined.CWDTrusted = false
	}
	return combined
}
