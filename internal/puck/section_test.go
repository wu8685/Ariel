package puck

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSidebarSectionsPersistWithoutDeletingEntries(t *testing.T) {
	dir := t.TempDir()
	module, err := NewModule(dir, &fakeModuleBridge{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"小帕","brainDeviceId":"brain-a"}`))
	second := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"罗宾","brainDeviceId":"brain-a"}`))
	firstID := first.Data["assistant"].(map[string]any)["assistantId"].(string)
	secondID := second.Data["assistant"].(map[string]any)["assistantId"].(string)
	createdGroup := module.Handle(ctx, "puck.group.create", mustJSON(map[string]any{"name": "研究组", "memberIds": []string{firstID, secondID}}))
	if createdGroup.Error != nil {
		t.Fatalf("group=%+v", createdGroup)
	}
	groupID := createdGroup.Data["group"].(Group).ID
	if err := module.store.AppendGroupMessage(ctx, GroupMessage{ID: "group-msg-1", GroupID: groupID, Role: "user", Kind: "text", Text: "一起看一下"}); err != nil {
		t.Fatal(err)
	}
	beforeAssistants, err := module.store.Assistants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeMessages, err := module.store.GroupMessages(ctx, groupID)
	if err != nil {
		t.Fatal(err)
	}

	created := module.Handle(ctx, "puck.section.create", mustJSON(map[string]any{"name": "工作"}))
	if created.Error != nil {
		t.Fatalf("create=%+v", created)
	}
	sections := created.Data["sections"].([]SidebarSection)
	if len(sections) != 1 || sections[0].Name != "工作" || sections[0].Collapsed {
		t.Fatalf("sections=%+v", sections)
	}
	sectionID := sections[0].ID
	renamed := module.Handle(ctx, "puck.section.rename", mustJSON(map[string]any{"sectionId": sectionID, "name": "工作机"}))
	if renamed.Error != nil || renamed.Data["sections"].([]SidebarSection)[0].Name != "工作机" {
		t.Fatalf("rename=%+v", renamed)
	}
	movedAssistant := module.Handle(ctx, "puck.section.move", mustJSON(map[string]any{"kind": "assistant", "entryId": firstID, "sectionId": sectionID, "position": 0}))
	movedGroup := module.Handle(ctx, "puck.section.move", mustJSON(map[string]any{"kind": "group", "entryId": groupID, "sectionId": sectionID, "position": 1}))
	if movedAssistant.Error != nil || movedGroup.Error != nil {
		t.Fatalf("assistant=%+v group=%+v", movedAssistant, movedGroup)
	}
	placements := movedGroup.Data["placements"].([]SidebarPlacement)
	if len(placements) != 2 || placements[0].Kind != "assistant" || placements[1].Kind != "group" || placements[0].SectionID != sectionID {
		t.Fatalf("placements=%+v", placements)
	}
	collapsed := module.Handle(ctx, "puck.section.collapse", mustJSON(map[string]any{"sectionId": sectionID, "collapsed": true}))
	ungrouped := module.Handle(ctx, "puck.section.collapse", mustJSON(map[string]any{"collapsed": true}))
	if collapsed.Error != nil || ungrouped.Error != nil || !collapsed.Data["sections"].([]SidebarSection)[0].Collapsed || ungrouped.Data["ungroupedCollapsed"] != true {
		t.Fatalf("collapsed=%+v ungrouped=%+v", collapsed, ungrouped)
	}
	module.Close()

	reopened, err := NewModule(dir, &fakeModuleBridge{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	listed := reopened.Handle(ctx, "puck.section.list", json.RawMessage(`{}`))
	if listed.Error != nil {
		t.Fatalf("list=%+v", listed)
	}
	restored := listed.Data["sections"].([]SidebarSection)
	if len(restored) != 1 || restored[0].Name != "工作机" || !restored[0].Collapsed || listed.Data["ungroupedCollapsed"] != true {
		t.Fatalf("restored=%+v collapsed=%v", restored, listed.Data["ungroupedCollapsed"])
	}
	groups, err := reopened.store.Groups(ctx)
	if err != nil || len(groups) != 1 || groups[0].LastMessagePreview != "一起看一下" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	deleted := reopened.Handle(ctx, "puck.section.delete", mustJSON(map[string]any{"sectionId": sectionID}))
	if deleted.Error != nil || len(deleted.Data["sections"].([]SidebarSection)) != 0 {
		t.Fatalf("delete=%+v", deleted)
	}
	afterPlacements := deleted.Data["placements"].([]SidebarPlacement)
	if len(afterPlacements) != 2 || afterPlacements[0].SectionID != "" || afterPlacements[1].SectionID != "" {
		t.Fatalf("ungrouped placements=%+v", afterPlacements)
	}
	afterAssistants, err := reopened.store.Assistants(ctx)
	if err != nil || len(afterAssistants) != len(beforeAssistants) {
		t.Fatalf("assistants before=%d after=%d err=%v", len(beforeAssistants), len(afterAssistants), err)
	}
	afterMessages, err := reopened.store.GroupMessages(ctx, groupID)
	if err != nil || len(afterMessages) != len(beforeMessages) || afterMessages[0].Text != "一起看一下" {
		t.Fatalf("messages=%+v err=%v", afterMessages, err)
	}
	if _, err := reopened.store.Group(ctx, groupID); err != nil {
		t.Fatal(err)
	}
}
