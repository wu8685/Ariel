package probe

import (
	"errors"
	"strings"
	"testing"
)

func TestBinarySelectionMatchesDesktopAndHonorsExplicitPath(t *testing.T) {
	versions := map[string]string{"/bundle/codex": "0.160.0", "/chosen/codex": "0.160.0", "/path/codex": "0.145.0"}
	read := func(path string) (string, error) {
		if v, ok := versions[path]; ok {
			return v, nil
		}
		return "", errors.New("missing")
	}
	for _, tc := range []struct {
		explicit, want string
		fail           bool
	}{
		{"", "/bundle/codex", false}, {"/chosen/codex", "/chosen/codex", false}, {"/path/codex", "", true}, {"/missing", "", true},
	} {
		got, _, err := SelectBinary(tc.explicit, "/bundle/codex", read)
		if (err != nil) != tc.fail || got != tc.want {
			t.Fatalf("explicit %q: got %q %v", tc.explicit, got, err)
		}
	}
	if _, _, err := SelectBinary("", "/missing", read); err == nil {
		t.Fatal("missing bundle silently substituted")
	}
}

func TestIPCProfileUsesMinimumVersionsAndLabelsEvidence(t *testing.T) {
	for _, tc := range []struct {
		desktop, cli string
		status       IPCProfileStatus
		fail         bool
	}{
		{"26.930.31730", "0.160.0", IPCProfileVerified, false},
		{"v26.930.31730", "v0.160.0", IPCProfileVerified, false},
		{"26.930.51102", "0.160.0", IPCProfileUnverified, false},
		{"26.931.1", "0.160.0", IPCProfileUnverified, false},
		{"27.0.0", "0.161.0", IPCProfileUnverified, false},
		{"26.930.31730", "0.161.0", IPCProfileUnverified, false},
		{"26.930.9999", "0.160.0", IPCProfileUnsupported, true},
		{"26.930.31730", "0.160.0-alpha.1", IPCProfileUnsupported, true},
		{"26.930.31729", "0.999.0", IPCProfileUnsupported, true},
		{"26.999.0", "0.159.999", IPCProfileUnsupported, true},
		{"", "0.160.0", IPCProfileUnsupported, true},
		{"26.930", "0.160.0", IPCProfileUnsupported, true},
		{"26.930.latest", "0.160.0", IPCProfileUnsupported, true},
	} {
		got, err := CheckIPCProfile(tc.desktop, tc.cli)
		if got != tc.status || (err != nil) != tc.fail {
			t.Fatalf("%+v: status=%q err=%v", tc, got, err)
		}
		if tc.fail && (!strings.Contains(err.Error(), MinimumDesktopVersion) || !strings.Contains(err.Error(), MinimumCodexVersion)) {
			t.Fatalf("failure omits minimum versions: %v", err)
		}
	}
}
