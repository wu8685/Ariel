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
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const storeSchemaVersion = 11

type Store struct {
	db       *sql.DB
	notifyMu sync.Mutex
	notify   chan struct{}
	revision atomic.Uint64
}

var ErrAssistantLimitReached = errors.New("active assistant limit reached")

type Assistant struct {
	ID, Name, Avatar, Description, Persona, CustomContext           string
	BrainDeviceID, BrainCWD, BrainModel, BrainReasoning             string
	BrainThreadID, BrainThreadHistory, AllowedScope, Policy, Limits string
	Status                                                          string
	ToolsetVersion                                                  int
	UnreadCount                                                     int
	LastMessagePreview                                              string
	LastActivityAt                                                  time.Time
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
	UpdatedAt                                                                                                                                     time.Time `json:"-"`
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
	store := &Store{db: db, notify: make(chan struct{})}
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

func (s *Store) watchState() (uint64, <-chan struct{}) {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	return s.revision.Load(), s.notify
}

func (s *Store) markChanged() {
	s.notifyMu.Lock()
	s.revision.Add(1)
	close(s.notify)
	s.notify = make(chan struct{})
	s.notifyMu.Unlock()
}

func (s *Store) changed(err error) error {
	if err == nil {
		s.markChanged()
	}
	return err
}

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
	if version < 6 {
		statements = append(statements,
			`CREATE TABLE IF NOT EXISTS groups (
            id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL, deleted_at TEXT)`,
			`CREATE TABLE IF NOT EXISTS group_members (
            group_id TEXT NOT NULL REFERENCES groups(id), assistant_id TEXT NOT NULL REFERENCES assistants(id),
            position INTEGER NOT NULL, PRIMARY KEY (group_id, assistant_id))`,
			`CREATE INDEX IF NOT EXISTS group_members_assistant ON group_members(assistant_id)`,
			`CREATE TABLE IF NOT EXISTS group_messages (
            id TEXT PRIMARY KEY, group_id TEXT NOT NULL REFERENCES groups(id), role TEXT NOT NULL,
            author_assistant_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS group_messages_group_at ON group_messages(group_id, created_at)`,
		)
	}
	if version < 7 {
		statements = append(statements,
			`CREATE TABLE IF NOT EXISTS sidebar_sections (
            id TEXT PRIMARY KEY, name TEXT NOT NULL, position INTEGER NOT NULL, collapsed INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS sidebar_placements (
            kind TEXT NOT NULL, entry_id TEXT NOT NULL, section_id TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL, PRIMARY KEY (kind, entry_id))`,
			`CREATE INDEX IF NOT EXISTS sidebar_placements_section ON sidebar_placements(section_id, position)`,
			`CREATE TABLE IF NOT EXISTS sidebar_state (
            id TEXT PRIMARY KEY, collapsed INTEGER NOT NULL DEFAULT 0)`,
		)
	}
	if version < 8 {
		statements = append(statements,
			`CREATE TABLE IF NOT EXISTS thread_watches (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, device_id TEXT NOT NULL, thread_id TEXT NOT NULL,
            status TEXT NOT NULL, last_turn_id TEXT NOT NULL DEFAULT '', last_summary TEXT NOT NULL DEFAULT '',
            notified_key TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
            UNIQUE(assistant_id, device_id, thread_id))`,
			`CREATE INDEX IF NOT EXISTS thread_watches_status ON thread_watches(status, device_id)`,
			`CREATE TABLE IF NOT EXISTS brain_wakes (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, task_key TEXT NOT NULL, dedupe_key TEXT NOT NULL,
            status TEXT NOT NULL, summary TEXT NOT NULL, watch_only INTEGER NOT NULL DEFAULT 0,
            not_before TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(assistant_id, task_key))`,
			`CREATE TABLE IF NOT EXISTS wake_log (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, task_key TEXT NOT NULL, at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS wake_log_assistant_at ON wake_log(assistant_id, at)`,
		)
	}
	if version < 9 {
		statements = append(statements, `ALTER TABLE thread_watches ADD COLUMN until_text TEXT NOT NULL DEFAULT ''`)
	}
	if version < 10 {
		statements = append(statements, `ALTER TABLE assistants ADD COLUMN custom_context TEXT NOT NULL DEFAULT ''`)
	}
	if version < 11 {
		statements = append(statements,
			`CREATE TABLE IF NOT EXISTS bot_tasks (
            id TEXT PRIMARY KEY, assistant_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', goal TEXT NOT NULL DEFAULT '',
            status TEXT NOT NULL, merged_into TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS bot_tasks_assistant_status ON bot_tasks(assistant_id, status, created_at)`,
			`CREATE TABLE IF NOT EXISTS bot_task_messages (
            bot_task_id TEXT NOT NULL, assistant_id TEXT NOT NULL, message_id TEXT NOT NULL, created_at TEXT NOT NULL,
            PRIMARY KEY (bot_task_id, message_id))`,
			`CREATE INDEX IF NOT EXISTS bot_task_messages_assistant ON bot_task_messages(assistant_id, message_id)`,
			`CREATE TABLE IF NOT EXISTS bot_task_sessions (
            bot_task_id TEXT NOT NULL, assistant_id TEXT NOT NULL, kind TEXT NOT NULL, ref_id TEXT NOT NULL,
            device_id TEXT NOT NULL DEFAULT '', thread_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
            PRIMARY KEY (bot_task_id, kind, ref_id))`,
			`CREATE INDEX IF NOT EXISTS bot_task_sessions_thread ON bot_task_sessions(assistant_id, device_id, thread_id)`,
			`CREATE TABLE IF NOT EXISTS bot_inbox (
            assistant_id TEXT NOT NULL, message_id TEXT NOT NULL, created_at TEXT NOT NULL,
            PRIMARY KEY (assistant_id, message_id))`,
		)
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, storeSchemaVersion)); err != nil {
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
	return s.changed(insertAssistant(ctx, s.db, a))
}

type assistantInserter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertAssistant(ctx context.Context, target assistantInserter, a Assistant) error {
	now := nowText(time.Time{})
	_, err := target.ExecContext(ctx, `INSERT INTO assistants
		(id,name,avatar,description,persona,custom_context,brain_device_id,brain_cwd,brain_model,brain_reasoning,brain_thread_id,brain_thread_history,toolset_version,allowed_scope,policy,limits,status,unread_count,last_message_preview,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Name, a.Avatar, a.Description, a.Persona, a.CustomContext, a.BrainDeviceID, a.BrainCWD, a.BrainModel, a.BrainReasoning, a.BrainThreadID, defaultJSON(a.BrainThreadHistory, "[]"), a.ToolsetVersion, defaultJSON(a.AllowedScope, "{}"), defaultJSON(a.Policy, "{}"), defaultJSON(a.Limits, "{}"), a.Status, a.UnreadCount, a.LastMessagePreview, now, now)
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
	return s.changed(tx.Commit())
}

func defaultJSON(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (s *Store) Assistant(ctx context.Context, id string) (Assistant, error) {
	var a Assistant
	err := s.db.QueryRowContext(ctx, `SELECT id,name,avatar,description,persona,custom_context,brain_device_id,brain_cwd,brain_model,brain_reasoning,brain_thread_id,brain_thread_history,toolset_version,allowed_scope,policy,limits,status,unread_count,last_message_preview FROM assistants WHERE id=? AND deleted_at IS NULL`, id).Scan(&a.ID, &a.Name, &a.Avatar, &a.Description, &a.Persona, &a.CustomContext, &a.BrainDeviceID, &a.BrainCWD, &a.BrainModel, &a.BrainReasoning, &a.BrainThreadID, &a.BrainThreadHistory, &a.ToolsetVersion, &a.AllowedScope, &a.Policy, &a.Limits, &a.Status, &a.UnreadCount, &a.LastMessagePreview)
	return a, err
}

func (s *Store) Assistants(ctx context.Context) ([]Assistant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,avatar,description,persona,custom_context,brain_device_id,brain_cwd,brain_model,brain_reasoning,brain_thread_id,brain_thread_history,toolset_version,allowed_scope,policy,limits,status,unread_count,last_message_preview,COALESCE((SELECT created_at FROM conversation_messages WHERE assistant_id=assistants.id ORDER BY created_at DESC,rowid DESC LIMIT 1), updated_at) FROM assistants WHERE deleted_at IS NULL ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assistant
	for rows.Next() {
		var a Assistant
		var activity string
		if err := rows.Scan(&a.ID, &a.Name, &a.Avatar, &a.Description, &a.Persona, &a.CustomContext, &a.BrainDeviceID, &a.BrainCWD, &a.BrainModel, &a.BrainReasoning, &a.BrainThreadID, &a.BrainThreadHistory, &a.ToolsetVersion, &a.AllowedScope, &a.Policy, &a.Limits, &a.Status, &a.UnreadCount, &a.LastMessagePreview, &activity); err != nil {
			return nil, err
		}
		a.LastActivityAt, _ = time.Parse(time.RFC3339Nano, activity)
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
	s.markChanged()
	return nil
}

func (s *Store) UpdateAssistant(ctx context.Context, a Assistant) error {
	result, err := s.db.ExecContext(ctx, `UPDATE assistants SET name=?,avatar=?,description=?,persona=?,custom_context=?,brain_device_id=?,brain_thread_id=?,brain_thread_history=?,toolset_version=?,allowed_scope=?,policy=?,status=?,updated_at=? WHERE id=? AND deleted_at IS NULL`,
		a.Name, a.Avatar, a.Description, a.Persona, a.CustomContext, a.BrainDeviceID, a.BrainThreadID, defaultJSON(a.BrainThreadHistory, "[]"), a.ToolsetVersion, defaultJSON(a.AllowedScope, "{}"), defaultJSON(a.Policy, "{}"), a.Status, nowText(time.Time{}), a.ID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	s.markChanged()
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
	s.markChanged()
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
	return s.changed(err)
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_watches WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM brain_wakes WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM wake_log WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_task_messages WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_task_sessions WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_tasks WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_inbox WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE assistant_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sidebar_placements WHERE kind='assistant' AND entry_id=?`, assistantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE assistants SET custom_context='', brain_thread_id='', brain_thread_history='[]', toolset_version=0, last_message_preview='', unread_count=0 WHERE id=?`, assistantID); err != nil {
		return err
	}
	return s.changed(tx.Commit())
}

func (s *Store) AppendMessage(ctx context.Context, m Message) error {
	if len(m.Text) > 64<<10 || len(m.CardJSON) > 8<<10 {
		return errors.New("Puck message exceeds storage limit")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_messages(id,assistant_id,role,kind,text,card_json,reaction,work_summary,brain_turn_id,reply_to,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.AssistantID, m.Role, m.Kind, m.Text, m.CardJSON, m.Reaction, m.WorkSummary, m.BrainTurnID, m.ReplyTo, nowText(m.CreatedAt))
	if err == nil {
		err = s.updateAssistantMessageState(ctx, m, true)
	}
	return s.changed(err)
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
	s.markChanged()
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
	s.markChanged()
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
	if err == nil {
		s.markChanged()
	}
	return string(encoded), err
}

func (s *Store) Messages(ctx context.Context, assistantID string, limit int) ([]Message, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,assistant_id,role,kind,text,card_json,reaction,work_summary,brain_turn_id,reply_to,created_at FROM conversation_messages WHERE assistant_id=? ORDER BY created_at DESC, id DESC LIMIT ?`, assistantID, limit)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The query keeps the newest page. Callers still receive it oldest-first.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
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
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET device_id=excluded.device_id,cwd=excluded.cwd,native_thread_id=excluded.native_thread_id,goal=excluded.goal,status=excluded.status,last_turn_id=CASE WHEN excluded.last_turn_id='' THEN tasks.last_turn_id ELSE excluded.last_turn_id END,source_message_id=excluded.source_message_id,last_seen_revision=CASE WHEN excluded.last_seen_revision='' THEN tasks.last_seen_revision ELSE excluded.last_seen_revision END,last_summary=CASE WHEN excluded.last_summary='' THEN tasks.last_summary ELSE excluded.last_summary END,updated_at=excluded.updated_at
        WHERE tasks.assistant_id=excluded.assistant_id`, task.ID, task.AssistantID, task.DeviceID, task.CWD, task.NativeThreadID, task.Goal, task.Status, defaultJSON(task.RiskTags, "[]"), task.CreatedBy, task.LastTurnID, task.SourceMessageID, task.LastSeenRevision, task.LastSummary, now, now)
	return s.changed(err)
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

func (s *Store) TasksByActivity(ctx context.Context, assistantID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id,assistant_id,source_message_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,last_seen_revision,last_summary,updated_at FROM tasks WHERE assistant_id=? ORDER BY updated_at DESC, task_id DESC`, assistantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var task Task
		var updated string
		if err := rows.Scan(&task.ID, &task.AssistantID, &task.SourceMessageID, &task.DeviceID, &task.CWD, &task.NativeThreadID, &task.Goal, &task.Status, &task.RiskTags, &task.CreatedBy, &task.LastTurnID, &task.LastSeenRevision, &task.LastSummary, &updated); err != nil {
			return nil, err
		}
		task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
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
	return s.changed(err)
}

func (s *Store) InsertAuthorization(ctx context.Context, a Authorization) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO authorizations(id,assistant_id,action_json,action_hash,source_message_id,rules_hit,status,expires_at,decided_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, a.ID, a.AssistantID, a.ActionJSON, a.ActionHash, a.SourceMessageID, a.RulesHit, a.Status, nowText(a.ExpiresAt), nullTimeText(a.DecidedAt), nowText(time.Time{}))
	return s.changed(err)
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
	if err == nil {
		s.markChanged()
	}
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
	s.markChanged()
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

type Group struct {
	ID                 string        `json:"groupId"`
	Name               string        `json:"name"`
	Members            []GroupMember `json:"members"`
	LastMessagePreview string        `json:"lastMessagePreview,omitempty"`
	LastActivityAt     time.Time     `json:"lastActivityAt,omitempty"`
}

type GroupMember struct {
	AssistantID string `json:"assistantId"`
	Name        string `json:"name"`
}

type GroupMessage struct {
	ID                string    `json:"id"`
	GroupID           string    `json:"groupId"`
	Role              string    `json:"role"`
	AuthorAssistantID string    `json:"authorAssistantId,omitempty"`
	AuthorName        string    `json:"authorName,omitempty"`
	Kind              string    `json:"kind"`
	Text              string    `json:"text"`
	CreatedAt         time.Time `json:"createdAt"`
}

func (s *Store) CountGroups(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE deleted_at IS NULL`).Scan(&count)
	return count, err
}

func (s *Store) CreateGroup(ctx context.Context, group Group) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowText(time.Time{})
	if _, err := tx.ExecContext(ctx, `INSERT INTO groups(id,name,created_at) VALUES(?,?,?)`, group.ID, group.Name, now); err != nil {
		return err
	}
	for index, member := range group.Members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO group_members(group_id,assistant_id,position) VALUES(?,?,?)`, group.ID, member.AssistantID, index); err != nil {
			return err
		}
	}
	return s.changed(tx.Commit())
}

func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name FROM groups WHERE deleted_at IS NULL ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []Group{}
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range groups {
		members, err := s.groupMembers(ctx, groups[index].ID)
		if err != nil {
			return nil, err
		}
		groups[index].Members = members
		var preview, created string
		err = s.db.QueryRowContext(ctx, `SELECT text, created_at FROM group_messages WHERE group_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, groups[index].ID).Scan(&preview, &created)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		groups[index].LastMessagePreview = previewText(preview)
		if created != "" {
			groups[index].LastActivityAt, _ = time.Parse(time.RFC3339Nano, created)
		}
	}
	return groups, nil
}

func previewText(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return value
}

func (s *Store) Group(ctx context.Context, groupID string) (Group, error) {
	var group Group
	err := s.db.QueryRowContext(ctx, `SELECT id,name FROM groups WHERE id=? AND deleted_at IS NULL`, groupID).Scan(&group.ID, &group.Name)
	if err != nil {
		return Group{}, err
	}
	group.Members, err = s.groupMembers(ctx, groupID)
	return group, err
}

func (s *Store) groupMembers(ctx context.Context, groupID string) ([]GroupMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.assistant_id,a.name FROM group_members m JOIN assistants a ON a.id=m.assistant_id WHERE m.group_id=? AND a.deleted_at IS NULL ORDER BY m.position,m.assistant_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []GroupMember{}
	for rows.Next() {
		var member GroupMember
		if err := rows.Scan(&member.AssistantID, &member.Name); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *Store) AppendGroupMessage(ctx context.Context, m GroupMessage) error {
	if len(m.Text) > 64<<10 {
		return errors.New("Puck group message exceeds storage limit")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_messages(id,group_id,role,author_assistant_id,kind,text,created_at) VALUES(?,?,?,?,?,?,?)`, m.ID, m.GroupID, m.Role, m.AuthorAssistantID, m.Kind, m.Text, nowText(m.CreatedAt))
	return s.changed(err)
}

func (s *Store) UpsertGroupMessage(ctx context.Context, m GroupMessage) error {
	if len(m.Text) > 64<<10 {
		return errors.New("Puck group message exceeds storage limit")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_messages(id,group_id,role,author_assistant_id,kind,text,created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,text=excluded.text
		WHERE group_messages.group_id=excluded.group_id`, m.ID, m.GroupID, m.Role, m.AuthorAssistantID, m.Kind, m.Text, nowText(m.CreatedAt))
	return s.changed(err)
}

func (s *Store) DeleteGroupMessage(ctx context.Context, groupID, messageID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM group_messages WHERE group_id=? AND id=?`, groupID, messageID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return sql.ErrNoRows
	}
	s.markChanged()
	return nil
}

func (s *Store) GroupMessage(ctx context.Context, groupID, messageID string) (GroupMessage, error) {
	var message GroupMessage
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,group_id,role,author_assistant_id,kind,text,created_at FROM group_messages WHERE group_id=? AND id=?`, groupID, messageID).Scan(&message.ID, &message.GroupID, &message.Role, &message.AuthorAssistantID, &message.Kind, &message.Text, &created)
	if err != nil {
		return GroupMessage{}, err
	}
	message.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return message, nil
}

func (s *Store) GroupMessages(ctx context.Context, groupID string) ([]GroupMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,group_id,role,author_assistant_id,kind,text,created_at FROM group_messages WHERE group_id=? ORDER BY created_at,rowid LIMIT 200`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []GroupMessage{}
	for rows.Next() {
		var message GroupMessage
		var created string
		if err := rows.Scan(&message.ID, &message.GroupID, &message.Role, &message.AuthorAssistantID, &message.Kind, &message.Text, &created); err != nil {
			return nil, err
		}
		message.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

type SidebarSection struct {
	ID        string `json:"sectionId"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	Collapsed bool   `json:"collapsed"`
}

type SidebarPlacement struct {
	Kind      string `json:"kind"`
	EntryID   string `json:"entryId"`
	SectionID string `json:"sectionId"`
	Position  int    `json:"position"`
}

type SidebarView struct {
	Sections           []SidebarSection   `json:"sections"`
	Placements         []SidebarPlacement `json:"placements"`
	UngroupedCollapsed bool               `json:"ungroupedCollapsed"`
}

func (s *Store) Sidebar(ctx context.Context) (SidebarView, error) {
	view := SidebarView{Sections: []SidebarSection{}, Placements: []SidebarPlacement{}}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,position,collapsed FROM sidebar_sections ORDER BY position,id`)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var section SidebarSection
		var collapsed int
		if err := rows.Scan(&section.ID, &section.Name, &section.Position, &collapsed); err != nil {
			return view, err
		}
		section.Collapsed = collapsed != 0
		view.Sections = append(view.Sections, section)
	}
	if err := rows.Err(); err != nil {
		return view, err
	}
	rows.Close()
	placements, err := s.db.QueryContext(ctx, `SELECT kind,entry_id,section_id,position FROM sidebar_placements ORDER BY position,entry_id`)
	if err != nil {
		return view, err
	}
	defer placements.Close()
	for placements.Next() {
		var placement SidebarPlacement
		if err := placements.Scan(&placement.Kind, &placement.EntryID, &placement.SectionID, &placement.Position); err != nil {
			return view, err
		}
		view.Placements = append(view.Placements, placement)
	}
	if err := placements.Err(); err != nil {
		return view, err
	}
	var collapsed int
	err = s.db.QueryRowContext(ctx, `SELECT collapsed FROM sidebar_state WHERE id='ungrouped'`).Scan(&collapsed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return view, err
	}
	view.UngroupedCollapsed = collapsed != 0
	return view, nil
}

func (s *Store) CreateSidebarSection(ctx context.Context, name string) (SidebarSection, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SidebarSection{}, err
	}
	defer tx.Rollback()
	var count, position int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(position), -1) FROM sidebar_sections`).Scan(&count, &position); err != nil {
		return SidebarSection{}, err
	}
	if count >= 20 {
		return SidebarSection{}, ErrAssistantLimitReached
	}
	section := SidebarSection{ID: randomID("section"), Name: name, Position: position + 1}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sidebar_sections(id,name,position,collapsed,created_at) VALUES(?,?,?,0,?)`, section.ID, section.Name, section.Position, nowText(time.Time{})); err != nil {
		return SidebarSection{}, err
	}
	return section, s.changed(tx.Commit())
}

func (s *Store) RenameSidebarSection(ctx context.Context, sectionID, name string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sidebar_sections SET name=? WHERE id=?`, name, sectionID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	s.markChanged()
	return nil
}

func (s *Store) DeleteSidebarSection(ctx context.Context, sectionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM sidebar_sections WHERE id=?`, sectionID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sidebar_placements SET section_id='' WHERE section_id=?`, sectionID); err != nil {
		return err
	}
	return s.changed(tx.Commit())
}

func (s *Store) CollapseSidebar(ctx context.Context, sectionID string, collapsed bool) error {
	flag := 0
	if collapsed {
		flag = 1
	}
	if sectionID == "" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO sidebar_state(id,collapsed) VALUES('ungrouped',?) ON CONFLICT(id) DO UPDATE SET collapsed=excluded.collapsed`, flag)
		return s.changed(err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sidebar_sections SET collapsed=? WHERE id=?`, flag, sectionID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	s.markChanged()
	return nil
}

func (s *Store) MoveSidebarEntry(ctx context.Context, kind, entryID, sectionID string, position int) error {
	if kind != "assistant" && kind != "group" {
		return errors.New("invalid sidebar entry")
	}
	if sectionID != "" {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sidebar_sections WHERE id=?`, sectionID).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return sql.ErrNoRows
		}
	}
	var exists int
	query := `SELECT COUNT(*) FROM groups WHERE id=? AND deleted_at IS NULL`
	if kind == "assistant" {
		query = `SELECT COUNT(*) FROM assistants WHERE id=? AND deleted_at IS NULL`
	}
	if err := s.db.QueryRowContext(ctx, query, entryID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return sql.ErrNoRows
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sidebar_placements(kind,entry_id,section_id,position) VALUES(?,?,?,?)
		ON CONFLICT(kind,entry_id) DO UPDATE SET section_id=excluded.section_id, position=excluded.position`, kind, entryID, sectionID, position)
	return s.changed(err)
}
