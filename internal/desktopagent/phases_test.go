package desktopagent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
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

func TestIPCEventPressureLogsOnlyBoundedCapacityFields(t *testing.T) {
	capture := &capturedPhases{}
	restore := SetPhaseLogger(slog.New(capture))
	defer restore()
	opts := productionLargeIPCOptions("private-thread-do-not-log")
	opts.OnEventOverload(desktopipc.EventQueuePressure{Reason: "queue_bytes", QueuedEvents: 2, QueuedBytes: 80 << 20, IncomingBytes: 50 << 20, MaxEvents: 1024, MaxEventBytes: 128 << 20, SharedBytes: 80 << 20, MaxSharedBytes: 512 << 20})
	if capture.leaks("private-thread-do-not-log") {
		t.Fatal("raw thread leaked")
	}
	attrs := map[string]slog.Value{}
	capture.mu.Lock()
	for _, record := range capture.records {
		fields := map[string]slog.Value{}
		record.Attrs(func(attr slog.Attr) bool { fields[attr.Key] = attr.Value; return true })
		if fields["phase"].String() == "ipc-event-overload" && fields["incomingBytes"].Kind() == slog.KindInt64 && fields["incomingBytes"].Int64() == 50<<20 {
			attrs = fields
		}
	}
	capture.mu.Unlock()
	if attrs["phase"].String() != "ipc-event-overload" || attrs["reason"].String() != "queue_bytes" || attrs["queuedBytes"].Int64() != 80<<20 || attrs["maxSharedBytes"].Int64() != 512<<20 {
		t.Fatalf("missing safe capacity fields: %v", attrs)
	}
}
