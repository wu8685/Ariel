package puck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	embedderModelID    = "qwen3-embedding-0.6b-q8"
	embedderDim        = 1024
	qwen3Q8SHA256      = "06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439"
	defaultEmbedIdle   = 5 * time.Minute
	defaultStartBudget = 5 * time.Second
	defaultEmbedBudget = 2 * time.Second
	embedderCooldown   = 30 * time.Second
	qwenQueryPrefix    = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "
)

// Embedder turns memory text into vectors. A failure means this call has no
// vector; callers keep the text and search with keywords.
type Embedder interface {
	ModelID() string
	Dim() int
	Available() bool
	Probe(context.Context) error
	EmbedDocuments(context.Context, []string) ([][]float32, error)
	EmbedQuery(context.Context, string) ([]float32, error)
	Close()
}

type disabledEmbedder struct{}

func (disabledEmbedder) ModelID() string { return embedderModelID }
func (disabledEmbedder) Dim() int        { return embedderDim }
func (disabledEmbedder) Available() bool { return false }
func (disabledEmbedder) Probe(context.Context) error {
	return errors.New("embedder unavailable")
}
func (disabledEmbedder) EmbedDocuments(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedder unavailable")
}
func (disabledEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedder unavailable")
}
func (disabledEmbedder) Close() {}

func newEmbedder(dataDir string) Embedder {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("ARIEL_PUCK_EMBEDDER")))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "off":
		return disabledEmbedder{}
	case "http":
		return newHTTPEmbedder(os.Getenv("ARIEL_PUCK_EMBEDDER_URL"), false)
	case "auto":
		root := dataDir
		if env := strings.TrimSpace(os.Getenv("ARIEL_PUCK_DATA_DIR")); env != "" {
			root = env
		}
		bin := strings.TrimSpace(os.Getenv("ARIEL_LLAMA_SERVER"))
		if bin == "" {
			bin = filepath.Join(root, "bin", "llama-server")
		}
		model := filepath.Join(root, "models", "Qwen3-Embedding-0.6B-Q8_0.gguf")
		if _, err := os.Stat(bin); err != nil {
			return disabledEmbedder{}
		}
		if _, err := os.Stat(model); err != nil {
			return disabledEmbedder{}
		}
		return newLlamaEmbedder(llamaConfig{Bin: bin, Model: model, WantSHA256: qwen3Q8SHA256, Idle: envDuration("ARIEL_PUCK_EMBEDDER_IDLE", defaultEmbedIdle), StartTimeout: envDuration("ARIEL_PUCK_EMBEDDER_START_TIMEOUT", defaultStartBudget), EmbedTimeout: defaultEmbedBudget})
	default:
		log.Printf("puck embedder mode %q is unknown; keyword search only", mode)
		return disabledEmbedder{}
	}
}

func newHTTPEmbedder(raw string, ownProcess bool) Embedder {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || !loopbackHost(parsed.Hostname()) {
		log.Printf("puck embedder URL is not a loopback http address; keyword search only")
		return disabledEmbedder{}
	}
	return &httpEmbedder{base: strings.TrimRight(parsed.String(), "/"), client: &http.Client{Timeout: defaultEmbedBudget}, ownProcess: ownProcess}
}

func loopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type httpEmbedder struct {
	base       string
	client     *http.Client
	ownProcess bool
	mu         sync.Mutex
	failures   int
	cooldown   time.Time
}

func (e *httpEmbedder) ModelID() string { return embedderModelID }
func (e *httpEmbedder) Dim() int        { return embedderDim }
func (e *httpEmbedder) Available() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.failures < 3 && time.Now().After(e.cooldown)
}
func (e *httpEmbedder) Probe(ctx context.Context) error {
	vectors, err := e.EmbedDocuments(ctx, []string{"测试"})
	if err != nil {
		return err
	}
	return checkEmbedding(vectors[0])
}
func (e *httpEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if !e.Available() {
		return nil, errors.New("embedder unavailable")
	}
	vectors, err := e.post(ctx, texts)
	if err != nil {
		e.noteFailure()
		return nil, err
	}
	e.noteSuccess()
	return vectors, nil
}
func (e *httpEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vectors, err := e.EmbedDocuments(ctx, []string{qwenQueryPrefix + query})
	if err != nil || len(vectors) == 0 {
		if err == nil {
			err = errors.New("embedder returned no vector")
		}
		return nil, err
	}
	return vectors[0], nil
}
func (e *httpEmbedder) Close() {}
func (e *httpEmbedder) noteFailure() {
	e.mu.Lock()
	e.failures++
	e.cooldown = time.Now().Add(embedderCooldown)
	e.mu.Unlock()
}
func (e *httpEmbedder) noteSuccess() {
	e.mu.Lock()
	e.failures = 0
	e.cooldown = time.Time{}
	e.mu.Unlock()
}
func (e *httpEmbedder) post(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedder status %d", response.StatusCode)
	}
	var decoded struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) != len(texts) {
		return nil, errors.New("embedder returned the wrong number of vectors")
	}
	vectors := make([][]float32, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, errors.New("embedder returned a bad index")
		}
		vector := make([]float32, len(item.Embedding))
		for i, value := range item.Embedding {
			vector[i] = float32(value)
		}
		if err := normalize(vector); err != nil {
			return nil, err
		}
		vectors[item.Index] = vector
	}
	return vectors, nil
}

type llamaConfig struct {
	Bin, Model, WantSHA256           string
	Idle, StartTimeout, EmbedTimeout time.Duration
	startDelay                       time.Duration
}

type llamaEmbedder struct {
	cfg       llamaConfig
	http      *httpEmbedder
	mu        sync.Mutex
	cmd       *exec.Cmd
	starting  chan struct{}
	startErr  error
	healthy   bool
	failures  int
	cooldown  time.Time
	idleTimer *time.Timer
	hashOnce  bool
	hashMatch bool
	starts    int
}

func newLlamaEmbedder(cfg llamaConfig) *llamaEmbedder {
	if cfg.Idle <= 0 {
		cfg.Idle = defaultEmbedIdle
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = defaultStartBudget
	}
	if cfg.EmbedTimeout <= 0 {
		cfg.EmbedTimeout = defaultEmbedBudget
	}
	return &llamaEmbedder{cfg: cfg}
}

func (e *llamaEmbedder) ModelID() string { return embedderModelID }
func (e *llamaEmbedder) Dim() int        { return embedderDim }
func (e *llamaEmbedder) Available() bool {
	if !e.hashOK() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.failures < 3 && time.Now().After(e.cooldown)
}
func (e *llamaEmbedder) Probe(ctx context.Context) error {
	vectors, err := e.EmbedDocuments(ctx, []string{"测试"})
	if err != nil {
		return err
	}
	return checkEmbedding(vectors[0])
}
func (e *llamaEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if !e.Available() {
		return nil, errors.New("embedder unavailable")
	}
	if err := e.ensure(ctx); err != nil {
		e.noteFailure()
		return nil, err
	}
	embedCtx, cancel := context.WithTimeout(ctx, e.cfg.EmbedTimeout)
	defer cancel()
	e.mu.Lock()
	client := e.http
	e.mu.Unlock()
	if client == nil {
		return nil, errors.New("embedder unavailable")
	}
	vectors, err := client.post(embedCtx, texts)
	if err != nil {
		e.mu.Lock()
		e.healthy = false
		e.mu.Unlock()
		return nil, err
	}
	e.touch()
	return vectors, nil
}
func (e *llamaEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vectors, err := e.EmbedDocuments(ctx, []string{qwenQueryPrefix + query})
	if err != nil || len(vectors) == 0 {
		if err == nil {
			err = errors.New("embedder returned no vector")
		}
		return nil, err
	}
	return vectors[0], nil
}
func (e *llamaEmbedder) Close() { e.stop() }

func (e *llamaEmbedder) hashOK() bool {
	e.mu.Lock()
	if e.hashOnce {
		ok := e.hashMatch
		e.mu.Unlock()
		return ok
	}
	e.mu.Unlock()
	sum, err := fileSHA256(e.cfg.Model)
	match := err == nil && strings.EqualFold(sum, e.cfg.WantSHA256)
	e.mu.Lock()
	e.hashOnce = true
	e.hashMatch = match
	e.mu.Unlock()
	if !match {
		log.Printf("puck embedder model hash does not match the pinned Qwen3-Embedding-0.6B Q8_0 file; keyword search only")
	}
	return match
}

func (e *llamaEmbedder) ensure(ctx context.Context) error {
	e.mu.Lock()
	if e.healthy && e.http != nil {
		e.mu.Unlock()
		return nil
	}
	if e.starting != nil {
		wait := e.starting
		e.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		e.mu.Lock()
		err := e.startErr
		healthy := e.healthy
		e.mu.Unlock()
		if !healthy {
			if err == nil {
				err = errors.New("embedder failed to start")
			}
			return err
		}
		return nil
	}
	wait := make(chan struct{})
	e.starting = wait
	e.mu.Unlock()
	err := e.start(ctx)
	e.mu.Lock()
	e.startErr = err
	e.healthy = err == nil
	e.starting = nil
	close(wait)
	e.mu.Unlock()
	return err
}

func (e *llamaEmbedder) start(ctx context.Context) error {
	if e.cfg.startDelay > 0 {
		timer := time.NewTimer(e.cfg.startDelay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(e.cfg.Bin, "-m", e.cfg.Model, "--embedding", "--pooling", "last", "-c", "2048", "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port), "-t", "4")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	e.mu.Lock()
	e.cmd = cmd
	e.starts++
	e.mu.Unlock()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(e.cfg.StartTimeout)
	client := &http.Client{Timeout: time.Second}
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			e.stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("embedder start timed out")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		if err == nil {
			response, err := client.Do(req)
			if err == nil {
				response.Body.Close()
				if response.StatusCode < 500 {
					break
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	httpEmbed := &httpEmbedder{base: base, client: &http.Client{Timeout: e.cfg.EmbedTimeout}}
	e.mu.Lock()
	e.http = httpEmbed
	e.mu.Unlock()
	e.touch()
	return nil
}

func (e *llamaEmbedder) touch() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.idleTimer != nil {
		e.idleTimer.Stop()
	}
	e.idleTimer = time.AfterFunc(e.cfg.Idle, e.stop)
}

func (e *llamaEmbedder) noteFailure() {
	e.mu.Lock()
	e.failures++
	e.cooldown = time.Now().Add(embedderCooldown)
	e.mu.Unlock()
}

func (e *llamaEmbedder) stop() {
	e.mu.Lock()
	cmd := e.cmd
	timer := e.idleTimer
	e.cmd = nil
	e.http = nil
	e.healthy = false
	e.idleTimer = nil
	e.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func normalize(vector []float32) error {
	if len(vector) != embedderDim {
		return fmt.Errorf("embedding dimension %d", len(vector))
	}
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	if sum == 0 {
		return errors.New("embedding is empty")
	}
	scale := float32(1 / math.Sqrt(sum))
	for i := range vector {
		vector[i] *= scale
	}
	return nil
}

func checkEmbedding(vector []float32) error {
	if len(vector) != embedderDim {
		return fmt.Errorf("embedding dimension %d", len(vector))
	}
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	norm := math.Sqrt(sum)
	if norm < 0.9 || norm > 1.1 {
		return fmt.Errorf("embedding norm %f", norm)
	}
	return nil
}
