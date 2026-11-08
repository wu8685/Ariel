package puck

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestStoreTracksServerUnreadPreviewAndActorReactions(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "puck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	a := Assistant{ID: "assistant-a", Name: "Puck", BrainDeviceID: "brain-a", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1}
	if err := store.CreateAssistant(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, Message{ID: "user-a", AssistantID: a.ID, Role: "user", Kind: "text", Text: "开始吧"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, Message{ID: "assistant-a", AssistantID: a.ID, Role: "assistant", Kind: "text", Text: "已经开始"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Assistant(ctx, a.ID)
	if err != nil || got.UnreadCount != 1 || got.LastMessagePreview != "已经开始" {
		t.Fatalf("assistant=%+v err=%v", got, err)
	}
	if _, err := store.UpdateMessageReaction(ctx, a.ID, "user-a", "user", "👍"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMessage(ctx, Message{ID: "user-a", AssistantID: a.ID, Role: "user", Kind: "text", Text: "开始吧（已同步）"}); err != nil {
		t.Fatal(err)
	}
	reaction, err := store.UpdateMessageReaction(ctx, a.ID, "user-a", "assistant", "👀")
	if err != nil {
		t.Fatal(err)
	}
	var actors map[string]string
	if json.Unmarshal([]byte(reaction), &actors) != nil || actors["user"] != "👍" || actors["assistant"] != "👀" {
		t.Fatalf("reaction=%s", reaction)
	}
	if _, err := store.UpdateMessageReaction(ctx, a.ID, "user-a", "user", "👍"); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages(ctx, a.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	actors = map[string]string{}
	if json.Unmarshal([]byte(messages[0].Reaction), &actors) != nil || actors["user"] != "" || actors["assistant"] != "👀" {
		t.Fatalf("messages=%+v actors=%v", messages, actors)
	}
	if err := store.MarkAssistantRead(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Assistant(ctx, a.ID)
	if got.UnreadCount != 0 {
		t.Fatalf("unread=%d", got.UnreadCount)
	}
}

func TestStoreSchemaMigratesV1ToCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "puck-v1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
		CREATE TABLE assistants (id TEXT PRIMARY KEY);
		CREATE TABLE tasks (task_id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, device_id TEXT NOT NULL, cwd TEXT NOT NULL, native_thread_id TEXT NOT NULL, goal TEXT NOT NULL, status TEXT NOT NULL, risk_tags TEXT NOT NULL DEFAULT '[]', created_by TEXT NOT NULL DEFAULT 'puck', last_turn_id TEXT NOT NULL DEFAULT '', last_seen_revision TEXT NOT NULL DEFAULT '', last_summary TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
		CREATE TABLE authorizations (id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, action_json TEXT NOT NULL, action_hash TEXT NOT NULL, rules_hit TEXT NOT NULL, status TEXT NOT NULL, expires_at TEXT NOT NULL, decided_at TEXT, created_at TEXT NOT NULL);
		PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != storeSchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	rows, err := store.db.Query(`PRAGMA table_info(assistants)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if !columns["unread_count"] || !columns["last_message_preview"] {
		t.Fatalf("columns=%v", columns)
	}
	for table, column := range map[string]string{"tasks": "source_message_id", "authorizations": "source_message_id"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s.%s count=%d err=%v", table, column, count, err)
		}
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

func TestStoreMigratesV2TaskIdempotencyColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "puck-v2.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
		CREATE TABLE tasks (task_id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL);
		CREATE TABLE authorizations (id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, status TEXT NOT NULL);
		PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for table, column := range map[string]string{"tasks": "source_message_id", "authorizations": "source_message_id"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s.%s count=%d err=%v", table, column, count, err)
		}
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
