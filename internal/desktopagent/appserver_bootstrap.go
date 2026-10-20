package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type AppServerBootstrapper struct{ Binary string }

type appServerBootstrapLive struct {
	process                 *appserver.InteractiveProcess
	threadID, turnID, cwd   string
	clientMessageID         string
	mu                      sync.Mutex
	thread                  map[string]any
	failure                 error
	pending                 map[string]appserver.IncomingFrame
	updates                 chan struct{}
	terminal                chan struct{}
	terminalOnce, closeOnce sync.Once
}

func (b AppServerBootstrapper) Bootstrap(ctx context.Context, cwd, messageID, text string, images []string) (BootstrapResult, error) {
	if b.Binary == "" {
		return BootstrapResult{}, appserver.ErrMethodUnavailable
	}
	process, err := appserver.StartInteractive(ctx, b.Binary, cwd)
	if err != nil {
		return BootstrapResult{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = process.Close()
		}
	}()
	var started struct {
		Thread appserver.Thread `json:"thread"`
	}
	if err := process.Call(ctx, "thread/start", map[string]any{"cwd": cwd, "ephemeral": false, "serviceName": "ariel"}, &started); err != nil {
		return BootstrapResult{}, appserver.ErrOutcomeUnknown
	}
	if !threadIDPattern.MatchString(started.Thread.ID) || started.Thread.CWD != cwd {
		return BootstrapResult{}, appserver.ErrOutcomeUnknown
	}
	input := make([]map[string]any, 0, 1+len(images))
	if text != "" {
		input = append(input, map[string]any{"type": "text", "text": text, "text_elements": []any{}})
	}
	for _, image := range images {
		input = append(input, map[string]any{"type": "image", "url": image})
	}
	var turnStarted struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := process.Call(ctx, "turn/start", map[string]any{"threadId": started.Thread.ID, "clientUserMessageId": messageID, "input": input}, &turnStarted); err != nil || turnStarted.Turn.ID == "" {
		return BootstrapResult{}, appserver.ErrOutcomeUnknown
	}
	live := &appServerBootstrapLive{
		process: process, threadID: started.Thread.ID, turnID: turnStarted.Turn.ID, cwd: cwd, clientMessageID: messageID,
		pending: map[string]appserver.IncomingFrame{}, updates: make(chan struct{}, 1), terminal: make(chan struct{}),
	}
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRefresh()
	for {
		if err := live.refresh(refreshCtx); err == nil {
			break
		}
		select {
		case <-refreshCtx.Done():
			return BootstrapResult{}, appserver.ErrOutcomeUnknown
		case <-process.Done():
			return BootstrapResult{}, appserver.ErrOutcomeUnknown
		case <-time.After(50 * time.Millisecond):
		}
	}
	keep = true
	go live.run()
	return BootstrapResult{ThreadID: live.threadID, TurnID: live.turnID, Title: started.Thread.Name, CWD: cwd, Live: live}, nil
}

func (l *appServerBootstrapLive) refresh(ctx context.Context) error {
	var out struct {
		Thread appserver.Thread `json:"thread"`
	}
	if err := l.process.Call(ctx, "thread/read", map[string]any{"threadId": l.threadID, "includeTurns": true}, &out); err != nil {
		return err
	}
	if out.Thread.ID != l.threadID || out.Thread.CWD != l.cwd {
		return ErrNativeShape
	}
	start := max(0, len(out.Thread.Turns)-recentTurnLimit)
	out.Thread.Turns = out.Thread.Turns[start:]
	thread, err := NormalizeStored(out.Thread)
	if err != nil {
		return err
	}
	thread["historyComplete"] = start == 0
	thread["recentComplete"] = true
	l.mu.Lock()
	interactions := make([]any, 0, len(l.pending))
	for id, frame := range l.pending {
		interactions = append(interactions, normalizeBootstrapInteraction(id, frame))
	}
	thread["pendingInteractions"] = interactions
	l.thread = thread
	l.mu.Unlock()
	return checkThreadSize(thread)
}

func (l *appServerBootstrapLive) run() {
	var refreshTimer *time.Timer
	var refreshDeadline <-chan time.Time
	terminalPending := false
	terminalRefreshFailures := 0
	defer func() {
		if refreshTimer != nil {
			refreshTimer.Stop()
		}
	}()
	for {
		select {
		case frame := <-l.process.Incoming():
			if !l.matches(frame.Params) {
				if frame.IsRequest() {
					_ = l.process.Reply(context.Background(), frame.ID, nil, map[string]any{"code": -32602, "message": "identity mismatch"})
				}
				l.fail(fmt.Errorf("identity mismatch for %s", frame.Method))
				return
			}
			if frame.IsRequest() {
				if !supportedBootstrapRequest(frame.Method) {
					_ = l.process.Reply(context.Background(), frame.ID, nil, map[string]any{"code": -32601, "message": "unsupported bootstrap interaction"})
					l.fail(fmt.Errorf("unsupported server request %s", frame.Method))
					return
				}
				id := "bootstrap-" + rand.Text()
				l.mu.Lock()
				l.pending[id] = frame
				l.mu.Unlock()
				l.signalUpdate()
				continue
			}
			if frame.Method == "turn/completed" && terminalTurn(frame.Params, l.turnID) {
				terminalPending = true
			}
			if refreshTimer == nil {
				refreshTimer = time.NewTimer(50 * time.Millisecond)
				refreshDeadline = refreshTimer.C
			}
		case <-refreshDeadline:
			refreshTimer = nil
			refreshDeadline = nil
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := l.refresh(ctx)
			cancel()
			if err != nil {
				if terminalPending {
					terminalRefreshFailures++
					if terminalRefreshFailures >= 100 {
						l.fail(fmt.Errorf("terminal refresh: %w", err))
						return
					}
					refreshTimer = time.NewTimer(100 * time.Millisecond)
					refreshDeadline = refreshTimer.C
				}
				continue
			}
			terminalRefreshFailures = 0
			l.signalUpdate()
			if terminalPending {
				l.terminalOnce.Do(func() { close(l.terminal) })
				terminalPending = false
			}
		case <-l.process.Done():
			return
		}
	}
}

func (l *appServerBootstrapLive) matches(params json.RawMessage) bool {
	var identity struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Thread   *struct {
			ID string `json:"id"`
		} `json:"thread"`
		Turn *struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &identity) != nil {
		return false
	}
	threadID := identity.ThreadID
	if threadID == "" && identity.Thread != nil {
		threadID = identity.Thread.ID
	}
	if threadID != "" && threadID != l.threadID {
		return false
	}
	turnID := identity.TurnID
	if turnID == "" && identity.Turn != nil {
		turnID = identity.Turn.ID
	}
	return turnID == "" || turnID == l.turnID
}

func terminalTurn(params json.RawMessage, expected string) bool {
	var value struct {
		Turn struct{ ID, Status string } `json:"turn"`
	}
	if json.Unmarshal(params, &value) != nil || value.Turn.ID != expected {
		return false
	}
	switch value.Turn.Status {
	case "completed", "failed", "interrupted":
		return true
	}
	return false
}

func supportedBootstrapRequest(method string) bool {
	return method == "item/commandExecution/requestApproval" || method == "item/fileChange/requestApproval" || method == "item/tool/requestUserInput" || strings.Contains(method, "permission")
}

func normalizeBootstrapInteraction(id string, frame appserver.IncomingFrame) map[string]any {
	kind, decisions := "unsupported", []any{"deny"}
	switch {
	case frame.Method == "item/commandExecution/requestApproval":
		kind, decisions = "command_approval", []any{"deny", "deny_and_stop", "accept_once"}
	case frame.Method == "item/fileChange/requestApproval":
		kind, decisions = "file_approval", []any{"deny", "deny_and_stop", "accept_once"}
	case frame.Method == "item/tool/requestUserInput":
		kind, decisions = "user_input", []any{"answer", "deny_and_stop"}
	case strings.Contains(frame.Method, "permission"):
		kind, decisions = "permission_request", []any{"deny", "deny_and_stop", "accept_once"}
	}
	prompt := strings.TrimSpace(string(frame.Params))
	if len(prompt) > 2048 {
		prompt = prompt[:2048]
	}
	card := map[string]any{"interactionId": id, "kind": kind, "prompt": prompt, "availableDecisions": decisions}
	if kind == "user_input" {
		var value struct {
			Questions []struct {
				ID, Header, Question string
				IsSecret             bool `json:"isSecret"`
				Options              []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
		}
		if json.Unmarshal(frame.Params, &value) == nil && len(value.Questions) > 0 && len(value.Questions) <= 3 {
			questions := make([]any, 0, len(value.Questions))
			for _, question := range value.Questions {
				if question.IsSecret || strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Question) == "" {
					card["kind"] = "unsupported"
					card["prompt"] = "此问题需要在电脑端处理。"
					card["availableDecisions"] = []any{}
					delete(card, "questions")
					return card
				}
				options := make([]any, 0, len(question.Options))
				for _, option := range question.Options {
					if strings.TrimSpace(option.Label) == "" {
						card["kind"] = "unsupported"
						card["prompt"] = "此问题需要在电脑端处理。"
						card["availableDecisions"] = []any{}
						return card
					}
					normalized := map[string]any{"label": option.Label}
					if option.Description != "" {
						normalized["description"] = option.Description
					}
					options = append(options, normalized)
				}
				normalized := map[string]any{"id": question.ID, "question": question.Question, "options": options}
				if question.Header != "" {
					normalized["header"] = question.Header
				}
				questions = append(questions, normalized)
			}
			card["questions"] = questions
		} else {
			card["kind"] = "unsupported"
			card["prompt"] = "此问题需要在电脑端处理。"
			card["availableDecisions"] = []any{}
		}
	}
	return card
}

func (l *appServerBootstrapLive) Current() (json.RawMessage, error) { return nil, nil }
func (l *appServerBootstrapLive) CurrentThread() (map[string]any, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.thread == nil {
		return nil, errors.New("RESYNC_REQUIRED")
	}
	body, _ := json.Marshal(l.thread)
	var copied map[string]any
	if json.Unmarshal(body, &copied) != nil {
		return nil, ErrNativeShape
	}
	return copied, nil
}
func (l *appServerBootstrapLive) Failure() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.failure
}
func (l *appServerBootstrapLive) fail(err error) {
	l.mu.Lock()
	if l.failure == nil {
		l.failure = err
	}
	l.mu.Unlock()
	_ = l.Close()
}
func (l *appServerBootstrapLive) Start(context.Context, string, string) (string, error) {
	return "", appserver.ErrMethodUnavailable
}
func (l *appServerBootstrapLive) Interrupt(ctx context.Context, turnID string) error {
	if turnID != l.turnID {
		return errors.New("STALE_TURN")
	}
	return l.process.Call(ctx, "turn/interrupt", map[string]any{"threadId": l.threadID, "turnId": l.turnID}, nil)
}
func (l *appServerBootstrapLive) Respond(ctx context.Context, interactionID, decision string, answers map[string][]string) error {
	l.mu.Lock()
	frame, ok := l.pending[interactionID]
	if ok {
		delete(l.pending, interactionID)
	}
	l.mu.Unlock()
	if !ok {
		return errors.New("STALE_INTERACTION")
	}
	var result any
	if frame.Method == "item/tool/requestUserInput" {
		mapped := map[string]any{}
		for id, values := range answers {
			mapped[id] = map[string]any{"answers": values}
		}
		result = map[string]any{"answers": mapped}
	} else {
		native := map[string]string{"accept_once": "accept", "deny": "decline", "deny_and_stop": "cancel"}[decision]
		if native == "" {
			return appserver.ErrInvalidArgument
		}
		result = map[string]any{"decision": native}
	}
	if err := l.process.Reply(ctx, frame.ID, result, nil); err != nil {
		return err
	}
	l.signalUpdate()
	if decision == "deny_and_stop" {
		return l.Interrupt(ctx, l.turnID)
	}
	return nil
}
func (l *appServerBootstrapLive) Updates() <-chan struct{}  { return l.updates }
func (l *appServerBootstrapLive) Done() <-chan struct{}     { return l.process.Done() }
func (l *appServerBootstrapLive) Terminal() <-chan struct{} { return l.terminal }
func (l *appServerBootstrapLive) Verify(ctx context.Context) error {
	if err := l.refresh(ctx); err != nil {
		return err
	}
	thread, _ := l.CurrentThread()
	for _, raw := range thread["turns"].([]any) {
		turn := raw.(map[string]any)
		if turn["turnId"] != l.turnID {
			continue
		}
		status := turn["status"]
		if status == "completed" || status == "failed" || status == "interrupted" {
			for _, itemRaw := range turn["items"].([]any) {
				item := itemRaw.(map[string]any)
				if item["clientMessageId"] == l.clientMessageID {
					return nil
				}
			}
		}
	}
	return errors.New("NATIVE_STATE_UNCERTAIN")
}
func (l *appServerBootstrapLive) Close() error {
	var err error
	l.closeOnce.Do(func() { err = l.process.Close() })
	return err
}
func (l *appServerBootstrapLive) signalUpdate() {
	select {
	case l.updates <- struct{}{}:
	default:
	}
}
