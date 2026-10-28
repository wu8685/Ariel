package filecontext

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const staticSVG = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="72pt" height="36pt" viewBox="0 0 72 36"><defs><path id="glyph" d="M0 0L10 0L5 10Z"/><clipPath id="clip"><rect width="72" height="36"/></clipPath><linearGradient id="paint"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#000"/></linearGradient></defs><g clip-path="url(#clip)" transform="translate(1 2)"><use xlink:href="#glyph" fill="url(#paint)"/><text x="15" y="20" style="fill: black; font-size: 12px">diagram</text></g></svg>`

func svgFixture(t *testing.T, body string, optIn bool) (*Service, AssetReadRequest, string) {
	t.Helper()
	s, req, root := assetFixture(t)
	if err := os.WriteFile(filepath.Join(root, "图.svg"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/read.md"), []byte("# 图\n\n![diagram](../%E5%9B%BE.svg)"), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := s.WorkspaceRead(context.Background(), WorkspaceReadRequest{ThreadID: req.ThreadID, Path: req.Path})
	if err != nil {
		t.Fatal(err)
	}
	req.DocumentSHA256, req.ImageReference = doc.SHA256, "../%E5%9B%BE.svg"
	if optIn {
		encoded, _ := json.Marshal(req)
		var values map[string]any
		_ = json.Unmarshal(encoded, &values)
		values["svgVersion"] = 1
		encoded, _ = json.Marshal(values)
		_ = json.Unmarshal(encoded, &req)
	}
	return s, req, root
}

func TestStaticSVGRequiresOptInAndPreservesVectorGlyphs(t *testing.T) {
	s, req, _ := svgFixture(t, staticSVG, true)
	doc, _ := s.WorkspaceRead(context.Background(), WorkspaceReadRequest{ThreadID: req.ThreadID, Path: req.Path})
	encoded, _ := json.Marshal(doc.DocumentAssets)
	if !strings.Contains(string(encoded), `"svgVersion":1`) {
		t.Fatal("SVG capability missing")
	}
	result, err := s.AssetRead(context.Background(), req)
	if err != nil || result.MIME != "image/svg+xml" || result.Width != 96 || result.Height != 48 {
		t.Fatalf("SVG not supported: %+v %v", result, err)
	}
	body, err := base64.StdEncoding.DecodeString(result.DataBase64)
	if err != nil || !strings.Contains(string(body), `href="#glyph"`) || !strings.Contains(string(body), "clipPath") || strings.Contains(string(body), "style=") {
		t.Fatalf("bad canonical SVG: %v", err)
	}
	legacy, oldReq, _ := svgFixture(t, staticSVG, false)
	if _, err := legacy.AssetRead(context.Background(), oldReq); CodeOf(err) != CodeUnsupportedFileType {
		t.Fatalf("legacy received SVG: %v", err)
	}
}

func TestStaticSVGHexColorIsNotAnExponent(t *testing.T) {
	s, req, _ := svgFixture(t, `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><path fill="#5e55ff" d="M0 0L10 10"/></svg>`, true)
	if _, err := s.AssetRead(context.Background(), req); err != nil {
		t.Fatalf("valid hex color rejected: %v", err)
	}
}

func TestStaticSVGViewBoxOnlySuppliesMissingDimensions(t *testing.T) {
	for _, attrs := range []string{`viewBox="0 0 96 48"`, `width="96" viewBox="0 0 96 48"`, `height="48" viewBox="0 0 96 48"`, `width="1in" height="0.5in"`} {
		_, width, height, err := staticSVGImage(context.Background(), []byte(`<svg xmlns="http://www.w3.org/2000/svg" `+attrs+`/>`))
		if err != nil || width != 96 || height != 48 {
			t.Fatalf("dimensions for %s: %d×%d %v", attrs, width, height, err)
		}
	}
}

func TestStaticSVGRejectsActiveExternalAndAmbiguousContent(t *testing.T) {
	for _, inner := range []string{
		`<script>alert(1)</script>`, `<rect onload="alert(1)"/>`, `<foreignObject/>`, `<animate/>`, `<a href="#x"/>`, `<image href="data:image/png;base64,AAAA"/>`, `<filter/>`, `<style>* { fill: red }</style>`,
		`<use href="https://example.invalid/x.svg#x"/>`, `<use href="file:///secret"/>`, `<use href="a.svg#x"/>`, `<use href="data:image/svg+xml,a"/>`,
		`<path fill="url(https://example.invalid/x)"/>`, `<path style="fill: u\72l(https://example.invalid/x)"/>`, `<path style="fill: var(--x)"/>`,
		`<g xml:base="https://example.invalid/"/>`, `<path xmlns="http://www.w3.org/1999/xhtml"/>`, `<g id="x"/><g id="x"/>`, `<use href="#missing"/>`,
		`<g id="x"><use href="#x"/></g>`, `<g id="x"><use href="#y"/></g><g id="y"><use href="#x"/></g>`, `<path d="M1e999 0"/>`,
	} {
		t.Run(inner, func(t *testing.T) {
			s, req, _ := svgFixture(t, `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">`+inner+`</svg>`, true)
			if _, err := s.AssetRead(context.Background(), req); err == nil {
				t.Fatal("unsafe SVG accepted")
			}
		})
	}
	for _, prefix := range []string{`<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///secret">]>`, `<?xml-stylesheet href="https://example.invalid/x.css"?>`} {
		s, req, _ := svgFixture(t, prefix+staticSVG, true)
		if _, err := s.AssetRead(context.Background(), req); err == nil {
			t.Fatal("XML resource declaration accepted")
		}
	}
}

func TestStaticSVGBoundsDimensionsDepthNodesAndExpansion(t *testing.T) {
	for _, body := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg" width="8193" height="1"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="4000" height="4000"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="10" viewBox="0 0 10 10"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="-1" height="10" viewBox="0 0 10 10"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="10" viewBox="0 0 10 10"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">` + strings.Repeat("<g>", 65) + strings.Repeat("</g>", 65) + `</svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">` + strings.Repeat("<path/>", 20_001) + `</svg>`,
	} {
		s, req, _ := svgFixture(t, body, true)
		if _, err := s.AssetRead(context.Background(), req); err == nil {
			t.Fatal("oversized SVG accepted")
		}
	}
	body := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><defs><path id="p0"/>`
	for i := 1; i <= 17; i++ {
		body += `<g id="p` + string(rune('a'+i)) + `">` + `<use href="#p0"/>` + `</g>`
	}
	// Exponential expansion without a deep XML tree.
	previous := "p0"
	for i := 0; i < 15; i++ {
		id := "expanded" + strings.Repeat("x", i)
		body += `<g id="` + id + `"><use href="#` + previous + `"/><use href="#` + previous + `"/></g>`
		previous = id
	}
	body += `</defs><use href="#` + previous + `"/></svg>`
	s, req, _ := svgFixture(t, body, true)
	if _, err := s.AssetRead(context.Background(), req); err == nil {
		t.Fatal("unbounded expanded graph accepted")
	}
}

func TestStaticSVGDepthCancellationAndAuthorizedSources(t *testing.T) {
	s, req, _ := svgFixture(t, staticSVG, true)
	f := s.source.(*fakeSource)
	f.items = []Item{{ID: "change", Type: "fileChange", Changes: []Change{{Path: req.Path, Kind: "update"}}}, {ID: "ref", Type: "agentMessage", Text: "[document](" + req.Path + ")"}}
	for _, source := range []SelectionSource{SelectionSourceTurnChange, SelectionSourceMessageReference} {
		r := req
		r.Source = source
		r.TurnID = "turn"
		r.ItemID = "change"
		if source == SelectionSourceMessageReference {
			r.ItemID = "ref"
			r.ReferenceKind = ReferenceKind("markdown_link")
			r.RawReference = req.Path
		}
		if _, err := s.AssetRead(context.Background(), r); err != nil {
			t.Fatalf("source %s failed: %v", source, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := staticSVGImage(ctx, []byte(staticSVG)); err == nil {
		t.Fatal("cancelled SVG work continued")
	}
	body := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><defs><path id="p0"/>`
	for i := 1; i <= 18; i++ {
		body += `<g id="p` + strconv.Itoa(i) + `"><use href="#p` + strconv.Itoa(i-1) + `"/></g>`
	}
	body += `</defs><use href="#p18"/></svg>`
	if _, _, _, err := staticSVGImage(context.Background(), []byte(body)); err == nil {
		t.Fatal("reference depth unbounded")
	}
}

func FuzzStaticSVGImage(f *testing.F) {
	f.Add([]byte(staticSVG))
	f.Add([]byte(`<svg><script/></svg>`))
	f.Add([]byte(`<!DOCTYPE svg><svg/>`))
	f.Fuzz(func(t *testing.T, body []byte) {
		result, width, height, err := staticSVGImage(context.Background(), body)
		if err == nil && (len(result) > MaxAssetBytes || width < 1 || height < 1 || width > 8192 || height > 8192 || width*height > 12_000_000) {
			t.Fatal("SVG validation bounds violated")
		}
	})
}

func TestStaticSVGExpansionWeightsPathGeometryNotOnlyElementCount(t *testing.T) {
	body := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><defs><path id="p0" d="M0 0 ` + strings.Repeat("L1 1 ", 1000) + `"/>`
	for i := 1; i <= 10; i++ {
		body += `<g id="p` + strconv.Itoa(i) + `"><use href="#p` + strconv.Itoa(i-1) + `"/><use href="#p` + strconv.Itoa(i-1) + `"/></g>`
	}
	body += `</defs><use href="#p10"/></svg>`
	if _, _, _, err := staticSVGImage(context.Background(), []byte(body)); err == nil {
		t.Fatal("small node graph expanded into excessive path work")
	}
}

func TestStaticSVGExpansionWeightsTextNotOnlyNumbers(t *testing.T) {
	body := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><defs><text id="p0">` + strings.Repeat("x", 2048) + `</text>`
	for i := 1; i <= 10; i++ {
		body += `<g id="p` + strconv.Itoa(i) + `"><use href="#p` + strconv.Itoa(i-1) + `"/><use href="#p` + strconv.Itoa(i-1) + `"/></g>`
	}
	body += `</defs><use href="#p10"/></svg>`
	if _, _, _, err := staticSVGImage(context.Background(), []byte(body)); err == nil {
		t.Fatal("small node graph expanded into excessive text work")
	}
}

func TestStaticSVGChunksBindOptInAndDigestToCanonicalBytes(t *testing.T) {
	s, req, _ := svgFixture(t, `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><desc>`+strings.Repeat("x", 300<<10)+`</desc><path d="M0 0L10 10"/></svg>`, true)
	first, err := s.AssetRead(context.Background(), req)
	if err != nil || first.NextCursor == "" || first.MIME != "image/svg+xml" {
		t.Fatalf("first SVG chunk: %+v %v", first, err)
	}
	req.Cursor = first.NextCursor
	legacy := req
	legacy.SVGVersion = 0
	if _, err := s.AssetRead(context.Background(), legacy); CodeOf(err) != CodeInvalidArgument {
		t.Fatalf("SVG cursor crossed opt-in boundary: %v", err)
	}
	last, err := s.AssetRead(context.Background(), req)
	if err != nil || last.NextCursor != "" || last.Offset != MaxAssetChunk || last.AssetSHA256 != first.AssetSHA256 {
		t.Fatalf("last SVG chunk: %+v %v", last, err)
	}
	var body bytes.Buffer
	for _, chunk := range []AssetResponse{first, last} {
		decoded, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
		if err != nil || len(decoded) > MaxAssetChunk {
			t.Fatal("invalid SVG chunk")
		}
		body.Write(decoded)
	}
	digest := sha256.Sum256(body.Bytes())
	if hex.EncodeToString(digest[:]) != first.AssetSHA256 || body.Len() != first.TotalBytes {
		t.Fatal("SVG digest did not cover canonical transmitted bytes")
	}
}
