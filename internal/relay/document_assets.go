package relay

import (
	"encoding/base64"
	"regexp"
)

var assetSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)
var assetCursor = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func validAssetReply(body []byte, value any, svgOptIn ...bool) bool {
	if len(body) > 384<<10 {
		return false
	}
	data, ok := value.(map[string]any)
	if !ok {
		return false
	}
	mime, _ := data["mime"].(string)
	allowSVG := len(svgOptIn) > 0 && svgOptIn[0]
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp" && !(allowSVG && mime == "image/svg+xml") {
		return false
	}
	digest, _ := data["assetSha256"].(string)
	if !assetSHA.MatchString(digest) {
		return false
	}
	total, tok := data["totalBytes"].(float64)
	offset, ook := data["offset"].(float64)
	width, wok := data["width"].(float64)
	height, hok := data["height"].(float64)
	if !tok || !ook || !wok || !hok || total < 1 || total > 4<<20 || offset < 0 || offset != float64(int(offset)) || total != float64(int(total)) || width != float64(int(width)) || height != float64(int(height)) || width < 1 || height < 1 || width > 8192 || height > 8192 || width*height > 12_000_000 {
		return false
	}
	chunk, ok := data["dataBase64"].(string)
	if !ok || len(chunk) > base64.StdEncoding.EncodedLen(256<<10) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(chunk)
	if err != nil || len(raw) < 1 || len(raw) > 256<<10 || offset+float64(len(raw)) > total {
		return false
	}
	cursor, _ := data["nextCursor"].(string)
	if cursor != "" && !assetCursor.MatchString(cursor) {
		return false
	}
	return (cursor != "") == (offset+float64(len(raw)) < total)
}
