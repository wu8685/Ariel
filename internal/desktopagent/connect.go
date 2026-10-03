package desktopagent

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

func OpenFollower(ctx context.Context, socket, threadID, cwd string) (Live, error) {
	if socket == "" || cwd == "" || !threadIDPattern.MatchString(threadID) {
		return nil, errors.New("invalid Desktop connection target")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	client := desktopipc.NewClient(conn, productionIPCOptions(threadID))
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
