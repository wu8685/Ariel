package relay

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// Task credentials are private deployment configuration, never event payloads.
// A shared legacy Agent token grants no task role and cannot claim a reserved ID.
type TaskDeviceCredential struct {
	Token            string   `json:"token"`
	TaskStoreIDs     []string `json:"taskStoreIds,omitempty"`
	ExecutionTargets []string `json:"executionTargets,omitempty"`
	Execute          bool     `json:"execute,omitempty"`
}

func LoadTaskDeviceCredentials(enabled bool, path string) (map[string]TaskDeviceCredential, error) {
	if !enabled {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("task device credential file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64<<10 {
		return nil, errors.New("task device credential file must be private and bounded")
	}
	var out map[string]TaskDeviceCredential
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil {
		return nil, errors.New("invalid task device credential configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid task device credential configuration")
	}
	return out, validateTaskDevices(out, "")
}

func validateTaskDevices(devices map[string]TaskDeviceCredential, legacyToken string) error {
	if len(devices) == 0 || len(devices) > 64 {
		return errors.New("tasks require bounded per-device credentials")
	}
	seen := map[string]bool{}
	for id, device := range devices {
		if id == "" || len(id) > 128 || len(device.Token) < 32 || device.Token == legacyToken || seen[device.Token] || len(device.TaskStoreIDs) > 16 || len(device.ExecutionTargets) > 64 {
			return errors.New("invalid or shared task device credential")
		}
		seen[device.Token] = true
		for _, store := range device.TaskStoreIDs {
			if store == "" || len(store) > 128 {
				return errors.New("invalid task store authorization")
			}
		}
		for _, target := range device.ExecutionTargets {
			if _, ok := devices[target]; !ok {
				return errors.New("task target requires its own device credential")
			}
		}
	}
	return nil
}

func containsTaskValue(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (s *Server) authorizeAgent(deviceID, token string) bool {
	if s.cfg.TasksEnabled {
		if credential, ok := s.cfg.TaskDevices[deviceID]; ok {
			return subtle.ConstantTimeCompare([]byte(token), []byte(credential.Token)) == 1
		}
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.Token)) == 1
}
