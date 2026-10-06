package startup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDiscoverPrivateLANIPv4(t *testing.T) {
	for _, tc := range []struct {
		name, route, address, want string
		fail                       bool
	}{
		{"ten", "route to: default\n interface: en0\n", "10.2.3.4\n", "10.2.3.4", false},
		{"172 lower", "interface: en7\n", "172.16.0.1\n", "172.16.0.1", false},
		{"172 upper", "interface: en7\n", "172.31.255.254\n", "172.31.255.254", false},
		{"192", "interface: en0\n", "192.168.50.7\n", "192.168.50.7", false},
		{"public", "interface: en0\n", "8.8.8.8\n", "", true},
		{"loopback", "interface: lo0\n", "127.0.0.1\n", "", true},
		{"link local", "interface: en0\n", "169.254.1.2\n", "", true},
		{"ipv6", "interface: en0\n", "fd00::1\n", "", true},
		{"malformed address", "interface: en0\n", "not-an-ip\n", "", true},
		{"missing interface", "route to: default\n", "192.168.1.2\n", "", true},
		{"unsafe interface", "interface: ../../bad\n", "192.168.1.2\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(_ context.Context, command string, _ ...string) (string, error) {
				switch filepath.Base(command) {
				case "route":
					return tc.route, nil
				case "ipconfig":
					return tc.address, nil
				default:
					return "", errors.New("unexpected command")
				}
			}
			got, err := discoverPrivateLANIPv4(context.Background(), run)
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestDiscoverPrivateLANIPv4PropagatesCommandFailures(t *testing.T) {
	routeFailure := func(context.Context, string, ...string) (string, error) { return "", errors.New("route failed") }
	if _, err := discoverPrivateLANIPv4(context.Background(), routeFailure); err == nil {
		t.Fatal("route failure accepted")
	}
	ipFailure := func(_ context.Context, command string, _ ...string) (string, error) {
		if filepath.Base(command) == "route" {
			return "interface: en0\n", nil
		}
		return "", errors.New("ipconfig failed")
	}
	if _, err := discoverPrivateLANIPv4(context.Background(), ipFailure); err == nil {
		t.Fatal("ipconfig failure accepted")
	}
}
