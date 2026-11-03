package puck

import (
	"context"
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
