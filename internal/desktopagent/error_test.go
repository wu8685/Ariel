package desktopagent

import (
	"context"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

func TestLargeSessionErrorsAreScopedAndClassified(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{desktopipc.ErrFrameTooLarge, "HISTORY_TOO_LARGE"},
		{appserver.ErrResponseTooLarge, "HISTORY_TOO_LARGE"},
		{desktopipc.ErrOverloaded, "OVERLOADED"},
	} {
		if got := requestErrorCode("thread.read", tc.err); got != tc.code {
			t.Fatalf("%v classified as %s, want %s", tc.err, got, tc.code)
		}
	}
}

func TestNativeCompatibilityErrorsAreExplicitlyClassified(t *testing.T) {
	for _, err := range []error{
		ErrNativeShape,
		appserver.ErrMethodUnavailable,
		desktopipc.ErrProtocol,
	} {
		if got := requestErrorCode("thread.read", err); got != "PROTOCOL_UNSUPPORTED" {
			t.Fatalf("%v classified as %s", err, got)
		}
	}
}

func TestNativeReadDeadlineIsExplicitlyClassified(t *testing.T) {
	if got := requestErrorCode("thread.list", context.DeadlineExceeded); got != "TIMEOUT" {
		t.Fatalf("deadline classified as %s, want TIMEOUT", got)
	}
	if got := requestErrorCode("turn.start", context.DeadlineExceeded); got != "OUTCOME_UNKNOWN" {
		t.Fatalf("mutation deadline classified as %s, want OUTCOME_UNKNOWN", got)
	}
}
