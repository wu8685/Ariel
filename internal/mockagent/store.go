package mockagent

import (
	"crypto/rand"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StoreConfig struct {
	StepInterval time.Duration
}

type itemState struct {
	id, role, text string
}

type turnState struct {
	id, status string
	items      []*itemState
}

type threadState struct {
	id, title, cwd, runtime string
	updated                 time.Time
	turns                   []*turnState
	pending                 []map[string]any
	seen                    map[string]struct{}
	seenOrder               []string
}

type storeSubscription struct {
	id, streamID, threadID string
	seq                    int
	emit                   func(map[string]any)
}

type Store struct {
	mu       sync.Mutex
	threads  map[string]*threadState
	order    []string
	subs     map[string]*storeSubscription
	interval time.Duration
}

func NewStore(cfg StoreConfig) *Store {
	if cfg.StepInterval <= 0 {
		cfg.StepInterval = 180 * time.Millisecond
	}
	now := time.Now().UTC()
	a := &threadState{id: "mock-thread-a", title: "Ariel 演示会话", cwd: "/demo/project-alpha", runtime: "idle", updated: now, seen: map[string]struct{}{}}
	b := &threadState{id: "mock-thread-b", title: "Ariel 演示会话", cwd: "/demo/project-beta", runtime: "idle", updated: now.Add(-time.Hour), seen: map[string]struct{}{}, pending: []map[string]any{{"interactionId": "mock-input-b", "kind": "user_input", "prompt": "这是 Mock 补充问题", "availableDecisions": []string{"answer"}, "questions": []map[string]any{{"id": "choice", "question": "选择演示选项", "options": []string{"A", "B"}}}}}}
	return &Store{threads: map[string]*threadState{a.id: a, b.id: b}, order: []string{a.id, b.id}, subs: map[string]*storeSubscription{}, interval: cfg.StepInterval}
}

func (s *Store) List(limit int, cursor string) ([]map[string]any, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", errors.New("INVALID_ARGUMENT")
	}
	start := 0
	if cursor != "" {
		if !strings.HasPrefix(cursor, "mock:") {
			return nil, "", errors.New("INVALID_ARGUMENT")
		}
		var err error
		start, err = strconv.Atoi(strings.TrimPrefix(cursor, "mock:"))
		if err != nil || start < 1 || start >= len(s.order) {
			return nil, "", errors.New("INVALID_ARGUMENT")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	end := min(start+limit, len(s.order))
	result := make([]map[string]any, 0, end-start)
	for _, id := range s.order[start:end] {
		result = append(result, threadData(s.threads[id]))
	}
	next := ""
	if end < len(s.order) {
		next = "mock:" + strconv.Itoa(end)
	}
	return result, next, nil
}

func (s *Store) Read(id string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	if t == nil {
		return nil, errors.New("NOT_FOUND")
	}
	return threadData(t), nil
}

func threadData(t *threadState) map[string]any {
	turns := make([]any, 0, len(t.turns))
	for _, turn := range t.turns {
		items := make([]any, 0, len(turn.items))
		for _, item := range turn.items {
			items = append(items, map[string]any{"itemId": item.id, "role": item.role, "text": item.text})
		}
		turns = append(turns, map[string]any{"turnId": turn.id, "status": turn.status, "items": items})
	}
	pending := make([]any, 0, len(t.pending))
	for _, p := range t.pending {
		pending = append(pending, p)
	}
	return map[string]any{"threadId": t.id, "title": t.title, "cwd": t.cwd, "updatedAt": t.updated.Format(time.RFC3339Nano), "runtime": t.runtime, "turns": turns, "pendingInteractions": pending}
}

func (s *Store) Start(threadID, clientMessageID, text string) (string, error) {
	if clientMessageID == "" || strings.TrimSpace(text) == "" || len([]byte(text)) > 65536 {
		return "", errors.New("INVALID_ARGUMENT")
	}
	s.mu.Lock()
	t := s.threads[threadID]
	if t == nil {
		s.mu.Unlock()
		return "", errors.New("NOT_FOUND")
	}
	if _, used := t.seen[clientMessageID]; used {
		s.mu.Unlock()
		return "", errors.New("INVALID_ARGUMENT")
	}
	if t.runtime != "idle" || len(t.pending) > 0 {
		s.mu.Unlock()
		return "", errors.New("TURN_BUSY")
	}
	turnID := "mock-turn-" + rand.Text()
	turn := &turnState{id: turnID, status: "inProgress", items: []*itemState{{id: "mock-user-" + rand.Text(), role: "user", text: text}, {id: "mock-assistant-" + rand.Text(), role: "assistant"}}}
	t.turns = append(t.turns, turn)
	t.runtime = "inProgress"
	t.updated = time.Now().UTC()
	t.seen[clientMessageID] = struct{}{}
	t.seenOrder = append(t.seenOrder, clientMessageID)
	if len(t.seenOrder) > 128 {
		delete(t.seen, t.seenOrder[0])
		t.seenOrder = t.seenOrder[1:]
	}
	s.mu.Unlock()
	s.notify(threadID)
	go s.stream(threadID, turnID, text)
	return turnID, nil
}

func (s *Store) stream(threadID, turnID, text string) {
	parts := []string{"这是 Mock 响应。", "\n已收到输入：", text}
	for i, part := range parts {
		time.Sleep(s.interval)
		s.mu.Lock()
		t := s.threads[threadID]
		if t == nil || len(t.turns) == 0 || t.turns[len(t.turns)-1].id != turnID || t.turns[len(t.turns)-1].status != "inProgress" {
			s.mu.Unlock()
			return
		}
		turn := t.turns[len(t.turns)-1]
		turn.items[1].text += part
		if i == len(parts)-1 {
			turn.status = "completed"
			t.runtime = "idle"
		}
		t.updated = time.Now().UTC()
		s.mu.Unlock()
		s.notify(threadID)
	}
}

func (s *Store) Interrupt(threadID, expectedTurnID string) error {
	s.mu.Lock()
	t := s.threads[threadID]
	if t == nil {
		s.mu.Unlock()
		return errors.New("NOT_FOUND")
	}
	if t.runtime != "inProgress" || len(t.turns) == 0 || t.turns[len(t.turns)-1].id != expectedTurnID {
		s.mu.Unlock()
		return errors.New("STALE_TURN")
	}
	t.turns[len(t.turns)-1].status = "interrupted"
	t.runtime = "idle"
	t.updated = time.Now().UTC()
	s.mu.Unlock()
	s.notify(threadID)
	return nil
}

func (s *Store) Respond(threadID, interactionID, decision string, answers map[string][]string) error {
	s.mu.Lock()
	t := s.threads[threadID]
	if t == nil {
		s.mu.Unlock()
		return errors.New("NOT_FOUND")
	}
	index := -1
	for i, p := range t.pending {
		if p["interactionId"] == interactionID {
			index = i
			break
		}
	}
	if index < 0 || decision != "answer" {
		s.mu.Unlock()
		return errors.New("INTERACTION_UNSUPPORTED")
	}
	values := answers["choice"]
	if len(values) != 1 || (values[0] != "A" && values[0] != "B") {
		s.mu.Unlock()
		return errors.New("INVALID_ARGUMENT")
	}
	t.pending = append(t.pending[:index], t.pending[index+1:]...)
	t.updated = time.Now().UTC()
	s.mu.Unlock()
	s.notify(threadID)
	return nil
}

func (s *Store) Subscribe(threadID string, emit func(map[string]any)) (string, string, map[string]any, error) {
	if emit == nil {
		return "", "", nil, errors.New("INVALID_ARGUMENT")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[threadID]
	if t == nil {
		return "", "", nil, errors.New("NOT_FOUND")
	}
	id, streamID := "mock-sub-"+rand.Text(), "mock-stream-"+rand.Text()
	s.subs[id] = &storeSubscription{id: id, streamID: streamID, threadID: threadID, seq: 1, emit: emit}
	return id, streamID, threadData(t), nil
}

func (s *Store) Unsubscribe(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[id] == nil {
		return false
	}
	delete(s.subs, id)
	return true
}

func (s *Store) notify(threadID string) {
	type delivery struct {
		emit  func(map[string]any)
		event map[string]any
	}
	s.mu.Lock()
	t := s.threads[threadID]
	if t == nil {
		s.mu.Unlock()
		return
	}
	deliveries := make([]delivery, 0)
	for _, sub := range s.subs {
		if sub.threadID != threadID {
			continue
		}
		base := sub.seq
		sub.seq++
		deliveries = append(deliveries, delivery{emit: sub.emit, event: map[string]any{"event": "thread.update", "threadId": threadID, "subscriptionId": sub.id, "streamId": sub.streamID, "baseSeq": base, "seq": sub.seq, "thread": threadData(t)}})
	}
	s.mu.Unlock()
	for _, delivery := range deliveries {
		delivery.emit(delivery.event)
	}
}
