package puck

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthorizationFreezesExpiresAndConsumesActionOnce(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "puck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateAssistant(ctx, Assistant{ID: "assistant-a", Name: "Puck", BrainDeviceID: "brain", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	service := NewAuthorizationService(store)
	service.now = func() time.Time { return now }
	action := FrozenAction{Tool: "start_task", Arguments: []byte(`{"goal":"ship","deviceId":"exec"}`)}
	auth, err := service.Request(ctx, "assistant-a", action, []string{"B5"}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if auth.ActionHash != HashAction(action) || auth.Status != "pending" {
		t.Fatalf("authorization=%+v", auth)
	}
	if err := service.Decide(ctx, "assistant-a", auth.ID, "approve_once", HashAction(FrozenAction{Tool: "start_task", Arguments: []byte(`{"goal":"tampered"}`)})); !errors.Is(err, ErrActionChanged) {
		t.Fatalf("tamper error=%v", err)
	}
	if err := service.Decide(ctx, "assistant-a", auth.ID, "approve_once", auth.ActionHash); err != nil {
		t.Fatal(err)
	}
	if got, err := service.Consume(ctx, "assistant-a", auth.ID); err != nil || string(got.Arguments) != string(action.Arguments) {
		t.Fatalf("consume=%+v err=%v", got, err)
	}
	if _, err := service.Consume(ctx, "assistant-a", auth.ID); !errors.Is(err, ErrAuthorizationConsumed) {
		t.Fatalf("second consume=%v", err)
	}

	expiring, err := service.Request(ctx, "assistant-a", action, []string{"B10"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := service.Decide(ctx, "assistant-a", expiring.ID, "approve_once", expiring.ActionHash); !errors.Is(err, ErrAuthorizationExpired) {
		t.Fatalf("expired decision=%v", err)
	}
}

func TestBrainCannotDecideAuthorization(t *testing.T) {
	if ActorCanDecideAuthorization("brain") || ActorCanDecideAuthorization("routine") || !ActorCanDecideAuthorization("user") {
		t.Fatal("authorization actor boundary changed")
	}
}

func TestAuthorizationRequestIsIdempotentForSourceMessage(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "puck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateAssistant(ctx, Assistant{ID: "assistant-a", Name: "Puck", BrainDeviceID: "brain", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	service := NewAuthorizationService(store)
	action := FrozenAction{Tool: "start_task", SourceMessageID: "user-message-a", Arguments: []byte(`{"goal":"ship","deviceId":"exec"}`)}
	first, err := service.Request(ctx, "assistant-a", action, []string{"B5"}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Request(ctx, "assistant-a", action, []string{"B5"}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorizations WHERE assistant_id=?`, "assistant-a").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || count != 1 {
		t.Fatalf("first=%s second=%s count=%d", first.ID, second.ID, count)
	}
}

func TestApproveAlwaysCreatesScopedRuleAndMatchesOnlyDescendants(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "puck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, id := range []string{"assistant-a", "assistant-b"} {
		if err := store.CreateAssistant(ctx, Assistant{ID: id, Name: id, BrainDeviceID: "brain", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	service := NewAuthorizationService(store)
	action := FrozenAction{Tool: "start_task", ActionType: "start_task", DeviceID: "exec-a", CWD: "/work/project", Arguments: []byte(`{"deviceId":"exec-a","cwd":"/work/project","goal":"test"}`)}
	auth, err := service.Request(ctx, "assistant-a", action, []string{"B1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Decide(ctx, "assistant-a", auth.ID, "approve_always", auth.ActionHash); err != nil {
		t.Fatal(err)
	}
	rules, err := store.AuthorizationRules(ctx, "assistant-a")
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	for _, tc := range []struct {
		assistant, action, device, cwd string
		want                           bool
	}{
		{"assistant-a", "start_task", "exec-a", "/work/project", true},
		{"assistant-a", "start_task", "exec-a", "/work/project/sub", true},
		{"assistant-a", "start_task", "exec-a", "/work/project2", false},
		{"assistant-a", "start_task", "exec-b", "/work/project", false},
		{"assistant-a", "send_turn", "exec-a", "/work/project", false},
		{"assistant-b", "start_task", "exec-a", "/work/project", false},
	} {
		matched, err := store.MatchAuthorizationRule(ctx, tc.assistant, tc.action, tc.device, tc.cwd)
		if err != nil || (matched != nil) != tc.want {
			t.Errorf("match(%q,%q,%q,%q)=%+v err=%v want=%v", tc.assistant, tc.action, tc.device, tc.cwd, matched, err, tc.want)
		}
	}
	if err := store.RevokeAuthorizationRule(ctx, "assistant-b", rules[0].ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-bot revoke=%v", err)
	}
	if err := store.RevokeAuthorizationRule(ctx, "assistant-a", rules[0].ID); err != nil {
		t.Fatal(err)
	}
	if matched, err := store.MatchAuthorizationRule(ctx, "assistant-a", "start_task", "exec-a", "/work/project/sub"); err != nil || matched != nil {
		t.Fatalf("revoked match=%+v err=%v", matched, err)
	}
}

func TestApproveAlwaysRejectsInvalidFrozenScopeWithoutConsumingAuthorization(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "puck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateAssistant(ctx, Assistant{ID: "assistant-a", Name: "Puck", BrainDeviceID: "brain", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	service := NewAuthorizationService(store)
	auth, err := service.Request(ctx, "assistant-a", FrozenAction{Tool: "start_task", ActionType: "start_task", DeviceID: "exec-a", CWD: "/", Arguments: []byte(`{}`)}, []string{"B1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Decide(ctx, "assistant-a", auth.ID, "approve_always", auth.ActionHash); !errors.Is(err, ErrAuthorizationScopeInvalid) {
		t.Fatalf("decision=%v", err)
	}
	stored, err := store.Authorization(ctx, "assistant-a", auth.ID)
	if err != nil || stored.Status != "pending" {
		t.Fatalf("authorization=%+v err=%v", stored, err)
	}
}
