package desktopipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
)

// PermissionRequest keeps the complete native request and grants no more than
// the explicitly requested profile, always for the current turn only.
type PermissionRequest struct {
	ID        json.RawMessage
	TurnID    string
	ItemID    string
	Reason    string
	Profile   json.RawMessage
	Grantable bool
	grant     map[string]json.RawMessage
}

func ParsePermissionRequest(raw json.RawMessage, threadID, cwd string) (PermissionRequest, error) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ThreadID    string          `json:"threadId"`
			TurnID      string          `json:"turnId"`
			ItemID      string          `json:"itemId"`
			CWD         string          `json:"cwd"`
			Reason      *string         `json:"reason"`
			Permissions json.RawMessage `json:"permissions"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &request) != nil || request.Method != "item/permissions/requestApproval" || !validRequestID(request.ID) || request.Params.ThreadID != threadID || request.Params.TurnID == "" || request.Params.ItemID == "" || request.Params.CWD != cwd {
		return PermissionRequest{}, ErrProtocol
	}
	p := PermissionRequest{ID: request.ID, TurnID: request.Params.TurnID, ItemID: request.Params.ItemID, Profile: request.Params.Permissions}
	if request.Params.Reason != nil {
		p.Reason = *request.Params.Reason
	}
	p.grant, p.Grantable = validatePermissionProfile(p.Profile)
	return p, nil
}

func validatePermissionProfile(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var profile map[string]json.RawMessage
	if json.Unmarshal(raw, &profile) != nil || len(profile) != 2 {
		return nil, false
	}
	network, netOK := profile["network"]
	files, filesOK := profile["fileSystem"]
	if !netOK || !filesOK {
		return nil, false
	}
	grant := make(map[string]json.RawMessage)
	if !bytes.Equal(bytes.TrimSpace(network), []byte("null")) {
		var n struct {
			Enabled *bool `json:"enabled"`
		}
		if strictDecode(network, &n) != nil || n.Enabled == nil || !*n.Enabled {
			return nil, false
		}
		grant["network"] = network
	}
	if !bytes.Equal(bytes.TrimSpace(files), []byte("null")) {
		var f struct {
			Read             json.RawMessage   `json:"read"`
			Write            json.RawMessage   `json:"write"`
			GlobScanMaxDepth *int              `json:"globScanMaxDepth"`
			Entries          []json.RawMessage `json:"entries"`
		}
		if strictDecode(files, &f) != nil || len(f.Read) == 0 || len(f.Write) == 0 || (f.GlobScanMaxDepth != nil && *f.GlobScanMaxDepth < 0) || !validPermissionPaths(f.Read) || !validPermissionPaths(f.Write) {
			return nil, false
		}
		for _, entry := range f.Entries {
			if !validPermissionEntry(entry) {
				return nil, false
			}
		}
		grant["fileSystem"] = files
	}
	return grant, len(grant) > 0
}

func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing data")
	}
	return nil
}

func validPermissionPaths(raw json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var paths []string
	if json.Unmarshal(raw, &paths) != nil || paths == nil {
		return false
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return false
		}
	}
	return true
}

func validPermissionEntry(raw json.RawMessage) bool {
	var entry struct {
		Access string          `json:"access"`
		Path   json.RawMessage `json:"path"`
	}
	if strictDecode(raw, &entry) != nil || (entry.Access != "read" && entry.Access != "write" && entry.Access != "deny") {
		return false
	}
	var path struct {
		Type    string          `json:"type"`
		Path    string          `json:"path"`
		Pattern string          `json:"pattern"`
		Value   json.RawMessage `json:"value"`
	}
	if strictDecode(entry.Path, &path) != nil {
		return false
	}
	switch path.Type {
	case "path":
		return filepath.IsAbs(path.Path) && path.Pattern == "" && len(path.Value) == 0
	case "glob_pattern":
		return path.Pattern != "" && path.Path == "" && len(path.Value) == 0
	case "special":
		return path.Path == "" && path.Pattern == "" && validSpecialPermissionPath(path.Value)
	default:
		return false
	}
}

func validSpecialPermissionPath(raw json.RawMessage) bool {
	var path struct {
		Kind    string  `json:"kind"`
		Subpath *string `json:"subpath"`
		Path    string  `json:"path"`
	}
	if strictDecode(raw, &path) != nil {
		return false
	}
	switch path.Kind {
	case "root", "minimal", "tmpdir", "slash_tmp":
		return path.Subpath == nil && path.Path == ""
	case "project_roots":
		return path.Path == ""
	case "unknown":
		return path.Path != ""
	default:
		return false
	}
}
