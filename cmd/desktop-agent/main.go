package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/desktopagent"
	"github.com/wu8685/Ariel/internal/probe"
)

type config struct{ url, token, deviceID, deviceName, appPath, socket string }

func configFrom(relayURL, token, deviceID, deviceName, appPath, socket string) (config, error) {
	if relayURL == "" || token == "" || deviceID == "" || deviceName == "" || appPath == "" || socket == "" {
		return config{}, errors.New("Relay URL, token, device ID/name, app path and IPC socket are required")
	}
	parsed, err := url.Parse(relayURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" || parsed.Path != "/ws" || parsed.RawQuery != "" {
		return config{}, errors.New("ARIEL_RELAY_URL must be ws(s)://host/ws")
	}
	return config{relayURL, token, deviceID, deviceName, appPath, socket}, nil
}

func main() {
	defaults, err := probe.Defaults()
	if err != nil {
		log.Fatal(err)
	}
	appPath := os.Getenv("ARIEL_APP_PATH")
	if appPath == "" {
		appPath = defaults.AppPath
	}
	socket := os.Getenv("ARIEL_IPC_SOCKET")
	if socket == "" {
		socket = defaults.Socket
	}
	cfg, err := configFrom(os.Getenv("ARIEL_RELAY_URL"), os.Getenv("ARIEL_TOKEN"), os.Getenv("ARIEL_DEVICE_ID"), os.Getenv("ARIEL_DEVICE_NAME"), appPath, socket)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	desktopVersion, err := probe.DesktopVersion(ctx, cfg.appPath)
	if err != nil {
		log.Fatal("Desktop version unavailable")
	}
	binary, cliVersion, err := probe.SelectBinary(os.Getenv("ARIEL_CODEX_BINARY"), probe.BundledBinary(cfg.appPath), func(p string) (string, error) { return probe.BinaryVersion(ctx, p) })
	if err != nil {
		log.Fatal(err)
	}
	if !probe.IPCProfileVerified(desktopVersion, cliVersion) {
		log.Fatalf("Desktop IPC profile not verified for Desktop %s / Codex %s", desktopVersion, cliVersion)
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := desktopagent.Run(ctx, desktopagent.Config{URL: cfg.url, Token: cfg.token, DeviceID: cfg.deviceID, DeviceName: cfg.deviceName, Binary: binary, Socket: cfg.socket})
		if ctx.Err() != nil {
			break
		}
		log.Printf("Desktop Agent disconnected: %v; reconnecting", err)
		wait := time.Duration(float64(backoff) * (0.8 + rand.Float64()*0.4))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}
