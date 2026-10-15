package desktopagent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// Issue #5: the cold-subscribe pipeline needs structured per-phase timing so
// real slow opens can be attributed without ever logging content or raw IDs.

type capturedPhases struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *capturedPhases) Enabled(context.Context, slog.Level) bool { return true }
func (c *capturedPhases) Handle(_ context.Context, record slog.Record) error {
	c.mu.Lock()
	c.records = append(c.records, record)
	c.mu.Unlock()
	return nil
}
func (c *capturedPhases) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capturedPhases) WithGroup(string) slog.Handler      { return c }

func (c *capturedPhases) phases() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.records))
	for _, record := range c.records {
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "phase" {
				names = append(names, attr.Value.String())
			}
			return true
		})
	}
	return names
}

func (c *capturedPhases) leaks(raw string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, record := range c.records {
		if strings.Contains(record.Message, raw) {
			return true
		}
		leaked := false
		record.Attrs(func(attr slog.Attr) bool {
			if strings.Contains(attr.Value.String(), raw) {
				leaked = true
			}
			return !leaked
		})
		if leaked {
			return true
		}
	}
	return false
}

func TestSubscribeLogsStructuredPhasesWithoutRawThreadID(t *testing.T) {
	capture := &capturedPhases{}
	restore := SetPhaseLogger(slog.New(capture))
	defer restore()
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(anyThreadHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer s.Close()
	const rawThreadID = "thread-phase-raw-id"
	_, _, _, activate, err := s.Subscribe(context.Background(), rawThreadID, func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	phases := capture.phases()
	joined := strings.Join(phases, ",")
	for _, required := range []string{"owner-attach", "normalize", "first-snapshot"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("subscribe pipeline missed phase %q: %v", required, phases)
		}
	}
	if capture.leaks(rawThreadID) {
		t.Fatalf("phase log leaked the raw thread ID: %v", phases)
	}
}

func TestThreadDigestIsIrreversibleAndStable(t *testing.T) {
	a := threadDigest("thread-phase-raw-id")
	if a == threadDigest("thread-phase-other") {
		t.Fatal("digest collision between distinct thread IDs")
	}
	if a != threadDigest("thread-phase-raw-id") {
		t.Fatal("digest not stable")
	}
	if strings.Contains(a, "thread") || len(a) != 16 {
		t.Fatalf("digest must be a short opaque hex prefix: %q", a)
	}
}
