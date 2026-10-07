package desktopagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxScreenshotBytes = 4 << 20
const maxScreenshotPixels = 25_000_000
const maxScreenshotCount = 3

var markdownImage = regexp.MustCompile(`!\[([^\]]*)\]\((?:<([^>]+)>|([^\s)]+))(?:\s+"[^"]*")?\)`)

type imageSource struct{ kind, value, alt, source string }

func nativeImageSources(raw json.RawMessage) []imageSource {
	var item struct {
		Type      string                             `json:"type"`
		Text      string                             `json:"text"`
		Result    string                             `json:"result"`
		SavedPath string                             `json:"savedPath"`
		Content   []struct{ Type, URL, Path string } `json:"content"`
		Input     []struct{ Type, URL, Path string } `json:"input"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return nil
	}
	sources := []imageSource{}
	switch item.Type {
	case "userMessage", "steeringUserMessage":
		content := item.Content
		if item.Type == "steeringUserMessage" {
			content = item.Input
		}
		for _, c := range content {
			if len(sources) >= 24 {
				break
			}
			if c.Type == "image" && strings.HasPrefix(c.URL, "data:image/") {
				sources = append(sources, imageSource{kind: "native", value: c.URL, alt: "用户截图"})
			}
			if c.Type == "localImage" && c.Path != "" {
				sources = append(sources, imageSource{kind: "native", value: c.Path, alt: "用户截图"})
			}
		}
	case "imageGeneration":
		if item.SavedPath != "" {
			sources = append(sources, imageSource{kind: "native", value: item.SavedPath, alt: "Codex 图片"})
		} else if strings.HasPrefix(item.Result, "data:image/") {
			sources = append(sources, imageSource{kind: "native", value: item.Result, alt: "Codex 图片"})
		}
	case "agentMessage":
		for _, match := range markdownImage.FindAllStringSubmatch(item.Text, 24) {
			source := match[2]
			if source == "" {
				source = match[3]
			}
			decoded, err := url.PathUnescape(source)
			if err != nil || len(source) > 2048 || source == "" || strings.ContainsAny(decoded, "\x00\r\n") || strings.Contains(decoded, ":") || strings.HasPrefix(decoded, "//") {
				continue
			}
			alt, _ := boundedActivityText(256, match[1])
			sources = append(sources, imageSource{kind: "markdown", value: decoded, alt: alt, source: source})
		}
	}
	if len(sources) > 24 {
		return sources[:24]
	}
	return sources
}

func imageReferences(raw json.RawMessage) []any {
	sources := nativeImageSources(raw)
	if len(sources) == 0 {
		return nil
	}
	refs := make([]any, 0, len(sources))
	for i, source := range sources {
		refs = append(refs, map[string]any{"index": i, "kind": source.kind, "alt": source.alt, "source": source.valueIfMarkdown()})
	}
	return refs
}

func (s imageSource) valueIfMarkdown() string { return s.source }

func validateImageBytes(data []byte) (string, error) {
	if len(data) == 0 || len(data) > maxScreenshotBytes {
		return "", errors.New("INVALID_ARGUMENT")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > maxScreenshotPixels {
		return "", errors.New("INVALID_ARGUMENT")
	}
	switch format {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	}
	return "", errors.New("INVALID_ARGUMENT")
}

func decodeImageURI(value string) ([]byte, string, error) {
	mime := ""
	switch {
	case strings.HasPrefix(value, "data:image/png;base64,"):
		mime = "image/png"
	case strings.HasPrefix(value, "data:image/jpeg;base64,"):
		mime = "image/jpeg"
	default:
		return nil, "", errors.New("INVALID_ARGUMENT")
	}
	raw := value[len("data:")+len(mime)+len(";base64,"):]
	if len(raw) > base64.StdEncoding.EncodedLen(maxScreenshotBytes) {
		return nil, "", errors.New("INVALID_ARGUMENT")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil {
		return nil, "", errors.New("INVALID_ARGUMENT")
	}
	actual, err := validateImageBytes(data)
	if err != nil || actual != mime {
		return nil, "", errors.New("INVALID_ARGUMENT")
	}
	return data, mime, nil
}

func validateUploadImages(images []string) error {
	if len(images) > maxScreenshotCount {
		return errors.New("INVALID_ARGUMENT")
	}
	total := 0
	for _, value := range images {
		data, _, err := decodeImageURI(value)
		if err != nil {
			return err
		}
		total += len(data)
		if total > maxScreenshotBytes {
			return errors.New("INVALID_ARGUMENT")
		}
	}
	return nil
}

func imageFromNativeItem(raw json.RawMessage, cwd string, index int) (string, error) {
	sources := nativeImageSources(raw)
	if index < 0 || index >= len(sources) {
		return "", errors.New("NOT_FOUND")
	}
	source := sources[index].value
	if strings.HasPrefix(source, "data:") {
		data, mime, err := decodeImageURI(source)
		if err != nil {
			return "", err
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(cwd, source)
	}
	pathInfo, err := os.Lstat(source)
	if err != nil || !pathInfo.Mode().IsRegular() {
		return "", errors.New("NOT_FOUND")
	}
	f, err := os.Open(source)
	if err != nil {
		return "", errors.New("NOT_FOUND")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxScreenshotBytes || info.Size() == 0 {
		return "", errors.New("INVALID_ARGUMENT")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxScreenshotBytes+1))
	if err != nil {
		return "", errors.New("NOT_FOUND")
	}
	mime, err := validateImageBytes(data)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func nativeItemFromState(state json.RawMessage, turnID, itemID string) json.RawMessage {
	var snapshot struct {
		Turns []struct {
			TurnID string            `json:"turnId"`
			Items  []json.RawMessage `json:"items"`
		} `json:"turns"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Entities map[string]struct {
					TurnID string            `json:"turnId"`
					Items  []json.RawMessage `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &snapshot) != nil {
		return nil
	}
	find := func(items []json.RawMessage) json.RawMessage {
		for _, raw := range items {
			var item struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &item) == nil && item.ID == itemID {
				return raw
			}
		}
		return nil
	}
	if snapshot.TurnHistory.Kind == "canonical" {
		for _, turn := range snapshot.TurnHistory.History.Entities {
			if turn.TurnID == turnID {
				return find(turn.Items)
			}
		}
	}
	if snapshot.TurnHistory.Kind == "" {
		for _, turn := range snapshot.Turns {
			if turn.TurnID == turnID {
				return find(turn.Items)
			}
		}
	}
	return nil
}

func (s *Service) Image(ctx context.Context, threadID, turnID, itemID string, index int) (string, error) {
	if threadID == "" || turnID == "" || itemID == "" || index < 0 || index >= maxScreenshotCount*8 {
		return "", errors.New("INVALID_ARGUMENT")
	}
	source, err := s.history.Read(ctx, threadID)
	if err != nil {
		return "", err
	}
	if source.ID != threadID || source.CWD == "" {
		return "", ErrNativeShape
	}
	s.mu.Lock()
	controller := s.threads[threadID]
	s.mu.Unlock()
	if controller != nil {
		controller.mu.Lock()
		live := controller.live
		controller.mu.Unlock()
		if live != nil {
			if state, err := live.Current(); err == nil {
				if raw := nativeItemFromState(state, turnID, itemID); raw != nil {
					return imageFromNativeItem(raw, source.CWD, index)
				}
			}
		}
	}
	if paged, ok := s.history.(PagedHistory); ok {
		cursor := ""
		budget := 64 << 20
		for pages := 0; pages < 640; pages++ {
			page, err := paged.Items(ctx, threadID, turnID, cursor, 100)
			if err != nil {
				return "", err
			}
			for _, raw := range page.Data {
				budget -= len(raw)
				if budget < 0 {
					return "", errors.New("HISTORY_TOO_LARGE")
				}
				var item struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(raw, &item) == nil && item.ID == itemID {
					return imageFromNativeItem(raw, source.CWD, index)
				}
			}
			if page.NextCursor == nil || *page.NextCursor == "" || *page.NextCursor == cursor {
				break
			}
			cursor = *page.NextCursor
		}
	} else {
		for _, turn := range source.Turns {
			if turn.ID == turnID {
				for _, raw := range turn.Items {
					var item struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(raw, &item) == nil && item.ID == itemID {
						return imageFromNativeItem(raw, source.CWD, index)
					}
				}
			}
		}
	}
	return "", errors.New("NOT_FOUND")
}
