package desktopagent

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

var largeFollowerSlot = make(chan struct{}, 1)

func acquireLargeFollower() (func(), error) {
	select {
	case largeFollowerSlot <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-largeFollowerSlot }) }, nil
	default:
		return nil, desktopipc.ErrOverloaded
	}
}

func productionLargeIPCOptions(threadID string) desktopipc.Options {
	opts := productionIPCOptions(threadID)
	opts.MaxFrameBytes = 64 << 20
	opts.MaxEventBytes = 64 << 20
	return opts
}

func OpenFollower(ctx context.Context, socket, threadID, cwd string) (Live, error) {
	if socket == "" || cwd == "" || !threadIDPattern.MatchString(threadID) {
		return nil, errors.New("invalid Desktop connection target")
	}
	return openFollowerWithFallback(func(large bool) (Live, error) {
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

func (l *leasedLive) Close() error {
	err := l.Live.Close()
	l.release()
	return err
}

func openFollowerWithFallback(open func(bool) (Live, error)) (Live, error) {
	live, err := open(false)
	if err == nil || !errors.Is(err, desktopipc.ErrFrameTooLarge) {
		return live, err
	}
	release, err := acquireLargeFollower()
	if err != nil {
		return nil, err
	}
	live, err = open(true)
	if err != nil {
		release()
		return nil, err
	}
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
		Open: OpenThread,
		Verify: func(ctx context.Context, id, owner, expectedCWD string) error {
			var err error
			follower, err = desktopipc.Follow(ctx, client, id, owner, expectedCWD)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	keep = true
	return follower, nil
}

func productionIPCOptions(threadID string) desktopipc.Options {
	return desktopipc.Options{ThreadID: threadID, ClientType: "ariel-desktop-agent", RequestTimeout: 30 * time.Second}
}
