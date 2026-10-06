package appserver

import (
	"context"
	"errors"
)

const MaxQueuedSubmissions = 64

type QueueInput struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	URL          string `json:"url,omitempty"`
	Path         string `json:"path,omitempty"`
	TextElements []any  `json:"text_elements,omitempty"`
}

type QueuedSubmission struct {
	ID                  string       `json:"id"`
	Input               []QueueInput `json:"input"`
	ClientUserMessageID string       `json:"clientUserMessageId"`
}

type QueueReader struct{ RPC RPC }

func (q QueueReader) Supported(ctx context.Context) (bool, error) {
	_, err := q.ListPage(ctx, "00000000-0000-4000-8000-000000000000", "", 1)
	switch {
	case err == nil, errors.Is(err, ErrInvalidArgument), errors.Is(err, ErrRemote):
		return true, nil
	case errors.Is(err, ErrMethodUnavailable):
		return false, nil
	default:
		return false, err
	}
}

type queuePage struct {
	Data       []QueuedSubmission `json:"data"`
	NextCursor *string            `json:"nextCursor"`
}

func (q QueueReader) ListPage(ctx context.Context, threadID, cursor string, limit int) (queuePage, error) {
	var out queuePage
	if threadID == "" || limit < 1 || limit > 100 {
		return out, ErrInvalidArgument
	}
	params := map[string]any{"threadId": threadID, "limit": limit}
	if cursor != "" {
		params["cursor"] = cursor
	}
	err := q.RPC.Call(ctx, "thread/queue/list", params, &out)
	return out, err
}

func (q QueueReader) List(ctx context.Context, threadID string) ([]QueuedSubmission, error) {
	var all []QueuedSubmission
	cursor := ""
	seenIDs := map[string]struct{}{}
	seenCursors := map[string]struct{}{}
	for {
		page, err := q.ListPage(ctx, threadID, cursor, MaxQueuedSubmissions)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Data {
			if item.ID == "" || item.ClientUserMessageID == "" {
				return nil, ErrProtocol
			}
			if _, duplicate := seenIDs[item.ID]; duplicate {
				return nil, ErrProtocol
			}
			seenIDs[item.ID] = struct{}{}
			all = append(all, item)
			if len(all) > MaxQueuedSubmissions {
				return nil, ErrProtocol
			}
		}
		if page.NextCursor == nil {
			return all, nil
		}
		next := *page.NextCursor
		if next == "" || next == cursor {
			return nil, ErrProtocol
		}
		if _, duplicate := seenCursors[next]; duplicate {
			return nil, ErrProtocol
		}
		seenCursors[next] = struct{}{}
		cursor = next
	}
}

func queueInput(text string, images []string) []QueueInput {
	input := make([]QueueInput, 0, 1+len(images))
	if text != "" {
		input = append(input, QueueInput{Type: "text", Text: text, TextElements: []any{}})
	}
	for _, image := range images {
		input = append(input, QueueInput{Type: "image", URL: image})
	}
	return input
}

func (q QueueReader) Add(ctx context.Context, threadID, clientMessageID, text string, images []string) (QueuedSubmission, error) {
	var out struct {
		QueuedSubmission QueuedSubmission `json:"queuedSubmission"`
	}
	err := q.RPC.Call(ctx, "thread/queue/add", map[string]any{"threadId": threadID, "clientUserMessageId": clientMessageID, "input": queueInput(text, images)}, &out)
	return out.QueuedSubmission, err
}

func (q QueueReader) Update(ctx context.Context, threadID, queueID, text string, images []string) (QueuedSubmission, error) {
	var out struct {
		QueuedSubmission QueuedSubmission `json:"queuedSubmission"`
	}
	err := q.RPC.Call(ctx, "thread/queue/update", map[string]any{"threadId": threadID, "queuedSubmissionId": queueID, "input": queueInput(text, images)}, &out)
	return out.QueuedSubmission, err
}

func (q QueueReader) Delete(ctx context.Context, threadID, queueID string) (bool, error) {
	var out struct {
		Deleted bool `json:"deleted"`
	}
	err := q.RPC.Call(ctx, "thread/queue/delete", map[string]any{"threadId": threadID, "queuedSubmissionId": queueID}, &out)
	return out.Deleted, err
}

func (q QueueReader) Reorder(ctx context.Context, threadID string, queueIDs []string) error {
	var out struct{}
	return q.RPC.Call(ctx, "thread/queue/reorder", map[string]any{"threadId": threadID, "queuedSubmissionIds": queueIDs}, &out)
}
