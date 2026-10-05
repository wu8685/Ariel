package startup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type options struct {
	command, mode, listen, relayURL, deviceID, deviceName string
	tokenFile, pinFile                                    string
	allowInsecureWS, replace                              bool
}

type config struct {
	Mode            string `json:"mode"`
	Listen          string `json:"listen,omitempty"`
	RelayURL        string `json:"relayUrl"`
	DeviceID        string `json:"deviceId"`
	DeviceName      string `json:"deviceName"`
	AllowInsecureWS bool   `json:"allowInsecureWs,omitempty"`
}

var deviceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var pinPattern = regexp.MustCompile(`^[0-9]{6}$`)

func parseArgs(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, errors.New("choose up, status, stop, show-pin or help")
	}
	opts := options{command: args[0]}
	if opts.command != "up" {
		if opts.command != "status" && opts.command != "stop" && opts.command != "show-pin" && opts.command != "help" {
			return options{}, fmt.Errorf("unknown command %q", opts.command)
		}
		if len(args) != 1 {
			return options{}, fmt.Errorf("%s takes no arguments", opts.command)
		}
		return opts, nil
	}
	args = args[1:]
	if len(args) != 0 && (args[0] == "local" || args[0] == "agent") {
		opts.mode, args = args[0], args[1:]
	}
	if opts.mode == "" && len(args) != 0 {
		return options{}, errors.New("plain up takes no flags; choose up local or up agent")
	}
	seen := map[string]bool{}
	for len(args) != 0 {
		key := args[0]
		args = args[1:]
		if seen[key] {
			return options{}, fmt.Errorf("duplicate option %s", key)
		}
		seen[key] = true
		if key == "--allow-insecure-ws" || key == "--replace-config" {
			if key == "--allow-insecure-ws" {
				opts.allowInsecureWS = true
			} else {
				opts.replace = true
			}
			continue
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "--") {
			return options{}, fmt.Errorf("%s needs a value", key)
		}
		value := args[0]
		args = args[1:]
		switch key {
		case "--listen":
			opts.listen = value
		case "--relay-url":
			opts.relayURL = value
		case "--device-id":
			opts.deviceID = value
		case "--device-name":
			opts.deviceName = value
		case "--token-file":
			opts.tokenFile = value
		case "--pin-file":
			opts.pinFile = value
		default:
			return options{}, fmt.Errorf("unknown option %s", key)
		}
	}
	return opts, nil
}

func validateListen(listen string) error {
	host, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return errors.New("--listen must be an explicit local IP:port")
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(portText)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || err != nil || port < 1 || port > 65535 {
		return errors.New("--listen must be an explicit local IP and valid port; wildcard binding is refused")
	}
	return nil
}

func validateRelayURL(value string, allowInsecure bool) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.Path != "/ws" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || (u.Scheme != "wss" && u.Scheme != "ws") {
		return errors.New("Relay URL must be wss://host/ws (or explicitly allowed ws://host/ws)")
	}
	if u.Scheme == "ws" && !allowInsecure {
		return errors.New("plain ws:// requires --allow-insecure-ws on a trusted LAN")
	}
	return nil
}

func validateConfig(cfg config) error {
	if !deviceIDPattern.MatchString(cfg.DeviceID) || strings.TrimSpace(cfg.DeviceName) == "" || strings.ContainsAny(cfg.DeviceName, "\r\n\x00") {
		return errors.New("device ID/name is missing or invalid")
	}
	if cfg.Mode == "local" {
		if err := validateListen(cfg.Listen); err != nil {
			return err
		}
		expected := "ws://" + cfg.Listen + "/ws"
		if cfg.RelayURL != expected || cfg.AllowInsecureWS {
			return errors.New("local Relay URL does not match listen address")
		}
		return nil
	}
	if cfg.Mode != "agent" || cfg.Listen != "" {
		return errors.New("mode must be local or agent")
	}
	return validateRelayURL(cfg.RelayURL, cfg.AllowInsecureWS)
}

func configForOptions(opts options) (config, error) {
	if opts.deviceID == "" || opts.deviceName == "" {
		return config{}, errors.New("--device-id and --device-name are required")
	}
	cfg := config{Mode: opts.mode, Listen: opts.listen, RelayURL: opts.relayURL, DeviceID: opts.deviceID, DeviceName: opts.deviceName, AllowInsecureWS: opts.allowInsecureWS}
	switch opts.mode {
	case "local":
		if opts.listen == "" || opts.relayURL != "" || opts.allowInsecureWS {
			return config{}, errors.New("up local needs --listen and cannot take --relay-url or --allow-insecure-ws")
		}
		cfg.RelayURL = "ws://" + opts.listen + "/ws"
	case "agent":
		if opts.relayURL == "" || opts.listen != "" || opts.pinFile != "" {
			return config{}, errors.New("up agent needs --relay-url and cannot take --listen or --pin-file")
		}
	default:
		return config{}, errors.New("choose up local or up agent")
	}
	return cfg, validateConfig(cfg)
}

func runtimeDir(root string) string { return filepath.Join(root, ".local", "runtime") }
func configPath(root string) string { return filepath.Join(runtimeDir(root), "config.json") }

func checkPrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("private directory %s must not be a symlink or file", path)
	}
	return nil
}

func ensureRuntime(root string) error {
	local := filepath.Join(root, ".local")
	if err := os.MkdirAll(local, 0700); err != nil {
		return err
	}
	if err := checkPrivateDirectory(local); err != nil {
		return err
	}
	runtime := runtimeDir(root)
	if err := os.MkdirAll(runtime, 0700); err != nil {
		return err
	}
	if err := checkPrivateDirectory(runtime); err != nil {
		return err
	}
	return os.Chmod(runtime, 0700)
}

func loadConfig(root string) (config, error) {
	var cfg config
	if err := checkPrivateDirectory(filepath.Join(root, ".local")); err != nil {
		return cfg, err
	}
	if err := checkPrivateDirectory(runtimeDir(root)); err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(configPath(root))
	if err != nil {
		return cfg, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("invalid saved config: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return config{}, fmt.Errorf("invalid saved config: %w", err)
	}
	return cfg, nil
}

func readCredential(path string, token bool) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read credential file: %w", err)
	}
	if len(data) > 4096 {
		return "", errors.New("credential file is too large")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if token {
		if len(value) < 32 || strings.ContainsAny(value, "\r\n\x00 \t") {
			return "", errors.New("Agent token must be at least 32 non-whitespace characters")
		}
	} else if !pinPattern.MatchString(value) {
		return "", errors.New("Web PIN must be exactly six ASCII digits")
	}
	return value, nil
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func randomPIN() (string, error) {
	var data [4]byte
	for {
		if _, err := rand.Read(data[:]); err != nil {
			return "", err
		}
		value := uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
		if value < 4294000000 { // Largest multiple of 1,000,000 below 2^32.
			return fmt.Sprintf("%06d", value%1000000), nil
		}
	}
}

func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".ariel-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func prepareConfig(root string, opts options) (config, error) {
	cfg, err := configForOptions(opts)
	if err != nil {
		return config{}, err
	}
	if err := ensureRuntime(root); err != nil {
		return config{}, err
	}
	existing, oldErr := loadConfig(root)
	if oldErr != nil && !os.IsNotExist(oldErr) {
		return config{}, oldErr
	}
	if oldErr == nil && existing == cfg && !opts.replace {
		for _, source := range []struct {
			provided string
			stored   string
			token    bool
		}{{opts.tokenFile, "agent-token", true}, {opts.pinFile, "web-pin", false}} {
			if source.provided == "" {
				continue
			}
			incoming, err := readCredential(source.provided, source.token)
			if err != nil {
				return config{}, err
			}
			current, err := readCredential(filepath.Join(runtimeDir(root), source.stored), source.token)
			if err != nil {
				return config{}, err
			}
			if incoming != current {
				return config{}, errors.New("credential differs; stop and repeat with --replace-config")
			}
		}
		return existing, nil
	}
	if oldErr == nil && !opts.replace {
		return config{}, errors.New("saved configuration differs; stop and repeat with --replace-config")
	}
	if opts.mode == "agent" && (oldErr != nil || existing.Mode != "agent" || existing.RelayURL != cfg.RelayURL) && opts.tokenFile == "" {
		return config{}, errors.New("new external Relay requires --token-file")
	}
	var token, pin string
	if opts.tokenFile != "" {
		token, err = readCredential(opts.tokenFile, true)
	} else if oldErr == nil && existing.Mode == cfg.Mode {
		token, err = readCredential(filepath.Join(runtimeDir(root), "agent-token"), true)
	} else {
		token, err = randomToken()
	}
	if err != nil {
		return config{}, err
	}
	if cfg.Mode == "local" {
		if opts.pinFile != "" {
			pin, err = readCredential(opts.pinFile, false)
		} else if oldErr == nil && existing.Mode == "local" {
			pin, err = readCredential(filepath.Join(runtimeDir(root), "web-pin"), false)
		} else {
			pin, err = randomPIN()
		}
		if err != nil {
			return config{}, err
		}
		if token == pin {
			return config{}, errors.New("Agent token and Web PIN must differ")
		}
	}
	runtime := runtimeDir(root)
	if err := writePrivate(filepath.Join(runtime, "agent-token"), []byte(token+"\n")); err != nil {
		return config{}, err
	}
	if cfg.Mode == "local" {
		if err := writePrivate(filepath.Join(runtime, "web-pin"), []byte(pin+"\n")); err != nil {
			return config{}, err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return config{}, err
	}
	if err := writePrivate(configPath(root), append(data, '\n')); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func buildSteps(cfg config) []string {
	if cfg.Mode == "local" {
		return []string{"npm ci", "npm run build", "go build relay", "go build desktop-agent"}
	}
	return []string{"go build desktop-agent"}
}

func localAgentEnvironment(base []string, host string) []string {
	out := make([]string, 0, len(base)+2)
	var noProxy string
	for _, item := range base {
		if strings.HasPrefix(item, "NO_PROXY=") || strings.HasPrefix(item, "no_proxy=") {
			if noProxy == "" {
				noProxy = strings.SplitN(item, "=", 2)[1]
			}
			continue
		}
		out = append(out, item)
	}
	if noProxy != "" {
		noProxy += ","
	}
	noProxy += host
	return append(out, "NO_PROXY="+noProxy, "no_proxy="+noProxy)
}

func commandMatches(expected, actual string) bool {
	return filepath.Clean(strings.TrimSpace(expected)) == filepath.Clean(strings.TrimSpace(actual))
}
