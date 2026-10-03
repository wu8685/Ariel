package main

import "testing"

func TestConfigurationRequiresRelayURLTokenAndStableDevice(t *testing.T) {
	for _, cfg := range []struct{ url, token, device string }{{"", "secret", "mac"}, {"ws://localhost:8080/ws", "", "mac"}, {"ws://localhost:8080/ws", "secret", ""}} {
		if _, err := configFrom(cfg.url, cfg.token, cfg.device, "My Mac"); err == nil {
			t.Fatalf("accepted missing field: %+v", cfg)
		}
	}
	if _, err := configFrom("ws://localhost:8080/ws", "secret", "mac", "My Mac"); err != nil {
		t.Fatal(err)
	}
}
