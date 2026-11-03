package puck

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrActionChanged         = errors.New("authorization action changed")
	ErrAuthorizationExpired  = errors.New("authorization expired")
	ErrAuthorizationConsumed = errors.New("authorization already consumed")
	ErrAuthorizationRejected = errors.New("authorization rejected")
)

type FrozenAction struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
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
		Tool      string `json:"tool"`
		Arguments any    `json:"arguments"`
	}{action.Tool, arguments})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func (s *AuthorizationService) Request(ctx context.Context, assistantID string, action FrozenAction, rules []string, lifetime time.Duration) (Authorization, error) {
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}
	actionJSON, err := json.Marshal(action)
	if err != nil {
		return Authorization{}, err
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return Authorization{}, err
	}
	authorization := Authorization{ID: randomID("auth"), AssistantID: assistantID, ActionJSON: string(actionJSON), ActionHash: HashAction(action), RulesHit: string(rulesJSON), Status: "pending", ExpiresAt: s.now().Add(lifetime)}
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
	var storedHash, status, expires string
	if err := tx.QueryRowContext(ctx, `SELECT action_hash,status,expires_at FROM authorizations WHERE assistant_id=? AND id=?`, assistantID, authorizationID).Scan(&storedHash, &status, &expires); err != nil {
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
