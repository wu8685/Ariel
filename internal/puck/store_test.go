package puck

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreMigratesAndIsolatesAssistantRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "puck.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("database mode: info=%v err=%v", info, err)
	}
	ctx := context.Background()
	for _, assistant := range []Assistant{
		{ID: "assistant-a", Name: "Puck A", BrainDeviceID: "brain-a", BrainCWD: "/brain/a", Status: "provisioning", ToolsetVersion: 1},
		{ID: "assistant-b", Name: "Puck B", BrainDeviceID: "brain-b", BrainCWD: "/brain/b", Status: "ready", ToolsetVersion: 1},
	} {
		if err := store.CreateAssistant(ctx, assistant); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AppendMessage(ctx, Message{ID: "message-a", AssistantID: "assistant-a", Role: "user", Kind: "text", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, Task{ID: "task-a", AssistantID: "assistant-a", DeviceID: "exec", CWD: "/work", NativeThreadID: "thread-a", Goal: "goal", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertAuthorization(ctx, Authorization{ID: "auth-a", AssistantID: "assistant-a", ActionJSON: `{"tool":"start_task"}`, ActionHash: "hash", RulesHit: `["B5"]`, Status: "pending", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Audit(ctx, AuditEntry{ID: "audit-a", AssistantID: "assistant-a", Actor: "brain", Action: "tool_call", Target: "task-a", Result: "accepted"}); err != nil {
		t.Fatal(err)
	}
	if messages, err := store.Messages(ctx, "assistant-a", 10); err != nil || len(messages) != 1 {
		t.Fatalf("assistant-a messages=%+v err=%v", messages, err)
	}
	if messages, err := store.Messages(ctx, "assistant-b", 10); err != nil || len(messages) != 0 {
		t.Fatalf("cross-assistant messages=%+v err=%v", messages, err)
	}
	if task, err := store.Task(ctx, "assistant-b", "task-a"); err != sql.ErrNoRows || task.ID != "" {
		t.Fatalf("cross-assistant task=%+v err=%v", task, err)
	}
	if assistant, err := store.Assistant(ctx, "assistant-a"); err != nil || assistant.Name != "Puck A" {
		t.Fatalf("assistant=%+v err=%v", assistant, err)
	}
}

func TestStoreRejectsDatabaseFromNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "puck.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err := OpenStore(path); err == nil {
		t.Fatal("newer schema accepted")
	}
}

func TestStorePersistsAvatarAndUpdatesCardInPlace(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "puck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	a := Assistant{ID: "assistant-a", Name: "Puck", Avatar: `{"shape":"cloud","color":"#7c6ff2"}`, BrainDeviceID: "brain-a", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1}
	if err := store.CreateAssistant(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Avatar = `{"shape":"star","color":"#ef8354"}`
	if err := store.UpdateAssistant(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, Message{ID: "card-a", AssistantID: a.ID, Role: "assistant", Kind: "question", Text: "选择？", CardJSON: `{"cardState":"pending"}`}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateMessageCard(ctx, a.ID, "card-a", `{"cardState":"answered","answer":"A"}`); err != nil {
		t.Fatal(err)
	}
	got, err := store.Assistant(ctx, a.ID)
	if err != nil || got.Avatar != a.Avatar {
		t.Fatalf("assistant=%+v err=%v", got, err)
	}
	messages, err := store.Messages(ctx, a.ID, 10)
	if err != nil || len(messages) != 1 || messages[0].CardJSON != `{"cardState":"answered","answer":"A"}` {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}
