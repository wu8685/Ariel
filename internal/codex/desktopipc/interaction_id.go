package desktopipc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// InteractionID binds a Web card to the complete native pending request.
// It reveals neither the native request ID nor command/question contents.
func InteractionID(raw json.RawMessage) (string, error) {
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil || !validRequestID(value["id"]) {
		return "", ErrProtocol
	}
	var canonical any
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&canonical) != nil {
		return "", ErrProtocol
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", ErrProtocol
	}
	digest := sha256.Sum256(encoded)
	return "req:" + hex.EncodeToString(digest[:16]), nil
}

func InteractionIDWithContext(request, contextItem json.RawMessage) (string, error) {
	id, err := InteractionID(request)
	if err != nil {
		return "", err
	}
	var item any
	decoder := json.NewDecoder(bytes.NewReader(contextItem))
	decoder.UseNumber()
	if decoder.Decode(&item) != nil || item == nil {
		return "", ErrProtocol
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return "", ErrProtocol
	}
	digest := sha256.Sum256(encoded)
	return id + ":" + hex.EncodeToString(digest[:16]), nil
}
