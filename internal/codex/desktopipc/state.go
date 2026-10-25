package desktopipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

var ErrResyncRequired = errors.New("IPC state invalid; fresh snapshot required")

// Reducer is confined to a subscription's ordered event loop. Any failed change
// invalidates the entire view; a later snapshot can establish a new baseline.
type Reducer struct {
	state    json.RawMessage
	revision int64
	limit    int
}

func NewReducer(limit int) *Reducer {
	if limit <= 0 {
		limit = int(DefaultMaxFrameBytes)
	}
	return &Reducer{limit: limit}
}

func (r *Reducer) Snapshot() (json.RawMessage, int64, bool) {
	return bytes.Clone(r.state), r.revision, r.state != nil
}

func (r *Reducer) Apply(body json.RawMessage) (err error) {
	defer func() {
		if err != nil {
			r.state = nil
			r.revision = 0
		}
	}()
	if len(body) > r.limit+64*1024 {
		return fmt.Errorf("%w: projected change envelope exceeds limit", ErrResyncRequired)
	}
	var change struct {
		Type         string          `json:"type"`
		Revision     *int64          `json:"revision"`
		BaseRevision *int64          `json:"baseRevision"`
		State        json.RawMessage `json:"conversationState"`
		Patches      []patch         `json:"patches"`
	}
	if json.Unmarshal(body, &change) != nil {
		return fmt.Errorf("%w: projected change cannot be decoded", ErrResyncRequired)
	}
	if change.Revision == nil || *change.Revision < 0 {
		return fmt.Errorf("%w: projected change has no valid revision", ErrResyncRequired)
	}
	var state any
	switch change.Type {
	case "snapshot":
		if !validObject(change.State) {
			return fmt.Errorf("%w: projected snapshot is not an object", ErrResyncRequired)
		}
		if len(change.State) > r.limit {
			return fmt.Errorf("%w: projected snapshot bytes %d exceed %d", ErrResyncRequired, len(change.State), r.limit)
		}
		if r.state != nil && *change.Revision < r.revision {
			return fmt.Errorf("%w: snapshot revision regressed", ErrResyncRequired)
		}
		state, err = decodeValue(change.State)
	case "patches":
		if r.state == nil {
			return fmt.Errorf("%w: patch arrived before projected snapshot", ErrResyncRequired)
		}
		if change.Patches == nil || change.BaseRevision == nil || *change.BaseRevision != r.revision || *change.Revision <= r.revision {
			return fmt.Errorf("%w: projected patch revision is invalid", ErrResyncRequired)
		}
		state, err = decodeValue(r.state)
		if err != nil {
			return fmt.Errorf("%w: previous projected state cannot be decoded", ErrResyncRequired)
		}
		for _, p := range change.Patches {
			var path []any
			if len(p.Path) == 0 || json.Unmarshal(p.Path, &path) != nil || path == nil {
				return fmt.Errorf("%w: projected patch path is invalid", ErrResyncRequired)
			}
			// Decode numeric path indices exactly rather than converting through float64.
			decoded, e := decodeValue(p.Path)
			if e != nil {
				return fmt.Errorf("%w: projected patch path cannot be decoded", ErrResyncRequired)
			}
			path = decoded.([]any)
			var value any
			if p.Op == "add" || p.Op == "replace" {
				if len(p.Value) == 0 {
					return fmt.Errorf("%w: projected patch value is missing", ErrResyncRequired)
				}
				value, err = decodeValue(p.Value)
				if err != nil {
					return fmt.Errorf("%w: projected patch value cannot be decoded", ErrResyncRequired)
				}
			} else if p.Op != "remove" {
				return fmt.Errorf("%w: projected patch operation is unsupported", ErrResyncRequired)
			}
			state, err = applyPatch(state, path, p.Op, value)
			if err != nil {
				return fmt.Errorf("%w: projected patch cannot be applied", ErrResyncRequired)
			}
		}
	default:
		return fmt.Errorf("%w: projected change type is unsupported", ErrResyncRequired)
	}
	if err != nil {
		return fmt.Errorf("%w: projected snapshot value cannot be decoded", ErrResyncRequired)
	}
	if _, ok := state.(map[string]any); !ok {
		return fmt.Errorf("%w: projected snapshot root is not an object", ErrResyncRequired)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("%w: projected snapshot cannot be encoded", ErrResyncRequired)
	}
	if len(encoded) > r.limit {
		return fmt.Errorf("%w: normalized snapshot bytes %d exceed %d", ErrResyncRequired, len(encoded), r.limit)
	}
	r.state = encoded
	r.revision = *change.Revision
	return nil
}

type patch struct {
	Op    string          `json:"op"`
	Path  json.RawMessage `json:"path"`
	Value json.RawMessage `json:"value"`
}

func decodeValue(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}

func applyPatch(node any, path []any, op string, value any) (any, error) {
	if node == "__ARIEL_LAZY_OMITTED__" {
		// Detail-only patches below a projected-away payload do not change the
		// resident owner summary. Advancing the revision preserves ordering.
		return node, nil
	}
	if len(path) == 0 {
		if op == "remove" {
			return nil, ErrResyncRequired
		}
		return value, nil
	}
	last := len(path) == 1
	switch n := node.(type) {
	case map[string]any:
		key, ok := path[0].(string)
		if !ok {
			return nil, ErrResyncRequired
		}
		child, exists := n[key]
		if last {
			if op != "add" && !exists {
				return nil, ErrResyncRequired
			}
			if op == "remove" {
				delete(n, key)
			} else {
				n[key] = value
			}
		} else {
			if !exists {
				return nil, ErrResyncRequired
			}
			changed, err := applyPatch(child, path[1:], op, value)
			if err != nil {
				return nil, err
			}
			n[key] = changed
		}
		return n, nil
	case []any:
		index, ok := path[0].(json.Number)
		if !ok {
			return nil, ErrResyncRequired
		}
		i, err := strconv.Atoi(string(index))
		if err != nil || i < 0 || i > len(n) {
			return nil, ErrResyncRequired
		}
		if last && op == "add" {
			n = append(n, nil)
			copy(n[i+1:], n[i:])
			n[i] = value
			return n, nil
		}
		if i >= len(n) {
			return nil, ErrResyncRequired
		}
		if last {
			if op == "remove" {
				n = append(n[:i], n[i+1:]...)
			} else {
				n[i] = value
			}
		} else {
			child, err := applyPatch(n[i], path[1:], op, value)
			if err != nil {
				return nil, err
			}
			n[i] = child
		}
		return n, nil
	default:
		return nil, ErrResyncRequired
	}
}
