package chilling

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestTaskClientUsesSeparateUnixContractAndTrustedPrincipal(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "task-client-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "task.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var method string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/tasks/capabilities", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"contract": "task-service/v1", "enabled": true, "ready": true, "taskStoreId": "store", "buildVersion": "fixture"})
	})
	mux.HandleFunc("POST /v1/tasks/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Chilling-User-ID") != "ariel-owner" {
			t.Error("missing trusted principal")
		}
		var in struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		method = in.Method
		json.NewEncoder(w).Encode(map[string]any{"taskStoreId": "store", "tasks": []any{}})
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	client, err := NewTaskClient(context.Background(), Config{Enabled: true, Endpoint: "unix://" + path})
	if err != nil {
		t.Fatal(err)
	}
	if !client.Ready() || client.TaskStoreID() != "store" {
		t.Fatal("task capability missing")
	}
	var out map[string]any
	if err := client.Call(context.Background(), "task.list", map[string]any{"taskStoreId": "store"}, &out); err != nil {
		t.Fatal(err)
	}
	if method != "task.list" {
		t.Fatalf("method=%s", method)
	}
	if err := client.Call(context.Background(), "task.proxy", map[string]any{"url": "http://localhost"}, &out); err == nil {
		t.Fatal("arbitrary method allowed")
	}
}

func TestTaskClientRecoversBackendReadinessWithoutAgentRestart(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "task-retry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "task.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var ready atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{"code": "TASK_NOT_READY"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"contract": "task-service/v1", "enabled": true, "ready": true, "taskStoreId": "store"})
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	client, err := NewTaskClient(context.Background(), Config{Enabled: true, Endpoint: "unix://" + path})
	if client == nil || err == nil || client.Ready() {
		t.Fatalf("unready backend must retain retryable client: client=%v error=%v", client, err)
	}
	ready.Store(true)
	refreshable, ok := any(client).(interface{ Refresh(context.Context) error })
	if !ok {
		t.Fatal("task client has no readiness recovery")
	}
	if err := refreshable.Refresh(context.Background()); err != nil || !client.Ready() || client.TaskStoreID() != "store" {
		t.Fatalf("backend did not recover: %v", err)
	}
}
