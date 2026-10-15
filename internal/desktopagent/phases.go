package desktopagent

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// Structured per-phase observability for the thread subscribe pipeline
// (spec 0086). Records carry an attempt ID, an irreversible thread digest,
// durations, frame sizes and error classes — never tokens, content, or raw
// thread IDs.

var activePhaseLogger atomic.Pointer[slog.Logger]

func init() {
	activePhaseLogger.Store(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

// SetPhaseLogger swaps the pipeline logger and returns a restore function.
// Tests use it to capture records; production keeps the stderr JSON default.
func SetPhaseLogger(logger *slog.Logger) func() {
	previous := activePhaseLogger.Swap(logger)
	return func() { activePhaseLogger.Store(previous) }
}

// threadDigest maps a thread ID to an irreversible short digest so phase logs
// can correlate stages of one open without persisting the raw ID.
func threadDigest(threadID string) string {
	sum := sha256.Sum256([]byte(threadID))
	return hex.EncodeToString(sum[:8])
}

func logPhase(phase, threadID string, attrs ...any) {
	fields := make([]any, 0, len(attrs)+4)
	fields = append(fields, "phase", phase, "thread", threadDigest(threadID))
	fields = append(fields, attrs...)
	activePhaseLogger.Load().Info("thread-pipeline", fields...)
}

func millisSince(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
