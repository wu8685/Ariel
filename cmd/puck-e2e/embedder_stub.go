package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// embedStub is the loopback stand-in for llama-server. It is not a production mode.
type embedStub struct {
	server *httptest.Server
	once   sync.Once
}

func startEmbedStub() *embedStub {
	stub := &embedStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		var body struct {
			Input any `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		texts := inputTexts(body.Input)
		data := make([]map[string]any, len(texts))
		for i, text := range texts {
			data[i] = map[string]any{"index": i, "embedding": stubVector(text)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	return stub
}

func (s *embedStub) url() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.URL
}

func (s *embedStub) close() {
	if s == nil || s.server == nil {
		return
	}
	s.once.Do(s.server.Close)
}

func inputTexts(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		texts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				texts = append(texts, text)
			}
		}
		return texts
	default:
		return nil
	}
}

func stubVector(text string) []float64 {
	vector := make([]float64, 1024)
	query := text
	if index := strings.LastIndex(text, "Query:"); index >= 0 {
		query = strings.TrimSpace(text[index+len("Query:"):])
		if strings.Contains(query, "PR-1842") {
			vector[3] = 1
			return vector
		}
		if strings.Contains(query, "早上") || strings.Contains(query, "早晨") {
			vector[0] = 1
			return vector
		}
		vector[2] = 1
		return vector
	}
	switch {
	case strings.Contains(text, "早上发版"), strings.Contains(text, "早晨发布"):
		vector[0] = 1
	case strings.Contains(text, "PR-1842"):
		vector[1] = 1
	default:
		vector[2] = 1
	}
	return vector
}
