package desktopagent

import (
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
		if got := requestErrorCode(tc.err); got != tc.code {
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
		if got := requestErrorCode(err); got != "PROTOCOL_UNSUPPORTED" {
			t.Fatalf("%v classified as %s", err, got)
		}
	}
}
