package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type History interface {
	List(context.Context, string, int) (appserver.Page, error)
	Read(context.Context, string) (appserver.Thread, error)
}

var ErrHistoryTooLarge = errors.New("HISTORY_TOO_LARGE")

const maxThreadPayloadBytes = 7 << 20
const maxThreadControllers = 64
const maxSubscriptionsPerThread = 16
const maxRecentMessageIDs = 4096

func checkThreadSize(thread map[string]any) error {
	body, err := json.Marshal(thread)
	if err != nil {
		return ErrNativeShape
	}
	if len(body) > maxThreadPayloadBytes {
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
	emit         func(map[string]any)
}
type threadController struct {
	mu             sync.Mutex
	startMu        sync.Mutex
	id, title, cwd string
	live           Live
	subs           map[string]*subscription
	leases         int
	lastUsed       uint64
}
type Service struct {
	history   History
	attach    LiveFactory
	mu        sync.Mutex
	threads   map[string]*threadController
	clock     uint64
	seen      map[string]struct{}
	seenOrder []string
	closed    bool
}

func NewService(history History, attach LiveFactory) *Service {
	return &Service{history: history, attach: attach, threads: map[string]*threadController{}, seen: map[string]struct{}{}}
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
	var retired Live
	if c.leases == 0 && len(c.subs) == 0 {
		retired = c.live
		c.live = nil
	}
	c.mu.Unlock()
	s.mu.Unlock()
	if retired != nil {
		_ = retired.Close()
	}
}

func (s *Service) List(ctx context.Context, limit int, cursor string) ([]map[string]any, string, error) {
	page, err := s.history.List(ctx, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	threads := make([]map[string]any, 0, len(page.Data))
	for _, source := range page.Data {
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
	thread, err := NormalizeStored(source)
	if err != nil {
		return nil, err
	}
	return thread, checkThreadSize(thread)
}

func (s *Service) ensureLive(ctx context.Context, c *threadController) error {
	if c.live != nil {
		return nil
	}
	source, err := s.history.Read(ctx, c.id)
	if err != nil {
		return err
	}
	if source.ID != c.id || source.CWD == "" {
		return ErrNativeShape
	}
	live, err := s.attach(ctx, c.id, source.CWD)
	if err != nil {
		return err
	}
	c.live = live
	c.title = source.Name
	c.cwd = source.CWD
	go s.pump(c, live)
	return nil
}

// Mutation requests may be sent without an active Web subscription. They
// still need the same fail-closed native-state gate as thread.subscribe.
func validateLive(c *threadController) error {
	raw, err := c.live.Current()
	if err != nil {
		return err
	}
	if _, err := NormalizeLive(c.id, c.title, c.cwd, raw); err != nil {
		return desktopipc.ErrNativeStateUncertain
	}
	return nil
}

func (s *Service) pump(c *threadController, live Live) {
	for {
		select {
		case <-live.Updates():
			raw, err := live.Current()
			if err != nil {
				s.invalidateLive(c, live, "RESYNC_REQUIRED")
				return
			}
			c.mu.Lock()
			if c.live != live {
				c.mu.Unlock()
				return
			}
			thread, e := NormalizeLive(c.id, c.title, c.cwd, raw)
			if e != nil {
				c.mu.Unlock()
				s.invalidateLive(c, live, "NATIVE_STATE_UNCERTAIN")
				return
			}
			tooLarge := checkThreadSize(thread) == ErrHistoryTooLarge
			type delivery struct {
				emit  func(map[string]any)
				event map[string]any
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
	if emit == nil {
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
	if err := s.ensureLive(ctx, c); err != nil {
		c.mu.Unlock()
		return "", "", nil, nil, err
	}
	raw, err := c.live.Current()
	if err != nil {
		c.mu.Unlock()
		return "", "", nil, nil, err
	}
	thread, err := NormalizeLive(c.id, c.title, c.cwd, raw)
	if err != nil {
		c.mu.Unlock()
		return "", "", nil, nil, desktopipc.ErrNativeStateUncertain
	}
	if err := checkThreadSize(thread); err != nil {
		c.mu.Unlock()
		return "", "", nil, nil, err
	}
	sub := &subscription{id: "desktop-sub-" + rand.Text(), streamID: "desktop-stream-" + rand.Text(), seq: 1, emit: emit}
	c.subs[sub.id] = sub
	c.mu.Unlock()
	activate := func() {
		c.mu.Lock()
		if c.subs[sub.id] == sub {
			live := c.live
			latestRaw, currentErr := live.Current()
			latest, normalizeErr := NormalizeLive(c.id, c.title, c.cwd, latestRaw)
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
			if currentErr == nil && normalizeErr == nil && checkThreadSize(latest) == ErrHistoryTooLarge {
				delete(c.subs, sub.id)
				sub.emit(map[string]any{"event": "thread.error", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "code": "HISTORY_TOO_LARGE"})
			} else if currentErr == nil && normalizeErr == nil && !sameThreadContent(latest, thread) {
				sub.seq++
				sub.emit(map[string]any{"event": "thread.update", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "baseSeq": 1, "seq": sub.seq, "thread": latest})
			}
		}
		c.mu.Unlock()
	}
	return sub.id, sub.streamID, thread, activate, nil
}

func sameThreadContent(a, b map[string]any) bool {
	return a["runtime"] == b["runtime"] && reflect.DeepEqual(a["turns"], b["turns"]) && reflect.DeepEqual(a["pendingInteractions"], b["pendingInteractions"])
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

func (s *Service) retireIdle(c *threadController) {
	s.mu.Lock()
	c.mu.Lock()
	var retired Live
	if c.leases == 0 && len(c.subs) == 0 {
		retired = c.live
		c.live = nil
	}
	c.mu.Unlock()
	s.mu.Unlock()
	if retired != nil {
		_ = retired.Close()
	}
}

func (s *Service) Start(ctx context.Context, threadID, messageID, text string) (string, error) {
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
	if err := s.ensureLive(ctx, c); err != nil {
		c.mu.Unlock()
		return "", err
	}
	live := c.live
	if err := validateLive(c); err != nil {
		c.mu.Unlock()
		return "", err
	}
	c.mu.Unlock()
	turnID, err := live.Start(ctx, messageID, text)
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
	if err := validateLive(c); err != nil {
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
	if err := validateLive(c); err != nil {
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
		if c.live != nil {
			_ = c.live.Close()
		}
		c.mu.Unlock()
	}
}
