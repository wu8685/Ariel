package desktopagent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/modelsettings"
)

func (a *agent) releaseSettingsSubscriptions() {
	a.settingsMu.Lock()
	ids := a.settingsSubs
	a.settingsSubs = nil
	a.settingsMu.Unlock()
	for id := range ids {
		a.service.Unsubscribe(id)
	}
}
func (a *agent) handleSettings(ctx context.Context, id, method string, raw json.RawMessage) {
	if a.service == nil || !a.service.SettingsEnabled() {
		a.reply(ctx, id, method, nil, appserver.ErrMethodUnavailable)
		return
	}
	var p struct {
		ThreadID         string              `json:"threadId"`
		SubscriptionID   string              `json:"subscriptionId"`
		ExpectedRevision string              `json:"expectedRevision"`
		OperationID      string              `json:"operationId"`
		Patch            modelsettings.Patch `json:"patch"`
		Refresh          bool                `json:"refresh"`
	}
	if json.Unmarshal(raw, &p) != nil {
		a.reply(ctx, id, method, nil, modelsettings.ErrInvalid)
		return
	}
	var data map[string]any
	var err error
	switch method {
	case "model.catalog.read":
		var catalog modelsettings.Catalog
		catalog, err = a.service.ModelCatalog(ctx, p.Refresh)
		data = map[string]any{"revision": modelsettings.Revision(catalog), "catalog": catalog, "settingsSupported": true, "agentEpoch": a.agentEpoch}
	case "thread.settings.read":
		var snapshot modelsettings.Snapshot
		snapshot, err = a.service.ReadSettings(ctx, p.ThreadID)
		data = map[string]any{"settings": snapshot, "agentEpoch": a.agentEpoch}
	case "thread.settings.update":
		var snapshot modelsettings.Snapshot
		key := "settings\x00" + p.ThreadID + "\x00" + p.OperationID
		a.service.mu.Lock()
		_, seen := a.service.seen[key]
		if !seen && p.OperationID != "" {
			a.service.seen[key] = struct{}{}
			a.service.seenOrder = append(a.service.seenOrder, key)
			if len(a.service.seenOrder) > maxRecentMessageIDs {
				delete(a.service.seen, a.service.seenOrder[0])
				a.service.seenOrder = a.service.seenOrder[1:]
			}
		}
		a.service.mu.Unlock()
		if seen || p.OperationID == "" {
			err = modelsettings.ErrInvalid
		} else {
			snapshot, err = a.service.UpdateSettings(ctx, p.ThreadID, p.ExpectedRevision, p.Patch)
		}
		if errors.Is(err, modelsettings.ErrConflict) {
			err = errors.New("STALE_TURN")
		}
		if err != nil && err.Error() == "QUEUE_NOT_EMPTY" {
			err = errors.New("TURN_BUSY")
		}
		data = map[string]any{"settings": snapshot, "agentEpoch": a.agentEpoch}
	case "thread.settings.watch":
		var sub, stream string
		var snapshot modelsettings.Snapshot
		var activate func()
		sub, stream, snapshot, activate, err = a.service.WatchSettings(ctx, p.ThreadID, func(event map[string]any) {
			event["type"] = "event"
			event["v"] = 1
			event["deviceId"] = a.deviceID
			event["agentEpoch"] = a.agentEpoch
			_ = a.send(ctx, event)
		})
		if err == nil {
			a.settingsMu.Lock()
			if a.settingsSubs == nil {
				a.settingsSubs = map[string]struct{}{}
			}
			a.settingsSubs[sub] = struct{}{}
			a.settingsMu.Unlock()
			a.reply(ctx, id, method, map[string]any{"subscriptionId": sub, "streamId": stream, "seq": 1, "settings": snapshot, "agentEpoch": a.agentEpoch}, nil)
			activate()
			return
		}
	case "thread.settings.unwatch":
		a.settingsMu.Lock()
		_, owned := a.settingsSubs[p.SubscriptionID]
		delete(a.settingsSubs, p.SubscriptionID)
		a.settingsMu.Unlock()
		if !owned {
			err = modelsettings.ErrInvalid
		} else {
			a.service.Unsubscribe(p.SubscriptionID)
			data = map[string]any{}
		}
	default:
		err = appserver.ErrMethodUnavailable
	}
	a.reply(ctx, id, method, data, err)
}
