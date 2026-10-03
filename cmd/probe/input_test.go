package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUserInputCLIRequiresWriteAndManifest(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"user-input"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestUnknownFixturePurposeRejectedBeforeStartingServer(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"create", "--purpose", "all-tools", "--write-enabled", "--manifest", "unused.json"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "unknown fixture purpose") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestCommandDeclineCLIRequiresWriteAndManifest(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"command-decline"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestCommandCancelCLIRequiresWriteAndManifest(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"command-cancel"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestBusySubmitCLIRequiresWriteAndManifest(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"busy-submit"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestUserInputRaceCLIRequiresWriteAndManifest(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"user-input-race"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestUserInputInterruptCLIRequiresWriteAndManifest(t *testing.T) {
	var out, errout bytes.Buffer
	code := fixtureCommand(context.Background(), []string{"user-input-interrupt"}, &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
		t.Fatalf("code %d: %s", code, errout.String())
	}
}

func TestPendingInteractionCLIRequiresWriteAndManifest(t *testing.T) {
	for _, operation := range []string{"command-pending", "user-input-pending", "file-pending", "permission-pending"} {
		var out, errout bytes.Buffer
		code := fixtureCommand(context.Background(), []string{operation}, &out, &errout)
		if code != 2 || !strings.Contains(errout.String(), "requires --write-enabled and --manifest") {
			t.Fatalf("%s: code=%d output=%s", operation, code, errout.String())
		}
	}
}
