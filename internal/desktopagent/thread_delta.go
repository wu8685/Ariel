package desktopagent

import (
	"errors"
	"reflect"
)

var errInvalidThreadDelta = errors.New("thread delta contains duplicate or unsupported identities")

var threadStateKeys = []string{
	"threadId", "title", "searchSnippet", "cwd", "updatedAt", "isPinned", "runtime",
	"historyComplete", "recentComplete", "permissions", "pendingInteractions", "queuedMessages",
}

func buildThreadPatch(previous, current map[string]any) (map[string]any, bool, error) {
	previousTurns, previousOrder, err := indexDeltaTurns(previous["turns"])
	if err != nil {
		return nil, false, err
	}
	currentTurns, currentOrder, err := indexDeltaTurns(current["turns"])
	if err != nil {
		return nil, false, err
	}

	turnPatches := make([]any, 0, len(currentOrder))
	for _, turnID := range currentOrder {
		turn := currentTurns[turnID]
		before, existed := previousTurns[turnID]
		turnPatch, changed, patchErr := buildTurnPatch(before, turn, !existed)
		if patchErr != nil {
			return nil, false, patchErr
		}
		if changed {
			turnPatches = append(turnPatches, turnPatch)
		}
	}
	removedTurns := make([]any, 0)
	for _, turnID := range previousOrder {
		if _, exists := currentTurns[turnID]; !exists {
			removedTurns = append(removedTurns, turnID)
		}
	}

	state := projectThreadState(current)
	meaningfulStateChanged := !reflect.DeepEqual(projectComparableThreadState(previous), projectComparableThreadState(current))
	changed := meaningfulStateChanged || !reflect.DeepEqual(previousOrder, currentOrder) || len(turnPatches) > 0 || len(removedTurns) > 0
	if !changed {
		return nil, false, nil
	}
	return map[string]any{
		"threadState":    state,
		"turnOrder":      stringsAsAny(currentOrder),
		"turnPatches":    turnPatches,
		"removedTurnIds": removedTurns,
	}, true, nil
}

func projectThreadState(thread map[string]any) map[string]any {
	state := make(map[string]any, len(threadStateKeys))
	for _, key := range threadStateKeys {
		if value, ok := thread[key]; ok {
			state[key] = value
		}
	}
	return state
}

func projectComparableThreadState(thread map[string]any) map[string]any {
	state := projectThreadState(thread)
	delete(state, "updatedAt")
	return state
}

func indexDeltaTurns(raw any) (map[string]map[string]any, []string, error) {
	turns, ok := raw.([]any)
	if !ok {
		return nil, nil, errInvalidThreadDelta
	}
	indexed := make(map[string]map[string]any, len(turns))
	order := make([]string, 0, len(turns))
	for _, rawTurn := range turns {
		turn, ok := rawTurn.(map[string]any)
		if !ok {
			return nil, nil, errInvalidThreadDelta
		}
		turnID, ok := turn["turnId"].(string)
		if !ok || turnID == "" {
			return nil, nil, errInvalidThreadDelta
		}
		if _, duplicate := indexed[turnID]; duplicate {
			return nil, nil, errInvalidThreadDelta
		}
		if _, _, err := indexDeltaItems(turn["items"]); err != nil {
			return nil, nil, err
		}
		indexed[turnID] = turn
		order = append(order, turnID)
	}
	return indexed, order, nil
}

func buildTurnPatch(previous, current map[string]any, newTurn bool) (map[string]any, bool, error) {
	currentItems, currentOrder, err := indexDeltaItems(current["items"])
	if err != nil {
		return nil, false, err
	}
	previousItems := map[string]map[string]any{}
	previousOrder := []string{}
	if !newTurn {
		previousItems, previousOrder, err = indexDeltaItems(previous["items"])
		if err != nil {
			return nil, false, err
		}
	}

	upserts := make([]any, 0, len(currentItems))
	for _, itemID := range currentOrder {
		item := currentItems[itemID]
		if before, exists := previousItems[itemID]; newTurn || !exists || !reflect.DeepEqual(before, item) {
			upserts = append(upserts, item)
		}
	}
	removed := make([]any, 0)
	for _, itemID := range previousOrder {
		if _, exists := currentItems[itemID]; !exists {
			removed = append(removed, itemID)
		}
	}

	metadataChanged := newTurn || previous["status"] != current["status"] || previous["itemsComplete"] != current["itemsComplete"] || previous["nextItemCursor"] != current["nextItemCursor"]
	changed := metadataChanged || !reflect.DeepEqual(previousOrder, currentOrder) || len(upserts) > 0 || len(removed) > 0
	if !changed {
		return nil, false, nil
	}
	patch := map[string]any{
		"turnId":         current["turnId"],
		"status":         current["status"],
		"itemOrder":      stringsAsAny(currentOrder),
		"upsertItems":    upserts,
		"removedItemIds": removed,
	}
	if value, ok := current["itemsComplete"]; ok {
		patch["itemsComplete"] = value
	}
	if value, ok := current["nextItemCursor"]; ok {
		patch["nextItemCursor"] = value
	}
	return patch, true, nil
}

func indexDeltaItems(raw any) (map[string]map[string]any, []string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, nil, errInvalidThreadDelta
	}
	indexed := make(map[string]map[string]any, len(items))
	order := make([]string, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, nil, errInvalidThreadDelta
		}
		itemID, ok := item["itemId"].(string)
		if !ok || itemID == "" {
			return nil, nil, errInvalidThreadDelta
		}
		if _, duplicate := indexed[itemID]; duplicate {
			return nil, nil, errInvalidThreadDelta
		}
		indexed[itemID] = item
		order = append(order, itemID)
	}
	return indexed, order, nil
}

func stringsAsAny(values []string) []any {
	result := make([]any, len(values))
	for index := range values {
		result[index] = values[index]
	}
	return result
}
