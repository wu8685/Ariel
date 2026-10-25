package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type History interface {
	List(context.Context, string, int) (appserver.Page, error)
	Read(context.Context, string) (appserver.Thread, error)
}

type SearchHistory interface {
	Search(context.Context, string, string, int) (appserver.SearchPage, error)
}

type PinnedHistory interface {
	ListPinned(context.Context, int) (appserver.Page, error)
}

type PagedHistory interface {
	Turns(context.Context, string, string, int, string) (appserver.TurnPage, error)
	Items(context.Context, string, string, string, int) (appserver.ItemPage, error)
}

type FollowUpQueue interface {
	List(context.Context, string) ([]appserver.QueuedSubmission, error)
	Add(context.Context, string, string, string, []string) (appserver.QueuedSubmission, error)
	Update(context.Context, string, string, string, []string) (appserver.QueuedSubmission, error)
	Delete(context.Context, string, string) (bool, error)
	Reorder(context.Context, string, []string) error
}

type ThreadCreator interface {
	Create(context.Context, string) (appserver.Thread, error)
}

type BootstrapResult struct {
	ThreadID, TurnID, Title, CWD string
	Live                         Live
}

type ThreadBootstrapper interface {
	Bootstrap(context.Context, string, string, string, []string) (BootstrapResult, error)
}

type ThreadArchiver interface {
	Archive(context.Context, string) error
	Unarchive(context.Context, string) error
}

type ThreadMetadataWriter interface {
	Rename(context.Context, string, string) (appserver.Thread, error)
	Pin(context.Context, string, bool) (appserver.Thread, error)
}

var ErrHistoryTooLarge = errors.New("HISTORY_TOO_LARGE")
var ErrArchiveStateUnknown = errors.New("NATIVE_STATE_UNCERTAIN")
var ErrArchiveUndoInvalid = errors.New("INVALID_ARGUMENT")
var ErrThreadMetadataTargetUnknown = errors.New("INVALID_ARGUMENT")

const maxThreadPayloadBytes = 7 << 20
const maxHistoryPageBytes = 6 << 20
const maxThreadControllers = 64
const maxSubscriptionsPerThread = 16
const maxRecentMessageIDs = 4096
const maxPinnedThreads = 100
const maxKnownThreadIDs = 1024

// defaultIdleGrace keeps an idle live follower warm after the last Web
// subscription disappears (spec 0086): mobile backgrounding and transient
// WebSocket reconnects then reattach without a full owner cold load. The
// capacity LRU, invalidation and Close still retire followers immediately.
const defaultIdleGrace = 90 * time.Second

func checkThreadSize(thread map[string]any) error {
	return checkPayloadSize(thread, maxThreadPayloadBytes)
}

func checkPayloadSize(thread map[string]any, limit int) error {
	body, err := json.Marshal(thread)
	if err != nil {
		return ErrNativeShape
	}
	if len(body) > limit {
		return ErrHistoryTooLarge
	}
	return nil
}

type Live interface {
	Current() (json.RawMessage, error)
	Start(context.Context, string, string) (string, error)
	Interrupt(context.Context, string) error
	Respond(context.Context, string, string, map[string][]string) error
	Updates() <-chan struct{}
	Done() <-chan struct{}
	Close() error
}
type LiveFactory func(context.Context, string, string) (Live, error)

type subscription struct {
	id, streamID string
	seq          int
	active       bool
	updateMode   string
	baseline     map[string]any
	emit         func(map[string]any)
}
type threadController struct {
	mu             sync.Mutex
	startMu        sync.Mutex
	id, title, cwd string
	bootstrap      bool
	bootstrapHold  bool
	migrating      bool
	live           Live
	current        map[string]any
	subs           map[string]*subscription
	leases         int
	lastUsed       uint64
	retireTimer    *time.Timer
}
type Service struct {
	history          History
	queue            FollowUpQueue
	creator          ThreadCreator
	bootstrapper     ThreadBootstrapper
	archiver         ThreadArchiver
	metadata         ThreadMetadataWriter
	attach           LiveFactory
	mu               sync.Mutex
	archiveMu        sync.Mutex
	queueMu          sync.Mutex
	imageMu          sync.Mutex
	queueCache       map[string]queueCacheEntry
	threads          map[string]*threadController
	clock            uint64
	seen             map[string]struct{}
	seenOrder        []string
	knownThreads     map[string]struct{}
	knownThreadOrder []string
	threadMutations  [64]sync.Mutex
	metadataSlots    chan struct{}
	bootstrapSlots   chan struct{}
	closed           bool
	placeholderGrace time.Duration
	idleGrace        time.Duration
	archiveUndo      []archiveUndoRecord
}

type archiveUndoRecord struct {
	operationID string
	threadID    string
	expiresAt   time.Time
}

type queueCacheEntry struct {
	items     []map[string]any
	checkedAt time.Time
}

func NewService(history History, attach LiveFactory, queue ...FollowUpQueue) *Service {
	var followUps FollowUpQueue
	if len(queue) > 0 {
		followUps = queue[0]
	}
	return &Service{history: history, queue: followUps, attach: attach, queueCache: map[string]queueCacheEntry{}, threads: map[string]*threadController{}, seen: map[string]struct{}{}, knownThreads: map[string]struct{}{}, metadataSlots: make(chan struct{}, 2), bootstrapSlots: make(chan struct{}, 2), placeholderGrace: 8 * time.Second, idleGrace: defaultIdleGrace}
}

func (s *Service) QueueEnabled() bool { return s.queue != nil }

func (s *Service) SetThreadCreator(creator ThreadCreator) { s.creator = creator }
func (s *Service) ThreadCreationEnabled() bool            { return s.creator != nil }
func (s *Service) SetThreadBootstrapper(bootstrapper ThreadBootstrapper) {
	s.bootstrapper = bootstrapper
}
func (s *Service) ThreadBootstrapEnabled() bool                        { return s.bootstrapper != nil }
func (s *Service) SetThreadArchiver(archiver ThreadArchiver)           { s.archiver = archiver }
func (s *Service) ThreadArchiveEnabled() bool                          { return s.archiver != nil }
func (s *Service) SetThreadMetadataWriter(writer ThreadMetadataWriter) { s.metadata = writer }
func (s *Service) ThreadRenameEnabled() bool                           { return s.metadata != nil }
func (s *Service) ThreadPinEnabled() bool                              { return s.metadata != nil }

func (s *Service) lockThreadMutation(threadID string) func() {
	var bucket uint32
	for index := 0; index < len(threadID); index++ {
		bucket = bucket*33 + uint32(threadID[index])
	}
	lock := &s.threadMutations[int(bucket)%len(s.threadMutations)]
	lock.Lock()
	return lock.Unlock
}

func (s *Service) enterMetadataMutation(ctx context.Context, threadID string) (func(), error) {
	unlock := s.lockThreadMutation(threadID)
	select {
	case s.metadataSlots <- struct{}{}:
		return func() { <-s.metadataSlots; unlock() }, nil
	case <-ctx.Done():
		unlock()
		return nil, ctx.Err()
	}
}

func (s *Service) rememberThread(threadID string) {
	if !threadIDPattern.MatchString(threadID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.knownThreads[threadID]; exists {
		return
	}
	s.knownThreads[threadID] = struct{}{}
	s.knownThreadOrder = append(s.knownThreadOrder, threadID)
	if len(s.knownThreadOrder) > maxKnownThreadIDs {
		oldest := s.knownThreadOrder[0]
		s.knownThreadOrder = s.knownThreadOrder[1:]
		delete(s.knownThreads, oldest)
	}
}

func (s *Service) knownThread(threadID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.knownThreads[threadID]
	return exists
}

func (s *Service) RenameThread(ctx context.Context, threadID, name string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if s.metadata == nil || !threadIDPattern.MatchString(threadID) || !validThreadName(name) {
		return nil, appserver.ErrInvalidArgument
	}
	leave, err := s.enterMetadataMutation(ctx, threadID)
	if err != nil {
		return nil, err
	}
	defer leave()
	if !s.knownThread(threadID) {
		return nil, ErrThreadMetadataTargetUnknown
	}
	stored, err := s.metadata.Rename(ctx, threadID, name)
	if err != nil {
		return nil, err
	}
	thread, err := normalizeMetadataThread(threadID, stored)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	controller := s.threads[threadID]
	s.mu.Unlock()
	if controller != nil {
		controller.mu.Lock()
		controller.title = stored.Name
		controller.mu.Unlock()
	}
	return thread, nil
}

func (s *Service) PinThread(ctx context.Context, threadID string, pinned bool) (map[string]any, error) {
	if s.metadata == nil || !threadIDPattern.MatchString(threadID) {
		return nil, appserver.ErrInvalidArgument
	}
	leave, err := s.enterMetadataMutation(ctx, threadID)
	if err != nil {
		return nil, err
	}
	defer leave()
	if !s.knownThread(threadID) {
		return nil, ErrThreadMetadataTargetUnknown
	}
	stored, err := s.metadata.Pin(ctx, threadID, pinned)
	if err != nil {
		return nil, err
	}
	return normalizeMetadataThread(threadID, stored)
}

func validThreadName(name string) bool {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 || len(name) > 800 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}

func normalizeMetadataThread(threadID string, stored appserver.Thread) (map[string]any, error) {
	if stored.ID != threadID {
		return nil, appserver.ErrOutcomeUnknown
	}
	stored.Turns = nil
	thread, err := NormalizeStored(stored)
	if err != nil {
		return nil, appserver.ErrOutcomeUnknown
	}
	return thread, checkThreadSize(thread)
}

func (s *Service) ArchiveThread(ctx context.Context, threadID string) (string, error) {
	if s.archiver == nil || !threadIDPattern.MatchString(threadID) {
		return "", appserver.ErrMethodUnavailable
	}
	unlock := s.lockThreadMutation(threadID)
	defer unlock()
	s.archiveMu.Lock()
	defer s.archiveMu.Unlock()
	s.mu.Lock()
	controller := s.threads[threadID]
	s.mu.Unlock()
	if controller == nil {
		return "", ErrArchiveStateUnknown
	}
	controller.mu.Lock()
	if controller.live == nil {
		controller.mu.Unlock()
		return "", ErrArchiveStateUnknown
	}
	raw, err := controller.live.Current()
	if err != nil {
		controller.mu.Unlock()
		return "", ErrArchiveStateUnknown
	}
	current, err := NormalizeLive(controller.id, controller.title, controller.cwd, raw)
	controller.mu.Unlock()
	if err != nil || current["runtime"] != "idle" {
		return "", ErrArchiveStateUnknown
	}
	if err := s.archiver.Archive(ctx, threadID); err != nil {
		return "", err
	}
	now := time.Now()
	kept := s.archiveUndo[:0]
	for _, record := range s.archiveUndo {
		if record.expiresAt.After(now) && record.threadID != threadID {
			kept = append(kept, record)
		}
	}
	s.archiveUndo = kept
	if len(s.archiveUndo) >= 16 {
		s.archiveUndo = s.archiveUndo[len(s.archiveUndo)-15:]
	}
	operationID := "archive-" + rand.Text()
	s.archiveUndo = append(s.archiveUndo, archiveUndoRecord{operationID: operationID, threadID: threadID, expiresAt: now.Add(15 * time.Second)})
	return operationID, nil
}

func (s *Service) UndoArchive(ctx context.Context, threadID, operationID string) error {
	if s.archiver == nil || !threadIDPattern.MatchString(threadID) || operationID == "" {
		return ErrArchiveUndoInvalid
	}
	unlock := s.lockThreadMutation(threadID)
	defer unlock()
	s.archiveMu.Lock()
	defer s.archiveMu.Unlock()
	now := time.Now()
	index := -1
	for i, record := range s.archiveUndo {
		if record.threadID == threadID && record.operationID == operationID && record.expiresAt.After(now) {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrArchiveUndoInvalid
	}
	s.archiveUndo = append(s.archiveUndo[:index], s.archiveUndo[index+1:]...)
	return s.archiver.Unarchive(ctx, threadID)
}

func canonicalProjectDirectory(cwd string) (string, error) {
	if cwd == "" || strings.ContainsRune(cwd, 0) || utf8.RuneCountInString(cwd) > 4096 || !filepath.IsAbs(cwd) {
		return "", errors.New("INVALID_ARGUMENT")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return "", errors.New("INVALID_ARGUMENT")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("INVALID_ARGUMENT")
	}
	return resolved, nil
}

func (s *Service) CreateThread(ctx context.Context, cwd string) (map[string]any, error) {
	if s.creator == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	canonical, err := canonicalProjectDirectory(cwd)
	if err != nil {
		return nil, err
	}
	created, err := s.creator.Create(ctx, canonical)
	if err != nil {
		return nil, err
	}
	if !threadIDPattern.MatchString(created.ID) || created.CWD != canonical {
		return nil, appserver.ErrOutcomeUnknown
	}
	s.rememberThread(created.ID)
	created.Turns = nil
	thread, err := NormalizeStored(created)
	if err != nil {
		return nil, appserver.ErrOutcomeUnknown
	}
	thread["historyComplete"] = true
	thread["recentComplete"] = true
	// thread/start may acknowledge before the new thread reaches the
	// persistent history index. Keep only the validated routing metadata in
	// the existing bounded controller cache so the immediate first subscribe
	// can still attach to the original owner.
	if controller, controllerErr := s.controller(created.ID); controllerErr == nil {
		controller.mu.Lock()
		controller.title = created.Name
		controller.cwd = canonical
		controller.bootstrap = true
		controller.mu.Unlock()
		s.releaseController(controller)
	}
	return thread, nil
}

type normalizedLive interface {
	CurrentThread() (map[string]any, error)
}

type terminalBootstrapLive interface {
	Live
	normalizedLive
	Terminal() <-chan struct{}
	Verify(context.Context) error
}

func (s *Service) BootstrapThread(ctx context.Context, cwd, messageID, text string, images []string) (map[string]any, string, error) {
	if s.bootstrapper == nil {
		return nil, "", appserver.ErrMethodUnavailable
	}
	if messageID == "" || (text == "" && len(images) == 0) || validateUploadImages(images) != nil {
		return nil, "", appserver.ErrInvalidArgument
	}
	canonical, err := canonicalProjectDirectory(cwd)
	if err != nil {
		return nil, "", err
	}
	select {
	case s.bootstrapSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	default:
		return nil, "", errors.New("OVERLOADED")
	}
	keepSlot := false
	defer func() {
		if !keepSlot {
			<-s.bootstrapSlots
		}
	}()
	result, err := s.bootstrapper.Bootstrap(ctx, canonical, messageID, text, images)
	if err != nil {
		return nil, "", err
	}
	live, ok := result.Live.(terminalBootstrapLive)
	if !ok || !threadIDPattern.MatchString(result.ThreadID) || result.TurnID == "" || result.CWD != canonical {
		if result.Live != nil {
			_ = result.Live.Close()
		}
		return nil, "", appserver.ErrOutcomeUnknown
	}
	thread, err := live.CurrentThread()
	if err != nil || thread["threadId"] != result.ThreadID || thread["cwd"] != canonical {
		_ = live.Close()
		return nil, "", appserver.ErrOutcomeUnknown
	}
	c, err := s.controller(result.ThreadID)
	if err != nil {
		_ = live.Close()
		return nil, "", err
	}
	c.mu.Lock()
	if c.live != nil {
		c.mu.Unlock()
		s.releaseController(c)
		_ = live.Close()
		return nil, "", appserver.ErrOutcomeUnknown
	}
	c.title, c.cwd, c.bootstrap = result.Title, canonical, true
	c.bootstrapHold = true
	c.live = live
	c.current = nil
	c.mu.Unlock()
	s.rememberThread(result.ThreadID)
	go s.pump(c, live)
	go s.migrateBootstrap(c, live)
	keepSlot = true
	s.releaseController(c)
	return thread, result.TurnID, nil
}

func (s *Service) migrateBootstrap(c *threadController, live terminalBootstrapLive) {
	defer func() { <-s.bootstrapSlots }()
	select {
	case <-live.Terminal():
	case <-live.Done():
		s.invalidateLive(c, live, "RESYNC_REQUIRED")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if live.Verify(ctx) != nil {
		s.invalidateLive(c, live, "NATIVE_STATE_UNCERTAIN")
		return
	}
	c.mu.Lock()
	if c.live != live {
		c.mu.Unlock()
		return
	}
	c.migrating = true
	c.live = nil
	c.current = nil
	c.mu.Unlock()
	_ = live.Close()
	var source appserver.Thread
	var err error
	for ctx.Err() == nil {
		source, err = s.history.Read(ctx, c.id)
		if err == nil && source.ID == c.id && source.CWD == c.cwd {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	if err != nil || source.ID != c.id || source.CWD != c.cwd {
		s.failMigration(c, "NATIVE_STATE_UNCERTAIN")
		return
	}
	desktop, err := s.attach(ctx, c.id, c.cwd)
	if err != nil {
		s.failMigration(c, "RESYNC_REQUIRED")
		return
	}
	c.mu.Lock()
	if c.live != nil {
		c.mu.Unlock()
		_ = desktop.Close()
		return
	}
	c.live = desktop
	c.current = nil
	c.bootstrap = false
	c.bootstrapHold = false
	c.migrating = false
	c.mu.Unlock()
	go s.pump(c, desktop)
}

func (s *Service) failMigration(c *threadController, code string) {
	c.mu.Lock()
	c.current = nil
	c.bootstrapHold = false
	c.migrating = false
	out := make([]*subscription, 0, len(c.subs))
	for id, sub := range c.subs {
		if sub.active {
			out = append(out, sub)
		}
		delete(c.subs, id)
	}
	c.mu.Unlock()
	for _, sub := range out {
		sub.emit(map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": code})
	}
}

func (s *Service) controller(id string) (*threadController, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("DEVICE_OFFLINE")
	}
	c := s.threads[id]
	if c == nil {
		var retired Live
		if len(s.threads) >= maxThreadControllers {
			var victim *threadController
			for _, candidate := range s.threads {
				candidate.mu.Lock()
				idle := candidate.leases == 0 && len(candidate.subs) == 0
				candidate.mu.Unlock()
				if idle && (victim == nil || candidate.lastUsed < victim.lastUsed) {
					victim = candidate
				}
			}
			if victim == nil {
				s.mu.Unlock()
				return nil, errors.New("OVERLOADED")
			}
			victim.mu.Lock()
			retired = victim.live
			victim.live = nil
			victim.current = nil
			stopWarmRetireLocked(victim)
			victim.mu.Unlock()
			delete(s.threads, victim.id)
		}
		c = &threadController{id: id, subs: map[string]*subscription{}}
		s.threads[id] = c
		if retired != nil {
			defer retired.Close()
		}
	}
	s.clock++
	c.lastUsed = s.clock
	c.leases++
	s.mu.Unlock()
	return c, nil
}

func (s *Service) releaseController(c *threadController) {
	s.mu.Lock()
	c.leases--
	c.mu.Lock()
	if c.leases == 0 && len(c.subs) == 0 && !c.bootstrapHold && !c.migrating {
		s.armWarmRetireLocked(c)
	}
	c.mu.Unlock()
	s.mu.Unlock()
}

// armWarmRetireLocked keeps an idle live follower warm for idleGrace instead
// of closing it immediately (spec 0086). The caller must hold c.mu. Capacity
// eviction, invalidation and Close still close followers immediately; only
// the last-subscription-disappeared path goes through the grace.
func (s *Service) armWarmRetireLocked(c *threadController) {
	if c.live == nil || c.retireTimer != nil {
		return
	}
	live := c.live
	c.retireTimer = time.AfterFunc(s.idleGrace, func() { s.retireWarm(c, live) })
}

func (s *Service) retireWarm(c *threadController, live Live) {
	c.mu.Lock()
	c.retireTimer = nil
	if c.live != live || c.leases != 0 || len(c.subs) != 0 || c.bootstrapHold || c.migrating {
		c.mu.Unlock()
		return
	}
	c.live = nil
	c.current = nil
	c.mu.Unlock()
	_ = live.Close()
}

func stopWarmRetireLocked(c *threadController) {
	if c.retireTimer != nil {
		c.retireTimer.Stop()
		c.retireTimer = nil
	}
}

// detachIdleWarmLargeFollower selects at most one LRU victim while preserving
// the single-large-follower process bound. The returned live is closed by the
// caller after all locks are released, which releases the large follower slot.
func (s *Service) detachIdleWarmLargeFollower(target *threadController) (Live, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var victim *threadController
	for _, candidate := range s.threads {
		if candidate == target {
			continue
		}
		candidate.mu.Lock()
		eligible := candidate.live != nil && isLargeFollowerLive(candidate.live) && candidate.leases == 0 && len(candidate.subs) == 0 && !candidate.bootstrapHold && !candidate.migrating && candidate.retireTimer != nil
		candidate.mu.Unlock()
		if eligible && (victim == nil || candidate.lastUsed < victim.lastUsed) {
			victim = candidate
		}
	}
	if victim == nil {
		return nil, ""
	}
	victim.mu.Lock()
	defer victim.mu.Unlock()
	if victim.live == nil || !isLargeFollowerLive(victim.live) || victim.leases != 0 || len(victim.subs) != 0 || victim.bootstrapHold || victim.migrating || victim.retireTimer == nil {
		return nil, ""
	}
	live := victim.live
	victim.live = nil
	victim.current = nil
	stopWarmRetireLocked(victim)
	return live, victim.id
}

func (s *Service) List(ctx context.Context, limit int, cursor string) ([]map[string]any, string, error) {
	page, err := s.history.List(ctx, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	sources := page.Data
	if cursor == "" {
		if reader, ok := s.history.(PinnedHistory); ok {
			pinned, pinnedErr := reader.ListPinned(ctx, maxPinnedThreads)
			switch {
			case pinnedErr == nil:
				if pinned.NextCursor != nil {
					return nil, "", errors.New("OVERLOADED")
				}
				isPinned := true
				for index := range pinned.Data {
					pinned.Data[index].IsPinned = &isPinned
				}
				sources = append(append(make([]appserver.Thread, 0, len(pinned.Data)+len(page.Data)), pinned.Data...), page.Data...)
			case errors.Is(pinnedErr, appserver.ErrMethodUnavailable), errors.Is(pinnedErr, appserver.ErrInvalidArgument):
				// Older App Server builds do not expose persisted pin metadata.
			default:
				return nil, "", pinnedErr
			}
		}
	}
	threads := make([]map[string]any, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if _, duplicate := seen[source.ID]; duplicate {
			continue
		}
		seen[source.ID] = struct{}{}
		// List is an index, not a second history transport. Some App Server
		// versions may include turns in list entries, so omit them explicitly.
		source.Turns = nil
		thread, e := NormalizeStored(source)
		if e != nil {
			return nil, "", e
		}
		if e = checkThreadSize(thread); e != nil {
			return nil, "", e
		}
		threads = append(threads, thread)
		s.rememberThread(source.ID)
	}
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	return threads, next, nil
}

func (s *Service) Search(ctx context.Context, query string, limit int, cursor string) ([]map[string]any, string, error) {
	reader, ok := s.history.(SearchHistory)
	if !ok {
		return nil, "", appserver.ErrMethodUnavailable
	}
	page, err := reader.Search(ctx, query, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	threads := make([]map[string]any, 0, len(page.Data))
	for _, result := range page.Data {
		result.Thread.Turns = nil
		thread, err := NormalizeStored(result.Thread)
		if err != nil {
			return nil, "", err
		}
		snippet := []rune(strings.TrimSpace(result.Snippet))
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		thread["searchSnippet"] = string(snippet)
		if err := checkThreadSize(thread); err != nil {
			return nil, "", err
		}
		threads = append(threads, thread)
		s.rememberThread(result.Thread.ID)
	}
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	return threads, next, nil
}

func (s *Service) Read(ctx context.Context, id string) (map[string]any, error) {
	source, err := s.history.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if source.ID != id {
		return nil, ErrNativeShape
	}
	s.rememberThread(source.ID)
	thread, err := NormalizeStored(source)
	if err != nil {
		return nil, err
	}
	if _, ok := s.history.(PagedHistory); !ok {
		thread["historyComplete"] = true
		thread["recentComplete"] = true
		if err := s.attachQueue(ctx, thread); err != nil {
			return nil, err
		}
		return thread, checkThreadSize(thread)
	}
	for limit := recentTurnLimit; ; limit = max(1, limit/2) {
		page, err := s.History(ctx, id, "", limit)
		if err != nil {
			return nil, err
		}
		descending := page["turns"].([]any)
		recent := make([]any, len(descending))
		for i, turn := range descending {
			recent[len(descending)-1-i] = turn
		}
		historyComplete := page["nextCursor"] == ""
		thread["turns"] = recent
		thread["historyComplete"] = historyComplete
		thread["recentComplete"] = historyComplete || len(recent) >= recentTurnLimit
		if err := s.attachQueue(ctx, thread); err != nil {
			return nil, err
		}
		if err := checkThreadSize(thread); err != nil {
			if errors.Is(err, ErrHistoryTooLarge) && limit > 1 {
				continue
			}
			return nil, err
		}
		return thread, nil
	}
}

// Keep every item in each transmitted turn. If the latest complete turn alone
// cannot fit, obtain its newest native item page instead of truncating text.
func (s *Service) boundLiveThread(ctx context.Context, thread map[string]any) error {
	turns, ok := thread["turns"].([]any)
	if !ok {
		return ErrNativeShape
	}
	for checkThreadSize(thread) == ErrHistoryTooLarge && len(turns) > 1 {
		turns = turns[1:]
		thread["turns"] = turns
		thread["historyComplete"] = false
		thread["recentComplete"] = false
	}
	if checkThreadSize(thread) != ErrHistoryTooLarge {
		return checkThreadSize(thread)
	}
	if len(turns) != 1 {
		return ErrHistoryTooLarge
	}
	last := turns[0].(map[string]any)
	if last["status"] == "inProgress" {
		return ErrHistoryTooLarge
	}
	page, err := s.History(ctx, thread["threadId"].(string), "", 1)
	if err != nil {
		return ErrHistoryTooLarge
	}
	pagedTurns := page["turns"].([]any)
	if len(pagedTurns) != 1 || pagedTurns[0].(map[string]any)["turnId"] != last["turnId"] {
		return ErrHistoryTooLarge
	}
	partial := pagedTurns[0].(map[string]any)
	if partial["itemsComplete"] != false {
		return ErrHistoryTooLarge
	}
	thread["turns"] = []any{partial}
	thread["recentComplete"] = false
	thread["historyComplete"] = false
	return checkThreadSize(thread)
}

func (s *Service) History(ctx context.Context, id, cursor string, limit int) (map[string]any, error) {
	if id == "" || limit < 1 || limit > historyTurnMaxLimit {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	paged, ok := s.history.(PagedHistory)
	if !ok {
		return nil, errors.New("PROTOCOL_UNSUPPORTED")
	}
	source, err := s.history.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if source.ID != id || source.CWD == "" {
		return nil, ErrNativeShape
	}
	for {
		page, err := paged.Turns(ctx, id, cursor, limit, appserver.TurnItemsFull)
		if err != nil {
			if errors.Is(err, appserver.ErrResponseTooLarge) {
				if limit > 1 {
					limit = max(1, limit/2)
					continue
				}
				return s.giantHistory(ctx, paged, id, cursor)
			}
			return nil, err
		}
		if len(page.Data) > limit || (len(page.Data) == 0 && page.NextCursor != nil) || (page.NextCursor != nil && *page.NextCursor == cursor) {
			return nil, ErrNativeShape
		}
		turns := make([]any, 0, len(page.Data))
		seen := make(map[string]struct{}, len(page.Data))
		for _, native := range page.Data {
			if _, duplicate := seen[native.ID]; duplicate {
				return nil, ErrNativeShape
			}
			seen[native.ID] = struct{}{}
			turn, err := normalizeTurn(native.ID, native.Status, native.Items)
			if err != nil {
				return nil, err
			}
			turns = append(turns, turn)
		}
		next := ""
		if page.NextCursor != nil {
			next = *page.NextCursor
		}
		result := map[string]any{"turns": turns, "nextCursor": next}
		if err := checkPayloadSize(result, maxHistoryPageBytes); err != nil {
			if errors.Is(err, ErrHistoryTooLarge) && limit > 1 {
				limit = max(1, limit/2)
				continue
			}
			if errors.Is(err, ErrHistoryTooLarge) {
				return s.giantHistory(ctx, paged, id, cursor)
			}
			return nil, err
		}
		return result, nil
	}
}

func (s *Service) giantHistory(ctx context.Context, paged PagedHistory, id, cursor string) (map[string]any, error) {
	page, err := paged.Turns(ctx, id, cursor, 1, appserver.TurnItemsNotLoaded)
	if err != nil {
		return nil, err
	}
	if len(page.Data) != 1 || page.Data[0].ID == "" || (page.NextCursor != nil && *page.NextCursor == cursor) {
		return nil, ErrNativeShape
	}
	native := page.Data[0]
	items, err := boundedItems(ctx, paged, id, native.ID, "", 100)
	if err != nil {
		return nil, err
	}
	turn, err := normalizeTurn(native.ID, native.Status, items.Data)
	if err != nil {
		return nil, err
	}
	turn["itemsComplete"] = items.NextCursor == nil
	nextItem := ""
	if items.NextCursor != nil {
		nextItem = *items.NextCursor
	}
	turn["nextItemCursor"] = nextItem
	nextTurn := ""
	if page.NextCursor != nil {
		nextTurn = *page.NextCursor
	}
	result := map[string]any{"turns": []any{turn}, "nextCursor": nextTurn}
	return result, checkPayloadSize(result, maxHistoryPageBytes)
}

func boundedItems(ctx context.Context, paged PagedHistory, threadID, turnID, cursor string, limit int) (appserver.ItemPage, error) {
	for {
		page, err := paged.Items(ctx, threadID, turnID, cursor, limit)
		if err != nil {
			if errors.Is(err, appserver.ErrResponseTooLarge) && limit > 1 {
				limit = max(1, limit/2)
				continue
			}
			return appserver.ItemPage{}, err
		}
		if len(page.Data) > limit || (len(page.Data) == 0 && page.NextCursor != nil) || (page.NextCursor != nil && *page.NextCursor == cursor) {
			return appserver.ItemPage{}, ErrNativeShape
		}
		seen := make(map[string]struct{}, len(page.Data))
		for _, raw := range page.Data {
			var identity struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &identity) != nil || identity.ID == "" {
				return appserver.ItemPage{}, ErrNativeShape
			}
			if _, duplicate := seen[identity.ID]; duplicate {
				return appserver.ItemPage{}, ErrNativeShape
			}
			seen[identity.ID] = struct{}{}
		}
		chronological := make([]json.RawMessage, len(page.Data))
		for i, item := range page.Data {
			chronological[len(page.Data)-1-i] = item
		}
		page.Data = chronological
		turn, err := normalizeTurn(turnID, "completed", page.Data)
		if err != nil {
			return appserver.ItemPage{}, err
		}
		if err := checkPayloadSize(map[string]any{"items": turn["items"], "nextItemCursor": page.NextCursor}, maxHistoryPageBytes); err != nil {
			if errors.Is(err, ErrHistoryTooLarge) && limit > 1 {
				limit = max(1, limit/2)
				continue
			}
			return appserver.ItemPage{}, err
		}
		return page, nil
	}
}

func (s *Service) HistoryItems(ctx context.Context, threadID, turnID, cursor string, limit int) (map[string]any, error) {
	if threadID == "" || turnID == "" || limit < 1 || limit > 100 {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	paged, ok := s.history.(PagedHistory)
	if !ok {
		return nil, errors.New("PROTOCOL_UNSUPPORTED")
	}
	source, err := s.history.Read(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if source.ID != threadID || source.CWD == "" {
		return nil, ErrNativeShape
	}
	page, err := boundedItems(ctx, paged, threadID, turnID, cursor, limit)
	if err != nil {
		return nil, err
	}
	turn, err := normalizeTurn(turnID, "completed", page.Data)
	if err != nil {
		return nil, err
	}
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	result := map[string]any{"turnId": turnID, "items": turn["items"], "nextItemCursor": next, "itemsComplete": page.NextCursor == nil}
	return result, checkPayloadSize(result, maxHistoryPageBytes)
}

func (s *Service) ensureLive(ctx context.Context, c *threadController) error {
	if c.live != nil {
		return nil
	}
	if c.migrating {
		return errors.New("RESYNC_REQUIRED")
	}
	title, cwd := c.title, c.cwd
	if !c.bootstrap {
		source, err := s.history.Read(ctx, c.id)
		if err != nil {
			return err
		}
		if source.ID != c.id || source.CWD == "" {
			return ErrNativeShape
		}
		title, cwd = source.Name, source.CWD
	}
	live, err := s.attach(ctx, c.id, cwd)
	if err != nil {
		return err
	}
	c.live = live
	c.current = nil
	c.title = title
	c.cwd = cwd
	go s.pump(c, live)
	return nil
}

func currentLiveThread(c *threadController, live Live) (map[string]any, json.RawMessage, error) {
	if normalized, ok := live.(normalizedLive); ok {
		thread, err := normalized.CurrentThread()
		return thread, nil, err
	}
	raw, err := live.Current()
	if err != nil {
		return nil, raw, err
	}
	thread, err := NormalizeLive(c.id, c.title, c.cwd, raw)
	return thread, raw, err
}

// Mutation requests may be sent without an active Web subscription. They
// still need the same fail-closed native-state gate as thread.subscribe.
func (s *Service) validateLive(c *threadController) error {
	if c.current != nil {
		return nil
	}
	thread, _, err := currentLiveThread(c, c.live)
	if err != nil {
		return desktopipc.ErrNativeStateUncertain
	}
	if err := s.boundLiveThread(context.Background(), thread); err != nil {
		return err
	}
	c.current = thread
	return nil
}

func (s *Service) pump(c *threadController, live Live) {
	var placeholderTimer *time.Timer
	var placeholderDeadline <-chan time.Time
	defer func() {
		if placeholderTimer != nil {
			placeholderTimer.Stop()
		}
	}()
	for {
		select {
		case <-live.Updates():
			thread, raw, err := currentLiveThread(c, live)
			if err != nil {
				if len(raw) > 0 && desktopipc.TransientCanonicalPlaceholder(raw, c.cwd) {
					if placeholderTimer == nil {
						placeholderTimer = time.NewTimer(s.placeholderGrace)
						placeholderDeadline = placeholderTimer.C
					}
					continue
				}
				code := "RESYNC_REQUIRED"
				if len(raw) > 0 {
					code = "NATIVE_STATE_UNCERTAIN"
				}
				s.invalidateLive(c, live, code)
				return
			}
			if placeholderTimer != nil {
				placeholderTimer.Stop()
				placeholderTimer = nil
				placeholderDeadline = nil
			}
			if err = s.attachQueue(context.Background(), thread); err != nil {
				s.invalidateLive(c, live, "RESYNC_REQUIRED")
				return
			}
			boundErr := s.boundLiveThread(context.Background(), thread)
			tooLarge := boundErr != nil
			type delivery struct {
				emit  func(map[string]any)
				event map[string]any
			}
			c.mu.Lock()
			if c.live != live {
				c.mu.Unlock()
				return
			}
			if tooLarge {
				c.current = nil
			} else {
				c.current = thread
			}
			out := make([]delivery, 0, len(c.subs))
			for _, sub := range c.subs {
				if !sub.active {
					continue
				}
				if tooLarge {
					out = append(out, delivery{sub.emit, map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": "HISTORY_TOO_LARGE"}})
					delete(c.subs, sub.id)
					continue
				}
				base := sub.seq
				if sub.updateMode == "item-delta-v1" {
					patch, changed, diffErr := buildThreadPatch(sub.baseline, thread)
					if diffErr != nil {
						out = append(out, delivery{sub.emit, map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": "RESYNC_REQUIRED"}})
						delete(c.subs, sub.id)
						continue
					}
					if !changed {
						continue
					}
					sub.seq++
					sub.baseline = thread
					out = append(out, delivery{sub.emit, map[string]any{"event": "thread.update", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "baseSeq": base, "seq": sub.seq, "patch": patch}})
					continue
				}
				sub.seq++
				out = append(out, delivery{sub.emit, map[string]any{"event": "thread.update", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "baseSeq": base, "seq": sub.seq, "thread": thread}})
			}
			c.mu.Unlock()
			for _, delivery := range out {
				delivery.emit(delivery.event)
			}
		case <-live.Done():
			s.invalidateLive(c, live, "RESYNC_REQUIRED")
			return
		case <-placeholderDeadline:
			s.invalidateLive(c, live, "NATIVE_STATE_UNCERTAIN")
			return
		}
	}
}

func (s *Service) invalidateLive(c *threadController, live Live, code string) {
	type delivery struct {
		emit  func(map[string]any)
		event map[string]any
	}
	c.mu.Lock()
	if c.live != live {
		c.mu.Unlock()
		return
	}
	c.live = nil
	c.current = nil
	stopWarmRetireLocked(c)
	out := make([]delivery, 0, len(c.subs))
	for id, sub := range c.subs {
		if sub.active {
			out = append(out, delivery{sub.emit, map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": code}})
		}
		delete(c.subs, id)
	}
	c.mu.Unlock()
	_ = live.Close()
	for _, delivery := range out {
		delivery.emit(delivery.event)
	}
}

func (s *Service) Subscribe(ctx context.Context, id string, emit func(map[string]any)) (string, string, map[string]any, func(), error) {
	return s.SubscribeWithMode(ctx, id, "", emit)
}

func (s *Service) SubscribeWithMode(ctx context.Context, id, updateMode string, emit func(map[string]any)) (string, string, map[string]any, func(), error) {
	if emit == nil {
		return "", "", nil, nil, errors.New("INVALID_ARGUMENT")
	}
	if updateMode != "" && updateMode != "item-delta-v1" {
		return "", "", nil, nil, errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(id)
	if err != nil {
		return "", "", nil, nil, err
	}
	defer s.releaseController(c)
	c.mu.Lock()
	if len(c.subs) >= maxSubscriptionsPerThread {
		c.mu.Unlock()
		return "", "", nil, nil, errors.New("OVERLOADED")
	}
	attachStart := time.Now()
	attachErr := s.ensureLive(ctx, c)
	if errors.Is(attachErr, desktopipc.ErrOverloaded) {
		// Do not hold the target controller lock while acquiring the Service lock:
		// all controller capacity paths use Service -> controller lock ordering.
		c.mu.Unlock()
		victim, victimID := s.detachIdleWarmLargeFollower(c)
		if victim != nil {
			closeErr := victim.Close()
			logPhase("large-follower-preempt", id, "victim", threadDigest(victimID), "closeOk", closeErr == nil)
			c.mu.Lock()
			retryStart := time.Now()
			attachErr = s.ensureLive(ctx, c)
			retryAttrs := []any{"durationMs", millisSince(retryStart), "ok", attachErr == nil}
			if attachErr != nil {
				retryAttrs = append(retryAttrs, "error", requestErrorCode("thread.subscribe", attachErr))
			}
			logPhase("large-follower-retry", id, retryAttrs...)
		} else {
			c.mu.Lock()
		}
	}
	if attachErr != nil {
		logPhase("owner-attach", id, "durationMs", millisSince(attachStart), "ok", false, "error", requestErrorCode("thread.subscribe", attachErr))
		c.mu.Unlock()
		return "", "", nil, nil, attachErr
	}
	logPhase("owner-attach", id, "durationMs", millisSince(attachStart), "ok", true)
	// A reattach during the warm grace cancels the pending retire timer, so
	// the grace restarts from the LAST unsubscribe (spec 0086), not the first.
	stopWarmRetireLocked(c)
	normalizeStart := time.Now()
	thread, _, err := currentLiveThread(c, c.live)
	if err != nil {
		logPhase("normalize", id, "durationMs", millisSince(normalizeStart), "ok", false, "error", requestErrorCode("thread.subscribe", err))
		c.mu.Unlock()
		return "", "", nil, nil, desktopipc.ErrNativeStateUncertain
	}
	if err := s.boundLiveThread(ctx, thread); err != nil {
		logPhase("normalize", id, "durationMs", millisSince(normalizeStart), "ok", false, "error", requestErrorCode("thread.subscribe", err))
		c.mu.Unlock()
		return "", "", nil, nil, err
	}
	logPhase("normalize", id, "durationMs", millisSince(normalizeStart), "ok", true)
	queueStart := time.Now()
	if err := s.attachQueue(ctx, thread); err != nil {
		c.mu.Unlock()
		return "", "", nil, nil, err
	}
	if s.queue != nil {
		logPhase("queue-attach", id, "durationMs", millisSince(queueStart), "ok", true)
	}
	if err := checkThreadSize(thread); err != nil {
		c.mu.Unlock()
		return "", "", nil, nil, err
	}
	c.current = thread
	logPhase("first-snapshot", id, "durationMs", millisSince(attachStart), "ok", true)
	sub := &subscription{id: "desktop-sub-" + rand.Text(), streamID: "desktop-stream-" + rand.Text(), seq: 1, updateMode: updateMode, baseline: thread, emit: emit}
	c.subs[sub.id] = sub
	c.mu.Unlock()
	activate := func() {
		c.mu.Lock()
		if c.subs[sub.id] == sub {
			live := c.live
			latest, _, currentErr := currentLiveThread(c, live)
			normalizeErr := currentErr
			if normalizeErr == nil {
				normalizeErr = s.attachQueue(context.Background(), latest)
			}
			if currentErr != nil || normalizeErr != nil {
				sub.active = true
				c.mu.Unlock()
				code := "RESYNC_REQUIRED"
				if normalizeErr != nil {
					code = "NATIVE_STATE_UNCERTAIN"
				}
				s.invalidateLive(c, live, code)
				return
			}
			sub.active = true
			if currentErr == nil && normalizeErr == nil && s.boundLiveThread(context.Background(), latest) != nil {
				c.current = nil
				delete(c.subs, sub.id)
				sub.emit(map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": "HISTORY_TOO_LARGE"})
			} else if currentErr == nil && normalizeErr == nil {
				c.current = latest
				if sameThreadContent(latest, thread) {
					c.mu.Unlock()
					return
				}
				sub.seq++
				if sub.updateMode == "item-delta-v1" {
					patch, changed, diffErr := buildThreadPatch(sub.baseline, latest)
					if diffErr != nil {
						delete(c.subs, sub.id)
						sub.emit(map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": "RESYNC_REQUIRED"})
					} else if changed {
						sub.baseline = latest
						sub.emit(map[string]any{"event": "thread.update", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "baseSeq": 1, "seq": sub.seq, "patch": patch})
					} else {
						sub.seq--
					}
				} else {
					sub.emit(map[string]any{"event": "thread.update", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "baseSeq": 1, "seq": sub.seq, "thread": latest})
				}
			}
		}
		c.mu.Unlock()
	}
	return sub.id, sub.streamID, thread, activate, nil
}

func sameThreadContent(a, b map[string]any) bool {
	return a["runtime"] == b["runtime"] && a["historyComplete"] == b["historyComplete"] && a["recentComplete"] == b["recentComplete"] && reflect.DeepEqual(a["turns"], b["turns"]) && reflect.DeepEqual(a["pendingInteractions"], b["pendingInteractions"]) && reflect.DeepEqual(a["permissions"], b["permissions"]) && reflect.DeepEqual(a["queuedMessages"], b["queuedMessages"])
}

func (s *Service) Unsubscribe(id string) bool {
	s.mu.Lock()
	controllers := make([]*threadController, 0, len(s.threads))
	for _, c := range s.threads {
		controllers = append(controllers, c)
	}
	s.mu.Unlock()
	for _, c := range controllers {
		c.mu.Lock()
		if _, ok := c.subs[id]; ok {
			delete(c.subs, id)
			c.mu.Unlock()
			s.retireIdle(c)
			return true
		}
		c.mu.Unlock()
	}
	return false
}

// UnsubscribeAndRetire releases a cache-owned subscription after its explicit
// warm grace has elapsed. Unlike a Web subscription, it must not enter the
// Service's second warm-grace window or the follower would remain resident for
// twice the configured cache lifetime.
func (s *Service) UnsubscribeAndRetire(id string) bool {
	s.mu.Lock()
	controllers := make([]*threadController, 0, len(s.threads))
	for _, c := range s.threads {
		controllers = append(controllers, c)
	}
	s.mu.Unlock()
	for _, c := range controllers {
		c.mu.Lock()
		if _, ok := c.subs[id]; !ok {
			c.mu.Unlock()
			continue
		}
		delete(c.subs, id)
		var live Live
		if c.leases == 0 && len(c.subs) == 0 && !c.bootstrapHold && !c.migrating {
			stopWarmRetireLocked(c)
			live = c.live
			c.live = nil
			c.current = nil
		}
		c.mu.Unlock()
		if live != nil {
			_ = live.Close()
		}
		return true
	}
	return false
}

func (s *Service) retireIdle(c *threadController) {
	s.mu.Lock()
	c.mu.Lock()
	if c.leases == 0 && len(c.subs) == 0 && !c.bootstrapHold && !c.migrating {
		s.armWarmRetireLocked(c)
	}
	c.mu.Unlock()
	s.mu.Unlock()
}

func (s *Service) Start(ctx context.Context, threadID, messageID, text string) (string, error) {
	return s.StartWithImages(ctx, threadID, messageID, text, nil)
}

func (s *Service) StartWithImages(ctx context.Context, threadID, messageID, text string, images []string) (string, error) {
	if (text == "" && len(images) == 0) || validateUploadImages(images) != nil {
		return "", errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(threadID)
	if err != nil {
		return "", err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	key := threadID + "\x00" + messageID
	s.mu.Lock()
	_, seen := s.seen[key]
	s.mu.Unlock()
	if seen {
		return "", errors.New("INVALID_ARGUMENT")
	}
	c.mu.Lock()
	fenceStart := time.Now()
	if err := s.ensureLive(ctx, c); err != nil {
		logPhase("turn-start-fence", threadID, "durationMs", millisSince(fenceStart), "ok", false, "error", requestErrorCode("turn.start", err))
		c.mu.Unlock()
		return "", err
	}
	live := c.live
	if err := s.validateLive(c); err != nil {
		logPhase("turn-start-fence", threadID, "durationMs", millisSince(fenceStart), "ok", false, "error", requestErrorCode("turn.start", err))
		c.mu.Unlock()
		return "", err
	}
	c.mu.Unlock()
	logPhase("turn-start-fence", threadID, "durationMs", millisSince(fenceStart), "ok", true)
	var turnID string
	nativeStart := time.Now()
	if len(images) > 0 {
		imageLive, ok := live.(interface {
			StartWithImages(context.Context, string, string, []string) (string, error)
		})
		if !ok {
			return "", errors.New("PROTOCOL_UNSUPPORTED")
		}
		turnID, err = imageLive.StartWithImages(ctx, messageID, text, images)
	} else {
		turnID, err = live.Start(ctx, messageID, text)
	}
	nativeAttrs := []any{"durationMs", millisSince(nativeStart), "ok", err == nil}
	if err != nil {
		nativeAttrs = append(nativeAttrs, "error", requestErrorCode("turn.start", err))
	}
	logPhase("turn-start-native", threadID, nativeAttrs...)
	if errors.Is(err, desktopipc.ErrTurnBusy) {
		return "", errors.New("TURN_BUSY")
	}
	var callErr *desktopipc.CallError
	if err == nil || (errors.As(err, &callErr) && callErr.Outcome == "unknown") {
		s.mu.Lock()
		s.seen[key] = struct{}{}
		s.seenOrder = append(s.seenOrder, key)
		if len(s.seenOrder) > maxRecentMessageIDs {
			delete(s.seen, s.seenOrder[0])
			s.seenOrder = s.seenOrder[1:]
		}
		s.mu.Unlock()
	}
	return turnID, err
}

func (s *Service) Interrupt(ctx context.Context, threadID, expectedTurnID string) error {
	c, err := s.controller(threadID)
	if err != nil {
		return err
	}
	defer s.releaseController(c)
	c.mu.Lock()
	if err := s.ensureLive(ctx, c); err != nil {
		c.mu.Unlock()
		return err
	}
	live := c.live
	if err := s.validateLive(c); err != nil {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	err = live.Interrupt(ctx, expectedTurnID)
	if errors.Is(err, desktopipc.ErrStaleTurn) {
		return errors.New("STALE_TURN")
	}
	return err
}

func (s *Service) Respond(ctx context.Context, threadID, interactionID, decision string, answers map[string][]string) error {
	c, err := s.controller(threadID)
	if err != nil {
		return err
	}
	defer s.releaseController(c)
	c.mu.Lock()
	if err := s.ensureLive(ctx, c); err != nil {
		c.mu.Unlock()
		return err
	}
	live := c.live
	if err := s.validateLive(c); err != nil {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	err = live.Respond(ctx, interactionID, decision, answers)
	if errors.Is(err, desktopipc.ErrStaleInteraction) {
		return errors.New("STALE_INTERACTION")
	}
	return err
}

func normalizeQueuedSubmission(item appserver.QueuedSubmission) (map[string]any, error) {
	if item.ID == "" || item.ClientUserMessageID == "" || len(item.Input) == 0 {
		return nil, ErrNativeShape
	}
	textParts := make([]string, 0, 1)
	images := make([]string, 0, 3)
	editable := true
	for _, input := range item.Input {
		switch input.Type {
		case "text":
			if input.Text == "" {
				return nil, ErrNativeShape
			}
			textParts = append(textParts, input.Text)
			if len(textParts) > 1 {
				editable = false
			}
		case "image":
			if validateUploadImages([]string{input.URL}) != nil {
				editable = false
				continue
			}
			images = append(images, input.URL)
		default:
			editable = false
		}
	}
	text := strings.Join(textParts, "\n")
	if text == "" && len(images) == 0 {
		text = "包含需在 Codex Desktop 管理的原生附件"
	}
	if len(images) > 3 {
		editable = false
	}
	return map[string]any{"queueId": item.ID, "clientMessageId": item.ClientUserMessageID, "text": text, "images": images, "editable": editable}, nil
}

func (s *Service) queueMessages(ctx context.Context, threadID string) ([]map[string]any, error) {
	if s.queue == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	items, err := s.queue.List(ctx, threadID)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		normalized, err := normalizeQueuedSubmission(item)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	s.queueMu.Lock()
	s.queueCache[threadID] = queueCacheEntry{items: result, checkedAt: time.Now()}
	s.queueMu.Unlock()
	return result, nil
}

func (s *Service) cachedQueueMessages(ctx context.Context, threadID string) ([]map[string]any, error) {
	s.queueMu.Lock()
	cached, ok := s.queueCache[threadID]
	s.queueMu.Unlock()
	if ok && time.Since(cached.checkedAt) < time.Second {
		return cached.items, nil
	}
	return s.queueMessages(ctx, threadID)
}

func (s *Service) attachQueue(ctx context.Context, thread map[string]any) error {
	if s.queue == nil {
		return nil
	}
	threadID, ok := thread["threadId"].(string)
	if !ok || threadID == "" {
		return ErrNativeShape
	}
	items, err := s.cachedQueueMessages(ctx, threadID)
	if err != nil {
		return err
	}
	thread["queuedMessages"] = items
	return nil
}

func validQueueContent(text string, images []string) bool {
	return (text != "" || len(images) > 0) && len([]rune(text)) <= 65536 && validateUploadImages(images) == nil
}

func findQueuedMessage(items []map[string]any, queueID string) (map[string]any, bool) {
	for _, item := range items {
		if item["queueId"] == queueID {
			return item, true
		}
	}
	return nil, false
}

func (s *Service) QueueAdd(ctx context.Context, threadID, clientMessageID, text string, images []string) ([]map[string]any, error) {
	if s.queue == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	if threadID == "" || clientMessageID == "" || !validQueueContent(text, images) {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(threadID)
	if err != nil {
		return nil, err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	before, err := s.queueMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	for _, item := range before {
		if item["clientMessageId"] == clientMessageID {
			return nil, errors.New("INVALID_ARGUMENT")
		}
	}
	accepted, err := s.queue.Add(ctx, threadID, clientMessageID, text, images)
	if err != nil {
		return nil, err
	}
	if accepted.ID == "" || accepted.ClientUserMessageID != clientMessageID {
		return nil, &desktopipc.CallError{Cause: ErrNativeShape, Outcome: "unknown"}
	}
	return s.queueMessages(ctx, threadID)
}

func (s *Service) QueueUpdate(ctx context.Context, threadID, queueID, text string, images []string) ([]map[string]any, error) {
	if s.queue == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	if threadID == "" || queueID == "" || !validQueueContent(text, images) {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(threadID)
	if err != nil {
		return nil, err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	before, err := s.queueMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	item, ok := findQueuedMessage(before, queueID)
	if !ok || item["editable"] != true {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	accepted, err := s.queue.Update(ctx, threadID, queueID, text, images)
	if err != nil {
		return nil, err
	}
	if accepted.ID != queueID || accepted.ClientUserMessageID != item["clientMessageId"] {
		return nil, &desktopipc.CallError{Cause: ErrNativeShape, Outcome: "unknown"}
	}
	return s.queueMessages(ctx, threadID)
}

func (s *Service) QueueDelete(ctx context.Context, threadID, queueID string) ([]map[string]any, error) {
	if s.queue == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	if threadID == "" || queueID == "" {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(threadID)
	if err != nil {
		return nil, err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	before, err := s.queueMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if _, ok := findQueuedMessage(before, queueID); !ok {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	deleted, err := s.queue.Delete(ctx, threadID, queueID)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	return s.queueMessages(ctx, threadID)
}

func (s *Service) QueueReorder(ctx context.Context, threadID string, queueIDs []string) ([]map[string]any, error) {
	if s.queue == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	if threadID == "" || len(queueIDs) == 0 || len(queueIDs) > appserver.MaxQueuedSubmissions {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(threadID)
	if err != nil {
		return nil, err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	before, err := s.queueMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if len(before) != len(queueIDs) {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	want := make(map[string]struct{}, len(before))
	for _, item := range before {
		want[item["queueId"].(string)] = struct{}{}
	}
	for _, id := range queueIDs {
		if _, ok := want[id]; !ok {
			return nil, errors.New("INVALID_ARGUMENT")
		}
		delete(want, id)
	}
	if len(want) != 0 {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	if err := s.queue.Reorder(ctx, threadID, queueIDs); err != nil {
		return nil, err
	}
	after, err := s.queueMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	for index, id := range queueIDs {
		if after[index]["queueId"] != id {
			return nil, &desktopipc.CallError{Cause: ErrNativeShape, Outcome: "unknown"}
		}
	}
	return after, nil
}

func (s *Service) QueueSteer(ctx context.Context, threadID, queueID, expectedTurnID string) ([]map[string]any, error) {
	if s.queue == nil {
		return nil, appserver.ErrMethodUnavailable
	}
	if threadID == "" || queueID == "" || expectedTurnID == "" {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	c, err := s.controller(threadID)
	if err != nil {
		return nil, err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	items, err := s.queueMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	item, ok := findQueuedMessage(items, queueID)
	if !ok || item["editable"] != true {
		return nil, errors.New("INVALID_ARGUMENT")
	}
	c.mu.Lock()
	if err := s.ensureLive(ctx, c); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	live := c.live
	if err := s.validateLive(c); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()
	steerer, ok := live.(interface {
		Steer(context.Context, string, string, string, string, []string) error
	})
	if !ok {
		return nil, errors.New("PROTOCOL_UNSUPPORTED")
	}
	text := item["text"].(string)
	images := item["images"].([]string)
	if err := steerer.Steer(ctx, expectedTurnID, queueID, item["clientMessageId"].(string), text, images); err != nil {
		if errors.Is(err, desktopipc.ErrStaleTurn) {
			return nil, errors.New("STALE_TURN")
		}
		return nil, err
	}
	deleted, err := s.queue.Delete(ctx, threadID, queueID)
	if err != nil {
		return nil, &desktopipc.CallError{Cause: err, Outcome: "unknown"}
	}
	if !deleted {
		remaining, listErr := s.queueMessages(ctx, threadID)
		if listErr == nil {
			if _, exists := findQueuedMessage(remaining, queueID); !exists {
				return remaining, nil
			}
		}
		return nil, &desktopipc.CallError{Cause: ErrNativeShape, Outcome: "unknown"}
	}
	return s.queueMessages(ctx, threadID)
}

func (s *Service) Close() {
	s.mu.Lock()
	controllers := make([]*threadController, 0, len(s.threads))
	for _, c := range s.threads {
		controllers = append(controllers, c)
	}
	s.closed = true
	s.mu.Unlock()
	for _, c := range controllers {
		c.mu.Lock()
		stopWarmRetireLocked(c)
		if c.live != nil {
			_ = c.live.Close()
		}
		c.mu.Unlock()
	}
}
