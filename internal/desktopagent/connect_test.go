package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type mutationCapableLive struct {
	*fakeLive
	started, steered bool
}

type refreshableMutationCapableLive struct {
	*mutationCapableLive
	refreshes int
}

func (l *refreshableMutationCapableLive) Refresh(context.Context) (json.RawMessage, error) {
	l.refreshes++
	return l.Current()
}

func (l *mutationCapableLive) StartWithImages(context.Context, string, string, []string) (string, error) {
	l.started = true
	return "image-turn", nil
}

func (l *mutationCapableLive) Steer(context.Context, string, string, string, string, []string) error {
	l.steered = true
	return nil
}

func TestProductionOwnerTimeoutAllowsSlowDesktopReceipt(t *testing.T) {
	if productionIPCOptions("thread").RequestTimeout < 30*time.Second {
		t.Fatal("native owner timeout too short")
	}
}

func TestOpenFollowerRejectsMissingSocketAndInvalidThreadBeforeDial(t *testing.T) {
	if _, err := OpenFollower(context.Background(), "", "00000000-0000-4000-8000-000000000001", "/fixture"); err == nil {
		t.Fatal("missing socket accepted")
	}
	if _, err := OpenFollower(context.Background(), "/not/a/socket", "../../bad", "/fixture"); err == nil {
		t.Fatal("unsafe thread accepted")
	}
}

func TestLargeFollowerHas64MiBBoundAndProcessByteBudget(t *testing.T) {
	regular := productionIPCOptions("thread")
	large := productionLargeIPCOptions("thread")
	if regular.MaxFrameBytes != 0 || large.MaxFrameBytes != 64<<20 || large.MaxWireFrameBytes != 256<<20 || large.MaxEventBytes != 64<<20 {
		t.Fatalf("follower options: ordinary=%+v large=%+v", regular, large)
	}
	resetLargeFollowerBudgetForTest()
	releases := make([]func(), 0, 8)
	for i := 0; i < 8; i++ {
		release, err := acquireLargeFollower()
		if err != nil {
			t.Fatalf("large follower %d: %v", i+1, err)
		}
		releases = append(releases, release)
	}
	if _, err := acquireLargeFollower(); !errors.Is(err, desktopipc.ErrOverloaded) {
		t.Fatalf("follower beyond 512 MiB budget should be rejected: %v", err)
	}
	for _, release := range releases {
		release()
	}
	releaseAgain, err := acquireLargeFollower()
	if err != nil {
		t.Fatalf("released slot was not reusable: %v", err)
	}
	releaseAgain()
}

func TestLargeFollowerFallbackRetriesOnlyFrameOverflowAndReleasesLease(t *testing.T) {
	resetLargeFollowerBudgetForTest()
	resetFrameHints()
	attempts := []bool{}
	live, err := openFollowerWithFallback("hint-retry", func(large bool) (Live, error) {
		attempts = append(attempts, large)
		if !large {
			return nil, desktopipc.ErrFrameTooLarge
		}
		return &fakeLive{updates: make(chan struct{})}, nil
	})
	if err != nil || len(attempts) != 2 || attempts[0] || !attempts[1] {
		t.Fatalf("large fallback: attempts=%v err=%v", attempts, err)
	}
	if got := largeFollowerBytesInUse(); got != 64<<20 {
		t.Fatalf("successful large follower reservation = %d, want 64 MiB", got)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	if got := largeFollowerBytesInUse(); got != 0 {
		t.Fatalf("close did not release reservation: %d", got)
	}
	attempts = nil
	_, err = openFollowerWithFallback("hint-protocol", func(large bool) (Live, error) {
		attempts = append(attempts, large)
		return nil, desktopipc.ErrProtocol
	})
	if !errors.Is(err, desktopipc.ErrProtocol) || len(attempts) != 1 {
		t.Fatalf("protocol error must not retry: attempts=%v err=%v", attempts, err)
	}
}

func TestLargeFollowerFallbackPreservesOwnerMutationCapabilities(t *testing.T) {
	resetFrameHints()
	capable := &mutationCapableLive{fakeLive: &fakeLive{updates: make(chan struct{})}}
	live, err := openFollowerWithFallback("hint-capable", func(large bool) (Live, error) {
		if !large {
			return nil, desktopipc.ErrFrameTooLarge
		}
		return capable, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	imageStarter, ok := live.(interface {
		StartWithImages(context.Context, string, string, []string) (string, error)
	})
	if !ok {
		t.Fatal("large follower erased StartWithImages capability")
	}
	if _, err := imageStarter.StartWithImages(context.Background(), "client", "", []string{"data:image/png;base64,AAAA"}); err != nil || !capable.started {
		t.Fatalf("large follower did not forward StartWithImages: %v", err)
	}
	steerer, ok := live.(interface {
		Steer(context.Context, string, string, string, string, []string) error
	})
	if !ok {
		t.Fatal("large follower erased Steer capability")
	}
	if err := steerer.Steer(context.Background(), "turn", "queue", "client", "guide", nil); err != nil || !capable.steered {
		t.Fatalf("large follower did not forward Steer: %v", err)
	}
}

func TestLargeFollowerFallbackPreservesRecoveryRefreshCapability(t *testing.T) {
	resetFrameHints()
	capable := &refreshableMutationCapableLive{mutationCapableLive: &mutationCapableLive{fakeLive: &fakeLive{updates: make(chan struct{})}}}
	live, err := openFollowerWithFallback("hint-refresh", func(large bool) (Live, error) {
		if !large {
			return nil, desktopipc.ErrFrameTooLarge
		}
		return capable, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	refreshable, ok := live.(interface {
		Refresh(context.Context) (json.RawMessage, error)
	})
	if !ok {
		t.Fatal("large follower erased recovery Refresh capability")
	}
	if _, err := refreshable.Refresh(context.Background()); err != nil || capable.refreshes != 1 {
		t.Fatalf("large follower did not forward recovery Refresh: refreshes=%d err=%v", capable.refreshes, err)
	}
}

func TestFrameHintSkipsSmallFollowerForKnownLargeThread(t *testing.T) {
	resetFrameHints()
	first := []bool{}
	live, err := openFollowerWithFallback("hint-large-thread", func(large bool) (Live, error) {
		first = append(first, large)
		if !large {
			return nil, desktopipc.ErrFrameTooLarge
		}
		return &fakeLive{updates: make(chan struct{})}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0] || !first[1] {
		t.Fatalf("cold open should try small then large: %v", first)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	second := []bool{}
	live2, err := openFollowerWithFallback("hint-large-thread", func(large bool) (Live, error) {
		second = append(second, large)
		return &fakeLive{updates: make(chan struct{})}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer live2.Close()
	if len(second) != 1 || !second[0] {
		t.Fatalf("known-large thread must skip the doomed small follower: %v", second)
	}
}

func TestFrameHintNotSetAfterSmallFollowerSuccess(t *testing.T) {
	resetFrameHints()
	for i := 0; i < 2; i++ {
		attempts := []bool{}
		live, err := openFollowerWithFallback("hint-small-thread", func(large bool) (Live, error) {
			attempts = append(attempts, large)
			return &fakeLive{updates: make(chan struct{})}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 1 || attempts[0] {
			t.Fatalf("small-only thread must keep using the ordinary follower: %v", attempts)
		}
		if err := live.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFrameHintStoreIsBounded(t *testing.T) {
	resetFrameHints()
	key := func(i int) string { return fmt.Sprintf("hint-bound-%d", i) }
	for i := 0; i < maxFrameHintEntries+16; i++ {
		recordFrameHint(key(i), true)
	}
	if frameHintPrefersLarge(key(0)) {
		t.Fatal("oldest frame hint was not evicted")
	}
	if !frameHintPrefersLarge(key(maxFrameHintEntries + 15)) {
		t.Fatal("newest frame hint missing")
	}
	if got := frameHintCount(); got > maxFrameHintEntries {
		t.Fatalf("frame hint store unbounded: %d", got)
	}
}

// Spec 0086 calls the hint store an LRU: re-recording an existing hint must
// refresh its recency so a still-active large thread is not evicted by newer
// one-off entries.
func TestFrameHintReRecordRefreshesRecency(t *testing.T) {
	resetFrameHints()
	key := func(i int) string { return fmt.Sprintf("hint-lru-%d", i) }
	for i := 0; i < maxFrameHintEntries; i++ {
		recordFrameHint(key(i), true)
	}
	// Touch the oldest entry; the next insert must evict key(1), not key(0).
	recordFrameHint(key(0), true)
	recordFrameHint(key(maxFrameHintEntries), true)
	if !frameHintPrefersLarge(key(0)) {
		t.Fatal("recently re-recorded hint was evicted")
	}
	if frameHintPrefersLarge(key(1)) {
		t.Fatal("untouched hint survived past a fresh insert")
	}
	if got := frameHintCount(); got > maxFrameHintEntries {
		t.Fatalf("frame hint store unbounded: %d", got)
	}
}
