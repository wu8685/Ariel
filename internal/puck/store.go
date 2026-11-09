package puck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const storeSchemaVersion = 5

type Store struct{ db *sql.DB }

var ErrAssistantLimitReached = errors.New("active assistant limit reached")

type Assistant struct {
	ID, Name, Avatar, Description, Persona                          string
	BrainDeviceID, BrainCWD, BrainModel, BrainReasoning             string
	BrainThreadID, BrainThreadHistory, AllowedScope, Policy, Limits string
	Status                                                          string
	ToolsetVersion                                                  int
	UnreadCount                                                     int
	LastMessagePreview                                              string
}

type Message struct {
	ID          string    `json:"id"`
	AssistantID string    `json:"assistantId"`
	Role        string    `json:"role"`
	Kind        string    `json:"kind"`
	Text        string    `json:"text"`
	CardJSON    string    `json:"cardJSON,omitempty"`
	Reaction    string    `json:"reaction,omitempty"`
	WorkSummary string    `json:"workSummary,omitempty"`
	BrainTurnID string    `json:"brainTurnId,omitempty"`
	ReplyTo     string    `json:"replyTo,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Task struct {
	ID, AssistantID, SourceMessageID, DeviceID, CWD, NativeThreadID, Goal, Status, RiskTags, CreatedBy, LastTurnID, LastSeenRevision, LastSummary string
}

type Authorization struct {
	ID, AssistantID, SourceMessageID, ActionJSON, ActionHash, RulesHit, Status string
	ExpiresAt, DecidedAt                                                       time.Time
}

type AuthorizationRule struct {
	ID          string    `json:"id"`
	AssistantID string    `json:"assistantId"`
	ActionType  string    `json:"actionType"`
	DeviceID    string    `json:"deviceId"`
	CWD         string    `json:"cwd"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AuditEntry struct {
	ID, AssistantID, Actor, Action, Target, Result string
	At                                             time.Time
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("Puck database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > storeSchemaVersion {
		return fmt.Errorf("Puck database schema %d is newer than supported %d", version, storeSchemaVersion)
	}
	if version == storeSchemaVersion {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{}
	if version == 0 {
		statements = append(statements,
			`CREATE TABLE assistants (
            id TEXT PRIMARY KEY, name TEXT NOT NULL, avatar TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', persona TEXT NOT NULL DEFAULT '',
            brain_device_id TEXT NOT NULL, brain_cwd TEXT NOT NULL, brain_model TEXT NOT NULL DEFAULT '', brain_reasoning TEXT NOT NULL DEFAULT '',
            brain_thread_id TEXT NOT NULL DEFAULT '', brain_thread_history TEXT NOT NULL DEFAULT '[]', toolset_version INTEGER NOT NULL,
            allowed_scope TEXT NOT NULL DEFAULT '{}', policy TEXT NOT NULL DEFAULT '{}', limits TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL,
            unread_count INTEGER NOT NULL DEFAULT 0, last_message_preview TEXT NOT NULL DEFAULT '',
            created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT)`,
			`CREATE TABLE conversation_messages (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL REFERENCES assistants(id), role TEXT NOT NULL, kind TEXT NOT NULL, text TEXT NOT NULL DEFAULT '',
            card_json TEXT NOT NULL DEFAULT '', reaction TEXT NOT NULL DEFAULT '', work_summary TEXT NOT NULL DEFAULT '', brain_turn_id TEXT NOT NULL DEFAULT '', reply_to TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
			`CREATE INDEX conversation_messages_assistant_at ON conversation_messages(assistant_id, created_at)`,
			`CREATE TABLE tasks (
            task_id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL REFERENCES assistants(id), device_id TEXT NOT NULL, cwd TEXT NOT NULL, native_thread_id TEXT NOT NULL,
            goal TEXT NOT NULL, status TEXT NOT NULL, risk_tags TEXT NOT NULL DEFAULT '[]', created_by TEXT NOT NULL DEFAULT 'puck', last_turn_id TEXT NOT NULL DEFAULT '',
			source_message_id TEXT NOT NULL DEFAULT '', last_seen_revision TEXT NOT NULL DEFAULT '', last_summary TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE INDEX tasks_assistant ON tasks(assistant_id)`,
			`CREATE UNIQUE INDEX tasks_assistant_source_message ON tasks(assistant_id,source_message_id) WHERE source_message_id<>''`,
			`CREATE TABLE authorizations (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL REFERENCES assistants(id), action_json TEXT NOT NULL, action_hash TEXT NOT NULL,
			source_message_id TEXT NOT NULL DEFAULT '', rules_hit TEXT NOT NULL, status TEXT NOT NULL, expires_at TEXT NOT NULL, decided_at TEXT, created_at TEXT NOT NULL)`,
			`CREATE INDEX authorizations_assistant ON authorizations(assistant_id, status)`,
			`CREATE UNIQUE INDEX authorizations_pending_source_message ON authorizations(assistant_id,source_message_id) WHERE source_message_id<>'' AND status='pending'`,
			`CREATE TABLE authorization_rules (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL REFERENCES assistants(id), action_type TEXT NOT NULL, device_id TEXT NOT NULL,
            cwd_root TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(assistant_id,action_type,device_id,cwd_root))`,
			`CREATE INDEX authorization_rules_assistant ON authorization_rules(assistant_id, created_at)`,
			`CREATE TABLE audit_log (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL REFERENCES assistants(id), actor TEXT NOT NULL, action TEXT NOT NULL,
            target TEXT NOT NULL DEFAULT '', result TEXT NOT NULL, at TEXT NOT NULL)`,
			`CREATE INDEX audit_log_assistant_at ON audit_log(assistant_id, at)`,
		)
	}
	if version > 0 && version < 2 {
		statements = append(statements,
			`ALTER TABLE assistants ADD COLUMN unread_count INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE assistants ADD COLUMN last_message_preview TEXT NOT NULL DEFAULT ''`,
		)
	}
	if version > 0 && version < 3 {
		statements = append(statements,
			`ALTER TABLE tasks ADD COLUMN source_message_id TEXT NOT NULL DEFAULT ''`,
			`CREATE UNIQUE INDEX tasks_assistant_source_message ON tasks(assistant_id,source_message_id) WHERE source_message_id<>''`,
			`ALTER TABLE authorizations ADD COLUMN source_message_id TEXT NOT NULL DEFAULT ''`,
			`CREATE UNIQUE INDEX authorizations_pending_source_message ON authorizations(assistant_id,source_message_id) WHERE source_message_id<>'' AND status='pending'`,
		)
	}
	if version > 0 && version < 4 {
		statements = append(statements,
			`CREATE TABLE authorization_rules (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL REFERENCES assistants(id), action_type TEXT NOT NULL, device_id TEXT NOT NULL,
            cwd_root TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(assistant_id,action_type,device_id,cwd_root))`,
			`CREATE INDEX authorization_rules_assistant ON authorization_rules(assistant_id, created_at)`,
		)
	}
	if version > 0 && version < 5 {
		statements = append(statements, `ALTER TABLE conversation_messages ADD COLUMN reply_to TEXT NOT NULL DEFAULT ''`)
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 5`); err != nil {
		return err
	}
	return tx.Commit()
}

func nowText(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func nullTimeText(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return nowText(value)
}

func (s *Store) CreateAssistant(ctx context.Context, a Assistant) error {
	return insertAssistant(ctx, s.db, a)
}

type assistantInserter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertAssistant(ctx context.Context, target assistantInserter, a Assistant) error {
	now := nowText(time.Time{})
	_, err := target.ExecContext(ctx, `INSERT INTO assistants
		(id,name,avatar,description,persona,brain_device_id,brain_cwd,brain_model,brain_reasoning,brain_thread_id,brain_thread_history,toolset_version,allowed_scope,policy,limits,status,unread_count,last_message_preview,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Name, a.Avatar, a.Description, a.Persona, a.BrainDeviceID, a.BrainCWD, a.BrainModel, a.BrainReasoning, a.BrainThreadID, defaultJSON(a.BrainThreadHistory, "[]"), a.ToolsetVersion, defaultJSON(a.AllowedScope, "{}"), defaultJSON(a.Policy, "{}"), defaultJSON(a.Limits, "{}"), a.Status, a.UnreadCount, a.LastMessagePreview, now, now)
	return err
}

// CreateAssistantLimited counts active assistants and inserts the new row in
// one transaction so concurrent callers cannot oversubscribe the limit.
func (s *Store) CreateAssistantLimited(ctx context.Context, a Assistant, limit int) error {
	if limit <= 0 {
		return ErrAssistantLimitReached
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assistants WHERE deleted_at IS NULL`).Scan(&active); err != nil {
		return err
	}
	if active >= limit {
		return ErrAssistantLimitReached
	}
	if err := insertAssistant(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit()
}

func defaultJSON(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (s *Store) Assistant(ctx context.Context, id string) (Assistant, error) {
	var a Assistant
	err := s.db.QueryRowContext(ctx, `SELECT id,name,avatar,description,persona,brain_device_id,brain_cwd,brain_model,brain_reasoning,brain_thread_id,brain_thread_history,toolset_version,allowed_scope,policy,limits,status,unread_count,last_message_preview FROM assistants WHERE id=? AND deleted_at IS NULL`, id).Scan(&a.ID, &a.Name, &a.Avatar, &a.Description, &a.Persona, &a.BrainDeviceID, &a.BrainCWD, &a.BrainModel, &a.BrainReasoning, &a.BrainThreadID, &a.BrainThreadHistory, &a.ToolsetVersion, &a.AllowedScope, &a.Policy, &a.Limits, &a.Status, &a.UnreadCount, &a.LastMessagePreview)
	return a, err
}

func (s *Store) Assistants(ctx context.Context) ([]Assistant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,avatar,description,persona,brain_device_id,brain_cwd,brain_model,brain_reasoning,brain_thread_id,brain_thread_history,toolset_version,allowed_scope,policy,limits,status,unread_count,last_message_preview FROM assistants WHERE deleted_at IS NULL ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assistant
	for rows.Next() {
		var a Assistant
		if err := rows.Scan(&a.ID, &a.Name, &a.Avatar, &a.Description, &a.Persona, &a.BrainDeviceID, &a.BrainCWD, &a.BrainModel, &a.BrainReasoning, &a.BrainThreadID, &a.BrainThreadHistory, &a.ToolsetVersion, &a.AllowedScope, &a.Policy, &a.Limits, &a.Status, &a.UnreadCount, &a.LastMessagePreview); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SetAssistantBrain(ctx context.Context, assistantID, threadID, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE assistants SET brain_thread_id=?,status=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, threadID, status, nowText(time.Time{}), assistantID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateAssistant(ctx context.Context, a Assistant) error {
	result, err := s.db.ExecContext(ctx, `UPDATE assistants SET name=?,avatar=?,description=?,persona=?,brain_device_id=?,brain_thread_id=?,brain_thread_history=?,toolset_version=?,allowed_scope=?,policy=?,status=?,updated_at=? WHERE id=? AND deleted_at IS NULL`,
		a.Name, a.Avatar, a.Description, a.Persona, a.BrainDeviceID, a.BrainThreadID, defaultJSON(a.BrainThreadHistory, "[]"), a.ToolsetVersion, defaultJSON(a.AllowedScope, "{}"), defaultJSON(a.Policy, "{}"), a.Status, nowText(time.Time{}), a.ID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateMessageCard(ctx context.Context, assistantID, messageID, cardJSON string) error {
	if len(cardJSON) > 8<<10 {
		return errors.New("Puck card exceeds storage limit")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_messages SET card_json=? WHERE assistant_id=? AND id=?`, cardJSON, assistantID, messageID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpsertMessage(ctx context.Context, m Message) error {
	if len(m.Text) > 64<<10 || len(m.CardJSON) > 8<<10 {
		return errors.New("Puck message exceeds storage limit")
	}
	var previousKind string
	_ = s.db.QueryRowContext(ctx, `SELECT kind FROM conversation_messages WHERE assistant_id=? AND id=?`, m.AssistantID, m.ID).Scan(&previousKind)
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_messages(id,assistant_id,role,kind,text,card_json,reaction,work_summary,brain_turn_id,reply_to,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,text=excluded.text,card_json=excluded.card_json,reaction=CASE WHEN excluded.reaction='' THEN conversation_messages.reaction ELSE excluded.reaction END,work_summary=excluded.work_summary,brain_turn_id=excluded.brain_turn_id,reply_to=CASE WHEN excluded.reply_to='' THEN conversation_messages.reply_to ELSE excluded.reply_to END
		WHERE conversation_messages.assistant_id=excluded.assistant_id`, m.ID, m.AssistantID, m.Role, m.Kind, m.Text, m.CardJSON, m.Reaction, m.WorkSummary, m.BrainTurnID, m.ReplyTo, nowText(m.CreatedAt))
	if err == nil {
		err = s.updateAssistantMessageState(ctx, m, previousKind == "" || !messageCountsUnread(previousKind))
	}
	return err
}

func (s *Store) SoftDeleteAssistant(ctx context.Context, assistantID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowText(time.Time{})
	result, err := tx.ExecContext(ctx, `UPDATE assistants SET status='deleted',deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, assistantID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorizations SET status='rejected',decided_at=? WHERE assistant_id=? AND status='pending'`, now, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM authorization_rules WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AppendMessage(ctx context.Context, m Message) error {
	if len(m.Text) > 64<<10 || len(m.CardJSON) > 8<<10 {
		return errors.New("Puck message exceeds storage limit")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_messages(id,assistant_id,role,kind,text,card_json,reaction,work_summary,brain_turn_id,reply_to,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.AssistantID, m.Role, m.Kind, m.Text, m.CardJSON, m.Reaction, m.WorkSummary, m.BrainTurnID, m.ReplyTo, nowText(m.CreatedAt))
	if err == nil {
		err = s.updateAssistantMessageState(ctx, m, true)
	}
	return err
}

func messageCountsUnread(kind string) bool {
	return kind != "processing" && kind != "acknowledged" && kind != "greeting_processing"
}

func preview(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 120 {
		runes = runes[:120]
	}
	return string(runes)
}

func (s *Store) updateAssistantMessageState(ctx context.Context, m Message, canIncrement bool) error {
	if m.Text == "" {
		return nil
	}
	increment := 0
	if m.Role == "assistant" && canIncrement && messageCountsUnread(m.Kind) {
		increment = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE assistants SET last_message_preview=?,unread_count=unread_count+?,updated_at=? WHERE id=? AND deleted_at IS NULL`, preview(m.Text), increment, nowText(time.Time{}), m.AssistantID)
	return err
}

func (s *Store) MarkAssistantRead(ctx context.Context, assistantID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE assistants SET unread_count=0,updated_at=? WHERE id=? AND deleted_at IS NULL`, nowText(time.Time{}), assistantID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteMessage(ctx context.Context, assistantID, messageID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM conversation_messages WHERE assistant_id=? AND id=?`, assistantID, messageID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateMessageReaction(ctx context.Context, assistantID, messageID, actor, emoji string) (string, error) {
	if actor != "user" && actor != "assistant" {
		return "", errors.New("invalid reaction actor")
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT reaction FROM conversation_messages WHERE assistant_id=? AND id=?`, assistantID, messageID).Scan(&raw); err != nil {
		return "", err
	}
	reactions := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &reactions)
	if reactions[actor] == emoji {
		delete(reactions, actor)
	} else {
		reactions[actor] = emoji
	}
	encoded, _ := json.Marshal(reactions)
	_, err := s.db.ExecContext(ctx, `UPDATE conversation_messages SET reaction=? WHERE assistant_id=? AND id=?`, string(encoded), assistantID, messageID)
	return string(encoded), err
}

func (s *Store) Messages(ctx context.Context, assistantID string, limit int) ([]Message, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,assistant_id,role,kind,text,card_json,reaction,work_summary,brain_turn_id,reply_to,created_at FROM conversation_messages WHERE assistant_id=? ORDER BY created_at,id LIMIT ?`, assistantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var message Message
		var created string
		if err := rows.Scan(&message.ID, &message.AssistantID, &message.Role, &message.Kind, &message.Text, &message.CardJSON, &message.Reaction, &message.WorkSummary, &message.BrainTurnID, &message.ReplyTo, &created); err != nil {
			return nil, err
		}
		message.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) Message(ctx context.Context, assistantID, messageID string) (Message, error) {
	var message Message
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,assistant_id,role,kind,text,card_json,reaction,work_summary,brain_turn_id,reply_to,created_at FROM conversation_messages WHERE assistant_id=? AND id=?`, assistantID, messageID).Scan(
		&message.ID, &message.AssistantID, &message.Role, &message.Kind, &message.Text, &message.CardJSON, &message.Reaction, &message.WorkSummary, &message.BrainTurnID, &message.ReplyTo, &created,
	)
	if err == nil {
		message.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	}
	return message, err
}

func (s *Store) UpsertTask(ctx context.Context, task Task) error {
	now := nowText(time.Time{})
	_, err := s.db.ExecContext(ctx, `INSERT INTO tasks(task_id,assistant_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,source_message_id,last_seen_revision,last_summary,created_at,updated_at)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET status=excluded.status,last_turn_id=excluded.last_turn_id,last_seen_revision=excluded.last_seen_revision,last_summary=excluded.last_summary,updated_at=excluded.updated_at
        WHERE tasks.assistant_id=excluded.assistant_id`, task.ID, task.AssistantID, task.DeviceID, task.CWD, task.NativeThreadID, task.Goal, task.Status, defaultJSON(task.RiskTags, "[]"), task.CreatedBy, task.LastTurnID, task.SourceMessageID, task.LastSeenRevision, task.LastSummary, now, now)
	return err
}

func (s *Store) Task(ctx context.Context, assistantID, taskID string) (Task, error) {
	var task Task
	err := s.db.QueryRowContext(ctx, `SELECT task_id,assistant_id,source_message_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,last_seen_revision,last_summary FROM tasks WHERE assistant_id=? AND task_id=?`, assistantID, taskID).Scan(&task.ID, &task.AssistantID, &task.SourceMessageID, &task.DeviceID, &task.CWD, &task.NativeThreadID, &task.Goal, &task.Status, &task.RiskTags, &task.CreatedBy, &task.LastTurnID, &task.LastSeenRevision, &task.LastSummary)
	return task, err
}

func (s *Store) TaskBySourceMessage(ctx context.Context, assistantID, sourceMessageID string) (Task, error) {
	var task Task
	err := s.db.QueryRowContext(ctx, `SELECT task_id,assistant_id,source_message_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,last_seen_revision,last_summary FROM tasks WHERE assistant_id=? AND source_message_id=?`, assistantID, sourceMessageID).Scan(&task.ID, &task.AssistantID, &task.SourceMessageID, &task.DeviceID, &task.CWD, &task.NativeThreadID, &task.Goal, &task.Status, &task.RiskTags, &task.CreatedBy, &task.LastTurnID, &task.LastSeenRevision, &task.LastSummary)
	return task, err
}

func (s *Store) Tasks(ctx context.Context, assistantID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id,assistant_id,source_message_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,last_seen_revision,last_summary FROM tasks WHERE assistant_id=? ORDER BY created_at,task_id`, assistantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.AssistantID, &task.SourceMessageID, &task.DeviceID, &task.CWD, &task.NativeThreadID, &task.Goal, &task.Status, &task.RiskTags, &task.CreatedBy, &task.LastTurnID, &task.LastSeenRevision, &task.LastSummary); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) SetTaskStatus(ctx context.Context, assistantID, taskID, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?,updated_at=? WHERE assistant_id=? AND task_id=?`, status, nowText(time.Time{}), assistantID, taskID)
	return err
}

func (s *Store) InsertAuthorization(ctx context.Context, a Authorization) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO authorizations(id,assistant_id,action_json,action_hash,source_message_id,rules_hit,status,expires_at,decided_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, a.ID, a.AssistantID, a.ActionJSON, a.ActionHash, a.SourceMessageID, a.RulesHit, a.Status, nowText(a.ExpiresAt), nullTimeText(a.DecidedAt), nowText(time.Time{}))
	return err
}

func (s *Store) Authorization(ctx context.Context, assistantID, authorizationID string) (Authorization, error) {
	var a Authorization
	var expires string
	var decided sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,assistant_id,source_message_id,action_json,action_hash,rules_hit,status,expires_at,decided_at FROM authorizations WHERE assistant_id=? AND id=?`, assistantID, authorizationID).Scan(&a.ID, &a.AssistantID, &a.SourceMessageID, &a.ActionJSON, &a.ActionHash, &a.RulesHit, &a.Status, &expires, &decided)
	a.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	if decided.Valid {
		a.DecidedAt, _ = time.Parse(time.RFC3339Nano, decided.String)
	}
	return a, err
}

func (s *Store) UpsertAuthorizationRule(ctx context.Context, rule AuthorizationRule) (AuthorizationRule, error) {
	if rule.ID == "" {
		rule.ID = randomID("rule")
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO authorization_rules(id,assistant_id,action_type,device_id,cwd_root,created_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(assistant_id,action_type,device_id,cwd_root) DO NOTHING`, rule.ID, rule.AssistantID, rule.ActionType, rule.DeviceID, rule.CWD, nowText(rule.CreatedAt))
	if err != nil {
		return AuthorizationRule{}, err
	}
	var created string
	err = s.db.QueryRowContext(ctx, `SELECT id,assistant_id,action_type,device_id,cwd_root,created_at FROM authorization_rules WHERE assistant_id=? AND action_type=? AND device_id=? AND cwd_root=?`, rule.AssistantID, rule.ActionType, rule.DeviceID, rule.CWD).Scan(&rule.ID, &rule.AssistantID, &rule.ActionType, &rule.DeviceID, &rule.CWD, &created)
	rule.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return rule, err
}

func (s *Store) AuthorizationRules(ctx context.Context, assistantID string) ([]AuthorizationRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,assistant_id,action_type,device_id,cwd_root,created_at FROM authorization_rules WHERE assistant_id=? ORDER BY created_at,id`, assistantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []AuthorizationRule
	for rows.Next() {
		var rule AuthorizationRule
		var created string
		if err := rows.Scan(&rule.ID, &rule.AssistantID, &rule.ActionType, &rule.DeviceID, &rule.CWD, &created); err != nil {
			return nil, err
		}
		rule.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) MatchAuthorizationRule(ctx context.Context, assistantID, actionType, deviceID, cwd string) (*AuthorizationRule, error) {
	rules, err := s.AuthorizationRules(ctx, assistantID)
	if err != nil {
		return nil, err
	}
	cwd = filepath.Clean(cwd)
	for _, rule := range rules {
		if rule.ActionType != actionType || rule.DeviceID != deviceID {
			continue
		}
		rel, err := filepath.Rel(rule.CWD, cwd)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			matched := rule
			return &matched, nil
		}
	}
	return nil, nil
}

func (s *Store) RevokeAuthorizationRule(ctx context.Context, assistantID, ruleID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM authorization_rules WHERE assistant_id=? AND id=?`, assistantID, ruleID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) LatestUserMessageID(ctx context.Context, assistantID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM conversation_messages WHERE assistant_id=? AND role='user' ORDER BY created_at DESC,rowid DESC LIMIT 1`, assistantID).Scan(&id)
	return id, err
}

func (s *Store) Audit(ctx context.Context, entry AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(id,assistant_id,actor,action,target,result,at) VALUES(?,?,?,?,?,?,?)`, entry.ID, entry.AssistantID, entry.Actor, entry.Action, entry.Target, entry.Result, nowText(entry.At))
	return err
}
