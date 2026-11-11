package puck

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ThreadWatch struct {
	ID, AssistantID, DeviceID, ThreadID, Status, LastTurnID, LastSummary, NotifiedKey, Until string
}

type BrainWake struct {
	ID, AssistantID, TaskKey, DedupeKey, Status, Summary string
	WatchOnly                                            bool
	NotBefore                                            time.Time
	CreatedAt                                            time.Time
}

func taskWatchable(status string) bool {
	switch status {
	case "running", "dispatching", "review", "waiting_user", "attention", "reconciling", "stopping", "awaiting_authorization":
		return true
	default:
		return false
	}
}

func (s *Store) UpdateTaskObservation(ctx context.Context, assistantID, taskID, status, turnID, summary, notified string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, last_turn_id=CASE WHEN ?='' THEN last_turn_id ELSE ? END, last_summary=?, last_seen_revision=?, updated_at=? WHERE assistant_id=? AND task_id=?`,
		status, turnID, turnID, summary, notified, nowText(time.Time{}), assistantID, taskID)
	return s.changed(err)
}

func (s *Store) ActiveExecutionTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tasks.task_id,tasks.assistant_id,tasks.source_message_id,tasks.device_id,tasks.cwd,tasks.native_thread_id,tasks.goal,tasks.status,tasks.risk_tags,tasks.created_by,tasks.last_turn_id,tasks.last_seen_revision,tasks.last_summary
		FROM tasks JOIN assistants ON assistants.id=tasks.assistant_id
		WHERE assistants.deleted_at IS NULL AND tasks.native_thread_id<>'' AND tasks.status IN ('running','dispatching','review','waiting_user','attention','reconciling','stopping','awaiting_authorization')`)
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

func (s *Store) TasksForThread(ctx context.Context, deviceID, threadID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id,assistant_id,source_message_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,last_seen_revision,last_summary FROM tasks WHERE device_id=? AND native_thread_id=?`, deviceID, threadID)
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

func (s *Store) OwnedTaskForThread(ctx context.Context, assistantID, deviceID, threadID string) (Task, error) {
	var task Task
	err := s.db.QueryRowContext(ctx, `SELECT task_id,assistant_id,source_message_id,device_id,cwd,native_thread_id,goal,status,risk_tags,created_by,last_turn_id,last_seen_revision,last_summary FROM tasks WHERE assistant_id=? AND device_id=? AND native_thread_id=?`, assistantID, deviceID, threadID).Scan(
		&task.ID, &task.AssistantID, &task.SourceMessageID, &task.DeviceID, &task.CWD, &task.NativeThreadID, &task.Goal, &task.Status, &task.RiskTags, &task.CreatedBy, &task.LastTurnID, &task.LastSeenRevision, &task.LastSummary)
	return task, err
}

func (s *Store) BrainThreadMatch(ctx context.Context, threadID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM assistants WHERE brain_thread_id=? AND brain_thread_id<>''`, threadID).Scan(&found)
	return found > 0, err
}

func (s *Store) SaveThreadWatch(ctx context.Context, watch ThreadWatch) error {
	now := nowText(time.Time{})
	_, err := s.db.ExecContext(ctx, `INSERT INTO thread_watches(id,assistant_id,device_id,thread_id,status,last_turn_id,last_summary,notified_key,until_text,created_at,updated_at)
		VALUES(?,?,?,?, 'watching', '', '', '', ?, ?, ?)
		ON CONFLICT(assistant_id, device_id, thread_id) DO UPDATE SET status='watching', notified_key='', until_text=CASE WHEN excluded.until_text='' THEN thread_watches.until_text ELSE excluded.until_text END, updated_at=excluded.updated_at`,
		watch.ID, watch.AssistantID, watch.DeviceID, watch.ThreadID, watch.Until, now, now)
	return s.changed(err)
}

func (s *Store) ThreadWatch(ctx context.Context, assistantID, deviceID, threadID string) (ThreadWatch, error) {
	var watch ThreadWatch
	err := s.db.QueryRowContext(ctx, `SELECT id,assistant_id,device_id,thread_id,status,last_turn_id,last_summary,notified_key,until_text FROM thread_watches WHERE assistant_id=? AND device_id=? AND thread_id=?`, assistantID, deviceID, threadID).Scan(
		&watch.ID, &watch.AssistantID, &watch.DeviceID, &watch.ThreadID, &watch.Status, &watch.LastTurnID, &watch.LastSummary, &watch.NotifiedKey, &watch.Until)
	return watch, err
}

func (s *Store) ActiveThreadWatches(ctx context.Context) ([]ThreadWatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT thread_watches.id,thread_watches.assistant_id,thread_watches.device_id,thread_watches.thread_id,thread_watches.status,thread_watches.last_turn_id,thread_watches.last_summary,thread_watches.notified_key,thread_watches.until_text
		FROM thread_watches JOIN assistants ON assistants.id=thread_watches.assistant_id
		WHERE thread_watches.status='watching' AND assistants.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var watches []ThreadWatch
	for rows.Next() {
		var watch ThreadWatch
		if err := rows.Scan(&watch.ID, &watch.AssistantID, &watch.DeviceID, &watch.ThreadID, &watch.Status, &watch.LastTurnID, &watch.LastSummary, &watch.NotifiedKey, &watch.Until); err != nil {
			return nil, err
		}
		watches = append(watches, watch)
	}
	return watches, rows.Err()
}

func (s *Store) RememberTaskSeen(ctx context.Context, assistantID, taskID, seen string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET last_seen_revision=?, updated_at=? WHERE assistant_id=? AND task_id=?`, seen, nowText(time.Time{}), assistantID, taskID)
	return s.changed(err)
}

func (s *Store) RememberThreadSeen(ctx context.Context, assistantID, deviceID, threadID, seen string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE thread_watches SET last_turn_id=?, updated_at=? WHERE assistant_id=? AND device_id=? AND thread_id=?`, seen, nowText(time.Time{}), assistantID, deviceID, threadID)
	return s.changed(err)
}

func (s *Store) SetWatchNotified(ctx context.Context, assistantID, deviceID, threadID, told, summary, seen string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE thread_watches SET notified_key=?, last_summary=?, last_turn_id=?, updated_at=? WHERE assistant_id=? AND device_id=? AND thread_id=?`, told, summary, seen, nowText(time.Time{}), assistantID, deviceID, threadID)
	return s.changed(err)
}

func (s *Store) CloseThreadWatch(ctx context.Context, assistantID, deviceID, threadID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE thread_watches SET status='closed', updated_at=? WHERE assistant_id=? AND device_id=? AND thread_id=?`, nowText(time.Time{}), assistantID, deviceID, threadID)
	return s.changed(err)
}

type TaskArchiveResult struct {
	AlreadyArchived bool
	ClosedWatches   int
	ClosedWakes     int
}

// ArchiveOwnedTask marks one assistant's task archived and closes that assistant's
// watch plus pending wakes for the task. It does not delete conversation messages.
func (s *Store) ArchiveOwnedTask(ctx context.Context, assistantID, taskID, note string) (TaskArchiveResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskArchiveResult{}, err
	}
	defer tx.Rollback()
	var status, deviceID, threadID string
	err = tx.QueryRowContext(ctx, `SELECT status, device_id, native_thread_id FROM tasks WHERE assistant_id=? AND task_id=?`, assistantID, taskID).Scan(&status, &deviceID, &threadID)
	if err != nil {
		return TaskArchiveResult{}, err
	}
	result := TaskArchiveResult{AlreadyArchived: status == "archived"}
	now := nowText(time.Time{})
	if !result.AlreadyArchived {
		if _, err = tx.ExecContext(ctx, `UPDATE tasks SET status='archived', last_summary=CASE WHEN ?='' THEN last_summary ELSE ? END, updated_at=? WHERE assistant_id=? AND task_id=?`, note, note, now, assistantID, taskID); err != nil {
			return TaskArchiveResult{}, err
		}
	}
	if deviceID != "" && threadID != "" {
		var watchID string
		watchErr := tx.QueryRowContext(ctx, `SELECT id FROM thread_watches WHERE assistant_id=? AND device_id=? AND thread_id=?`, assistantID, deviceID, threadID).Scan(&watchID)
		if watchErr != nil && !errors.Is(watchErr, sql.ErrNoRows) {
			return TaskArchiveResult{}, watchErr
		}
		closed, err := tx.ExecContext(ctx, `UPDATE thread_watches SET status='closed', updated_at=? WHERE assistant_id=? AND device_id=? AND thread_id=? AND status<>'closed'`, now, assistantID, deviceID, threadID)
		if err != nil {
			return TaskArchiveResult{}, err
		}
		affected, _ := closed.RowsAffected()
		result.ClosedWatches = int(affected)
		keys := []string{taskID}
		if watchID != "" && watchID != taskID {
			keys = append(keys, watchID)
		}
		for _, key := range keys {
			removed, err := tx.ExecContext(ctx, `DELETE FROM brain_wakes WHERE assistant_id=? AND task_key=?`, assistantID, key)
			if err != nil {
				return TaskArchiveResult{}, err
			}
			n, _ := removed.RowsAffected()
			result.ClosedWakes += int(n)
		}
	} else {
		removed, err := tx.ExecContext(ctx, `DELETE FROM brain_wakes WHERE assistant_id=? AND task_key=?`, assistantID, taskID)
		if err != nil {
			return TaskArchiveResult{}, err
		}
		n, _ := removed.RowsAffected()
		result.ClosedWakes = int(n)
	}
	if err := tx.Commit(); err != nil {
		return TaskArchiveResult{}, err
	}
	s.markChanged()
	return result, nil
}

func (s *Store) UpsertBrainWake(ctx context.Context, wake BrainWake) error {
	watchOnly := 0
	if wake.WatchOnly {
		watchOnly = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO brain_wakes(id,assistant_id,task_key,dedupe_key,status,summary,watch_only,not_before,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(assistant_id, task_key) DO UPDATE SET dedupe_key=excluded.dedupe_key, status=excluded.status, summary=excluded.summary, watch_only=excluded.watch_only, not_before=excluded.not_before`,
		wake.ID, wake.AssistantID, wake.TaskKey, wake.DedupeKey, wake.Status, wake.Summary, watchOnly, nowText(wake.NotBefore), nowText(wake.CreatedAt))
	return s.changed(err)
}

func (s *Store) BrainWakeByTask(ctx context.Context, assistantID, taskKey string) (BrainWake, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,assistant_id,task_key,dedupe_key,status,summary,watch_only,not_before,created_at FROM brain_wakes WHERE assistant_id=? AND task_key=?`, assistantID, taskKey)
	return scanBrainWake(row)
}

func (s *Store) BrainWakes(ctx context.Context, assistantID string) ([]BrainWake, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,assistant_id,task_key,dedupe_key,status,summary,watch_only,not_before,created_at FROM brain_wakes WHERE assistant_id=? ORDER BY not_before, created_at`, assistantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wakes []BrainWake
	for rows.Next() {
		wake, err := scanBrainWake(rows)
		if err != nil {
			return nil, err
		}
		wakes = append(wakes, wake)
	}
	return wakes, rows.Err()
}

func (s *Store) AssistantsWithWakes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT assistant_id FROM brain_wakes`)
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

type wakeScanner interface {
	Scan(dest ...any) error
}

func scanBrainWake(row wakeScanner) (BrainWake, error) {
	var wake BrainWake
	var watchOnly int
	var notBefore, created string
	if err := row.Scan(&wake.ID, &wake.AssistantID, &wake.TaskKey, &wake.DedupeKey, &wake.Status, &wake.Summary, &watchOnly, &notBefore, &created); err != nil {
		return BrainWake{}, err
	}
	wake.WatchOnly = watchOnly != 0
	wake.NotBefore, _ = time.Parse(time.RFC3339Nano, notBefore)
	wake.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return wake, nil
}

func (s *Store) DeleteBrainWake(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM brain_wakes WHERE id=?`, id)
	return s.changed(err)
}

func (s *Store) RecordWake(ctx context.Context, assistantID, taskKey string, at time.Time) error {
	cutoff := nowText(at.Add(-6 * time.Minute))
	if _, err := s.db.ExecContext(ctx, `DELETE FROM wake_log WHERE assistant_id=? AND at<?`, assistantID, cutoff); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO wake_log(id,assistant_id,task_key,at) VALUES(?,?,?,?)`, randomID("wake"), assistantID, taskKey, nowText(at))
	return err
}

func (s *Store) WakeStats(ctx context.Context, assistantID, taskKey string, since time.Time) (count int, oldest, lastTask time.Time, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_key, at FROM wake_log WHERE assistant_id=? AND at>=?`, assistantID, nowText(since.Add(-5*time.Minute)))
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}
	defer rows.Close()
	window := since
	for rows.Next() {
		var key, atText string
		if err := rows.Scan(&key, &atText); err != nil {
			return 0, time.Time{}, time.Time{}, err
		}
		at, parseErr := time.Parse(time.RFC3339Nano, atText)
		if parseErr != nil {
			continue
		}
		if key == taskKey && (lastTask.IsZero() || at.After(lastTask)) {
			lastTask = at
		}
		if !at.Before(window) {
			count++
			if oldest.IsZero() || at.Before(oldest) {
				oldest = at
			}
		}
	}
	return count, oldest, lastTask, rows.Err()
}
