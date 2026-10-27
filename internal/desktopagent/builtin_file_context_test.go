package desktopagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wu8685/Ariel/internal/filecontext"
)

func TestFileResponseIsBoundedBeforeTransport(t *testing.T) {
	_, err := responseMap(filecontext.ReadResponse{Content: strings.Repeat("\\", 1<<20)}, nil)
	if err == nil || err.Error() != "FILE_TOO_LARGE" {
		t.Fatalf("oversized escaped JSON response accepted: %v", err)
	}
}

func TestFileSelectionsCannotBeSentIntoAnotherThread(t *testing.T) {
	selection := []filecontext.Selection{{ThreadID: "one"}}
	if !fileSelectionsMatchThread("one", selection) || fileSelectionsMatchThread("two", selection) {
		t.Fatal("selection target fencing is incorrect")
	}
}

type builtinProof struct {
	root  string
	calls int
}

func (p *builtinProof) Call(_ context.Context, method string, params, result any) error {
	p.calls++
	var value any
	switch method {
	case "thread/read":
		value = map[string]any{"thread": map[string]any{"id": "t", "cwd": p.root}}
	case "thread/items/list":
		value = map[string]any{"data": []any{map[string]any{"id": "i", "type": "fileChange", "changes": []any{map[string]any{"path": "a.md", "kind": map[string]any{"type": "update"}, "diff": "+hello"}}}}}
	default:
		tell := method
		return &filecontext.Error{Code: filecontext.CodeProtocolUnsupported, Message: tell}
	}
	raw, _ := json.Marshal(value)
	return json.Unmarshal(raw, result)
}
func TestBuiltinFileContextNeedsNoChillingEndpoint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	rpc := &builtinProof{root: root}
	cfg := Config{FileContextEnabled: true, WorkspaceBrowserEnabled: true, ChillingEndpoint: "unix:///no-service/file-context.sock"}
	configureFileContext(&cfg, rpc)
	if cfg.FileContext == nil || !cfg.FileContext.Ready() {
		t.Fatal("built-in provider not ready without Chilling")
	}
	changes, err := cfg.FileContext.Changes(context.Background(), filecontext.ChangesRequest{ThreadID: "t", TurnIDs: []string{"turn"}})
	if err != nil || len(changes.Turns) != 1 || changes.Turns[0].FileChanges[0].Diff != "+hello" {
		t.Fatalf("changes: %+v %v", changes, err)
	}
	got, err := cfg.FileContext.WorkspaceRead(context.Background(), filecontext.WorkspaceReadRequest{ThreadID: "t", Path: "a.md"})
	if err != nil || got.Content != "hello" {
		t.Fatalf("read: %+v %v", got, err)
	}
	quoted, err := cfg.FileContext.Prepare(context.Background(), filecontext.PrepareRequest{Selections: []filecontext.Selection{{Source: filecontext.SelectionSourceWorkspaceBrowser, ThreadID: "t", Path: got.Path, SHA256: got.SHA256, ModifiedAtMS: got.ModifiedAtMS, StartLine: 1, EndLine: 1, Text: "hello"}}})
	if err != nil || len(quoted.Quotes) != 1 {
		t.Fatalf("quote: %+v %v", quoted, err)
	}
	cfg = Config{}
	configureFileContext(&cfg, rpc)
	if cfg.FileContext != nil {
		t.Fatal("disabled provider initialized")
	}
}
