package puck

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrActionChanged             = errors.New("authorization action changed")
	ErrAuthorizationExpired      = errors.New("authorization expired")
	ErrAuthorizationConsumed     = errors.New("authorization already consumed")
	ErrAuthorizationRejected     = errors.New("authorization rejected")
	ErrAuthorizationScopeInvalid = errors.New("authorization scope invalid")
)

type FrozenAction struct {
	Tool            string          `json:"tool"`
	SourceMessageID string          `json:"sourceMessageId,omitempty"`
	ActionType      string          `json:"actionType,omitempty"`
	DeviceID        string          `json:"deviceId,omitempty"`
	CWD             string          `json:"cwd,omitempty"`
	Arguments       json.RawMessage `json:"arguments"`
}

type AuthorizationService struct {
	store *Store
	now   func() time.Time
}

func NewAuthorizationService(store *Store) *AuthorizationService {
	return &AuthorizationService{store: store, now: time.Now}
}

func HashAction(action FrozenAction) string {
	var arguments any
	if json.Unmarshal(action.Arguments, &arguments) != nil {
		arguments = string(action.Arguments)
	}
	canonical, _ := json.Marshal(struct {
		Tool, ActionType, DeviceID, CWD string
		Arguments                       any
	}{action.Tool, action.ActionType, action.DeviceID, action.CWD, arguments})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func (s *AuthorizationService) Request(ctx context.Context, assistantID string, action FrozenAction, rules []string, lifetime time.Duration) (Authorization, error) {
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}
	if action.SourceMessageID != "" {
		var existing Authorization
		var expires string
		err := s.store.db.QueryRowContext(ctx, `SELECT id,assistant_id,source_message_id,action_json,action_hash,rules_hit,status,expires_at FROM authorizations WHERE assistant_id=? AND source_message_id=? AND status='pending'`, assistantID, action.SourceMessageID).Scan(&existing.ID, &existing.AssistantID, &existing.SourceMessageID, &existing.ActionJSON, &existing.ActionHash, &existing.RulesHit, &existing.Status, &expires)
		if err == nil {
			existing.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
			if s.now().Before(existing.ExpiresAt) {
				return existing, nil
			}
			_, _ = s.store.db.ExecContext(ctx, `UPDATE authorizations SET status='expired',decided_at=? WHERE id=? AND status='pending'`, nowText(s.now()), existing.ID)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Authorization{}, err
		}
	}
	actionJSON, err := json.Marshal(action)
	if err != nil {
		return Authorization{}, err
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return Authorization{}, err
	}
	authorization := Authorization{ID: randomID("auth"), AssistantID: assistantID, SourceMessageID: action.SourceMessageID, ActionJSON: string(actionJSON), ActionHash: HashAction(action), RulesHit: string(rulesJSON), Status: "pending", ExpiresAt: s.now().Add(lifetime)}
	if err := s.store.InsertAuthorization(ctx, authorization); err != nil {
		return Authorization{}, err
	}
	return authorization, nil
}

func (s *AuthorizationService) Decide(ctx context.Context, assistantID, authorizationID, decision, actionHash string) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var storedHash, status, expires, actionJSON string
	if err := tx.QueryRowContext(ctx, `SELECT action_hash,status,expires_at,action_json FROM authorizations WHERE assistant_id=? AND id=?`, assistantID, authorizationID).Scan(&storedHash, &status, &expires, &actionJSON); err != nil {
		return err
	}
	if storedHash != actionHash {
		return ErrActionChanged
	}
	if status != "pending" {
		return ErrAuthorizationConsumed
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !s.now().Before(expiresAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE authorizations SET status='expired',decided_at=? WHERE id=? AND status='pending'`, nowText(s.now()), authorizationID)
		_ = tx.Commit()
		return ErrAuthorizationExpired
	}
	status = "approved"
	if decision == "reject" {
		status = "rejected"
	} else if decision == "approve_always" {
		var action FrozenAction
		if json.Unmarshal([]byte(actionJSON), &action) != nil || !validAuthorizationScope(action) {
			return ErrAuthorizationScopeInvalid
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO authorization_rules(id,assistant_id,action_type,device_id,cwd_root,created_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(assistant_id,action_type,device_id,cwd_root) DO NOTHING`, randomID("rule"), assistantID, action.ActionType, action.DeviceID, filepath.Clean(action.CWD), nowText(s.now()))
		if err != nil {
			return err
		}
	} else if decision != "approve_once" {
		return errors.New("invalid authorization decision")
	}
	result, err := tx.ExecContext(ctx, `UPDATE authorizations SET status=?,decided_at=? WHERE assistant_id=? AND id=? AND status='pending'`, status, nowText(s.now()), assistantID, authorizationID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrAuthorizationConsumed
	}
	return tx.Commit()
}

func validAuthorizationScope(action FrozenAction) bool {
	cwd := filepath.Clean(strings.TrimSpace(action.CWD))
	return strings.TrimSpace(action.ActionType) != "" && len(action.ActionType) <= 256 && strings.TrimSpace(action.DeviceID) != "" && len(action.DeviceID) <= 128 && filepath.IsAbs(cwd) && cwd != string(filepath.Separator)
}

func (s *AuthorizationService) Consume(ctx context.Context, assistantID, authorizationID string) (FrozenAction, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return FrozenAction{}, err
	}
	defer tx.Rollback()
	var actionJSON, status string
	if err := tx.QueryRowContext(ctx, `SELECT action_json,status FROM authorizations WHERE assistant_id=? AND id=?`, assistantID, authorizationID).Scan(&actionJSON, &status); err != nil {
		return FrozenAction{}, err
	}
	if status == "rejected" {
		return FrozenAction{}, ErrAuthorizationRejected
	}
	if status != "approved" {
		return FrozenAction{}, ErrAuthorizationConsumed
	}
	result, err := tx.ExecContext(ctx, `UPDATE authorizations SET status='consumed' WHERE assistant_id=? AND id=? AND status='approved'`, assistantID, authorizationID)
	if err != nil {
		return FrozenAction{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return FrozenAction{}, ErrAuthorizationConsumed
	}
	var action FrozenAction
	if err := json.Unmarshal([]byte(actionJSON), &action); err != nil {
		return FrozenAction{}, err
	}
	return action, tx.Commit()
}

func ActorCanDecideAuthorization(actor string) bool { return actor == "user" }

func randomID(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic("cryptographic randomness unavailable")
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
