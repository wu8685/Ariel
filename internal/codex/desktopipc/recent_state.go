package desktopipc

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// A native string cannot forge this marker. Exports use null, never pretend
// empty text. A turn with retired bodies may not reenter the display/proof window.
type retiredBody struct{}

var retiredJSON = []byte("null")

func (retiredBody) MarshalJSON() ([]byte, error) { return retiredJSON, nil }

func orderedNativeTurns(root map[string]any) ([]map[string]any, bool) {
	var turns []map[string]any
	if value, present := root["turnHistory"]; present {
		h, ok := value.(map[string]any)
		if !ok || h["kind"] != "canonical" {
			return nil, false
		}
		history, ok := h["history"].(map[string]any)
		if !ok {
			return nil, false
		}
		islands, ok := history["islands"].([]any)
		entities, entitiesOK := history["entitiesByKey"].(map[string]any)
		if !ok || !entitiesOK {
			return nil, false
		}
		for _, value := range islands {
			island, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			entries, ok := island["entries"].([]any)
			if !ok {
				return nil, false
			}
			for _, value := range entries {
				entry, ok := value.(map[string]any)
				if !ok {
					return nil, false
				}
				key, ok := entry["value"].(string)
				if !ok || key == "" {
					return nil, false
				}
				turn, ok := entities[key].(map[string]any)
				if !ok {
					return nil, false
				}
				turns = append(turns, turn)
			}
		}
	} else {
		values, ok := root["turns"].([]any)
		if !ok {
			return nil, false
		}
		for _, value := range values {
			turn, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			turns = append(turns, turn)
		}
	}
	return turns, true
}

func containsRetired(value any) bool {
	switch v := value.(type) {
	case retiredBody:
		return true
	case map[string]any:
		for _, child := range v {
			if containsRetired(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsRetired(child) {
				return true
			}
		}
	}
	return false
}

func requestTurnIDs(value any, ids map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		if id, ok := v["turnId"].(string); ok && id != "" {
			ids[id] = true
		}
		for _, child := range v {
			requestTurnIDs(child, ids)
		}
	case []any:
		for _, child := range v {
			requestTurnIDs(child, ids)
		}
	}
}

func retireHistoricalBodies(root map[string]any, pinned map[string]int) error {
	turns, known := orderedNativeTurns(root)
	if !known {
		return nil
	} // Unknown formats remain intact and bounded.
	requests, requestsKnown := root["requests"].([]any)
	pending := map[string]bool{}
	keepAll := !requestsKnown
	for _, request := range requests {
		ids := map[string]bool{}
		requestTurnIDs(request, ids)
		if len(ids) == 0 {
			keepAll = true
		}
		for id := range ids {
			pending[id] = true
		}
	}
	needed := map[string]bool{}
	for index, turn := range turns {
		id, _ := turn["turnId"].(string)
		status, _ := turn["status"].(string)
		terminal := status == "completed" || status == "interrupted" || status == "failed"
		if index >= len(turns)-2 || !terminal || pending[id] || pinned[id] > 0 || keepAll {
			needed[id] = true
		}
	}
	for _, turn := range turns {
		id, _ := turn["turnId"].(string)
		// A canonical entity can have more than one ordered reference. Retiring
		// through an older reference must not destroy its recent/active alias.
		if needed[id] {
			if containsRetired(turn) {
				return fmt.Errorf("%w: retired native turn became addressable again", ErrResyncRequired)
			}
			continue
		}
		if id == "" {
			continue
		}
		// Keep turn/item identities, addresses, statuses, and unknown metadata.
		if params, ok := turn["params"].(map[string]any); ok {
			retireFields(params, "input")
		}
		items, ok := turn["items"].([]any)
		if !ok {
			continue
		}
		for _, value := range items {
			item, ok := value.(map[string]any)
			if !ok {
				continue
			}
			switch item["type"] {
			case "agentMessage":
				retireFields(item, "text")
			case "userMessage":
				retireFields(item, "content", "input")
			case "steeringUserMessage":
				retireFields(item, "input", "restoreMessage")
			case "reasoning":
				retireFields(item, "summary", "content", "text")
			case "commandExecution":
				retireFields(item, "command", "output", "aggregatedOutput")
			case "mcpToolCall":
				retireFields(item, "arguments", "result", "output")
			case "fileChange":
				retireFields(item, "changes", "diff")
			case "imageGeneration":
				retireFields(item, "result")
			}
		}
	}
	return nil
}

func retireFields(object map[string]any, keys ...string) {
	for _, key := range keys {
		if value, exists := object[key]; exists && value != nil {
			object[key] = retiredBody{}
		}
	}
}

// Exact size of our decoded JSON domain with SetEscapeHTML(false), without
// allocating an encoded state. U+2028/U+2029 still use six-byte JSON escapes.
func encodedStringSize(s string) int {
	n := len(s) + 2
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' || c == '\b' || c == '\f':
			n++
		case c < 0x20:
			n += 5
		case c == 0xe2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xa8 || s[i+2] == 0xa9):
			n += 3
			i += 2
		case c >= utf8.RuneSelf:
			_, width := utf8.DecodeRuneInString(s[i:])
			i += width - 1
		}
	}
	return n
}

func encodedValueSize(value any) int {
	switch v := value.(type) {
	case nil, retiredBody:
		return 4
	case string:
		return encodedStringSize(v)
	case json.Number:
		return len(v)
	case bool:
		if v {
			return 4
		}
		return 5
	case []any:
		n := 2
		for i, child := range v {
			if i > 0 {
				n++
			}
			n += encodedValueSize(child)
		}
		return n
	case map[string]any:
		n := 2
		first := true
		for key, child := range v {
			if !first {
				n++
			}
			first = false
			n += encodedStringSize(key) + 1 + encodedValueSize(child)
		}
		return n
	default:
		panic("unexpected decoded JSON value")
	}
}
