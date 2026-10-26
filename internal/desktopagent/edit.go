package desktopagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
	"io"
	"strings"
	"time"
)

func jsonUnmarshalEdit(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("INVALID_ARGUMENT")
	}
	return nil
}

// Only the actually tested Desktop + bundled CLI pair is enabled. Version
// changes require repeating the isolated write gate, not optimistic probing.
func verifiedEditBinary(ctx context.Context, binary string) bool {
	i := strings.Index(binary, ".app/")
	if i < 0 {
		return false
	}
	app := binary[:i+4]
	if binary != probe.BundledBinary(app) {
		return false
	}
	desktop, e := probe.DesktopVersion(ctx, app)
	if e != nil || desktop != "26.930.51102" {
		return false
	}
	cli, e := probe.BinaryVersion(ctx, binary)
	return e == nil && cli == "0.160.0"
}
func (s *Service) EditEnabled() bool { return s.editEnabled }

type editingLive interface {
	EditLatest(context.Context, string, string, string, string) (string, error)
}

func (s *Service) editTargetLocked(ctx context.Context, c *threadController) (desktopipc.EditTarget, Live, error) {
	if !s.editEnabled {
		return desktopipc.EditTarget{}, nil, appserver.ErrMethodUnavailable
	}
	if c.bootstrapHold || c.migrating || c.live == nil || c.current == nil {
		return desktopipc.EditTarget{}, nil, desktopipc.ErrResyncRequired
	}
	if _, ok := c.live.(editingLive); !ok {
		return desktopipc.EditTarget{}, nil, appserver.ErrMethodUnavailable
	}
	if s.queue != nil {
		items, e := s.queue.List(ctx, c.id)
		if e != nil {
			return desktopipc.EditTarget{}, nil, e
		}
		if len(items) > 0 {
			return desktopipc.EditTarget{}, nil, errors.New("TURN_BUSY")
		}
	}
	raw, e := c.live.Current()
	if e != nil {
		return desktopipc.EditTarget{}, nil, e
	}
	target, e := desktopipc.LatestEditTarget(raw, c.cwd)
	return target, c.live, e
}
func (s *Service) ReadEditTarget(ctx context.Context, id string) (desktopipc.EditTarget, error) {
	c, e := s.controller(id)
	if e != nil {
		return desktopipc.EditTarget{}, e
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	target, _, e := s.editTargetLocked(ctx, c)
	return target, e
}
func (s *Service) EditLatest(ctx context.Context, id, operation, turn, item, hash, text string) (string, error) {
	if operation == "" || len(operation) > 128 || strings.TrimSpace(text) == "" || len(text) > 65536 {
		return "", errors.New("INVALID_ARGUMENT")
	}
	c, e := s.controller(id)
	if e != nil {
		return "", e
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	target, live, e := s.editTargetLocked(ctx, c)
	c.mu.Unlock()
	if e != nil {
		return "", e
	}
	if target.TurnID != turn || target.ItemID != item || target.Fingerprint != hash {
		return "", desktopipc.ErrStaleTurn
	}
	key := "edit\x00" + id + "\x00" + operation
	s.mu.Lock()
	_, seen := s.seen[key]
	if !seen {
		s.seen[key] = struct{}{}
		s.seenOrder = append(s.seenOrder, key)
		if len(s.seenOrder) > maxRecentMessageIDs {
			delete(s.seen, s.seenOrder[0])
			s.seenOrder = s.seenOrder[1:]
		}
	}
	s.mu.Unlock()
	if seen {
		return "", appserver.ErrOutcomeUnknown
	}
	return live.(editingLive).EditLatest(ctx, turn, item, hash, text)
}
func (a *agent) handleEditing(ctx context.Context, id, method string, raw []byte) {
	operationCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	var p struct {
		ThreadID       string `json:"threadId"`
		ExpectedTurnID string `json:"expectedTurnId"`
		ExpectedItemID string `json:"expectedItemId"`
		Fingerprint    string `json:"fingerprint"`
		OperationID    string `json:"operationId"`
		AgentEpoch     string `json:"agentEpoch"`
		Text           string `json:"text"`
	}
	if jsonUnmarshalEdit(raw, &p) != nil || a.service == nil || !a.service.EditEnabled() {
		a.reply(ctx, id, method, nil, appserver.ErrMethodUnavailable)
		return
	}
	if p.AgentEpoch != a.agentEpoch {
		a.reply(ctx, id, method, nil, desktopipc.ErrResyncRequired)
		return
	}
	if method == "turn.edit.read" {
		target, e := a.service.ReadEditTarget(operationCtx, p.ThreadID)
		a.reply(ctx, id, method, map[string]any{"target": target, "agentEpoch": a.agentEpoch}, e)
		return
	}
	turn, e := a.service.EditLatest(operationCtx, p.ThreadID, p.OperationID, p.ExpectedTurnID, p.ExpectedItemID, p.Fingerprint, p.Text)
	a.reply(ctx, id, method, map[string]any{"turnId": turn, "replacedTurnId": p.ExpectedTurnID, "operationId": p.OperationID, "agentEpoch": a.agentEpoch}, e)
}
