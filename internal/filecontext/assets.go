package filecontext

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	_ "golang.org/x/image/webp"
)

const MaxAssetBytes = 4 << 20
const MaxAssetChunk = 256 << 10
const maxAssetMemory = 16 << 20

type Options struct {
	WorkspaceBrowser, DocumentAssets bool
	AgentEpoch                       string
}
type DocumentAssetCapability struct {
	Version    int `json:"version"`
	MaxBytes   int `json:"maxBytes"`
	ChunkBytes int `json:"chunkBytes"`
	SVGVersion int `json:"svgVersion,omitempty"`
}

func NewWithOptions(source Source, options Options) *Service {
	s := &Service{source: source, workspaceBrowser: options.WorkspaceBrowser, slots: make(chan struct{}, 2), waiting: make(chan struct{}, 10)}
	if options.DocumentAssets {
		s.assets = &assetStore{epoch: options.AgentEpoch, now: time.Now, leases: map[string]*assetLease{}}
	}
	return s
}
func (s *Service) DocumentAssets() bool { return s.Ready() && s.assets != nil }
func (s *Service) AssetCapability() *DocumentAssetCapability {
	if !s.DocumentAssets() {
		return nil
	}
	return &DocumentAssetCapability{Version: 1, MaxBytes: MaxAssetBytes, ChunkBytes: MaxAssetChunk, SVGVersion: 1}
}

type DocumentRequest struct {
	Source        SelectionSource `json:"source"`
	ThreadID      string          `json:"threadId"`
	TurnID        string          `json:"turnId,omitempty"`
	ItemID        string          `json:"itemId,omitempty"`
	Path          string          `json:"path,omitempty"`
	ReferenceKind ReferenceKind   `json:"referenceKind,omitempty"`
	RawReference  string          `json:"rawReference,omitempty"`
}
type AssetReadRequest struct {
	DocumentRequest
	DocumentSHA256 string `json:"documentSha256"`
	ImageReference string `json:"imageReference"`
	Cursor         string `json:"cursor,omitempty"`
	SVGVersion     int    `json:"svgVersion,omitempty"`
}
type AssetResponse struct {
	MIME        string `json:"mime"`
	AssetSHA256 string `json:"assetSha256"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	TotalBytes  int    `json:"totalBytes"`
	Offset      int    `json:"offset"`
	DataBase64  string `json:"dataBase64"`
	NextCursor  string `json:"nextCursor,omitempty"`
}
type assetLease struct {
	timer     *time.Timer
	key, root string
	body      []byte
	response  AssetResponse
	offset    int
	expires   time.Time
}
type assetStore struct {
	mu     sync.Mutex
	epoch  string
	now    func() time.Time
	leases map[string]*assetLease
	bytes  int
}

func (s *Service) readDocument(ctx context.Context, p DocumentRequest) (ReadResponse, error) {
	var doc ReadResponse
	var err error
	switch p.Source {
	case SelectionSourceWorkspaceBrowser:
		if p.TurnID != "" || p.ItemID != "" || p.ReferenceKind != "" || p.RawReference != "" {
			return doc, fail(CodeInvalidArgument)
		}
		return s.WorkspaceRead(ctx, WorkspaceReadRequest{ThreadID: p.ThreadID, Path: p.Path})
	case SelectionSourceTurnChange:
		if p.ReferenceKind != "" || p.RawReference != "" {
			return doc, fail(CodeInvalidArgument)
		}
		return s.Read(ctx, ReadRequest{ThreadID: p.ThreadID, TurnID: p.TurnID, ItemID: p.ItemID, Path: p.Path})
	case SelectionSourceMessageReference:
		doc, err = s.ReferenceRead(ctx, ReferenceReadRequest{ThreadID: p.ThreadID, TurnID: p.TurnID, ItemID: p.ItemID, ReferenceKind: p.ReferenceKind, RawReference: p.RawReference})
		if err == nil && p.Path != "" && p.Path != doc.Path {
			return ReadResponse{}, fail(CodePathNotAllowed)
		}
		return doc, err
	default:
		return doc, fail(CodeInvalidArgument)
	}
}

func (s *Service) AssetRead(ctx context.Context, req AssetReadRequest) (AssetResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (AssetResponse, error) { return s.assetRead(ctx, req) })
}
func (s *Service) assetRead(ctx context.Context, req AssetReadRequest) (AssetResponse, error) {
	var empty AssetResponse
	if !s.DocumentAssets() {
		return empty, fail(CodeFeatureDisabled)
	}
	if len(req.DocumentSHA256) != 64 || req.ImageReference == "" || len(req.ImageReference) > 4096 {
		return empty, fail(CodeInvalidArgument)
	}
	if req.SVGVersion != 0 && req.SVGVersion != 1 {
		return empty, fail(CodeInvalidArgument)
	}
	identity := req
	identity.Cursor = ""
	encoded, _ := json.Marshal(identity)
	key := s.assets.epoch + string(encoded)
	if req.Cursor != "" {
		s.assets.mu.Lock()
		defer s.assets.mu.Unlock()
		s.assets.expire()
		lease := s.assets.leases[req.Cursor]
		if lease == nil {
			return empty, fail(CodeTimeout)
		}
		if lease.key != key {
			return empty, fail(CodeInvalidArgument)
		}
		delete(s.assets.leases, req.Cursor)
		s.assets.bytes -= len(lease.body)
		return s.assets.chunk(lease), nil
	}
	doc, err := s.readDocument(ctx, req.DocumentRequest)
	if err != nil {
		return empty, err
	}
	if doc.SHA256 != req.DocumentSHA256 {
		return empty, fail(CodeFileChanged)
	}
	ext := strings.ToLower(filepath.Ext(doc.Path))
	if ext != ".md" && ext != ".markdown" {
		return empty, fail(CodeUnsupportedFileType)
	}
	// Parsing the actual parent AST prevents fenced code, links and client paths
	// from authorizing a file read, including reference-style Markdown images.
	raw := []byte(doc.Content)
	tree := goldmark.DefaultParser().Parse(text.NewReader(raw))
	declared := false
	ast.Walk(tree, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if img, ok := node.(*ast.Image); ok {
				destination := html.UnescapeString(string(util.UnescapePunctuations(img.Destination)))
				if destination == req.ImageReference {
					declared = true
					return ast.WalkStop, nil
				}
			}
		}
		return ast.WalkContinue, nil
	})
	if !declared {
		return empty, fail(CodePathNotAllowed)
	}
	u, err := url.Parse(req.ImageReference)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || strings.HasPrefix(req.ImageReference, "/") || strings.ContainsRune(req.ImageReference, '\\') {
		return empty, fail(CodePathNotAllowed)
	}
	decoded := u.Path
	if decoded == "" || strings.IndexFunc(decoded, unicode.IsControl) >= 0 || strings.ContainsRune(decoded, '\\') {
		return empty, fail(CodePathNotAllowed)
	}
	rel := path.Clean(path.Join(path.Dir(doc.Path), decoded))
	if _, ok := safeRelativePath(rel); !ok {
		return empty, fail(CodePathNotAllowed)
	}
	thread, err := s.source.Thread(ctx, req.ThreadID)
	if err != nil {
		return empty, err
	}
	if thread.ID != req.ThreadID || thread.CWD != doc.root {
		return empty, fail(CodeFileChanged)
	}
	body, _, err := readBounded(ctx, doc.root, rel, MaxAssetBytes)
	if err != nil {
		return empty, err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	if err != nil && req.SVGVersion == 1 {
		var width, height int
		body, width, height, err = staticSVGImage(ctx, body)
		if err != nil {
			return empty, err
		}
		config = image.Config{Width: width, Height: height}
		mime = "image/svg+xml"
	}
	if err != nil || mime == "" {
		return empty, fail(CodeUnsupportedFileType)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 12_000_000 {
		return empty, fail(CodeFileTooLarge)
	}
	digest := sha256.Sum256(body)
	lease := &assetLease{key: key, root: doc.root, body: body, response: AssetResponse{MIME: mime, AssetSHA256: hex.EncodeToString(digest[:]), Width: config.Width, Height: config.Height, TotalBytes: len(body)}, expires: s.assets.now().Add(30 * time.Second)}
	s.assets.mu.Lock()
	defer s.assets.mu.Unlock()
	s.assets.expire()
	if len(s.assets.leases) >= 32 || s.assets.bytes+len(body) > maxAssetMemory {
		return empty, fail(CodeOverloaded)
	}
	return s.assets.chunk(lease), nil
}
func (a *assetStore) expire() {
	for key, lease := range a.leases {
		if !a.now().Before(lease.expires) {
			if lease.timer != nil {
				lease.timer.Stop()
			}
			a.bytes -= len(lease.body)
			delete(a.leases, key)
		}
	}
}
func (a *assetStore) chunk(lease *assetLease) AssetResponse {
	result := lease.response
	result.Offset = lease.offset
	end := min(lease.offset+MaxAssetChunk, len(lease.body))
	result.DataBase64 = base64.StdEncoding.EncodeToString(lease.body[lease.offset:end])
	lease.offset = end
	if end < len(lease.body) {
		if lease.timer == nil {
			lease.timer = time.AfterFunc(30*time.Second, func() { a.mu.Lock(); defer a.mu.Unlock(); a.expire() })
		}
		cursor := rand.Text()
		a.leases[cursor] = lease
		a.bytes += len(lease.body)
		result.NextCursor = cursor
	} else if lease.timer != nil {
		lease.timer.Stop()
	}
	return result
}
