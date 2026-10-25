package desktopagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	chillingclient "github.com/wu8685/Ariel/internal/chilling"
)

type fakeFileContext struct {
	ready      bool
	prepares   int
	prepareErr error
}

func (f *fakeFileContext) Ready() bool             { return f.ready }
func (f *fakeFileContext) MessageReferences() bool { return true }
func (f *fakeFileContext) WorkspaceBrowser() bool  { return true }
func (f *fakeFileContext) Changes(context.Context, chillingclient.ChangesRequest) (chillingclient.ChangesResponse, error) {
	return chillingclient.ChangesResponse{}, nil
}
func (f *fakeFileContext) Read(context.Context, chillingclient.ReadRequest) (chillingclient.ReadResponse, error) {
	return chillingclient.ReadResponse{}, nil
}
func (f *fakeFileContext) ReferenceRead(context.Context, chillingclient.ReferenceReadRequest) (chillingclient.ReadResponse, error) {
	return chillingclient.ReadResponse{}, nil
}
func (f *fakeFileContext) WorkspaceList(context.Context, chillingclient.WorkspaceListRequest) (chillingclient.WorkspaceListResponse, error) {
	return chillingclient.WorkspaceListResponse{Revision: "r"}, nil
}
func (f *fakeFileContext) WorkspaceRead(context.Context, chillingclient.WorkspaceReadRequest) (chillingclient.ReadResponse, error) {
	return chillingclient.ReadResponse{Path: "README.md", Content: "hello"}, nil
}
func (f *fakeFileContext) Prepare(_ context.Context, request chillingclient.PrepareRequest) (chillingclient.PrepareResponse, error) {
	f.prepares++
	if f.prepareErr != nil {
		return chillingclient.PrepareResponse{}, f.prepareErr
	}
	return chillingclient.PrepareResponse{Envelope: "<ariel_file_context>\n{}\n</ariel_file_context>"}, nil
}

func TestPrepareMessageIsZeroCallWithoutSelections(t *testing.T) {
	f := &fakeFileContext{ready: true}
	a := &agent{fileContext: f}
	got, err := a.prepareMessage(context.Background(), "hello", nil)
	if err != nil || got != "hello" || f.prepares != 0 {
		t.Fatalf("prepare: %q calls=%d err=%v", got, f.prepares, err)
	}
}
func TestPrepareMessageRequiresReadyGatewayAndAppendsCanonicalEnvelope(t *testing.T) {
	selection := []chillingclient.Selection{{ThreadID: "t", TurnID: "turn", ItemID: "item", Path: "a.go", SHA256: strings.Repeat("a", 64), StartLine: 1, EndLine: 1, Text: "x"}}
	if _, err := (&agent{}).prepareMessage(context.Background(), "question", selection); err == nil || err.Error() != "FEATURE_DISABLED" {
		t.Fatalf("disabled: %v", err)
	}
	f := &fakeFileContext{ready: true}
	got, err := (&agent{fileContext: f}).prepareMessage(context.Background(), "question", selection)
	if err != nil || got != "question\n\n<ariel_file_context>\n{}\n</ariel_file_context>" || f.prepares != 1 {
		t.Fatalf("prepared: %q %d %v", got, f.prepares, err)
	}
	f.prepareErr = errors.New("FILE_CHANGED")
	if _, err := (&agent{fileContext: f}).prepareMessage(context.Background(), "question", selection); err == nil || err.Error() != "FILE_CHANGED" {
		t.Fatalf("changed: %v", err)
	}
}
