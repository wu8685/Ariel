package main

import (
	"encoding/json"
	"os"
	"sync"
	"syscall"
)

type storedTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items"`
}

type storedThread struct {
	ID       string       `json:"id"`
	CWD      string       `json:"cwd"`
	Name     string       `json:"name"`
	Archived bool         `json:"archived"`
	Turns    []storedTurn `json:"turns"`
}

type world struct {
	Threads map[string]storedThread `json:"threads"`
	TaskID  string                  `json:"taskId"`
}

var stateMu sync.Mutex

func statePath() string { return os.Getenv("PUCK_E2E_FAKE_STATE") }

func withState(update func(*world)) {
	stateMu.Lock()
	defer stateMu.Unlock()
	path := statePath()
	if path != "" {
		lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err == nil {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
			defer func() {
				_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
				_ = lock.Close()
			}()
		}
	}
	state := world{Threads: map[string]storedThread{}}
	if path != "" {
		if body, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(body, &state)
		}
	} else if memory != nil {
		state = *memory
	}
	if state.Threads == nil {
		state.Threads = map[string]storedThread{}
	}
	update(&state)
	if path != "" {
		body, _ := json.Marshal(state)
		_ = os.WriteFile(path, body, 0o600)
		return
	}
	copied := state
	memory = &copied
}

var memory *world

func saveThread(thread storedThread) {
	withState(func(state *world) { state.Threads[thread.ID] = thread })
}

func loadThread(id string) storedThread {
	var thread storedThread
	withState(func(state *world) { thread = state.Threads[id] })
	return thread
}

func listThreads() []storedThread {
	var threads []storedThread
	withState(func(state *world) {
		for _, thread := range state.Threads {
			threads = append(threads, thread)
		}
	})
	return threads
}

func renameThread(id, name string) storedThread {
	var thread storedThread
	withState(func(state *world) {
		thread = state.Threads[id]
		if thread.ID == "" {
			thread.ID = id
		}
		thread.Name = name
		state.Threads[id] = thread
	})
	return thread
}

func rememberTask(id string) {
	if id == "" {
		return
	}
	withState(func(state *world) { state.TaskID = id })
}

func rememberedTask() string {
	var id string
	withState(func(state *world) { id = state.TaskID })
	return id
}

func threadView(thread storedThread) map[string]any {
	status, _ := json.Marshal(map[string]any{"type": "idle"})
	return map[string]any{
		"id": thread.ID, "name": thread.Name, "cwd": thread.CWD, "updatedAt": 1,
		"status": json.RawMessage(status), "turns": thread.Turns,
		"session_meta": map[string]any{"cwd": thread.CWD},
	}
}
