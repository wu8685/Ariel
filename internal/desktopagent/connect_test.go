package desktopagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type mutationCapableLive struct {
	*fakeLive
	started, steered bool
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

func TestLargeFollowerHas64MiBBoundAndOneProcessLease(t *testing.T) {
	regular := productionIPCOptions("thread")
	large := productionLargeIPCOptions("thread")
	if regular.MaxFrameBytes != 0 || large.MaxFrameBytes != 64<<20 || large.MaxEventBytes != 64<<20 {
		t.Fatalf("follower options: ordinary=%+v large=%+v", regular, large)
	}
	release, err := acquireLargeFollower()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLargeFollower(); !errors.Is(err, desktopipc.ErrOverloaded) {
		t.Fatalf("second large follower should be rejected: %v", err)
	}
	release()
	releaseAgain, err := acquireLargeFollower()
	if err != nil {
		t.Fatalf("released slot was not reusable: %v", err)
	}
	releaseAgain()
}

func TestLargeFollowerFallbackRetriesOnlyFrameOverflowAndReleasesLease(t *testing.T) {
	attempts := []bool{}
	live, err := openFollowerWithFallback(func(large bool) (Live, error) {
		attempts = append(attempts, large)
		if !large {
			return nil, desktopipc.ErrFrameTooLarge
		}
		return &fakeLive{updates: make(chan struct{})}, nil
	})
	if err != nil || len(attempts) != 2 || attempts[0] || !attempts[1] {
		t.Fatalf("large fallback: attempts=%v err=%v", attempts, err)
	}
	if _, err := acquireLargeFollower(); !errors.Is(err, desktopipc.ErrOverloaded) {
		t.Fatalf("successful large follower did not hold lease: %v", err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	release, err := acquireLargeFollower()
	if err != nil {
		t.Fatalf("close did not release lease: %v", err)
	}
	release()
	attempts = nil
	_, err = openFollowerWithFallback(func(large bool) (Live, error) {
		attempts = append(attempts, large)
		return nil, desktopipc.ErrProtocol
	})
	if !errors.Is(err, desktopipc.ErrProtocol) || len(attempts) != 1 {
		t.Fatalf("protocol error must not retry: attempts=%v err=%v", attempts, err)
	}
}

func TestLargeFollowerFallbackPreservesOwnerMutationCapabilities(t *testing.T) {
	capable := &mutationCapableLive{fakeLive: &fakeLive{updates: make(chan struct{})}}
	live, err := openFollowerWithFallback(func(large bool) (Live, error) {
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
