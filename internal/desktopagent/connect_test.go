package desktopagent

import (
	"context"
	"testing"
	"time"
)

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
