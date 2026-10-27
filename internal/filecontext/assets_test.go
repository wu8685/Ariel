package filecontext

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assetFixture(t *testing.T) (*Service, AssetReadRequest, string) {
	t.Helper()
	s, f := workspaceFixture(t)
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	// The body is valid PNG with opaque trailing bytes, large enough for paging.
	body := append(imageBytes.Bytes(), bytes.Repeat([]byte{42}, 300<<10)...)
	if err := os.WriteFile(filepath.Join(f.thread.CWD, "图 片.png"), body, 0600); err != nil {
		t.Fatal(err)
	}
	text := "# 文档\n\n![图](../%E5%9B%BE%20%E7%89%87.png)\n\n![引用图][diagram]\n\n[diagram]: ../%E5%9B%BE%20%E7%89%87.png\n"
	if err := os.WriteFile(filepath.Join(f.thread.CWD, "docs", "read.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	s = NewWithOptions(f, Options{WorkspaceBrowser: true, DocumentAssets: true, AgentEpoch: "fixture"})
	doc, err := s.WorkspaceRead(context.Background(), WorkspaceReadRequest{ThreadID: f.thread.ID, Path: "docs/read.md"})
	if err != nil {
		t.Fatal(err)
	}
	return s, AssetReadRequest{DocumentRequest: DocumentRequest{Source: SelectionSourceWorkspaceBrowser, ThreadID: f.thread.ID, Path: doc.Path}, DocumentSHA256: doc.SHA256, ImageReference: "../%E5%9B%BE%20%E7%89%87.png"}, f.thread.CWD
}

func TestAssetReadIsBoundedDeclaredAndSequential(t *testing.T) {
	s, req, _ := assetFixture(t)
	first, err := s.AssetRead(context.Background(), req)
	if err != nil || first.MIME != "image/png" || first.Width != 2 || first.Height != 3 || first.NextCursor == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	raw, err := base64.StdEncoding.DecodeString(first.DataBase64)
	if err != nil || len(raw) > 256<<10 {
		t.Fatalf("chunk: %d %v", len(raw), err)
	}
	old := first.NextCursor
	req.Cursor = old
	next, err := s.AssetRead(context.Background(), req)
	if err != nil || next.Offset != len(raw) || next.AssetSHA256 != first.AssetSHA256 || next.NextCursor != "" {
		t.Fatalf("next: %+v %v", next, err)
	}
	if _, err = s.AssetRead(context.Background(), req); CodeOf(err) != CodeTimeout {
		t.Fatalf("replayed cursor: %v", err)
	}
}

func TestAssetReadRejectsForgedSourceVersionPathAndFormat(t *testing.T) {
	s, original, root := assetFixture(t)
	for _, change := range []func(*AssetReadRequest){
		func(r *AssetReadRequest) { r.DocumentSHA256 = strings.Repeat("0", 64) },
		func(r *AssetReadRequest) { r.ImageReference = "../../outside.png" },
		func(r *AssetReadRequest) { r.ImageReference = "file:///secret.png" },
		func(r *AssetReadRequest) { r.Path = "README.md" },
		func(r *AssetReadRequest) { r.Source = SelectionSourceTurnChange },
	} {
		r := original
		change(&r)
		if _, err := s.AssetRead(context.Background(), r); err == nil {
			t.Fatalf("forged accepted: %+v", r)
		}
	}
	// Code fences / ordinary links are not image authorization.
	text := "```md\n![fake](../%E5%9B%BE%20%E7%89%87.png)\n```\n[link](../%E5%9B%BE%20%E7%89%87.png)"
	if err := os.WriteFile(filepath.Join(root, "docs/read.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	doc, _ := s.WorkspaceRead(context.Background(), WorkspaceReadRequest{ThreadID: original.ThreadID, Path: original.Path})
	original.DocumentSHA256 = doc.SHA256
	if _, err := s.AssetRead(context.Background(), original); CodeOf(err) != CodePathNotAllowed {
		t.Fatalf("fake image: %v", err)
	}
}

func TestAssetCursorExpiresAndCannotCrossDocument(t *testing.T) {
	s, req, _ := assetFixture(t)
	now := time.Now()
	s.assets.now = func() time.Time { return now }
	first, err := s.AssetRead(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Cursor = first.NextCursor
	req.ThreadID = "other"
	if _, err = s.AssetRead(context.Background(), req); err == nil {
		t.Fatal("cross-document cursor accepted")
	}
	req.ThreadID = "thread-a"
	now = now.Add(31 * time.Second)
	if _, err = s.AssetRead(context.Background(), req); CodeOf(err) != CodeTimeout {
		t.Fatalf("expiry: %v", err)
	}
}

func TestAssetDecodersRejectActiveContentAndOversizedDimensions(t *testing.T) {
	s, req, root := assetFixture(t)
	var jpegBody, gifBody bytes.Buffer
	if err := jpeg.Encode(&jpegBody, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifBody, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name, mime string
		body       []byte
	}{
		{"jpeg", "image/jpeg", jpegBody.Bytes()},
		{"gif", "image/gif", gifBody.Bytes()},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "图 片.png"), sample.body, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := s.AssetRead(context.Background(), req)
			if err != nil || got.MIME != sample.mime || got.Width != 2 || got.Height != 3 {
				t.Fatalf("decoder mismatch: %s %v", got.MIME, err)
			}
		})
	}
	for _, body := range [][]byte{[]byte("<svg><script>alert(1)</script></svg>"), []byte("<html>not raster</html>"), []byte{137, 80, 78, 71}} {
		if err := os.WriteFile(filepath.Join(root, "图 片.png"), body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AssetRead(context.Background(), req); CodeOf(err) != CodeUnsupportedFileType {
			t.Fatalf("non-raster accepted: %v", err)
		}
	}
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 8193, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "图 片.png"), wide.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssetRead(context.Background(), req); CodeOf(err) != CodeFileTooLarge {
		t.Fatalf("excessive dimensions accepted: %v", err)
	}
}

func TestAbandonedAssetLeasesStayBoundedAndExpire(t *testing.T) {
	s, req, _ := assetFixture(t)
	now := time.Now()
	s.assets.now = func() time.Time { return now }
	for i := 0; i < 32; i++ {
		if _, err := s.AssetRead(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AssetRead(context.Background(), req); CodeOf(err) != CodeOverloaded {
		t.Fatalf("unbounded leases: %v", err)
	}
	if len(s.assets.leases) != 32 || s.assets.bytes > maxAssetMemory {
		t.Fatal("lease accounting exceeded capacity")
	}
	now = now.Add(31 * time.Second)
	s.assets.mu.Lock()
	s.assets.expire()
	s.assets.mu.Unlock()
	if len(s.assets.leases) != 0 || s.assets.bytes != 0 {
		t.Fatal("expired lease bytes retained")
	}
}
