package desktopagent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnsureOwnerOpensExactlyOnceAndVerifiesOriginalThread(t *testing.T) {
	discoveries, opens, verified := 0, 0, 0
	owner, err := EnsureOwner(context.Background(), "00000000-0000-4000-8000-000000000001", "/fixture", OwnerOps{
		Discover: func(context.Context, string) (string, error) {
			discoveries++
			if discoveries < 3 {
				return "", errors.New("not loaded")
			}
			return "desktop-owner", nil
		},
		Open: func(context.Context, string) error { opens++; return nil },
		Verify: func(_ context.Context, threadID, ownerID, cwd string) error {
			verified++
			if threadID != "00000000-0000-4000-8000-000000000001" || ownerID != "desktop-owner" || cwd != "/fixture" {
				t.Fatal("wrong identity")
			}
			return nil
		},
		RetryInterval: time.Millisecond,
	})
	if err != nil || owner != "desktop-owner" || opens != 1 || verified != 1 {
		t.Fatalf("owner=%s err=%v opens=%d verified=%d", owner, err, opens, verified)
	}
}

func TestEnsureOwnerRejectsInvalidDeepLinkAndTimeout(t *testing.T) {
	opened := false
	ops := OwnerOps{Discover: func(context.Context, string) (string, error) { return "", errors.New("no owner") }, Open: func(context.Context, string) error { opened = true; return nil }, Verify: func(context.Context, string, string, string) error { return nil }, RetryInterval: time.Millisecond}
	if _, err := EnsureOwner(context.Background(), "../../bad", "/fixture", ops); err == nil || opened {
		t.Fatalf("unsafe link accepted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Millisecond)
	defer cancel()
	if _, err := EnsureOwner(ctx, "00000000-0000-4000-8000-000000000001", "/fixture", ops); err == nil {
		t.Fatal("missing owner accepted")
	}
}
