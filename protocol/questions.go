package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// AsyncQuestionSnapshot is an opt-in, memory-only sidecar, not a blocking
// interaction or a mutation authorization receipt.
type AsyncQuestionSnapshot struct {
	Version   int                    `json:"version"`
	Writable  bool                   `json:"writable"`
	Questions []AsyncQuestion        `json:"questions"`
	Warnings  []AsyncQuestionWarning `json:"warnings"`
}

type AsyncQuestion struct {
	ID          string   `json:"id"`
	Fingerprint string   `json:"fingerprint"`
	TurnID      string   `json:"turnId"`
	ItemID      string   `json:"itemId"`
	Index       int      `json:"index"`
	Title       string   `json:"title"`
	Options     []string `json:"options"`
	Status      string   `json:"status"`
	Answer      string   `json:"answer,omitempty"`
}

type AsyncQuestionWarning struct {
	TurnID string `json:"turnId"`
	ItemID string `json:"itemId"`
	Code   string `json:"code"`
}

func DecodeAsyncQuestions(raw []byte) (AsyncQuestionSnapshot, bool) {
	var out AsyncQuestionSnapshot
	if len(raw) == 0 || len(raw) > 64<<10 {
		return out, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || out.Version != 1 || out.Questions == nil || out.Warnings == nil || len(out.Questions) > 64 || len(out.Warnings) > 64 {
		return out, false
	}
	seen := map[string]bool{}
	bounded := func(s string, n int) bool { return utf8.ValidString(s) && utf8.RuneCountInString(s) <= n }
	for _, q := range out.Questions {
		if q.ID == "" || seen[q.ID] || !bounded(q.ID, 256) || q.Fingerprint == "" || !bounded(q.Fingerprint, 128) || q.TurnID == "" || !bounded(q.TurnID, 256) || q.ItemID == "" || !bounded(q.ItemID, 256) || q.Index < 0 || q.Index >= 16 || strings.TrimSpace(q.Title) == "" || !bounded(q.Title, 2048) || q.Options == nil || len(q.Options) > 32 || !bounded(q.Answer, 8192) {
			return out, false
		}
		if q.Status != "open" && q.Status != "answered" && q.Status != "ended" && q.Status != "unverified" {
			return out, false
		}
		if (q.Status == "answered") != (strings.TrimSpace(q.Answer) != "") {
			return out, false
		}
		seen[q.ID] = true
		for _, option := range q.Options {
			if strings.TrimSpace(option) == "" || !bounded(option, 512) {
				return out, false
			}
		}
	}
	for _, w := range out.Warnings {
		if !bounded(w.TurnID, 256) || !bounded(w.ItemID, 256) || !bounded(w.Code, 64) || w.Code == "" {
			return out, false
		}
	}
	return out, true
}
