package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/modelsettings"
)

const largeFollowerReservationBytes int64 = 64 << 20
const defaultLargeFollowerBudgetBytes int64 = 512 << 20

var largeFollowerBudget = struct {
	sync.Mutex
	used  int64
	limit int64
}{limit: defaultLargeFollowerBudgetBytes}

func acquireLargeFollower() (func(), error) {
	largeFollowerBudget.Lock()
	if largeFollowerBudget.used+largeFollowerReservationBytes > largeFollowerBudget.limit {
		largeFollowerBudget.Unlock()
		return nil, desktopipc.ErrOverloaded
	}
	largeFollowerBudget.used += largeFollowerReservationBytes
	largeFollowerBudget.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			largeFollowerBudget.Lock()
			largeFollowerBudget.used -= largeFollowerReservationBytes
			largeFollowerBudget.Unlock()
		})
	}, nil
}

func largeFollowerBytesInUse() int64 {
	largeFollowerBudget.Lock()
	defer largeFollowerBudget.Unlock()
	return largeFollowerBudget.used
}

func resetLargeFollowerBudgetForTest() {
	largeFollowerBudget.Lock()
	defer largeFollowerBudget.Unlock()
	largeFollowerBudget.used = 0
	largeFollowerBudget.limit = defaultLargeFollowerBudgetBytes
}

// maxFrameHintEntries bounds the per-thread owner frame size hints (spec
// 0086): a thread whose owner frame once overflowed the ordinary follower
// skips the doomed small attempt on the next open instead of paying a
// deterministic small→large retry. The store evicts least-recently-recorded
// entries first; re-recording an existing hint refreshes its recency.
const maxFrameHintEntries = 128

var frameHints = struct {
	sync.Mutex
	large map[string]bool
	order []string
}{large: map[string]bool{}}

func frameHintPrefersLarge(threadID string) bool {
	frameHints.Lock()
	defer frameHints.Unlock()
	return frameHints.large[threadID]
}

func recordFrameHint(threadID string, large bool) {
	frameHints.Lock()
	defer frameHints.Unlock()
	if !large {
		delete(frameHints.large, threadID)
		return
	}
	if _, exists := frameHints.large[threadID]; exists {
		for i, id := range frameHints.order {
			if id == threadID {
				frameHints.order = append(frameHints.order[:i], frameHints.order[i+1:]...)
				break
			}
		}
	}
	frameHints.order = append(frameHints.order, threadID)
	frameHints.large[threadID] = true
	for len(frameHints.order) > maxFrameHintEntries {
		oldest := frameHints.order[0]
		frameHints.order = frameHints.order[1:]
		delete(frameHints.large, oldest)
	}
}

func frameHintCount() int {
	frameHints.Lock()
	defer frameHints.Unlock()
	return len(frameHints.large)
}

func resetFrameHints() {
	frameHints.Lock()
	defer frameHints.Unlock()
	frameHints.large = map[string]bool{}
	frameHints.order = nil
}

func productionLargeIPCOptions(threadID string) desktopipc.Options {
	opts := productionIPCOptions(threadID)
	opts.MaxFrameBytes = 64 << 20
	opts.MaxWireFrameBytes = 256 << 20
	opts.MaxEventBytes = 64 << 20
	return opts
}

func OpenFollower(ctx context.Context, socket, threadID, cwd string) (Live, error) {
	if socket == "" || cwd == "" || !threadIDPattern.MatchString(threadID) {
		return nil, errors.New("invalid Desktop connection target")
	}
	return openFollowerWithFallback(threadID, func(large bool) (Live, error) {
		opts := productionIPCOptions(threadID)
		if large {
			opts = productionLargeIPCOptions(threadID)
		}
		return openFollower(ctx, socket, threadID, cwd, opts)
	})
}

type leasedLive struct {
	Live
	release func()
}

// holdsLargeFollowerLease is an internal marker used by the Service capacity
// controller. It never exposes the underlying Desktop state or lease handle.
func (*leasedLive) holdsLargeFollowerLease() {}

func isLargeFollowerLive(live Live) bool {
	_, ok := live.(interface{ holdsLargeFollowerLease() })
	return ok
}

func (l *leasedLive) StartWithImages(ctx context.Context, clientMessageID, text string, images []string) (string, error) {
	capable, ok := l.Live.(interface {
		StartWithImages(context.Context, string, string, []string) (string, error)
	})
	if !ok {
		return "", desktopipc.ErrProtocol
	}
	return capable.StartWithImages(ctx, clientMessageID, text, images)
}

func (l *leasedLive) Steer(ctx context.Context, expectedTurnID, queuedMessageID, clientMessageID, text string, images []string) error {
	capable, ok := l.Live.(interface {
		Steer(context.Context, string, string, string, string, []string) error
	})
	if !ok {
		return desktopipc.ErrProtocol
	}
	return capable.Steer(ctx, expectedTurnID, queuedMessageID, clientMessageID, text, images)
}

// Refresh preserves the optional recovery capability of the wrapped Desktop
// follower. Embedding the narrower Live interface alone would erase this
// method from leasedLive's method set and make large-history conversations
// fail the restart recovery guard with RESYNC_REQUIRED.
func (l *leasedLive) Refresh(ctx context.Context) (json.RawMessage, error) {
	capable, ok := l.Live.(interface {
		Refresh(context.Context) (json.RawMessage, error)
	})
	if !ok {
		return nil, desktopipc.ErrProtocol
	}
	return capable.Refresh(ctx)
}

func (l *leasedLive) ExecutionSettings() (modelsettings.Values, error) {
	capable, ok := l.Live.(executionSettingsLive)
	if !ok {
		return modelsettings.Values{}, desktopipc.ErrProtocol
	}
	return capable.ExecutionSettings()
}
func (l *leasedLive) UpdateExecutionSettings(ctx context.Context, v modelsettings.Values) error {
	capable, ok := l.Live.(executionSettingsLive)
	if !ok {
		return desktopipc.ErrProtocol
	}
	return capable.UpdateExecutionSettings(ctx, v)
}

func (l *leasedLive) Close() error {
	err := l.Live.Close()
	l.release()
	return err
}

func openFollowerWithFallback(threadID string, open func(bool) (Live, error)) (Live, error) {
	attempt := rand.Text()
	if !frameHintPrefersLarge(threadID) {
		live, err := open(false)
		if err == nil {
			recordFrameHint(threadID, false)
			return live, nil
		}
		if !errors.Is(err, desktopipc.ErrFrameTooLarge) {
			return nil, err
		}
		logPhase("small-frame-overflow", threadID, "attempt", attempt)
	} else {
		logPhase("large-follower-hint", threadID, "attempt", attempt)
	}
	release, err := acquireLargeFollower()
	if err != nil {
		return nil, err
	}
	logPhase("large-follower-retry", threadID, "attempt", attempt)
	live, err := open(true)
	if err != nil {
		release()
		return nil, err
	}
	recordFrameHint(threadID, true)
	return &leasedLive{Live: live, release: release}, nil
}

func openFollower(ctx context.Context, socket, threadID, cwd string, opts desktopipc.Options) (Live, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	client := desktopipc.NewClient(conn, opts)
	keep := false
	defer func() {
		if !keep {
			_ = client.Close()
		}
	}()
	if err := client.Initialize(ctx); err != nil {
		return nil, err
	}
	var follower *desktopipc.Follower
	discoveryStart := time.Now()
	_, err = EnsureOwner(ctx, threadID, cwd, OwnerOps{
		Discover: func(ctx context.Context, id string) (string, error) {
			query, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			reply, err := client.Call(query, desktopipc.Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": id}})
			if err != nil {
				return "", err
			}
			return reply.HandledByClientID, nil
		},
		Open: func(ctx context.Context, id string) error {
			openStart := time.Now()
			err := OpenThread(ctx, id)
			logPhase("deep-link-open", id, "durationMs", millisSince(openStart), "ok", err == nil)
			if err == nil {
				discoveryStart = time.Now()
			}
			return err
		},
		Verify: func(ctx context.Context, id, owner, expectedCWD string) error {
			followStart := time.Now()
			var err error
			follower, err = desktopipc.Follow(ctx, client, id, owner, expectedCWD)
			attrs := []any{"durationMs", millisSince(followStart), "ok", err == nil}
			if err == nil {
				attrs = append(attrs, "frameBytes", follower.StateBytes())
			}
			logPhase("follower-initialize", id, attrs...)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	logPhase("owner-discovery", threadID, "durationMs", millisSince(discoveryStart))
	keep = true
	return follower, nil
}

func productionIPCOptions(threadID string) desktopipc.Options {
	return desktopipc.Options{ThreadID: threadID, ClientType: "ariel-desktop-agent", RequestTimeout: 30 * time.Second}
}
