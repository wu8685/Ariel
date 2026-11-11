package main

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

func openHarnessDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS owner_session (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		subject TEXT NOT NULL,
		passkey INTEGER NOT NULL CHECK (passkey = 0),
		created_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func bootstrapOwner(path string) error {
	db, err := openHarnessDB(path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO owner_session(id, subject, passkey, created_at) VALUES(1, 'owner', 0, ?)
		ON CONFLICT(id) DO UPDATE SET subject='owner', passkey=0`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func ownerBootstrapped(path string) (bool, error) {
	db, err := openHarnessDB(path)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var subject string
	var passkey int
	err = db.QueryRow(`SELECT subject, passkey FROM owner_session WHERE id=1`).Scan(&subject, &passkey)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return subject == "owner" && passkey == 0, nil
}

type snapshot struct {
	Inbox          int
	BotTasks       int
	OpenBotTasks   int
	DoneBotTasks   int
	MergedBotTasks int
	QueueWakes     int
	Watches        int
	Wakes          int
	SessionLinks   int
}

func readSnapshot(ctx context.Context, dbPath, assistantID string) (snapshot, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return snapshot{}, err
	}
	defer db.Close()
	var snap snapshot
	queries := []struct {
		dest *int
		sql  string
	}{
		{&snap.Inbox, `SELECT COUNT(*) FROM bot_inbox WHERE assistant_id=?`},
		{&snap.BotTasks, `SELECT COUNT(*) FROM bot_tasks WHERE assistant_id=?`},
		{&snap.OpenBotTasks, `SELECT COUNT(*) FROM bot_tasks WHERE assistant_id=? AND status IN ('accepted','in_progress','waiting_user')`},
		{&snap.DoneBotTasks, `SELECT COUNT(*) FROM bot_tasks WHERE assistant_id=? AND status='done'`},
		{&snap.MergedBotTasks, `SELECT COUNT(*) FROM bot_tasks WHERE assistant_id=? AND status='merged'`},
		{&snap.QueueWakes, `SELECT COUNT(*) FROM brain_wakes WHERE assistant_id=? AND task_key LIKE 'queue:%'`},
		{&snap.Watches, `SELECT COUNT(*) FROM thread_watches WHERE assistant_id=? AND status='watching'`},
		{&snap.Wakes, `SELECT COUNT(*) FROM brain_wakes WHERE assistant_id=?`},
		{&snap.SessionLinks, `SELECT COUNT(*) FROM bot_task_sessions WHERE assistant_id=?`},
	}
	for _, query := range queries {
		if err := db.QueryRowContext(ctx, query.sql, assistantID).Scan(query.dest); err != nil {
			return snapshot{}, err
		}
	}
	return snap, nil
}

func messagePersisted(ctx context.Context, dbPath, assistantID, messageID string) (bool, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return false, err
	}
	defer db.Close()
	var inbox, attached int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_inbox WHERE assistant_id=? AND message_id=?`, assistantID, messageID).Scan(&inbox); err != nil {
		return false, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_task_messages WHERE assistant_id=? AND message_id=?`, assistantID, messageID).Scan(&attached); err != nil {
		return false, err
	}
	return inbox+attached > 0, nil
}

func messageTask(ctx context.Context, dbPath, assistantID, messageID string) (string, string, string, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return "", "", "", err
	}
	defer db.Close()
	var id, status, merged string
	err = db.QueryRowContext(ctx, `SELECT bot_tasks.id, bot_tasks.status, bot_tasks.merged_into FROM bot_task_messages
		JOIN bot_tasks ON bot_tasks.id=bot_task_messages.bot_task_id AND bot_tasks.assistant_id=bot_task_messages.assistant_id
		WHERE bot_task_messages.assistant_id=? AND bot_task_messages.message_id=?`, assistantID, messageID).Scan(&id, &status, &merged)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return id, status, merged, err
}

func taskByFixture(ctx context.Context, dbPath, assistantID, fixture string) (id, status, merged string, err error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return "", "", "", err
	}
	defer db.Close()
	err = db.QueryRowContext(ctx, `SELECT id, status, merged_into FROM bot_tasks
		WHERE assistant_id=? AND status<>'merged' AND (title=? OR goal=? OR title LIKE ? OR goal LIKE ?)
		ORDER BY created_at LIMIT 1`, assistantID, fixture, fixture, "%"+fixture+"%", "%"+fixture+"%").Scan(&id, &status, &merged)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return id, status, merged, err
}

func sessionLinksForThread(ctx context.Context, dbPath, assistantID, threadID string) (int, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_task_sessions WHERE assistant_id=? AND thread_id=?`, assistantID, threadID).Scan(&count)
	return count, err
}

func assistantGone(ctx context.Context, dbPath, assistantID string) (bool, error) {
	snap, err := readSnapshot(ctx, dbPath, assistantID)
	if err != nil {
		return false, err
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return false, err
	}
	defer db.Close()
	var messages int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_task_messages WHERE assistant_id=?`, assistantID).Scan(&messages); err != nil {
		return false, err
	}
	return snap.BotTasks == 0 && snap.Inbox == 0 && snap.SessionLinks == 0 && snap.Watches == 0 && snap.Wakes == 0 && messages == 0, nil
}
