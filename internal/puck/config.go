package puck

import (
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	StateOff         = "off"
	StateUnavailable = "unavailable"
	StateReady       = "ready"
	StateDegraded    = "degraded"

	ReasonRequiresPasskeyHTTPS = "PUCK_REQUIRES_PASSKEY_HTTPS"
	ReasonDataDirUnwritable    = "PUCK_DATA_DIR_UNWRITABLE"
	ReasonDataDirLocked        = "PUCK_DATA_DIR_LOCKED"
	ReasonCircuitOpen          = "PUCK_CIRCUIT_OPEN"

	DefaultDataDir = "/data/puck"
)

type Config struct {
	Enabled      bool
	WebAuth      string
	PublicOrigin string
	DataDir      string
}

type Availability struct {
	State, Reason, Detail string
	DataDir               string
	Lock                  *os.File
}

func Evaluate(cfg Config) Availability {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir
	}
	out := Availability{State: StateOff, DataDir: cfg.DataDir}
	if !cfg.Enabled {
		return out
	}
	if cfg.WebAuth != "passkey" {
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonRequiresPasskeyHTTPS, "pin_mode"
		return out
	}
	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonRequiresPasskeyHTTPS, "public_origin_not_https"
		return out
	}
	if info, err := os.Lstat(cfg.DataDir); err == nil && !info.IsDir() {
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonDataDirUnwritable, "data_dir_unwritable"
		return out
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonDataDirUnwritable, "data_dir_unwritable"
		return out
	}
	if err := os.Chmod(cfg.DataDir, 0700); err != nil {
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonDataDirUnwritable, "data_dir_unwritable"
		return out
	}
	lock, err := os.OpenFile(filepath.Join(cfg.DataDir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonDataDirUnwritable, "data_dir_unwritable"
		return out
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		out.State, out.Reason, out.Detail = StateUnavailable, ReasonDataDirLocked, "data_dir_locked"
		return out
	}
	out.State, out.Lock = StateReady, lock
	return out
}

func (a *Availability) Close() error {
	if a == nil || a.Lock == nil {
		return nil
	}
	lock := a.Lock
	a.Lock = nil
	_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	return lock.Close()
}
