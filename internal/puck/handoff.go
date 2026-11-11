package puck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

type handoffCandidate struct {
	taskID, deviceID, threadID, cwd, title, status, outcome, conclusion string
}

func (b toolBackend) HandoffContext(ctx context.Context, query pucktools.HandoffQuery) (pucktools.Handoff, error) {
	assistant, err := b.module.store.Assistant(ctx, query.AssistantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pucktools.Handoff{}, nil
		}
		return pucktools.Handoff{}, err
	}
	userText := ""
	var blobs []string
	if query.SourceMessageID != "" {
		message, messageErr := b.module.store.Message(ctx, query.AssistantID, query.SourceMessageID)
		if messageErr != nil && !errors.Is(messageErr, sql.ErrNoRows) {
			return pucktools.Handoff{}, messageErr
		}
		if messageErr == nil && message.Role == "user" {
			userText = message.Text
			blobs = append(blobs, message.Text, message.CardJSON)
			if message.ReplyTo != "" {
				reply, replyErr := b.module.store.Message(ctx, query.AssistantID, message.ReplyTo)
				if replyErr != nil && !errors.Is(replyErr, sql.ErrNoRows) {
					return pucktools.Handoff{}, replyErr
				}
				if replyErr == nil {
					blobs = append(blobs, reply.Text, reply.CardJSON)
				}
			}
		}
	}
	for _, id := range query.IncludeMessages {
		message, messageErr := b.module.store.Message(ctx, query.AssistantID, id)
		if messageErr != nil && !errors.Is(messageErr, sql.ErrNoRows) {
			return pucktools.Handoff{}, messageErr
		}
		if messageErr == nil {
			blobs = append(blobs, message.Text, message.CardJSON)
		}
	}
	candidates, err := b.handoffCandidates(ctx, assistant)
	if err != nil {
		return pucktools.Handoff{}, err
	}
	keys, omitted := selectHandoffKeys(candidates, query, strings.Join(blobs, "\n"))
	sessions := make([]pucktools.HandoffSession, 0, len(keys))
	for _, key := range keys {
		candidate := candidates[key]
		session := pucktools.HandoffSession{
			Title: candidate.title, DeviceName: deviceName(b.module.bridge.Devices(), candidate.deviceID),
			DeviceID: candidate.deviceID, CWD: candidate.cwd, ThreadID: candidate.threadID,
			Status: candidate.status, Outcome: candidate.outcome, Conclusion: candidate.conclusion,
		}
		if title, cwd, conclusion := b.readSessionFacts(ctx, query.AssistantID, candidate.deviceID, candidate.threadID); conclusion != "" || title != "" || cwd != "" {
			if title != "" {
				session.Title = title
			}
			if cwd != "" {
				session.CWD = cwd
			}
			if conclusion != "" {
				session.Conclusion = conclusion
			}
		}
		sessions = append(sessions, session)
	}
	return pucktools.Handoff{UserText: userText, Sessions: sessions, OmittedSessions: omitted}, nil
}

func (b toolBackend) handoffCandidates(ctx context.Context, assistant Assistant) (map[string]handoffCandidate, error) {
	blocked := brainThreadIDs(assistant)
	watches, err := b.module.store.ThreadWatches(ctx, assistant.ID)
	if err != nil {
		return nil, err
	}
	tasks, err := b.module.store.Tasks(ctx, assistant.ID)
	if err != nil {
		return nil, err
	}
	candidates := map[string]handoffCandidate{}
	for _, watch := range watches {
		blockedThread, blockErr := threadBlocked(ctx, b, blocked, watch.ThreadID)
		if blockErr != nil {
			return nil, blockErr
		}
		if watch.ThreadID == "" || watch.DeviceID == "" || blockedThread {
			continue
		}
		key := watch.DeviceID + "\x00" + watch.ThreadID
		candidates[key] = handoffCandidate{
			deviceID: watch.DeviceID, threadID: watch.ThreadID, status: watch.Status,
			outcome: watch.NotifiedKey, conclusion: watch.LastSummary,
		}
	}
	for _, task := range tasks {
		blockedThread, blockErr := threadBlocked(ctx, b, blocked, task.NativeThreadID)
		if blockErr != nil {
			return nil, blockErr
		}
		if task.NativeThreadID == "" || task.DeviceID == "" || blockedThread {
			continue
		}
		key := task.DeviceID + "\x00" + task.NativeThreadID
		current := candidates[key]
		current.taskID = task.ID
		current.deviceID = task.DeviceID
		current.threadID = task.NativeThreadID
		if task.CWD != "" {
			current.cwd = task.CWD
		}
		if current.title == "" {
			current.title = task.Goal
		}
		if current.status == "" {
			current.status = task.Status
		}
		if current.outcome == "" {
			current.outcome = task.Status
		}
		if current.conclusion == "" {
			current.conclusion = task.LastSummary
		}
		candidates[key] = current
	}
	return candidates, nil
}

func threadBlocked(ctx context.Context, b toolBackend, blocked map[string]struct{}, threadID string) (bool, error) {
	if _, found := blocked[threadID]; found {
		return true, nil
	}
	return b.module.store.BrainThreadMatch(ctx, threadID)
}

func brainThreadIDs(assistant Assistant) map[string]struct{} {
	blocked := map[string]struct{}{}
	if assistant.BrainThreadID != "" {
		blocked[assistant.BrainThreadID] = struct{}{}
	}
	var history []string
	_ = json.Unmarshal([]byte(assistant.BrainThreadHistory), &history)
	for _, id := range history {
		if id != "" {
			blocked[id] = struct{}{}
		}
	}
	return blocked
}

func selectHandoffKeys(candidates map[string]handoffCandidate, query pucktools.HandoffQuery, blob string) ([]string, int) {
	excluded := map[string]struct{}{}
	if query.TargetDeviceID != "" && query.TargetThreadID != "" {
		excluded[query.TargetDeviceID+"\x00"+query.TargetThreadID] = struct{}{}
	}
	seen := map[string]struct{}{}
	keys := make([]string, 0, pucktools.MaxHandoffSessions)
	add := func(key string) {
		if key == "" {
			return
		}
		if _, skip := excluded[key]; skip {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for _, ref := range query.ReferenceThreads {
		matched := make([]string, 0, 1)
		for key, candidate := range candidates {
			if candidate.taskID == ref || candidate.threadID == ref {
				matched = append(matched, key)
			}
		}
		sort.Strings(matched)
		for _, key := range matched {
			add(key)
		}
	}
	type hit struct {
		key string
		at  int
	}
	var hits []hit
	for key, candidate := range candidates {
		at := mentionAt(blob, candidate.threadID)
		if taskAt := mentionAt(blob, candidate.taskID); taskAt >= 0 && (at < 0 || taskAt < at) {
			at = taskAt
		}
		if at >= 0 {
			hits = append(hits, hit{key: key, at: at})
		}
	}
	for index := 1; index < len(hits); index++ {
		item := hits[index]
		place := index
		for place > 0 && (hits[place-1].at > item.at || (hits[place-1].at == item.at && hits[place-1].key > item.key)) {
			hits[place] = hits[place-1]
			place--
		}
		hits[place] = item
	}
	for _, item := range hits {
		add(item.key)
	}
	omitted := 0
	if len(keys) > pucktools.MaxHandoffSessions {
		omitted = len(keys) - pucktools.MaxHandoffSessions
		keys = keys[:pucktools.MaxHandoffSessions]
	}
	return keys, omitted
}

func mentionAt(blob, id string) int {
	if len(id) < 8 || blob == "" {
		return -1
	}
	return strings.Index(blob, id)
}

func deviceName(devices []map[string]any, deviceID string) string {
	for _, device := range devices {
		if device["deviceId"] == deviceID {
			name, _ := device["deviceName"].(string)
			return name
		}
	}
	return ""
}

func (b toolBackend) readSessionFacts(ctx context.Context, assistantID, deviceID, threadID string) (string, string, string) {
	result := b.module.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: assistantID, Trigger: "tool"}, deviceID, "thread.read", mustJSON(map[string]any{"threadId": threadID}))
	if result.Outcome != "accepted" || result.Error != nil || result.Data == nil {
		return "", "", ""
	}
	return pucktools.ExtractSessionFacts(result.Data)
}
