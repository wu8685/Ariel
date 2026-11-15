package puck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	memoryProfileCap   = 16
	memoryProfileRunes = 2400
	memoryJournalCap   = 2000
	memoryTextRunes    = 300
)

type Memory struct {
	ID, Scope, AssistantID, WriterName, Kind, Text, DisplayName string
	CreatedAt                                                   time.Time
}

type MemoryPage struct {
	Entries    []Memory
	NextCursor string
}

type MemoryFailure struct {
	Code, Message, Text string
}

func (e *MemoryFailure) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *MemoryFailure) MemoryCode() (string, string, string) {
	if e == nil {
		return "", "", ""
	}
	return e.Code, e.Message, e.Text
}

type sqlContext interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func memorySchemaSQL() []string {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS bot_memories (
            id TEXT PRIMARY KEY,
            scope TEXT NOT NULL CHECK (scope IN ('bot', 'shared')),
            assistant_id TEXT NOT NULL,
            writer_name TEXT NOT NULL,
            kind TEXT NOT NULL CHECK (kind IN ('profile', 'journal')),
            text TEXT NOT NULL,
            created_at TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS bot_memories_bot_text ON bot_memories(assistant_id, kind, text) WHERE scope = 'bot'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS bot_memories_shared_text ON bot_memories(kind, text) WHERE scope = 'shared'`,
		`CREATE INDEX IF NOT EXISTS bot_memories_bot_recent ON bot_memories(assistant_id, kind, created_at) WHERE scope = 'bot'`,
		`CREATE INDEX IF NOT EXISTS bot_memories_shared_recent ON bot_memories(kind, created_at) WHERE scope = 'shared'`,
	}
	return append(statements, memoryFTSSQL()...)
}

func memoryFTSSQL() []string {
	return []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS bot_memories_fts USING fts5(text, content='bot_memories', content_rowid='rowid', tokenize="trigram case_sensitive 0")`,
		`CREATE TRIGGER IF NOT EXISTS bot_memories_ai AFTER INSERT ON bot_memories BEGIN
            INSERT INTO bot_memories_fts(rowid, text) VALUES (new.rowid, new.text);
        END`,
		`CREATE TRIGGER IF NOT EXISTS bot_memories_ad AFTER DELETE ON bot_memories BEGIN
            INSERT INTO bot_memories_fts(bot_memories_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
        END`,
	}
}

func probeMemoryCapabilities(ctx context.Context, tx sqlContext) error {
	if _, err := tx.ExecContext(ctx, `CREATE VIRTUAL TABLE temp.memory_trigram_probe USING fts5(text, tokenize="trigram case_sensitive 0")`); err != nil {
		return err
	}
	defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp.memory_trigram_probe`)
	if _, err := tx.ExecContext(ctx, `INSERT INTO temp.memory_trigram_probe(text) VALUES ('机主啊')`); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM temp.memory_trigram_probe WHERE memory_trigram_probe MATCH '机主啊'`).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("FTS5 trigram probe did not match")
	}
	return nil
}

func normalizeMemoryText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsRune(text, 0) || utf8.RuneCountInString(text) > memoryTextRunes {
		return "", &MemoryFailure{Code: "INVALID_ARGUMENT", Message: "text: value has invalid type or length; expected 1 to 300 characters without NUL"}
	}
	return text, nil
}

func (s *Store) WriteMemory(ctx context.Context, assistantID, writerName, scope, kind, text string) (Memory, bool, error) {
	text, err := normalizeMemoryText(text)
	if err != nil {
		return Memory{}, false, err
	}
	if scope != "bot" && scope != "shared" || (kind != "profile" && kind != "journal") || assistantID == "" {
		return Memory{}, false, &MemoryFailure{Code: "INVALID_ARGUMENT", Message: "scope or kind is invalid"}
	}
	writerName = strings.TrimSpace(writerName)
	if writerName == "" {
		writerName = "Bot"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, false, err
	}
	defer tx.Rollback()
	if existing, ok, err := findMemory(ctx, tx, assistantID, scope, kind, text); err != nil {
		return Memory{}, false, err
	} else if ok {
		if err := tx.Commit(); err != nil {
			return Memory{}, false, err
		}
		existing.DisplayName = s.memoryDisplayName(ctx, existing.AssistantID, existing.WriterName)
		return existing, true, nil
	}
	if err := enforceMemoryCap(ctx, tx, assistantID, scope, kind, text); err != nil {
		return Memory{}, false, err
	}
	now := nowText(time.Time{})
	id := newUUID()
	owner := assistantID
	if _, err := tx.ExecContext(ctx, `INSERT INTO bot_memories(id, scope, assistant_id, writer_name, kind, text, created_at) VALUES (?,?,?,?,?,?,?)`, id, scope, owner, writerName, kind, text, now); err != nil {
		return Memory{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, false, err
	}
	saved, err := s.memoryByID(ctx, id)
	return saved, false, err
}

func (s *Store) ForgetMemory(ctx context.Context, assistantID, scope, kind, text string) (Memory, error) {
	text, err := normalizeMemoryText(text)
	if err != nil {
		return Memory{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback()
	existing, ok, err := findVisibleMemory(ctx, tx, assistantID, scope, kind, text)
	if err != nil {
		return Memory{}, err
	}
	if !ok {
		return Memory{}, &MemoryFailure{Code: "MEMORY_NOT_FOUND", Message: "没有找到这条原文", Text: text}
	}
	if err := deleteMemoryRow(ctx, tx, existing.ID); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, err
	}
	existing.DisplayName = s.memoryDisplayName(ctx, existing.AssistantID, existing.WriterName)
	return existing, nil
}

func (s *Store) ListMemories(ctx context.Context, assistantID, scope, kind string, offset, limit int) (MemoryPage, error) {
	if limit < 1 || limit > 50 {
		limit = 50
	}
	if offset < 0 {
		return MemoryPage{}, &MemoryFailure{Code: "INVALID_ARGUMENT", Message: "cursor: value is invalid; expected a non-negative offset"}
	}
	where, args := visibleMemoryWhere(assistantID, scope, kind)
	args = append(args, limit+1, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT m.id FROM bot_memories m WHERE `+where+` ORDER BY m.created_at DESC, m.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return MemoryPage{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return MemoryPage{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return MemoryPage{}, err
	}
	page := MemoryPage{}
	if len(ids) > limit {
		ids = ids[:limit]
		page.NextCursor = fmt.Sprintf("%d", offset+limit)
	}
	for _, id := range ids {
		entry, err := s.memoryByID(ctx, id)
		if err != nil {
			return MemoryPage{}, err
		}
		page.Entries = append(page.Entries, entry)
	}
	return page, nil
}

func (s *Store) DeleteVisibleMemory(ctx context.Context, assistantID, memoryID string) (Memory, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback()
	entry, err := memoryByID(ctx, tx, memoryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Memory{}, &MemoryFailure{Code: "PUCK_ASSISTANT_NOT_FOUND", Message: "memory not found"}
		}
		return Memory{}, err
	}
	if !memoryVisible(entry, assistantID) {
		return Memory{}, &MemoryFailure{Code: "PUCK_ASSISTANT_NOT_FOUND", Message: "memory not found"}
	}
	if err := deleteMemoryRow(ctx, tx, entry.ID); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, err
	}
	entry.DisplayName = s.memoryDisplayName(ctx, entry.AssistantID, entry.WriterName)
	return entry, nil
}

func (s *Store) InjectionMemories(ctx context.Context, assistantID string) (botProfile, sharedProfile, botJournal, sharedJournal []Memory, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.scope, m.assistant_id, m.writer_name, m.kind, m.text, m.created_at, COALESCE(NULLIF(a.name, ''), m.writer_name) FROM bot_memories m LEFT JOIN assistants a ON a.id = m.assistant_id WHERE (m.scope='bot' AND m.assistant_id=?) OR m.scope='shared' ORDER BY m.created_at, m.id`, assistantID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var all []Memory
	for rows.Next() {
		var entry Memory
		var created, display string
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.AssistantID, &entry.WriterName, &entry.Kind, &entry.Text, &created, &display); err != nil {
			rows.Close()
			return nil, nil, nil, nil, err
		}
		entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		entry.DisplayName = display
		all = append(all, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var botJ, sharedJ []Memory
	for _, entry := range all {
		switch {
		case entry.Scope == "bot" && entry.Kind == "profile":
			botProfile = append(botProfile, entry)
		case entry.Scope == "shared" && entry.Kind == "profile":
			sharedProfile = append(sharedProfile, entry)
		case entry.Scope == "bot" && entry.Kind == "journal":
			botJ = append(botJ, entry)
		case entry.Scope == "shared" && entry.Kind == "journal":
			sharedJ = append(sharedJ, entry)
		}
	}
	botJournal = newest(botJ, 8)
	sharedJournal = newest(sharedJ, 4)
	return botProfile, sharedProfile, botJournal, sharedJournal, nil
}

func newest(entries []Memory, limit int) []Memory {
	if len(entries) <= limit {
		out := append([]Memory(nil), entries...)
		reverseMemories(out)
		return out
	}
	out := append([]Memory(nil), entries[len(entries)-limit:]...)
	reverseMemories(out)
	return out
}

func reverseMemories(entries []Memory) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func (s *Store) SearchMemoryIDs(ctx context.Context, assistantID, scope, kind, query string, limit int) ([]string, error) {
	if limit < 1 {
		limit = 20
	}
	where, args := visibleMemoryWhere(assistantID, scope, kind)
	var ids []string
	if utf8.RuneCountInString(strings.TrimSpace(query)) >= 3 {
		match := ftsMatchQuery(query)
		ftsArgs := make([]any, 0, len(args)+2)
		ftsArgs = append(ftsArgs, match)
		ftsArgs = append(ftsArgs, args...)
		ftsArgs = append(ftsArgs, limit)
		rows, err := s.db.QueryContext(ctx, `SELECT m.id FROM bot_memories_fts f JOIN bot_memories m ON m.rowid = f.rowid WHERE f MATCH ? AND `+where+` ORDER BY bm25(f) LIMIT ?`, ftsArgs...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return nil, err
				}
				ids = append(ids, id)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return ids, nil
		}
	}
	pattern := likePattern(query)
	rows, err := s.db.QueryContext(ctx, `SELECT m.id FROM bot_memories m WHERE `+where+` AND m.text LIKE ? ESCAPE '\' ORDER BY m.created_at DESC, m.id DESC LIMIT ?`, append(args, pattern, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ExactMemoryIDs(ctx context.Context, assistantID, scope, kind, token string, limit int) ([]string, error) {
	where, args := visibleMemoryWhere(assistantID, scope, kind)
	pattern := likePattern(token)
	rows, err := s.db.QueryContext(ctx, `SELECT m.id FROM bot_memories m WHERE `+where+` AND lower(m.text) LIKE lower(?) ESCAPE '\' ORDER BY m.created_at DESC LIMIT ?`, append(args, pattern, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) SearchMessagesRecent(ctx context.Context, assistantID, query string, limit int) ([]Message, error) {
	if limit < 1 {
		limit = 8
	}
	pattern := likePattern(query)
	rows, err := s.db.QueryContext(ctx, `SELECT id, assistant_id, role, kind, text, card_json, reaction, work_summary, brain_turn_id, reply_to, created_at FROM conversation_messages WHERE assistant_id=? AND text LIKE ? ESCAPE '\' ORDER BY created_at DESC, id DESC LIMIT ?`, assistantID, pattern, limit)
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

func deletePrivateMemories(ctx context.Context, tx sqlContext, assistantID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM bot_memories WHERE scope='bot' AND assistant_id=?`, assistantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := deleteMemoryRow(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func deleteMemoryRow(ctx context.Context, tx sqlContext, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM bot_memories WHERE id=?`, id)
	return err
}

func findMemory(ctx context.Context, tx sqlContext, assistantID, scope, kind, text string) (Memory, bool, error) {
	query := memorySelect + ` WHERE m.scope=? AND m.kind=? AND m.text=?`
	args := []any{scope, kind, text}
	if scope == "bot" {
		query += ` AND m.assistant_id=?`
		args = append(args, assistantID)
	}
	entry, err := scanMemoryRow(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, err
	}
	return entry, true, nil
}

func findVisibleMemory(ctx context.Context, tx sqlContext, assistantID, scope, kind, text string) (Memory, bool, error) {
	entry, ok, err := findMemory(ctx, tx, assistantID, scope, kind, text)
	if err != nil || !ok {
		return entry, ok, err
	}
	if !memoryVisible(entry, assistantID) {
		return Memory{}, false, nil
	}
	return entry, true, nil
}

func memoryVisible(entry Memory, assistantID string) bool {
	if entry.Scope == "shared" {
		return true
	}
	return entry.Scope == "bot" && entry.AssistantID == assistantID
}

func enforceMemoryCap(ctx context.Context, tx sqlContext, assistantID, scope, kind, text string) error {
	where := `scope=? AND kind=?`
	args := []any{scope, kind}
	if scope == "bot" {
		where += ` AND assistant_id=?`
		args = append(args, assistantID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT text FROM bot_memories WHERE `+where, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	runes := 0
	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			return err
		}
		count++
		runes += utf8.RuneCountInString(existing)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if kind == "profile" {
		if count >= memoryProfileCap || runes+utf8.RuneCountInString(text) > memoryProfileRunes {
			return &MemoryFailure{Code: "MEMORY_PROFILE_FULL", Message: fmt.Sprintf("profile 已有 %d 条（上限 %d），原文共 %d 个字（上限 %d）。先 forget 再 write。", count, memoryProfileCap, runes, memoryProfileRunes)}
		}
		return nil
	}
	if count >= memoryJournalCap {
		return &MemoryFailure{Code: "MEMORY_JOURNAL_FULL", Message: fmt.Sprintf("journal 已有 %d 条（上限 %d）。先 forget 再 write。", count, memoryJournalCap)}
	}
	return nil
}

func visibleMemoryWhere(assistantID, scope, kind string) (string, []any) {
	where := `((m.scope='bot' AND m.assistant_id=?) OR m.scope='shared')`
	args := []any{assistantID}
	if scope == "bot" || scope == "shared" {
		where += ` AND m.scope=?`
		args = append(args, scope)
	}
	if kind == "profile" || kind == "journal" {
		where += ` AND m.kind=?`
		args = append(args, kind)
	}
	return where, args
}

func ftsMatchQuery(query string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(query), `"`, `""`) + `"`
}

const memorySelect = `SELECT m.id, m.scope, m.assistant_id, m.writer_name, m.kind, m.text, m.created_at FROM bot_memories m`

func (s *Store) memoryByID(ctx context.Context, id string) (Memory, error) {
	entry, err := memoryByID(ctx, s.db, id)
	if err != nil {
		return Memory{}, err
	}
	entry.DisplayName = s.memoryDisplayName(ctx, entry.AssistantID, entry.WriterName)
	return entry, nil
}

func memoryByID(ctx context.Context, q sqlContext, id string) (Memory, error) {
	return scanMemoryRow(q.QueryRowContext(ctx, memorySelect+` WHERE m.id=?`, id))
}

func (s *Store) memoryDisplayName(ctx context.Context, assistantID, writerName string) string {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM assistants WHERE id=?`, assistantID).Scan(&name)
	if err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	if strings.TrimSpace(writerName) == "" {
		return "Bot"
	}
	return writerName
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemoryRow(row memoryScanner) (Memory, error) {
	var entry Memory
	var created string
	if err := row.Scan(&entry.ID, &entry.Scope, &entry.AssistantID, &entry.WriterName, &entry.Kind, &entry.Text, &created); err != nil {
		return Memory{}, err
	}
	entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	entry.DisplayName = entry.WriterName
	return entry, nil
}
