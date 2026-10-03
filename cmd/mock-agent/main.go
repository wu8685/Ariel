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

	"github.com/wu8685/Ariel/internal/mockagent"
)

type config struct{ url, token, deviceID, deviceName string }

func configFrom(relayURL, token, deviceID, deviceName string) (config, error) {
	if relayURL == "" || token == "" || deviceID == "" || deviceName == "" {
		return config{}, errors.New("ARIEL_RELAY_URL, ARIEL_TOKEN, ARIEL_DEVICE_ID and ARIEL_DEVICE_NAME are required")
	}
	parsed, err := url.Parse(relayURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" || parsed.Path != "/ws" || parsed.RawQuery != "" {
		return config{}, errors.New("ARIEL_RELAY_URL must be ws(s)://host/ws")
	}
	return config{relayURL, token, deviceID, deviceName}, nil
}

func main() {
	cfg, err := configFrom(os.Getenv("ARIEL_RELAY_URL"), os.Getenv("ARIEL_TOKEN"), os.Getenv("ARIEL_DEVICE_ID"), os.Getenv("ARIEL_DEVICE_NAME"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	backoff := time.Second
	for ctx.Err() == nil {
		err := mockagent.Run(ctx, mockagent.Config{URL: cfg.url, Token: cfg.token, DeviceID: cfg.deviceID, DeviceName: cfg.deviceName, Store: mockagent.NewStore(mockagent.StoreConfig{})})
		if ctx.Err() != nil {
			break
		}
		log.Printf("Mock Agent disconnected: %T; reconnecting", err)
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
