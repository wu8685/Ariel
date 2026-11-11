package puck

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	botTaskAccepted    = "accepted"
	botTaskInProgress  = "in_progress"
	botTaskWaitingUser = "waiting_user"
	botTaskDone        = "done"
	botTaskCancelled   = "cancelled"
	botTaskMerged      = "merged"
)

var botTaskOpenStatuses = []string{botTaskAccepted, botTaskInProgress, botTaskWaitingUser}

type BotTask struct {
	ID, AssistantID, Title, Goal, Status, MergedInto string
	MessageIDs                                       []string
	Sessions                                         []BotTaskSession
	CreatedAt, UpdatedAt                             time.Time
}

type BotTaskSession struct {
	Kind, RefID, DeviceID, ThreadID string
}

type BotTaskPage struct {
	Tasks      []BotTask
	NextCursor string
}

type MessagePage struct {
	Messages   []Message
	NextCursor string
}

func validBotTaskStatus(status string) bool {
	switch status {
	case botTaskAccepted, botTaskInProgress, botTaskWaitingUser, botTaskDone, botTaskCancelled:
		return true
	default:
		return false
	}
}

func (s *Store) AddInbox(ctx context.Context, assistantID, messageID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO bot_inbox(assistant_id, message_id, created_at) VALUES(?,?,?) ON CONFLICT(assistant_id, message_id) DO NOTHING`, assistantID, messageID, nowText(at))
	return s.changed(err)
}

func (s *Store) Inbox(ctx context.Context, assistantID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT message_id FROM bot_inbox WHERE assistant_id=? ORDER BY created_at, message_id`, assistantID)
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

func (s *Store) OldestInbox(ctx context.Context, assistantID, skipMessageID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT message_id FROM bot_inbox WHERE assistant_id=? AND message_id<>? ORDER BY created_at, message_id LIMIT 1`, assistantID, skipMessageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) OldestOpenBotTaskID(ctx context.Context, assistantID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM bot_tasks WHERE assistant_id=? AND status IN ('accepted','in_progress','waiting_user') ORDER BY created_at, id LIMIT 1`, assistantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) SaveBotTask(ctx context.Context, task BotTask) (BotTask, error) {
	if task.AssistantID == "" || !validBotTaskStatus(task.Status) {
		return BotTask{}, errors.New("invalid bot task")
	}
	if strings.TrimSpace(task.Title) == "" && strings.TrimSpace(task.Goal) == "" {
		return BotTask{}, errors.New("title or goal is required")
	}
	now := nowText(time.Time{})
	if task.ID == "" {
		task.ID = randomID("btask")
		_, err := s.db.ExecContext(ctx, `INSERT INTO bot_tasks(id, assistant_id, title, goal, status, merged_into, created_at, updated_at) VALUES(?,?,?,?,?,'',?,?)`, task.ID, task.AssistantID, task.Title, task.Goal, task.Status, now, now)
		if err != nil {
			return BotTask{}, err
		}
		_ = s.changed(nil)
		return s.BotTask(ctx, task.AssistantID, task.ID)
	}
	existing, err := s.BotTask(ctx, task.AssistantID, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.ExecContext(ctx, `INSERT INTO bot_tasks(id, assistant_id, title, goal, status, merged_into, created_at, updated_at) VALUES(?,?,?,?,?,'',?,?)`, task.ID, task.AssistantID, task.Title, task.Goal, task.Status, now, now)
		if err != nil {
			return BotTask{}, err
		}
		_ = s.changed(nil)
		return s.BotTask(ctx, task.AssistantID, task.ID)
	}
	if err != nil {
		return BotTask{}, err
	}
	if existing.Status == botTaskMerged {
		return BotTask{}, errors.New("merged task cannot be updated")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE bot_tasks SET title=?, goal=?, status=?, updated_at=? WHERE assistant_id=? AND id=?`, task.Title, task.Goal, task.Status, now, task.AssistantID, task.ID)
	if err != nil {
		return BotTask{}, err
	}
	_ = s.changed(nil)
	return s.BotTask(ctx, task.AssistantID, task.ID)
}

func (s *Store) BotTask(ctx context.Context, assistantID, id string) (BotTask, error) {
	var task BotTask
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, assistant_id, title, goal, status, merged_into, created_at, updated_at FROM bot_tasks WHERE assistant_id=? AND id=?`, assistantID, id).Scan(&task.ID, &task.AssistantID, &task.Title, &task.Goal, &task.Status, &task.MergedInto, &created, &updated)
	if err != nil {
		return BotTask{}, err
	}
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	task.MessageIDs, err = s.botTaskMessageIDs(ctx, assistantID, id)
	if err != nil {
		return BotTask{}, err
	}
	task.Sessions, err = s.botTaskSessions(ctx, assistantID, id)
	return task, err
}

func (s *Store) InboxAssignment(ctx context.Context, assistantID, messageID string) (inInbox, attached bool, err error) {
	var inbox int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_inbox WHERE assistant_id=? AND message_id=?`, assistantID, messageID).Scan(&inbox); err != nil {
		return false, false, err
	}
	var links int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_task_messages WHERE assistant_id=? AND message_id=?`, assistantID, messageID).Scan(&links); err != nil {
		return false, false, err
	}
	return inbox > 0, links > 0, nil
}

func (s *Store) AssistantsForBotSession(ctx context.Context, deviceID, threadID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT assistant_id FROM bot_task_sessions WHERE device_id=? AND thread_id=? AND device_id<>'' AND thread_id<>''`, deviceID, threadID)
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

func (s *Store) RecentOpenBotTasks(ctx context.Context, assistantID string, limit int) ([]BotTask, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM bot_tasks WHERE assistant_id=? AND status IN ('accepted','in_progress','waiting_user') ORDER BY updated_at DESC, id LIMIT ?`, assistantID, limit)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tasks := make([]BotTask, 0, len(ids))
	for _, id := range ids {
		task, err := s.BotTask(ctx, assistantID, id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Store) ListBotTasks(ctx context.Context, assistantID, query, status string, offset, limit int) (BotTaskPage, error) {
	limit = pageLimit(limit)
	args := []any{assistantID}
	where := `assistant_id=?`
	if status != "" {
		where += ` AND status=?`
		args = append(args, status)
	}
	if strings.TrimSpace(query) != "" {
		where += ` AND (title LIKE ? ESCAPE '\' OR goal LIKE ? ESCAPE '\')`
		pattern := likePattern(query)
		args = append(args, pattern, pattern)
	}
	args = append(args, limit+1, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM bot_tasks WHERE `+where+` ORDER BY updated_at DESC, id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return BotTaskPage{}, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return BotTaskPage{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return BotTaskPage{}, err
	}
	page := BotTaskPage{}
	if len(ids) > limit {
		ids = ids[:limit]
		page.NextCursor = strconv.Itoa(offset + limit)
	}
	for _, id := range ids {
		task, err := s.BotTask(ctx, assistantID, id)
		if err != nil {
			return BotTaskPage{}, err
		}
		page.Tasks = append(page.Tasks, task)
	}
	return page, nil
}

func (s *Store) SearchMessages(ctx context.Context, assistantID, query string, offset, limit int) (MessagePage, error) {
	limit = pageLimit(limit)
	args := []any{assistantID}
	where := `assistant_id=?`
	if strings.TrimSpace(query) != "" {
		where += ` AND text LIKE ? ESCAPE '\'`
		args = append(args, likePattern(query))
	}
	args = append(args, limit+1, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, assistant_id, role, kind, text, card_json, reaction, work_summary, brain_turn_id, reply_to, created_at FROM conversation_messages WHERE `+where+` ORDER BY created_at, id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return MessagePage{}, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var message Message
		var created string
		if err := rows.Scan(&message.ID, &message.AssistantID, &message.Role, &message.Kind, &message.Text, &message.CardJSON, &message.Reaction, &message.WorkSummary, &message.BrainTurnID, &message.ReplyTo, &created); err != nil {
			return MessagePage{}, err
		}
		message.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{}
	if len(messages) > limit {
		messages = messages[:limit]
		page.NextCursor = strconv.Itoa(offset + limit)
	}
	page.Messages = messages
	return page, nil
}

func (s *Store) AttachBotTask(ctx context.Context, assistantID, taskID string, messageIDs []string, sessions []BotTaskSession) (BotTask, error) {
	task, err := s.BotTask(ctx, assistantID, taskID)
	if err != nil {
		return BotTask{}, err
	}
	if task.Status == botTaskMerged {
		return BotTask{}, errors.New("merged task cannot accept attachments")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BotTask{}, err
	}
	defer tx.Rollback()
	now := nowText(time.Time{})
	for _, messageID := range messageIDs {
		var role string
		if err := tx.QueryRowContext(ctx, `SELECT role FROM conversation_messages WHERE assistant_id=? AND id=?`, assistantID, messageID).Scan(&role); err != nil {
			return BotTask{}, err
		}
		if role != "user" {
			return BotTask{}, errors.New("only user messages can be attached")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_task_messages(bot_task_id, assistant_id, message_id, created_at) VALUES(?,?,?,?) ON CONFLICT(bot_task_id, message_id) DO NOTHING`, taskID, assistantID, messageID, now); err != nil {
			return BotTask{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM bot_inbox WHERE assistant_id=? AND message_id=?`, assistantID, messageID); err != nil {
			return BotTask{}, err
		}
	}
	for _, session := range sessions {
		switch session.Kind {
		case "task":
			var deviceID, threadID string
			if err := tx.QueryRowContext(ctx, `SELECT device_id, native_thread_id FROM tasks WHERE assistant_id=? AND task_id=?`, assistantID, session.RefID).Scan(&deviceID, &threadID); err != nil {
				return BotTask{}, err
			}
			session.DeviceID, session.ThreadID = deviceID, threadID
		case "watch":
			var watchID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM thread_watches WHERE assistant_id=? AND device_id=? AND thread_id=?`, assistantID, session.DeviceID, session.ThreadID).Scan(&watchID); err != nil {
				return BotTask{}, err
			}
			session.RefID = watchID
		default:
			return BotTask{}, errors.New("unknown session kind")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_task_sessions(bot_task_id, assistant_id, kind, ref_id, device_id, thread_id, created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(bot_task_id, kind, ref_id) DO NOTHING`, taskID, assistantID, session.Kind, session.RefID, session.DeviceID, session.ThreadID, now); err != nil {
			return BotTask{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return BotTask{}, err
	}
	s.markChanged()
	return s.BotTask(ctx, assistantID, taskID)
}

func (s *Store) MergeBotTasks(ctx context.Context, assistantID, fromID, intoID string) (BotTask, error) {
	if fromID == "" || intoID == "" || fromID == intoID {
		return BotTask{}, errors.New("merge needs two different tasks")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BotTask{}, err
	}
	defer tx.Rollback()
	var fromStatus, intoStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM bot_tasks WHERE assistant_id=? AND id=?`, assistantID, fromID).Scan(&fromStatus); err != nil {
		return BotTask{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT status FROM bot_tasks WHERE assistant_id=? AND id=?`, assistantID, intoID).Scan(&intoStatus); err != nil {
		return BotTask{}, err
	}
	if fromStatus == botTaskMerged || intoStatus == botTaskMerged {
		return BotTask{}, errors.New("merged task cannot be merged again")
	}
	now := nowText(time.Time{})
	if _, err := tx.ExecContext(ctx, `INSERT INTO bot_task_messages(bot_task_id, assistant_id, message_id, created_at)
		SELECT ?, assistant_id, message_id, ? FROM bot_task_messages WHERE assistant_id=? AND bot_task_id=?
		ON CONFLICT(bot_task_id, message_id) DO NOTHING`, intoID, now, assistantID, fromID); err != nil {
		return BotTask{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bot_task_sessions(bot_task_id, assistant_id, kind, ref_id, device_id, thread_id, created_at)
		SELECT ?, assistant_id, kind, ref_id, device_id, thread_id, ? FROM bot_task_sessions WHERE assistant_id=? AND bot_task_id=?
		ON CONFLICT(bot_task_id, kind, ref_id) DO NOTHING`, intoID, now, assistantID, fromID); err != nil {
		return BotTask{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bot_tasks SET status='merged', merged_into=?, updated_at=? WHERE assistant_id=? AND id=?`, intoID, now, assistantID, fromID); err != nil {
		return BotTask{}, err
	}
	if err := tx.Commit(); err != nil {
		return BotTask{}, err
	}
	s.markChanged()
	return s.BotTask(ctx, assistantID, fromID)
}

func (s *Store) botTaskMessageIDs(ctx context.Context, assistantID, taskID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT message_id FROM bot_task_messages WHERE assistant_id=? AND bot_task_id=? ORDER BY created_at, message_id`, assistantID, taskID)
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

func (s *Store) botTaskSessions(ctx context.Context, assistantID, taskID string) ([]BotTaskSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, ref_id, device_id, thread_id FROM bot_task_sessions WHERE assistant_id=? AND bot_task_id=? ORDER BY created_at, kind, ref_id`, assistantID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []BotTaskSession
	for rows.Next() {
		var session BotTaskSession
		if err := rows.Scan(&session.Kind, &session.RefID, &session.DeviceID, &session.ThreadID); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func pageLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func likePattern(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(strings.TrimSpace(query)) + "%"
}

func pageOffset(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return offset, nil
}
