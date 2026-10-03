package desktopipc

import (
	"encoding/json"
	"testing"
)

func TestFileChangeItemUsesReferencedCanonicalTurnAndExactItem(t *testing.T) {
	state := json.RawMessage(`{"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"active"}]}],"entitiesByKey":{"active":{"turnId":"turn","items":[{"id":"file","type":"fileChange","status":"inProgress","changes":[{"path":"/fixture/note.txt","kind":{"type":"add"},"diff":"approved\\n"}]}]},"orphan":{"turnId":"turn","items":[{"id":"orphan","type":"fileChange","changes":[{"diff":"evil"}]}]}}}}}`)
	item, err := FileChangeItem(state, "turn", "file")
	if err != nil || len(item) == 0 {
		t.Fatalf("file item: %s %v", item, err)
	}
	if _, err := FileChangeItem(state, "turn", "orphan"); err == nil {
		t.Fatal("orphan file change counted as live")
	}
}
