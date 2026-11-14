package puck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLlamaEmbedderSingleFlightIdleAndTimeout(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for the embedder stub")
	}
	dir := t.TempDir()
	model := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(model, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("weights"))
	bin := filepath.Join(dir, "llama-server")
	script := `#!/bin/sh
port=9
while [ $# -gt 0 ]; do
  if [ "$1" = "--port" ]; then port=$2; shift 2; else shift; fi
done
exec python3 - "$port" << 'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers(); self.wfile.write(b"ok")
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n)
        body = json.loads(raw or b"{}")
        inputs = body.get("input") or []
        if isinstance(inputs, str):
            inputs = [inputs]
        data = []
        for i, _text in enumerate(inputs):
            vec = [0.0] * 1024
            vec[0] = 1.0
            data.append({"index": i, "embedding": vec})
        payload = json.dumps({"data": data}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def log_message(self, *args):
        return
ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	embedder := newLlamaEmbedder(llamaConfig{Bin: bin, Model: model, WantSHA256: hex.EncodeToString(sum[:]), Idle: 200 * time.Millisecond, StartTimeout: 3 * time.Second, EmbedTimeout: 2 * time.Second, startDelay: 200 * time.Millisecond})
	defer embedder.Close()
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := embedder.EmbedDocuments(context.Background(), []string{"测试"})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	embedder.mu.Lock()
	starts := embedder.starts
	embedder.mu.Unlock()
	if starts != 1 {
		t.Fatalf("starts=%d", starts)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := embedder.EmbedDocuments(context.Background(), []string{"再次"}); err != nil {
		t.Fatal(err)
	}
	embedder.mu.Lock()
	starts = embedder.starts
	embedder.mu.Unlock()
	if starts != 2 {
		t.Fatalf("after idle starts=%d", starts)
	}

	stuck := filepath.Join(dir, "stuck")
	if err := os.WriteFile(stuck, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	down := newLlamaEmbedder(llamaConfig{Bin: stuck, Model: model, WantSHA256: hex.EncodeToString(sum[:]), Idle: time.Minute, StartTimeout: 200 * time.Millisecond, EmbedTimeout: time.Second})
	defer down.Close()
	errCh = make(chan error, 2)
	wg = sync.WaitGroup{}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := down.EmbedDocuments(context.Background(), []string{"测试"})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err == nil {
			t.Fatal("stuck embedder succeeded")
		}
	}
	down.mu.Lock()
	starts = down.starts
	down.mu.Unlock()
	if starts != 1 {
		t.Fatalf("failed starts=%d", starts)
	}
	if _, err := down.EmbedDocuments(context.Background(), []string{"测试"}); err == nil {
		t.Fatal("cooldown still tried to embed")
	}

	bad := newLlamaEmbedder(llamaConfig{Bin: bin, Model: model, WantSHA256: "deadbeef", Idle: time.Minute, StartTimeout: time.Second, EmbedTimeout: time.Second})
	if bad.Available() {
		t.Fatal("mismatched model hash was accepted")
	}
}
