package desktopagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	chillingclient "github.com/wu8685/Ariel/internal/chilling"
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/filecontext"
	"github.com/wu8685/Ariel/internal/modelsettings"
	"github.com/wu8685/Ariel/internal/wsheartbeat"
	"github.com/wu8685/Ariel/protocol"
)

type Config struct {
	DisableExecutionSettings                                           bool
	TasksEnabled                                                       bool
	TasksManagerOnly                                                   bool
	TaskEndpoint                                                       string
	TaskBackend                                                        TaskBackend
	TaskAllowedRoots, TaskAllowedManagers                              []string
	TaskPollInterval                                                   time.Duration
	TaskExecutionCheck                                                 func(context.Context, string, string) error
	URL, Token, DeviceID, DeviceName, Binary, Socket, CWD              string
	AgentEpoch                                                         string
	ChillingEndpoint                                                   string
	ChillingEnabled                                                    bool // deprecated config alias; never connects Chilling
	FileContextEnabled, WorkspaceBrowserEnabled, DocumentAssetsEnabled bool
	HeartbeatInterval, HeartbeatTimeout                                time.Duration
	CacheWarmGrace, CachePublishInterval                               time.Duration
	CacheLeaseInterval                                                 time.Duration
	CacheRecoveryGuardDelay                                            time.Duration
	CacheEntryMaxBytes                                                 int
	CachePrewarmRecent                                                 bool
	FileContext                                                        FileContext
	OnReady                                                            func() error
	OnNotReady                                                         func()
}
type FileContext interface {
	Ready() bool
	MessageReferences() bool
	WorkspaceBrowser() bool
	Changes(context.Context, filecontext.ChangesRequest) (filecontext.ChangesResponse, error)
	Read(context.Context, filecontext.ReadRequest) (filecontext.ReadResponse, error)
	ReferenceRead(context.Context, filecontext.ReferenceReadRequest) (filecontext.ReadResponse, error)
	WorkspaceList(context.Context, filecontext.WorkspaceListRequest) (filecontext.WorkspaceListResponse, error)
	WorkspaceRead(context.Context, filecontext.WorkspaceReadRequest) (filecontext.ReadResponse, error)
	Prepare(context.Context, filecontext.PrepareRequest) (filecontext.PrepareResponse, error)
}
type agent struct {
	tasks                   *taskRuntime
	conn                    *websocket.Conn
	deviceID                string
	agentEpoch              string
	service                 *Service
	settingsMu              sync.Mutex
	settingsSubs            map[string]struct{}
	fileContext             FileContext
	writeMu                 sync.Mutex
	cacheMu                 sync.Mutex
	cachePublishMu          sync.Mutex
	cacheSubs               map[string]*cacheSubscription
	cacheWarmGrace          time.Duration
	cachePublishInterval    time.Duration
	cacheRecoveryGuardDelay time.Duration
	cacheEntryMaxBytes      int
	cacheLoad               chan struct{}
	cacheSendMu             sync.Mutex
	cachePending            map[string]cacheOutbound
	cacheWake               chan struct{}
	questionsEnabled        bool // negotiated for this Relay transport, cacheMu
}

type cacheOutbound struct {
	streamID string
	seq      int64
	thread   map[string]any
}

type cacheSubscription struct {
	subscriptionID  string
	streamID        string
	runtime         string
	watching        bool
	evictableAt     time.Time
	lastTouched     time.Time
	lastPublished   time.Time
	structure       string
	publishTimer    *time.Timer
	recoveryTimer   *time.Timer
	recoveryUsed    bool
	pendingSeq      int64
	pendingThread   map[string]any
	latestSeq       int64
	latestThread    map[string]any
	lastSentSeq     int64
	lastSentRuntime string
	recoveryFailed  bool
}

// Service.List may issue three sequential thread/list calls (recent, the
// modern pinned filter, and the Codex 0.160.0 pinned-section fallback). Every
// call shares one request budget; each individual attempt may use at most half
// of it so a hung recent-list endpoint can still be rebuilt once.
const (
	threadListAttemptTimeout       = 15 * time.Second
	threadListRequestTimeout       = 30 * time.Second
	threadSubscribeLoadTimeout     = 45 * time.Second
	defaultCacheEntryMaxBytes      = 8 << 20
	cacheSnapshotWriteTimeout      = 5 * time.Second
	defaultCachePublishInterval    = 5 * time.Second
	defaultCacheRecoveryGuardDelay = 45 * time.Second
)

var errCacheSnapshotOversized = errors.New("CACHE_OVERSIZED")

func threadListRequestContext(ctx context.Context, searchTerm string) (context.Context, context.CancelFunc) {
	if searchTerm != "" {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, threadListRequestTimeout)
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.TasksEnabled && cfg.TaskBackend == nil && cfg.TaskEndpoint != "" {
		client, err := chillingclient.NewTaskClient(ctx, chillingclient.Config{Enabled: true, Endpoint: cfg.TaskEndpoint})
		if client != nil {
			cfg.TaskBackend = client
		}
		if err != nil {
			log.Printf("task backend unavailable: %s", chillingclient.CodeOf(err))
		}
	}
	if cfg.TasksManagerOnly {
		if !cfg.TasksEnabled {
			return errors.New("task manager-only requires explicit task flag")
		}
		return RunWithService(ctx, cfg, nil)
	}
	if cfg.Binary == "" || cfg.Socket == "" {
		return errors.New("Codex binary and Desktop IPC socket are required")
	}
	check := func(ctx context.Context) error { return checkDesktop(ctx, cfg.Socket) }
	if err := check(ctx); err != nil {
		return err
	}
	cwd := cfg.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	readOnlyRPC, err := appserver.NewRestartingRPCWithThreadListTimeout(ctx, threadListAttemptTimeout, func(parent context.Context) (appserver.ReadOnlyEndpoint, error) {
		return appserver.Start(parent, cfg.Binary, cwd)
	})
	if err != nil {
		return err
	}
	defer readOnlyRPC.Close()
	if cfg.TasksEnabled && cfg.TaskExecutionCheck == nil {
		cfg.TaskExecutionCheck = NewOfficialTaskCheck(readOnlyRPC)
	}
	cfg.CWD = cwd
	queueReader := appserver.QueueReader{RPC: readOnlyRPC}
	queueSupported, err := queueReader.Supported(ctx)
	if err != nil {
		return err
	}
	var queues []FollowUpQueue
	if queueSupported {
		queues = append(queues, queueReader)
	}
	service := NewService(appserver.HistoryReader{RPC: readOnlyRPC}, func(ctx context.Context, id, cwd string) (Live, error) { return OpenFollower(ctx, cfg.Socket, id, cwd) }, queues...)
	service.SetThreadBootstrapper(AppServerBootstrapper{Binary: cfg.Binary})
	service.editEnabled = verifiedEditBinary(ctx, cfg.Binary)
	if !cfg.DisableExecutionSettings {
		service.SetModelReader(appserver.ModelReader{RPC: readOnlyRPC})
	}
	service.SetThreadArchiver(appserver.ThreadArchiver{RPC: readOnlyRPC})
	service.SetThreadMetadataWriter(appserver.ThreadMetadataWriter{RPC: readOnlyRPC})
	configureFileContext(&cfg, readOnlyRPC)
	defer service.Close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	healthResult := make(chan error, 1)
	go func() {
		err := monitorDesktop(runCtx, 5*time.Second, check)
		healthResult <- err
		if err != nil {
			cancel()
		}
	}()
	if cfg.AgentEpoch == "" {
		cfg.AgentEpoch = rand.Text()
	}
	err = runTransportLoop(runCtx, cfg, service)
	cancel()
	select {
	case healthErr := <-healthResult:
		if healthErr != nil {
			return healthErr
		}
	default:
	}
	return err
}

func runTransportLoop(ctx context.Context, cfg Config, service *Service) error {
	delay := 200 * time.Millisecond
	for ctx.Err() == nil {
		err := RunWithService(ctx, cfg, service)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("Relay transport disconnected: %v; reconnecting without restarting Codex service", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
	return ctx.Err()
}

func RunWithService(ctx context.Context, cfg Config, service *Service) error {
	if cfg.URL == "" || cfg.Token == "" || cfg.DeviceID == "" || cfg.DeviceName == "" || (service == nil && !(cfg.TasksEnabled && cfg.TasksManagerOnly)) {
		return errors.New("invalid Desktop Agent configuration")
	}
	conn, _, err := websocket.Dial(ctx, cfg.URL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	agentEpoch := cfg.AgentEpoch
	if agentEpoch == "" {
		agentEpoch = rand.Text()
	}
	cacheWarmGrace := cfg.CacheWarmGrace
	if cacheWarmGrace <= 0 {
		cacheWarmGrace = 90 * time.Second
	}
	cachePublishInterval := cfg.CachePublishInterval
	if cachePublishInterval <= 0 {
		cachePublishInterval = defaultCachePublishInterval
	}
	cacheEntryMaxBytes := cfg.CacheEntryMaxBytes
	if cacheEntryMaxBytes <= 0 || cacheEntryMaxBytes > defaultCacheEntryMaxBytes {
		cacheEntryMaxBytes = defaultCacheEntryMaxBytes
	}
	cacheRecoveryGuardDelay := cfg.CacheRecoveryGuardDelay
	if cacheRecoveryGuardDelay <= 0 {
		cacheRecoveryGuardDelay = defaultCacheRecoveryGuardDelay
	}
	a := &agent{conn: conn, deviceID: cfg.DeviceID, agentEpoch: agentEpoch, service: service, fileContext: cfg.FileContext, cacheSubs: map[string]*cacheSubscription{}, cacheWarmGrace: cacheWarmGrace, cachePublishInterval: cachePublishInterval, cacheRecoveryGuardDelay: cacheRecoveryGuardDelay, cacheEntryMaxBytes: cacheEntryMaxBytes, cacheLoad: make(chan struct{}, 1), cachePending: map[string]cacheOutbound{}, cacheWake: make(chan struct{}, 1)}
	defer a.releaseCacheSubscriptions()
	defer a.releaseSettingsSubscriptions()
	a.tasks = newTaskRuntime(a, cfg)
	requestCtx, cancelRequests := context.WithCancel(ctx)
	var requests sync.WaitGroup
	var cacheSender sync.WaitGroup
	cacheSender.Add(1)
	go func() { defer cacheSender.Done(); a.runCacheSender(requestCtx) }()
	queue := make(chan struct{}, 32)
	type cacheTouchRequest struct {
		id     string
		method string
		params json.RawMessage
	}
	cacheTouches := make(chan cacheTouchRequest, 32)
	var cacheTouchWorker sync.WaitGroup
	cacheTouchWorker.Add(1)
	go func() {
		defer cacheTouchWorker.Done()
		for request := range cacheTouches {
			a.handle(requestCtx, request.id, request.method, request.params)
		}
	}()
	defer func() {
		cancelRequests()
		cacheSender.Wait()
		close(cacheTouches)
		cacheTouchWorker.Wait()
		requests.Wait()
	}()
	fileContextReady := cfg.FileContext != nil && cfg.FileContext.Ready()
	messageReferences := fileContextReady && cfg.FileContext.MessageReferences()
	workspaceBrowser := fileContextReady && cfg.FileContext.WorkspaceBrowser()
	capabilities := map[string]bool{"autoLoad": service != nil, "codexReady": service != nil, "history": service != nil, "send": service != nil, "interrupt": service != nil, "interaction": service != nil, "chillingFileContext": fileContextReady, "chillingMessageReferences": messageReferences}
	capabilities["chillingWorkspaceBrowser"] = workspaceBrowser
	if service != nil {
		capabilities["turnEditing"] = service.EditEnabled()
		if !cfg.DisableExecutionSettings && service.SettingsEnabled() {
			capabilities["executionSettings"] = true
		}
		capabilities["queue"] = service.QueueEnabled()
		capabilities["threadBootstrap"] = service.ThreadBootstrapEnabled()
		capabilities["threadArchive"] = service.ThreadArchiveEnabled()
		capabilities["threadArchiveUndo"] = service.ThreadArchiveEnabled()
		capabilities["threadRename"] = service.ThreadRenameEnabled()
		capabilities["threadPin"] = service.ThreadPinEnabled()
	}
	hello := map[string]any{"type": "hello", "v": 1, "role": "agent", "token": cfg.Token, "deviceId": cfg.DeviceID, "deviceName": cfg.DeviceName, "agentEpoch": agentEpoch, "adapterVersion": "desktop-ipc-0.160.0", "capabilities": capabilities}
	if cfg.TasksEnabled {
		capabilities["taskManagement"] = a.tasks.managementReady()
		capabilities["taskExecution"] = a.tasks.executionReady(ctx)
		capabilities["taskExecutionAuthorized"] = a.tasks.executionAuthorized()
		capabilities["taskTracking"] = a.tasks.executionAuthorized()
		if cfg.TaskBackend != nil && cfg.TaskBackend.TaskStoreID() != "" {
			hello["taskStoreId"] = cfg.TaskBackend.TaskStoreID()
		}
	}
	if err := a.send(ctx, hello); err != nil {
		return err
	}
	_, body, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var ack struct {
		Type string `json:"type"`
	}
	if protocol.Validate(body) != nil || json.Unmarshal(body, &ack) != nil || ack.Type != "hello.ok" {
		return errors.New("invalid Relay acknowledgement")
	}
	if cfg.OnReady != nil {
		if err := cfg.OnReady(); err != nil {
			return err
		}
		if cfg.OnNotReady != nil {
			defer cfg.OnNotReady()
		}
	}
	a.tasks.start(requestCtx)
	defer a.tasks.close()
	leaseInterval := cfg.CacheLeaseInterval
	if leaseInterval <= 0 {
		leaseInterval = 30 * time.Second
	}
	leaseCtx, stopLeases := context.WithCancel(requestCtx)
	defer stopLeases()
	go a.publishCacheLeases(leaseCtx, leaseInterval)
	if service != nil && cfg.CachePrewarmRecent {
		requests.Add(1)
		go func() {
			defer requests.Done()
			prewarmCtx, stop := context.WithTimeout(requestCtx, threadSubscribeLoadTimeout+threadListRequestTimeout)
			defer stop()
			if err := a.prewarmRecent(prewarmCtx); err != nil && prewarmCtx.Err() == nil {
				log.Printf("recent cache prewarm unavailable: %T", err)
			}
		}()
	}
	heartCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatFailure := make(chan error, 1)
	interval, timeout := cfg.HeartbeatInterval, cfg.HeartbeatTimeout
	if interval <= 0 {
		interval = wsheartbeat.DefaultInterval
	}
	if timeout <= 0 {
		timeout = wsheartbeat.DefaultTimeout
	}
	go func() {
		if err := heartbeatRelay(heartCtx, conn, interval, timeout); err != nil {
			heartbeatFailure <- err
			conn.CloseNow()
		}
	}()
	for {
		_, body, err = conn.Read(ctx)
		if err != nil {
			select {
			case heartbeatErr := <-heartbeatFailure:
				return fmt.Errorf("Relay heartbeat failed: %w", heartbeatErr)
			default:
			}
			return err
		}
		if err := protocol.Validate(body); err != nil {
			return err
		}
		var req struct {
			Type      string          `json:"type"`
			RequestID string          `json:"requestId"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &req) != nil || (req.Type != "request" && !strings.HasPrefix(req.Type, "task.")) {
			return errors.New("unexpected Relay message")
		}
		// Cache watch/unwatch transitions arrive in WebSocket order but can be
		// expensive because a transition to watched may reopen a native owner.
		// Keep them ordered on a dedicated worker without blocking mutations and
		// the rest of the Agent request queue behind a cold follower load.
		if req.Type == "request" && req.Method == "thread.cache.touch" {
			select {
			case cacheTouches <- cacheTouchRequest{id: req.RequestID, method: req.Method, params: append(json.RawMessage(nil), req.Params...)}:
			default:
				var touch struct {
					ThreadID string `json:"threadId"`
				}
				if json.Unmarshal(req.Params, &touch) == nil && touch.ThreadID != "" {
					a.sendCacheStatus(requestCtx, touch.ThreadID, "", "error", "OVERLOADED")
				}
				a.reply(requestCtx, req.RequestID, req.Method, nil, errors.New("OVERLOADED"))
			}
			continue
		}
		select {
		case queue <- struct{}{}:
			requests.Add(1)
			go func(requestBody []byte) {
				defer requests.Done()
				defer func() { <-queue }()
				if strings.HasPrefix(req.Type, "task.") {
					a.tasks.handle(requestCtx, requestBody)
				} else {
					a.handle(requestCtx, req.RequestID, req.Method, req.Params)
				}
			}(body)
		default:
			a.reply(requestCtx, req.RequestID, req.Method, nil, errors.New("OVERLOADED"))
		}
	}
}

func (a *agent) releaseCacheSubscriptions() {
	a.cacheMu.Lock()
	ids := make([]string, 0, len(a.cacheSubs))
	for _, sub := range a.cacheSubs {
		if sub.publishTimer != nil {
			sub.publishTimer.Stop()
		}
		if sub.recoveryTimer != nil {
			sub.recoveryTimer.Stop()
		}
		if strings.HasPrefix(sub.subscriptionID, "desktop-sub-") {
			ids = append(ids, sub.subscriptionID)
		}
	}
	a.cacheSubs = map[string]*cacheSubscription{}
	a.cacheMu.Unlock()
	for _, id := range ids {
		a.service.Unsubscribe(id)
	}
}

func (a *agent) prewarmRecent(ctx context.Context) error {
	threads, _, err := a.service.List(ctx, 1, "")
	if err != nil {
		return err
	}
	var threadID string
	for _, thread := range threads {
		id, _ := thread["threadId"].(string)
		pinned, _ := thread["isPinned"].(bool)
		if id != "" && !pinned {
			threadID = id
			break
		}
	}
	if threadID == "" && len(threads) > 0 {
		threadID, _ = threads[0]["threadId"].(string)
	}
	if threadID == "" {
		return nil
	}
	if err := a.touchCache(ctx, threadID, true); err != nil {
		return err
	}
	return a.touchCache(ctx, threadID, false)
}

func heartbeatRelay(ctx context.Context, conn *websocket.Conn, interval, timeout time.Duration) error {
	return wsheartbeat.Run(ctx, interval, timeout, wsheartbeat.DefaultMaxConsecutiveFailures, conn.Ping)
}

func (a *agent) send(ctx context.Context, value any) error {
	return a.sendWithin(ctx, value, wsheartbeat.DefaultWriteTimeout)
}

func (a *agent) sendWithin(ctx context.Context, value any, timeout time.Duration) error {
	// Internal atomic question state must never leak into strict legacy thread
	// objects. Only negotiated cache sidecars carry it on the wire.
	if envelope, ok := value.(map[string]any); ok {
		copy := make(map[string]any, len(envelope))
		for key, v := range envelope {
			copy[key] = v
		}
		if thread, ok := copy["thread"].(map[string]any); ok {
			copy["thread"] = withoutAsyncQuestions(thread)
		}
		if data, ok := copy["data"].(map[string]any); ok {
			next := make(map[string]any, len(data))
			for k, v := range data {
				next[k] = v
			}
			if thread, ok := next["thread"].(map[string]any); ok {
				next["thread"] = withoutAsyncQuestions(thread)
			}
			copy["data"] = next
		}
		value = copy
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := protocol.Validate(body); err != nil {
		return fmt.Errorf("invalid outbound agent message: %w", err)
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return a.conn.Write(writeCtx, websocket.MessageText, body)
}

func (a *agent) handle(ctx context.Context, id, method string, raw json.RawMessage) {
	if method == "turn.edit" || method == "turn.edit.read" {
		a.handleEditing(ctx, id, method, raw)
		return
	}
	if method == "model.catalog.read" || strings.HasPrefix(method, "thread.settings.") {
		a.handleSettings(ctx, id, method, raw)
		return
	}
	if protocol.IsTaskMethod(method) {
		data, err := a.tasks.public(ctx, method, raw)
		a.reply(ctx, id, method, data, err)
		return
	}
	if a.service == nil {
		a.reply(ctx, id, method, nil, errors.New("PROTOCOL_UNSUPPORTED"))
		return
	}
	var p struct {
		ThreadID              string                  `json:"threadId"`
		AgentEpoch            string                  `json:"agentEpoch"`
		TurnID                string                  `json:"turnId"`
		TurnIDs               []string                `json:"turnIds"`
		SubscriptionID        string                  `json:"subscriptionId"`
		Cursor                string                  `json:"cursor"`
		SearchTerm            string                  `json:"searchTerm"`
		Limit                 int                     `json:"limit"`
		LimitBytes            int                     `json:"limitBytes"`
		ExpectedRevision      string                  `json:"expectedRevision"`
		ClientMessageID       string                  `json:"clientMessageId"`
		Text                  string                  `json:"text"`
		Images                []string                `json:"images"`
		ItemID                string                  `json:"itemId"`
		Path                  string                  `json:"path"`
		ReferenceKind         string                  `json:"referenceKind"`
		RawReference          string                  `json:"rawReference"`
		StartLine             int                     `json:"startLine"`
		EndLine               int                     `json:"endLine"`
		ImageIndex            int                     `json:"imageIndex"`
		ExpectedTurnID        string                  `json:"expectedTurnId"`
		QueueID               string                  `json:"queueId"`
		QueueIDs              []string                `json:"queueIds"`
		InteractionID         string                  `json:"interactionId"`
		Decision              string                  `json:"decision"`
		Answers               map[string][]string     `json:"answers"`
		CWD                   string                  `json:"cwd"`
		Archived              bool                    `json:"archived"`
		Name                  string                  `json:"name"`
		Pinned                *bool                   `json:"pinned"`
		OperationID           string                  `json:"operationId"`
		UpdateMode            string                  `json:"updateMode"`
		Watching              *bool                   `json:"watching"`
		AsyncQuestionsVersion int                     `json:"asyncQuestionsVersion"`
		FileContexts          []filecontext.Selection `json:"fileContexts"`
		Settings              *modelsettings.Values   `json:"settings"`
	}
	if json.Unmarshal(raw, &p) != nil {
		a.reply(ctx, id, method, nil, errors.New("INVALID_ARGUMENT"))
		return
	}
	if !fileSelectionsMatchThread(p.ThreadID, p.FileContexts) {
		a.reply(ctx, id, method, nil, errors.New("INVALID_ARGUMENT"))
		return
	}
	var data map[string]any
	var err error
	switch method {
	case "file.context.asset.read":
		provider, ok := a.fileContext.(interface {
			AssetRead(context.Context, filecontext.AssetReadRequest) (filecontext.AssetResponse, error)
		})
		if !ok || p.AgentEpoch != a.agentEpoch {
			err = errors.New("FEATURE_DISABLED")
			break
		}
		var request filecontext.AssetReadRequest
		if json.Unmarshal(raw, &request) != nil {
			err = errors.New("INVALID_ARGUMENT")
			break
		}
		var response filecontext.AssetResponse
		response, err = provider.AssetRead(ctx, request)
		data, err = responseMap(response, err)
	case "thread.create":
		err = appserver.ErrMethodUnavailable
	case "thread.bootstrap":
		var thread map[string]any
		var turnID string
		thread, turnID, err = a.service.BootstrapThreadWithSettings(ctx, p.CWD, p.ClientMessageID, p.Text, p.Images, p.Settings)
		threadID := ""
		if thread != nil {
			threadID, _ = thread["threadId"].(string)
		}
		data = map[string]any{"thread": thread, "threadId": threadID, "turnId": turnID, "clientMessageId": p.ClientMessageID}
	case "thread.archive.update":
		if !p.Archived {
			err = errors.New("INVALID_ARGUMENT")
			break
		}
		var operationID string
		operationID, err = a.service.ArchiveThread(ctx, p.ThreadID)
		data = map[string]any{"operationId": operationID}
	case "thread.archive.undo":
		err = a.service.UndoArchive(ctx, p.ThreadID, p.OperationID)
		data = map[string]any{}
	case "thread.name.update":
		var thread map[string]any
		thread, err = a.service.RenameThread(ctx, p.ThreadID, p.Name)
		data = map[string]any{"thread": thread}
	case "thread.pin.update":
		if p.Pinned == nil {
			err = errors.New("INVALID_ARGUMENT")
			break
		}
		var thread map[string]any
		thread, err = a.service.PinThread(ctx, p.ThreadID, *p.Pinned)
		data = map[string]any{"thread": thread}
	case "thread.list":
		listStarted := time.Now()
		if p.Limit == 0 {
			p.Limit = 50
		}
		listCtx, cancelList := threadListRequestContext(ctx, p.SearchTerm)
		defer cancelList()
		var list []map[string]any
		var cursor string
		if p.SearchTerm != "" {
			list, cursor, err = a.service.Search(listCtx, p.SearchTerm, p.Limit, p.Cursor)
		} else {
			list, cursor, err = a.service.List(listCtx, p.Limit, p.Cursor)
		}
		attrs := []any{"durationMs", millisSince(listStarted), "ok", err == nil, "count", len(list), "search", p.SearchTerm != "", "paged", p.Cursor != ""}
		if err != nil {
			attrs = append(attrs, "error", requestErrorCode("thread.list", err))
		}
		logPhase("thread-list", "", attrs...)
		data = map[string]any{"threads": list, "nextCursor": cursor}
	case "thread.read":
		var thread map[string]any
		thread, err = a.service.Read(ctx, p.ThreadID)
		if err == nil {
			var body []byte
			body, err = json.Marshal(thread)
			if err == nil && len(body) > a.cacheEntryMaxBytes {
				err = errors.New("HISTORY_TOO_LARGE")
				thread = nil
			}
		}
		data = map[string]any{"thread": thread}
	case "thread.history":
		if p.Limit == 0 {
			p.Limit = 10
		}
		data, err = a.service.History(ctx, p.ThreadID, p.Cursor, p.Limit)
	case "thread.history.items":
		if p.Limit == 0 {
			p.Limit = 100
		}
		data, err = a.service.HistoryItems(ctx, p.ThreadID, p.TurnID, p.Cursor, p.Limit)
	case "thread.image":
		var uri string
		uri, err = a.service.Image(ctx, p.ThreadID, p.TurnID, p.ItemID, p.ImageIndex)
		data = map[string]any{"dataUri": uri}
	case "thread.activity.read":
		data, err = a.service.Activity(ctx, p.ThreadID, p.TurnID, p.ItemID, p.Cursor, p.ExpectedRevision, p.LimitBytes)
	case "file.context.changes":
		if a.fileContext == nil || !a.fileContext.Ready() {
			err = errors.New("FEATURE_DISABLED")
			break
		}
		var response filecontext.ChangesResponse
		response, err = a.fileContext.Changes(ctx, filecontext.ChangesRequest{ThreadID: p.ThreadID, TurnIDs: p.TurnIDs})
		data, err = responseMap(response, err)
	case "file.context.read":
		if a.fileContext == nil || !a.fileContext.Ready() {
			err = errors.New("FEATURE_DISABLED")
			break
		}
		var response filecontext.ReadResponse
		response, err = a.fileContext.Read(ctx, filecontext.ReadRequest{ThreadID: p.ThreadID, TurnID: p.TurnID, ItemID: p.ItemID, Path: p.Path})
		data, err = responseMap(response, err)
	case "file.context.reference.read":
		if a.fileContext == nil || !a.fileContext.Ready() || !a.fileContext.MessageReferences() {
			err = errors.New("FEATURE_DISABLED")
			break
		}
		var response filecontext.ReadResponse
		response, err = a.fileContext.ReferenceRead(ctx, filecontext.ReferenceReadRequest{ThreadID: p.ThreadID, TurnID: p.TurnID, ItemID: p.ItemID, ReferenceKind: filecontext.ReferenceKind(p.ReferenceKind), RawReference: p.RawReference, StartLine: p.StartLine, EndLine: p.EndLine})
		data, err = responseMap(response, err)
	case "workspace.files.list":
		if a.fileContext == nil || !a.fileContext.Ready() || !a.fileContext.WorkspaceBrowser() {
			err = errors.New("FEATURE_DISABLED")
			break
		}
		var response filecontext.WorkspaceListResponse
		response, err = a.fileContext.WorkspaceList(ctx, filecontext.WorkspaceListRequest{ThreadID: p.ThreadID, Path: p.Path, Cursor: p.Cursor, Limit: p.Limit})
		data, err = responseMap(response, err)
	case "workspace.file.read":
		if a.fileContext == nil || !a.fileContext.Ready() || !a.fileContext.WorkspaceBrowser() {
			err = errors.New("FEATURE_DISABLED")
			break
		}
		var response filecontext.ReadResponse
		response, err = a.fileContext.WorkspaceRead(ctx, filecontext.WorkspaceReadRequest{ThreadID: p.ThreadID, Path: p.Path})
		data, err = responseMap(response, err)
	case "thread.subscribe":
		loadCtx, cancel := context.WithTimeout(ctx, threadSubscribeLoadTimeout)
		defer cancel()
		subscribeStart := time.Now()
		var subID, streamID string
		var thread map[string]any
		var activate func()
		subID, streamID, thread, activate, err = a.service.SubscribeWithMode(loadCtx, p.ThreadID, p.UpdateMode, func(event map[string]any) {
			event["type"] = "event"
			event["v"] = 1
			event["deviceId"] = a.deviceID
			_ = a.send(ctx, event)
		})
		if err == nil {
			data = map[string]any{"subscriptionId": subID, "streamId": streamID}
			a.reply(ctx, id, method, data, nil)
			snapshot := map[string]any{"type": "event", "v": 1, "event": "thread.snapshot", "deviceId": a.deviceID, "threadId": p.ThreadID, "subscriptionId": subID, "streamId": streamID, "seq": 1, "thread": thread}
			sendErr := a.send(ctx, snapshot)
			if body, marshalErr := json.Marshal(snapshot); marshalErr == nil {
				// Record after the send so the phase reflects delivery, not
				// just successful marshaling.
				attrs := []any{"request", id, "snapshotBytes", len(body), "durationMs", time.Since(subscribeStart).Milliseconds(), "ok", sendErr == nil}
				if sendErr != nil {
					attrs = append(attrs, "error", requestErrorCode(method, sendErr))
				}
				logPhase("first-snapshot-send", p.ThreadID, attrs...)
			}
			activate()
			return
		}
		logPhase("subscribe-failed", p.ThreadID, "request", id, "durationMs", time.Since(subscribeStart).Milliseconds(), "error", requestErrorCode(method, err))
	case "thread.unsubscribe":
		if !a.service.Unsubscribe(p.SubscriptionID) {
			err = errors.New("INVALID_ARGUMENT")
		} else {
			data = map[string]any{}
		}
	case "thread.cache.touch":
		if p.AsyncQuestionsVersion == 1 {
			a.enableAsyncQuestions()
		}
		watching := true
		if p.Watching != nil {
			watching = *p.Watching
		}
		err = a.touchCache(ctx, p.ThreadID, watching)
		data = map[string]any{"asyncQuestionsVersion": 1, "asyncQuestionWrite": false}
	case "turn.start":
		var turnID string
		var text string
		text, err = a.prepareMessage(ctx, p.Text, p.FileContexts)
		if err == nil {
			turnID, err = a.service.StartWithImages(ctx, p.ThreadID, p.ClientMessageID, text, p.Images)
		}
		data = map[string]any{"turnId": turnID}
	case "turn.interrupt":
		err = a.service.Interrupt(ctx, p.ThreadID, p.ExpectedTurnID)
		data = map[string]any{}
	case "queue.add":
		var queued []map[string]any
		var text string
		text, err = a.prepareMessage(ctx, p.Text, p.FileContexts)
		if err == nil {
			queued, err = a.service.QueueAdd(ctx, p.ThreadID, p.ClientMessageID, text, p.Images)
		}
		data = map[string]any{"queuedMessages": queued}
	case "queue.update":
		var queued []map[string]any
		queued, err = a.service.QueueUpdate(ctx, p.ThreadID, p.QueueID, p.Text, p.Images)
		data = map[string]any{"queuedMessages": queued}
	case "queue.delete":
		var queued []map[string]any
		queued, err = a.service.QueueDelete(ctx, p.ThreadID, p.QueueID)
		data = map[string]any{"queuedMessages": queued}
	case "queue.reorder":
		var queued []map[string]any
		queued, err = a.service.QueueReorder(ctx, p.ThreadID, p.QueueIDs)
		data = map[string]any{"queuedMessages": queued}
	case "queue.steer":
		var queued []map[string]any
		queued, err = a.service.QueueSteer(ctx, p.ThreadID, p.QueueID, p.ExpectedTurnID)
		data = map[string]any{"queuedMessages": queued}
	case "interaction.respond":
		err = a.service.Respond(ctx, p.ThreadID, p.InteractionID, p.Decision, p.Answers)
		data = map[string]any{}
	case "thread.questions.respond":
		// Native async writes remain closed until this Desktop profile passes
		// the dedicated async fixture, not the synchronous user-input probe.
		err = errors.New("PROTOCOL_UNSUPPORTED")
	default:
		err = errors.New("INVALID_ARGUMENT")
	}
	if data != nil {
		if questions, ok := data["_asyncQuestions"]; ok {
			if p.AsyncQuestionsVersion == 1 {
				data["asyncQuestions"] = questions
			}
			delete(data, "_asyncQuestions")
		}
	}
	a.reply(ctx, id, method, data, err)
}

func (a *agent) touchCache(ctx context.Context, threadID string, watching bool) error {
	if threadID == "" {
		return errors.New("INVALID_ARGUMENT")
	}
	a.cacheMu.Lock()
	if existing := a.cacheSubs[threadID]; existing != nil {
		existing.lastTouched = time.Now()
		if watching {
			// A follower that is still present is the live owner binding. Rewatching
			// only promotes its recency; freshness is fenced independently by the
			// Relay lease and by mutation-time owner validation.
			existing.watching = true
			existing.evictableAt = time.Time{}
			retryRecovery := existing.recoveryFailed
			a.cacheMu.Unlock()
			if retryRecovery {
				// This is an explicit promotion, not another periodic guard.
				// The refresh still publishes through the native Updates channel.
				return a.service.RefreshLive(ctx, threadID)
			}
			return nil
		} else {
			existing.watching = false
			existing.evictableAt = time.Now().Add(a.cacheWarmGrace)
			a.cacheMu.Unlock()
			return nil
		}
	}
	if !watching {
		a.cacheMu.Unlock()
		return nil
	}
	placeholder := "cache-" + rand.Text()
	state := &cacheSubscription{subscriptionID: placeholder, streamID: placeholder, watching: true, lastTouched: time.Now()}
	a.cacheSubs[threadID] = state
	a.cacheMu.Unlock()

	loadCtx, cancel := context.WithTimeout(ctx, threadSubscribeLoadTimeout)
	defer cancel()
	streamID := placeholder
	emit := func(event map[string]any) {
		kind, _ := event["event"].(string)
		if kind == "thread.update" {
			thread, _ := event["thread"].(map[string]any)
			seq, _ := event["seq"].(int)
			if thread != nil {
				a.queueCacheSnapshot(threadID, state, int64(seq), thread)
				runtime, _ := thread["runtime"].(string)
				a.observeCacheRecoveryState(threadID, state, runtime)
			}
			return
		}
		if kind == "thread.error" {
			code, _ := event["code"].(string)
			a.invalidateCacheSubscription(threadID, state, code)
		}
	}
	var err error
	select {
	case a.cacheLoad <- struct{}{}:
		defer func() { <-a.cacheLoad }()
	case <-loadCtx.Done():
		err = loadCtx.Err()
	}
	var subID, nativeStreamID string
	var thread map[string]any
	var activate func()
	if err == nil {
		subID, nativeStreamID, thread, activate, err = a.service.SubscribeWithMode(loadCtx, threadID, "", emit)
	}
	if err != nil && requestErrorCode("thread.cache.touch", err) == "OVERLOADED" && a.evictOneCacheFollower(threadID) {
		subID, nativeStreamID, thread, activate, err = a.service.SubscribeWithMode(loadCtx, threadID, "", emit)
	}
	if err != nil {
		a.cacheMu.Lock()
		delete(a.cacheSubs, threadID)
		a.cacheMu.Unlock()
		code := requestErrorCode("thread.cache.touch", err)
		if code == "OVERLOADED" {
			code = "AGENT_CAPACITY"
		}
		_ = a.send(context.Background(), map[string]any{"type": "event", "v": 1, "event": "thread.cache.status", "deviceId": a.deviceID, "threadId": threadID, "agentEpoch": a.agentEpoch, "cacheStreamId": placeholder, "freshness": "error", "reason": code})
		if code == "AGENT_CAPACITY" {
			return errors.New("AGENT_CAPACITY")
		}
		return err
	}
	streamID = nativeStreamID
	a.cacheMu.Lock()
	state.subscriptionID = subID
	state.streamID = streamID
	state.runtime, _ = thread["runtime"].(string)
	state.structure = cacheStructure(thread)
	state.latestSeq = 1
	state.latestThread = thread
	a.cacheMu.Unlock()
	if err := a.publishCacheSnapshot(ctx, threadID, streamID, 1, thread); err != nil {
		a.service.Unsubscribe(subID)
		a.cacheMu.Lock()
		delete(a.cacheSubs, threadID)
		a.cacheMu.Unlock()
		return err
	}
	a.cacheMu.Lock()
	if a.cacheSubs[threadID] == state {
		state.lastPublished = time.Now()
	}
	a.cacheMu.Unlock()
	activate()
	a.armCacheRecoveryGuard(threadID, state)
	return nil
}

func (a *agent) armCacheRecoveryGuard(threadID string, state *cacheSubscription) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if a.cacheSubs[threadID] != state || state.runtime != "inProgress" || state.recoveryUsed {
		return
	}
	a.resetCacheRecoveryTimerLocked(threadID, state)
}

func (a *agent) observeCacheRecoveryState(threadID string, state *cacheSubscription, runtime string) {
	a.cacheMu.Lock()
	if a.cacheSubs[threadID] != state {
		a.cacheMu.Unlock()
		return
	}
	state.runtime = runtime
	if runtime != "inProgress" {
		if state.recoveryTimer != nil {
			state.recoveryTimer.Stop()
			state.recoveryTimer = nil
		}
		a.cacheMu.Unlock()
		return
	}
	if state.recoveryUsed {
		a.cacheMu.Unlock()
		return
	}
	a.resetCacheRecoveryTimerLocked(threadID, state)
	a.cacheMu.Unlock()
}

func (a *agent) resetCacheRecoveryTimerLocked(threadID string, state *cacheSubscription) {
	if state.recoveryTimer != nil {
		state.recoveryTimer.Stop()
	}
	state.recoveryTimer = time.AfterFunc(a.cacheRecoveryGuardDelay, func() {
		a.runCacheRecoveryGuard(threadID, state)
	})
}

func (a *agent) runCacheRecoveryGuard(threadID string, state *cacheSubscription) {
	a.cacheMu.Lock()
	if a.cacheSubs[threadID] != state || state.recoveryUsed || state.runtime != "inProgress" {
		a.cacheMu.Unlock()
		return
	}
	state.recoveryUsed = true
	state.recoveryTimer = nil
	streamID := state.streamID
	seq := state.latestSeq
	a.cacheMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), threadSubscribeLoadTimeout)
	err := a.service.RefreshLive(ctx, threadID)
	cancel()
	logPhase("cache-recovery-guard", threadID, "ok", err == nil, "error", requestErrorCode("thread.cache.touch", err))
	if err == nil {
		return
	}
	a.cachePublishMu.Lock()
	defer a.cachePublishMu.Unlock()
	a.cacheMu.Lock()
	current := a.cacheSubs[threadID] == state && state.latestSeq == seq && state.runtime == "inProgress"
	if current {
		state.recoveryFailed = true
	}
	a.cacheMu.Unlock()
	if !current {
		return
	}

	// A bounded refresh is only a recovery aid. Its failure does not prove that
	// the existing follower is dead: native patches may still arrive later (in
	// particular the terminal patch that the guard was trying to recover).
	// Keep the subscription alive so that such an update can replace the stale
	// Relay cache without requiring the browser to reload and cold-attach again.
	a.sendCacheStatus(context.Background(), threadID, streamID, "error", "RESYNC_REQUIRED")
}

func (a *agent) invalidateCacheSubscription(threadID string, state *cacheSubscription, code string) {
	// Serialize status publication with snapshots, so a retired follower cannot
	// invalidate the replacement after its first snapshot has already arrived.
	a.cachePublishMu.Lock()
	defer a.cachePublishMu.Unlock()
	a.cacheMu.Lock()
	if a.cacheSubs[threadID] != state {
		a.cacheMu.Unlock()
		return
	}
	delete(a.cacheSubs, threadID)
	if state.publishTimer != nil {
		state.publishTimer.Stop()
	}
	if state.recoveryTimer != nil {
		state.recoveryTimer.Stop()
	}
	a.cacheSendMu.Lock()
	if pending := a.cachePending[threadID]; pending.streamID == state.streamID {
		delete(a.cachePending, threadID)
	}
	a.cacheSendMu.Unlock()
	streamID := state.streamID
	a.cacheMu.Unlock()
	a.sendCacheStatus(context.Background(), threadID, streamID, "error", code)
}

func cacheStructure(thread map[string]any) string {
	var b strings.Builder
	b.WriteString(fmt.Sprint(thread["runtime"]))
	if questions := thread["_asyncQuestions"]; questions != nil {
		raw, _ := json.Marshal(questions)
		fmt.Fprintf(&b, "!%x", sha256.Sum256(raw))
	}
	// Questions and approvals are structural, never ordinary streaming text.
	// Hash only the bounded interaction projection, not full turn bodies.
	if cards := cacheMapSlice(thread["pendingInteractions"]); len(cards) > 0 {
		body, _ := json.Marshal(cards)
		fmt.Fprintf(&b, "?%x", sha256.Sum256(body))
	}
	for _, turn := range cacheMapSlice(thread["turns"]) {
		b.WriteByte('|')
		b.WriteString(fmt.Sprint(turn["turnId"]))
		b.WriteByte(':')
		b.WriteString(fmt.Sprint(turn["status"]))
		for _, item := range cacheMapSlice(turn["items"]) {
			b.WriteByte('/')
			b.WriteString(fmt.Sprint(item["itemId"]))
			b.WriteByte(':')
			b.WriteString(fmt.Sprint(item["role"]))
		}
	}
	return b.String()
}

func cacheMapSlice(value any) []map[string]any {
	switch values := value.(type) {
	case []map[string]any:
		return values
	case []any:
		result := make([]map[string]any, 0, len(values))
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				result = append(result, item)
			}
		}
		return result
	default:
		return nil
	}
}

func (a *agent) queueCacheSnapshot(threadID string, state *cacheSubscription, seq int64, thread map[string]any) {
	now := time.Now()
	runtime, _ := thread["runtime"].(string)
	structure := cacheStructure(thread)
	a.cacheMu.Lock()
	if a.cacheSubs[threadID] != state || seq <= state.latestSeq {
		a.cacheMu.Unlock()
		return
	}
	state.latestSeq = seq
	state.latestThread = thread
	state.recoveryFailed = false
	immediate := runtime != state.runtime || structure != state.structure || state.lastPublished.IsZero() || now.Sub(state.lastPublished) >= a.cachePublishInterval
	state.runtime = runtime
	state.structure = structure
	if immediate {
		if state.publishTimer != nil {
			state.publishTimer.Stop()
			state.publishTimer = nil
		}
		state.pendingThread = nil
		state.pendingSeq = 0
		state.lastPublished = now
		streamID := state.streamID
		a.enqueueCacheSnapshot(threadID, streamID, seq, thread)
		a.cacheMu.Unlock()
		return
	}
	state.pendingSeq = seq
	state.pendingThread = thread
	if state.publishTimer == nil {
		delay := a.cachePublishInterval - now.Sub(state.lastPublished)
		state.publishTimer = time.AfterFunc(delay, func() { a.flushCacheSnapshot(threadID, state) })
	}
	a.cacheMu.Unlock()
}

func (a *agent) flushCacheSnapshot(threadID string, state *cacheSubscription) {
	a.cacheMu.Lock()
	if a.cacheSubs[threadID] != state || state.pendingThread == nil {
		a.cacheMu.Unlock()
		return
	}
	thread, seq, streamID := state.pendingThread, state.pendingSeq, state.streamID
	state.pendingThread = nil
	state.pendingSeq = 0
	state.publishTimer = nil
	state.lastPublished = time.Now()
	a.enqueueCacheSnapshot(threadID, streamID, seq, thread)
	a.cacheMu.Unlock()
}

func (a *agent) enqueueCacheSnapshot(threadID, streamID string, seq int64, thread map[string]any) {
	a.cacheSendMu.Lock()
	if prior, ok := a.cachePending[threadID]; ok && prior.streamID == streamID && prior.seq >= seq {
		a.cacheSendMu.Unlock()
		return
	}
	a.cachePending[threadID] = cacheOutbound{streamID: streamID, seq: seq, thread: thread}
	a.cacheSendMu.Unlock()
	select {
	case a.cacheWake <- struct{}{}:
	default:
	}
}

func (a *agent) runCacheSender(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.cacheWake:
		}
		for {
			a.cacheSendMu.Lock()
			var threadID string
			var next cacheOutbound
			for id, candidate := range a.cachePending {
				threadID, next = id, candidate
				delete(a.cachePending, id)
				break
			}
			a.cacheSendMu.Unlock()
			if threadID == "" {
				break
			}
			if err := a.publishCacheSnapshot(ctx, threadID, next.streamID, next.seq, next.thread); err != nil && ctx.Err() == nil {
				logPhase("cache-snapshot-send", threadID, "seq", next.seq, "error", requestErrorCode("thread.cache.touch", err))
			}
		}
	}
}

func (a *agent) evictOneCacheFollower(exclude string) bool {
	now := time.Now()
	a.cacheMu.Lock()
	var victimID string
	var victim *cacheSubscription
	for threadID, candidate := range a.cacheSubs {
		if threadID == exclude || candidate.watching || candidate.evictableAt.IsZero() || now.Before(candidate.evictableAt) || candidate.runtime == "running" || candidate.runtime == "processing" || candidate.runtime == "inProgress" {
			continue
		}
		if victim == nil || candidate.lastTouched.Before(victim.lastTouched) {
			victimID, victim = threadID, candidate
		}
	}
	if victim == nil {
		a.cacheMu.Unlock()
		return false
	}
	delete(a.cacheSubs, victimID)
	if victim.publishTimer != nil {
		victim.publishTimer.Stop()
	}
	if victim.recoveryTimer != nil {
		victim.recoveryTimer.Stop()
	}
	subscriptionID := victim.subscriptionID
	a.cacheMu.Unlock()
	return subscriptionID != "" && a.service.UnsubscribeAndRetire(subscriptionID)
}

func (a *agent) publishCacheSnapshot(ctx context.Context, threadID, streamID string, seq int64, thread map[string]any) error {
	a.cachePublishMu.Lock()
	defer a.cachePublishMu.Unlock()
	a.cacheMu.Lock()
	state := a.cacheSubs[threadID]
	questionsEnabled := a.questionsEnabled
	wireSeq := seq
	if questionsEnabled {
		wireSeq++
	}
	current := state != nil && state.streamID == streamID && wireSeq > state.lastSentSeq
	a.cacheMu.Unlock()
	if !current {
		return nil
	}
	wireThread := withoutAsyncQuestions(thread)
	body, err := json.Marshal(wireThread)
	if err != nil {
		return err
	}
	if len(body) > a.cacheEntryMaxBytes {
		statusErr := a.send(ctx, map[string]any{
			"type": "event", "v": 1, "event": "thread.cache.status", "deviceId": a.deviceID, "threadId": threadID,
			"agentEpoch": a.agentEpoch, "cacheStreamId": streamID, "freshness": "error", "reason": errCacheSnapshotOversized.Error(),
		})
		if statusErr != nil {
			return errors.Join(errCacheSnapshotOversized, statusErr)
		}
		return errCacheSnapshotOversized
	}
	message := map[string]any{
		"type": "event", "v": 1, "event": "thread.cache.snapshot", "deviceId": a.deviceID, "threadId": threadID,
		"agentEpoch": a.agentEpoch, "cacheStreamId": streamID, "seq": wireSeq, "capturedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"payloadBytes": len(body), "thread": wireThread,
	}
	if questionsEnabled {
		if snapshot := thread["_asyncQuestions"]; snapshot != nil {
			message["asyncQuestions"] = snapshot
		}
	}
	err = a.sendWithin(ctx, message, cacheSnapshotWriteTimeout)
	if err == nil {
		runtime, _ := thread["runtime"].(string)
		a.cacheMu.Lock()
		runtimeChanged := state.lastSentRuntime != runtime
		state.lastSentSeq = wireSeq
		state.lastSentRuntime = runtime
		a.cacheMu.Unlock()
		if runtimeChanged {
			logPhase("cache-snapshot-published", threadID, "seq", seq, "runtime", runtime, "bytes", len(body))
		}
	}
	return err
}

func (a *agent) sendCacheStatus(ctx context.Context, threadID, streamID, freshness, reason string) {
	if streamID == "" {
		streamID = "cache-status-" + rand.Text()
	}
	_ = a.send(ctx, map[string]any{
		"type": "event", "v": 1, "event": "thread.cache.status", "deviceId": a.deviceID, "threadId": threadID,
		"agentEpoch": a.agentEpoch, "cacheStreamId": streamID, "freshness": freshness, "reason": reason,
	})
}

func (a *agent) publishCacheLeases(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.cacheMu.Lock()
			type lease struct{ threadID, streamID string }
			leases := make([]lease, 0, len(a.cacheSubs))
			for threadID, sub := range a.cacheSubs {
				if sub.subscriptionID != "" && sub.streamID != "" {
					leases = append(leases, lease{threadID: threadID, streamID: sub.streamID})
				}
			}
			a.cacheMu.Unlock()
			for _, current := range leases {
				a.sendCacheStatus(ctx, current.threadID, current.streamID, "fresh", "")
			}
		}
	}
}

func responseMap(value any, prior error) (map[string]any, error) {
	if prior != nil {
		return nil, prior
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	switch value.(type) {
	case filecontext.ChangesResponse, filecontext.ReadResponse, filecontext.WorkspaceListResponse, filecontext.PrepareResponse:
		if len(body) > filecontext.MaxResponseBytes {
			return nil, errors.New("FILE_TOO_LARGE")
		}
	case filecontext.AssetResponse:
		if len(body) > (384<<10)-1024 {
			return nil, errors.New("FILE_TOO_LARGE")
		}
	}
	var out map[string]any
	if json.Unmarshal(body, &out) != nil {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	return out, nil
}

func (a *agent) prepareMessage(ctx context.Context, text string, selections []filecontext.Selection) (string, error) {
	if len(selections) == 0 {
		return text, nil
	}
	if a.fileContext == nil || !a.fileContext.Ready() {
		return "", errors.New("FEATURE_DISABLED")
	}
	prepared, err := a.fileContext.Prepare(ctx, filecontext.PrepareRequest{Selections: selections})
	if err != nil {
		return "", err
	}
	if prepared.Envelope == "" {
		return "", errors.New("INVALID_ARGUMENT")
	}
	if text == "" {
		return prepared.Envelope, nil
	}
	return text + "\n\n" + prepared.Envelope, nil
}

func fileSelectionsMatchThread(threadID string, selections []filecontext.Selection) bool {
	for _, selection := range selections {
		if threadID == "" || selection.ThreadID != threadID {
			return false
		}
	}
	return true
}

func (a *agent) reply(ctx context.Context, id, method string, data map[string]any, err error) {
	msg := map[string]any{"type": "response", "v": 1, "requestId": id}
	if err == nil {
		msg["outcome"] = "accepted"
		msg["data"] = data
	} else {
		outcome, code := "rejected", requestErrorCode(method, err)
		if code == "OUTCOME_UNKNOWN" {
			outcome = "unknown"
		}
		if errors.Is(err, appserver.ErrOutcomeUnknown) {
			outcome = "unknown"
			code = "OUTCOME_UNKNOWN"
		}
		var callErr *desktopipc.CallError
		if errors.As(err, &callErr) {
			outcome = callErr.Outcome
			if outcome == "unknown" {
				code = "OUTCOME_UNKNOWN"
			} else if outcome == "not_submitted" {
				code = "DEVICE_OFFLINE"
			} else {
				code = "INVALID_ARGUMENT"
			}
		}
		msg["outcome"] = outcome
		msg["error"] = map[string]string{"code": code, "message": code}
	}
	_ = a.send(ctx, msg)
}

func requestErrorCode(method string, err error) string {
	if err == nil {
		return ""
	}
	if protocol.IsTaskMethod(method) {
		switch code := chillingclient.CodeOf(err); code {
		case "TASK_FEEDBACK_IDENTITY_MISMATCH", "TASK_IDEMPOTENCY_CONFLICT", "TASK_INVALID_ARGUMENT", "TASK_INVALID_STATE", "TASK_METHOD_NOT_ALLOWED", "TASK_NATIVE_IDENTITY_REQUIRED", "TASK_NOT_FOUND", "TASK_NOT_READY", "TASK_RECONCILIATION_REQUIRED", "TASK_RESOURCE_LIMIT", "TASK_STALE_BINDING", "TASK_STALE_LEASE", "TASK_STORE_NOT_FOUND", "TASK_UNAUTHORIZED", "TASK_VERSION_CONFLICT":
			return code
		}
	}
	if code := chillingclient.CodeOf(err); code != "INTERNAL" {
		switch code {
		case "FEATURE_DISABLED", "CHILLING_UNAVAILABLE", "CONTRACT_UNSUPPORTED", "FILE_NOT_FOUND", "PATH_NOT_ALLOWED", "REFERENCE_NOT_FOUND", "DIRECTORY_CHANGED", "UNSUPPORTED_FILE_TYPE", "FILE_TOO_LARGE", "FILE_CHANGED", "DETAIL_PENDING", "DETAIL_CHANGED", "DETAIL_TOO_LARGE", "INVALID_ARGUMENT", "OVERLOADED":
			return code
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		if isReadRequest(method) {
			return "TIMEOUT"
		}
		return "OUTCOME_UNKNOWN"
	case errors.Is(err, desktopipc.ErrFrameTooLarge), errors.Is(err, appserver.ErrResponseTooLarge):
		return "HISTORY_TOO_LARGE"
	case errors.Is(err, desktopipc.ErrOverloaded):
		return "OVERLOADED"
	case errors.Is(err, ErrNativeShape), errors.Is(err, appserver.ErrMethodUnavailable), errors.Is(err, desktopipc.ErrProtocol):
		return "PROTOCOL_UNSUPPORTED"
	}
	switch code := err.Error(); code {
	case "INVALID_ARGUMENT", "NOT_FOUND", "TURN_BUSY", "STALE_TURN", "STALE_INTERACTION", "INTERACTION_UNSUPPORTED", "OUTCOME_UNKNOWN", "DEVICE_OFFLINE", "RESYNC_REQUIRED", "NATIVE_STATE_UNCERTAIN", "HISTORY_TOO_LARGE", "OVERLOADED", "PROTOCOL_UNSUPPORTED", "TIMEOUT", "CACHE_OVERSIZED", "FEATURE_DISABLED", "CHILLING_UNAVAILABLE", "CONTRACT_UNSUPPORTED", "FILE_NOT_FOUND", "PATH_NOT_ALLOWED", "REFERENCE_NOT_FOUND", "DIRECTORY_CHANGED", "UNSUPPORTED_FILE_TYPE", "FILE_TOO_LARGE", "FILE_CHANGED", "DETAIL_PENDING", "DETAIL_CHANGED", "DETAIL_TOO_LARGE":
		return code
	default:
		return "RESYNC_REQUIRED"
	}
}

func isReadRequest(method string) bool {
	if method == "turn.edit.read" {
		return true
	}
	switch method {
	case "model.catalog.read", "thread.settings.read", "thread.settings.watch", "thread.settings.unwatch":
		return true
	case "thread.list", "thread.read", "thread.history", "thread.history.items", "thread.image", "thread.activity.read", "thread.subscribe", "file.context.asset.read", "file.context.changes", "file.context.read", "file.context.reference.read", "workspace.files.list", "workspace.file.read":
		return true
	default:
		return false
	}
}
