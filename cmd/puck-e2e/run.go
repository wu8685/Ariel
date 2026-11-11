package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type options struct {
	Root    string
	Binary  string
	Socket  string
	Only    map[string]bool
	Retries int
	Timeout time.Duration
	Keep    bool
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
	iso := layout(root, opt.Binary, opt.Socket)
	if err := validateIsolation(iso); err != nil {
		return Report{}, err
	}
	stack, err := newStack(iso)
	if err != nil {
		return Report{}, err
	}
	defer func() {
		stack.close()
		if !opt.Keep {
			_ = os.RemoveAll(root)
		}
	}()
	if err := stack.startRelay(ctx); err != nil {
		return Report{}, err
	}
	if err := stack.startAgent(ctx); err != nil {
		return Report{}, err
	}
	owner, err := dialOwner(ctx, stack.wsURL(), webOrigin, stack.pin)
	if err != nil {
		return Report{}, err
	}
	run := &runner{ctx: ctx, stack: stack, client: owner, marker: "FIXTURE-MARKER-" + stack.token[:8], retries: opt.Retries, step: 6 * time.Minute, seen: map[string]struct{}{}}
	defer func() { run.client.Close() }()
	report := Report{OK: true, Listen: stack.listen, Isolation: iso}
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
		{"G", "ux metrics", false, run.scenarioG},
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
	r.childThread = r.latestTaskThread()
	views := r.packViews(r.childThread)
	view := combinePacks(views)
	checks := []Check{
		{Name: "codex-link", Pass: view.HasLink, Detail: "thread=" + r.fixtureThread},
		{Name: "device-and-thread", Pass: view.HasDeviceID && view.HasThreadID, Detail: "device=" + r.stack.deviceID},
		{Name: "cwd", Pass: view.CWDTrusted, Detail: "cwd=" + view.CWD},
		{Name: "no-summary", Pass: !view.HasSummary && !view.HasTruncation},
		{Name: "no-duplicate-sentinel", Pass: !view.Duplicate && view.SentinelCount <= 1, Detail: fmt.Sprintf("sentinel=%d", view.SentinelCount)},
	}
	followID := newID()
	_, _ = r.send(followID, "请对刚才的执行任务调用一次 send_turn，text 只写「继续看引用」。不要 start_task，不要传 clientMessageId。")
	_ = r.waitIdle()
	again := combinePacks(r.packViews(r.childThread))
	checks = append(checks, Check{Name: "send-turn-dedupe", Pass: !again.Duplicate, Detail: fmt.Sprintf("sentinel=%d", again.SentinelCount)})
	return passResult("A", "handoff pack", checks)
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
		Check{Name: "picked-up", Pass: attached == len(ids), Detail: fmt.Sprintf("attached=%d", attached)},
		Check{Name: "one-pending-wake", Pass: r.metrics.MaxQueueWakes <= 1, Detail: fmt.Sprintf("maxQueue=%d", r.metrics.MaxQueueWakes)},
	)
	return passResult("B", "queue while busy", checks)
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
	owner, err := dialOwner(r.ctx, r.stack.wsURL(), webOrigin, r.stack.pin)
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
		if best.Status == "soft_pass" {
			return best
		}
	}
	return best
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
		{Name: "ack-recorded", Pass: true, Detail: fmt.Sprintf("seen=%t", r.metrics.AckSeen)},
		{Name: "send-message-once", Pass: after-before == 1, Detail: fmt.Sprintf("delta=%d", after-before)},
		{Name: "tool-error-recovery", Pass: r.metrics.Fallbacks == 0, Detail: fmt.Sprintf("retries=%d fallbacks=%d", r.metrics.RetryNotices, r.metrics.Fallbacks)},
		{Name: "system-context-lines", Pass: !r.metrics.SystemObserved || (r.metrics.HasHandoff && r.metrics.HasSave && r.metrics.HasMerge), Detail: fmt.Sprintf("lines=%d observed=%t", r.metrics.SystemLines, r.metrics.SystemObserved)},
	}
	return softResult("G", "ux metrics", 1, checks)
}

func (r *runner) scenarioF() ScenarioResult {
	threadID := r.childThread
	if threadID == "" {
		threadID = r.fixtureThread
	}
	linksBefore, _ := sessionLinksForThread(r.ctx, r.stack.iso.DBPath, r.assistantID, threadID)
	if threadID != "" {
		_, _ = r.client.call(r.ctx, "thread.archive.update", r.stack.deviceID, map[string]any{"threadId": threadID, "archived": true}, 30*time.Second)
	}
	deadline := time.Now().Add(45 * time.Second)
	links := linksBefore
	for time.Now().Before(deadline) {
		links, _ = sessionLinksForThread(r.ctx, r.stack.iso.DBPath, r.assistantID, threadID)
		if linksBefore == 0 || links == 0 {
			break
		}
		time.Sleep(time.Second)
	}
	tasksBefore, _ := readSnapshot(r.ctx, r.stack.iso.DBPath, r.assistantID)
	deleted, err := r.client.call(r.ctx, "puck.delete", "relay", map[string]any{"assistantId": r.assistantID}, 30*time.Second)
	gone, goneErr := assistantGone(r.ctx, r.stack.iso.DBPath, r.assistantID)
	return passResult("F", "unlink and delete", []Check{
		{Name: "unlink-session", Pass: linksBefore == 0 || links == 0, Detail: fmt.Sprintf("before=%d after=%d", linksBefore, links)},
		{Name: "task-remained-until-delete", Pass: tasksBefore.BotTasks > 0 || linksBefore == 0, Detail: fmt.Sprintf("tasks=%d", tasksBefore.BotTasks)},
		{Name: "delete-bot", Pass: err == nil && outcomeCode(deleted) == "accepted" && goneErr == nil && gone},
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
		time.Sleep(time.Second)
	}
	return fmt.Errorf("desktop agent did not become ready")
}

func (r *runner) waitIdle() error {
	deadline := time.Now().Add(r.step)
	for time.Now().Before(deadline) {
		r.collectMetrics()
		r.sampleQueue()
		if !r.busy() {
			return nil
		}
		time.Sleep(2 * time.Second)
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
	for _, message := range messagesOf(history) {
		kind, _ := message["kind"].(string)
		if kind == "processing" || kind == "greeting_processing" || kind == "acknowledged" {
			return true
		}
	}
	return false
}

func (r *runner) history() (map[string]any, error) {
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
	for _, message := range messagesOf(history) {
		card, _ := message["cardJSON"].(string)
		if strings.Contains(card, "notify_user") {
			count++
		}
	}
	return count
}

func (r *runner) observeContext() {
	response, err := r.client.call(r.ctx, "thread.read", r.stack.deviceID, map[string]any{"threadId": r.brainThread}, 20*time.Second)
	if err != nil || outcomeCode(response) != "accepted" {
		return
	}
	text := findSystemContext(dataOf(response))
	lines, observed, handoff, save, merge := systemContextLines(text)
	r.metrics.SystemLines = lines
	r.metrics.SystemObserved = observed
	r.metrics.HasHandoff = handoff
	r.metrics.HasSave = save
	r.metrics.HasMerge = merge
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
