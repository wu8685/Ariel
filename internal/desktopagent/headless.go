package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func runHeadless(ctx context.Context, cfg Config) error {
	if cfg.Binary == "" {
		return errors.New("headless mode requires the Codex CLI binary")
	}
	if cfg.Socket != "" {
		return errors.New("headless mode refuses the Desktop IPC socket")
	}
	cwd := cfg.CWD
	if cwd == "" {
		return errors.New("headless mode requires an explicit working directory")
	}
	readOnlyRPC, err := appserver.NewRestartingRPCWithThreadListTimeout(ctx, threadListAttemptTimeout, func(parent context.Context) (appserver.ReadOnlyEndpoint, error) {
		return appserver.Start(parent, cfg.Binary, cwd)
	})
	if err != nil {
		return err
	}
	defer readOnlyRPC.Close()
	cfg.CWD = cwd
	service := NewService(appserver.HistoryReader{RPC: readOnlyRPC}, headlessLiveFactory(cfg.Binary))
	service.SetHeadless(true)
	service.SetAgentStateDir(cwd)
	service.SetThreadBootstrapper(AppServerBootstrapper{Binary: cfg.Binary})
	if !cfg.DisableExecutionSettings {
		service.SetModelReader(appserver.ModelReader{RPC: readOnlyRPC})
	}
	service.SetThreadArchiver(appserver.ThreadArchiver{RPC: readOnlyRPC})
	service.SetThreadMetadataWriter(appserver.ThreadMetadataWriter{RPC: readOnlyRPC})
	defer service.Close()
	return runTransportLoop(ctx, cfg, service)
}

func headlessLiveFactory(binary string) LiveFactory {
	return func(_ context.Context, threadID, cwd string) (Live, error) {
		if threadID == "" || cwd == "" || binary == "" {
			return nil, errors.New("INVALID_ARGUMENT")
		}
		return newHeadlessLive(binary, threadID, cwd), nil
	}
}

// headlessLive speaks to a private app-server stdio process. It never dials
// Desktop IPC and never launches a GUI.
type headlessLive struct {
	binary, threadID, cwd string
	mu                    sync.Mutex
	process               *appserver.InteractiveProcess
	updates               chan struct{}
	done                  chan struct{}
	closeOnce             sync.Once
}

func newHeadlessLive(binary, threadID, cwd string) *headlessLive {
	return &headlessLive{binary: binary, threadID: threadID, cwd: cwd, updates: make(chan struct{}), done: make(chan struct{})}
}

func (l *headlessLive) Current() (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"cwd": l.cwd, "threadRuntimeStatus": map[string]any{"type": "idle"},
		"requests": []any{}, "turns": []any{},
	})
}

func (l *headlessLive) Start(ctx context.Context, messageID, text string) (string, error) {
	return l.start(ctx, messageID, text, nil, "")
}

func (l *headlessLive) StartWithImages(ctx context.Context, messageID, text string, images []string) (string, error) {
	return l.start(ctx, messageID, text, images, "")
}

func (l *headlessLive) StartWithExecutionPolicy(ctx context.Context, messageID, text string, images []string, mode string) (string, error) {
	return l.start(ctx, messageID, text, images, mode)
}

func (l *headlessLive) start(ctx context.Context, messageID, text string, images []string, mode string) (string, error) {
	if err := l.ensure(ctx); err != nil {
		return "", err
	}
	input := make([]map[string]any, 0, 1+len(images))
	if text != "" {
		input = append(input, map[string]any{"type": "text", "text": text, "text_elements": []any{}})
	}
	for _, image := range images {
		input = append(input, map[string]any{"type": "image", "url": image})
	}
	params := map[string]any{"threadId": l.threadID, "clientUserMessageId": messageID, "input": input}
	if mode != "" {
		policy := appserver.PuckExecutionPolicyParams(mode, l.cwd)
		params["approvalPolicy"] = policy["approvalPolicy"]
		if reviewer, ok := policy["approvalsReviewer"].(string); ok && reviewer != "" {
			params["approvalsReviewer"] = reviewer
		}
		params["sandboxPolicy"] = policy["sandboxPolicy"]
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := l.process.Call(ctx, "turn/start", params, &started); err != nil || started.Turn.ID == "" {
		return "", appserver.ErrOutcomeUnknown
	}
	return started.Turn.ID, nil
}

func (l *headlessLive) ensure(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process != nil {
		return nil
	}
	process, err := appserver.StartInteractive(ctx, l.binary, l.cwd)
	if err != nil {
		return err
	}
	if err := process.Call(ctx, "thread/resume", map[string]any{"threadId": l.threadID, "cwd": l.cwd}, nil); err != nil {
		_ = process.Close()
		return err
	}
	l.process = process
	go func() {
		<-process.Done()
		l.closeOnce.Do(func() { close(l.done) })
	}()
	return nil
}

func (l *headlessLive) Interrupt(ctx context.Context, turnID string) error {
	l.mu.Lock()
	process := l.process
	l.mu.Unlock()
	if process == nil {
		return errors.New("HEADLESS_NO_IPC")
	}
	return process.Call(ctx, "turn/interrupt", map[string]any{"threadId": l.threadID, "turnId": turnID}, nil)
}

func (l *headlessLive) Respond(context.Context, string, string, map[string][]string) error {
	return errors.New("HEADLESS_NO_IPC")
}

func (l *headlessLive) Updates() <-chan struct{} { return l.updates }
func (l *headlessLive) Done() <-chan struct{}    { return l.done }

func (l *headlessLive) Close() error {
	l.mu.Lock()
	process := l.process
	l.process = nil
	l.mu.Unlock()
	l.closeOnce.Do(func() { close(l.done) })
	if process != nil {
		return process.Close()
	}
	return nil
}
