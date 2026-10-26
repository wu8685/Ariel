package desktopagent

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/modelsettings"
)

type settingsLive struct {
	*fakeLive
	values modelsettings.Values
	writes int
}

func (l *settingsLive) ExecutionSettings() (modelsettings.Values, error) { return l.values, nil }
func (l *settingsLive) UpdateExecutionSettings(_ context.Context, v modelsettings.Values) error {
	l.values = v
	l.writes++
	return nil
}

type settingsModels struct{ calls atomic.Int32 }

func (m *settingsModels) Models(context.Context) (modelsettings.Catalog, error) {
	m.calls.Add(1)
	return modelsettings.Catalog{Models: []modelsettings.Model{{ID: "m", Name: "M", Efforts: []modelsettings.Effort{{ID: "low"}, {ID: "high"}}, InputModalities: []string{"text"}, Tiers: []modelsettings.Tier{}}}}, nil
}
func TestSettingsCatalogSharedAndMutationUsesConfirmedRevision(t *testing.T) {
	models := &settingsModels{}
	s := NewService(fakeHistory{}, nil)
	s.SetModelReader(models)
	defer s.Close()
	for i := 0; i < 5; i++ {
		if _, err := s.ModelCatalog(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	if models.calls.Load() != 1 {
		t.Fatal("catalog queried per thread")
	}
	live := &settingsLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, values: modelsettings.Values{Model: "m", Effort: "low", Speed: modelsettings.Speed{Mode: "inherit"}}}
	c := &threadController{id: "thread", live: live, current: map[string]any{"runtime": "idle"}, subs: map[string]*subscription{}}
	s.threads["thread"] = c
	before, err := s.ReadSettings(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	effort := "high"
	after, err := s.UpdateSettings(context.Background(), "thread", before.Revision, modelsettings.Patch{Effort: &effort})
	if err != nil || after.Values.Effort != "high" || after.Revision == before.Revision || live.writes != 1 {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	if _, err = s.UpdateSettings(context.Background(), "thread", before.Revision, modelsettings.Patch{Effort: &effort}); err == nil || live.writes != 1 {
		t.Fatal("stale write reached owner")
	}
}
func TestSettingsWatchOnlyEmitsChangedValues(t *testing.T) {
	live := &settingsLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, values: modelsettings.Values{Model: "m", Effort: "low", Speed: modelsettings.Speed{Mode: "inherit"}}}
	s := NewService(fakeHistory{}, nil)
	defer s.Close()
	s.SetModelReader(&settingsModels{})
	c := &threadController{id: "thread", live: live, current: map[string]any{"runtime": "idle"}, subs: map[string]*subscription{}}
	s.threads["thread"] = c
	events := make(chan map[string]any, 3)
	_, _, _, activate, err := s.WatchSettings(context.Background(), "thread", func(m map[string]any) { events <- m })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	s.publishSettings(c, live, nil)
	select {
	case <-events:
		t.Fatal("unchanged settings emitted")
	case <-time.After(10 * time.Millisecond):
	}
	live.values.Effort = "high"
	s.publishSettings(c, live, nil)
	select {
	case e := <-events:
		if e["event"] != "thread.settings.update" {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("settings-only update missing")
	}
}

func TestSettingsQueueAndInvalidCombinationNeverReachOwner(t *testing.T) {
	live := &settingsLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, values: modelsettings.Values{Model: "m", Effort: "low", Speed: modelsettings.Speed{Mode: "inherit"}}}
	s := NewService(fakeHistory{}, nil, &fakeFollowUpQueue{items: []appserver.QueuedSubmission{{ID: "q", ClientUserMessageID: "c"}}})
	defer s.Close()
	s.SetModelReader(&settingsModels{})
	s.threads["thread"] = &threadController{id: "thread", live: live, current: map[string]any{"runtime": "idle"}, subs: map[string]*subscription{}}
	before, _ := s.ReadSettings(context.Background(), "thread")
	effort := "high"
	if _, err := s.UpdateSettings(context.Background(), "thread", before.Revision, modelsettings.Patch{Effort: &effort}); err == nil || err.Error() != "QUEUE_NOT_EMPTY" || live.writes != 0 {
		t.Fatal("queued settings reached owner", err)
	}
	effort = "invalid"
	if _, err := s.UpdateSettings(context.Background(), "thread", before.Revision, modelsettings.Patch{Effort: &effort}); err == nil || live.writes != 0 {
		t.Fatal("invalid settings reached owner")
	}
}

func TestSettingsImageValidationRejectsTextOnlyModel(t *testing.T) {
	s := NewService(fakeHistory{}, nil)
	defer s.Close()
	s.SetModelReader(&settingsModels{})
	live := &settingsLive{fakeLive: &fakeLive{}, values: modelsettings.Values{Model: "m", Effort: "low", Speed: modelsettings.Speed{Mode: "inherit"}}}
	if err := s.validateImageModel(context.Background(), live, []string{"image"}); err == nil {
		t.Fatal("text-only image reached owner")
	}
}

func TestSettingsDoesNotRetainClosedOwnerAfterWarmRetirement(t *testing.T) {
	live := &settingsLive{fakeLive: &fakeLive{}, values: modelsettings.Values{Model: "m", Speed: modelsettings.Speed{Mode: "inherit"}}}
	s := NewService(fakeHistory{}, nil)
	defer s.Close()
	s.SetModelReader(&settingsModels{})
	c := &threadController{id: "thread", live: live, current: map[string]any{"runtime": "idle"}, subs: map[string]*subscription{}}
	s.threads["thread"] = c
	if _, err := s.ReadSettings(context.Background(), "thread"); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	stopWarmRetireLocked(c)
	c.mu.Unlock()
	s.retireWarm(c, live)
	value := reflect.ValueOf(c).Elem()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Type() == reflect.TypeFor[Live]() && !field.IsNil() {
			t.Fatal("closed native owner retained through", value.Type().Field(i).Name)
		}
	}
}

func TestSettingsRevisionNeverReusesAnObservedPreviousValue(t *testing.T) {
	live := &settingsLive{fakeLive: &fakeLive{}, values: modelsettings.Values{Model: "m", Effort: "low", Speed: modelsettings.Speed{Mode: "inherit"}}}
	s := NewService(fakeHistory{}, nil)
	defer s.Close()
	s.SetModelReader(&settingsModels{})
	s.threads["thread"] = &threadController{id: "thread", live: live, current: map[string]any{"runtime": "idle"}, subs: map[string]*subscription{}}
	first, _ := s.ReadSettings(context.Background(), "thread")
	live.values.Effort = "high"
	middle, _ := s.ReadSettings(context.Background(), "thread")
	live.values.Effort = "low"
	last, _ := s.ReadSettings(context.Background(), "thread")
	if first.Revision == middle.Revision || first.Revision == last.Revision {
		t.Fatal("ABA settings changes reused an old revision")
	}
}

func TestBootstrapSettingsHandoffInitializesOnlyExplicitIntent(t *testing.T) {
	live := &settingsLive{fakeLive: &fakeLive{}, values: modelsettings.Values{Model: "m", Effort: "high", Speed: modelsettings.Speed{Mode: "tier", TierID: "priority"}}}
	wanted := live.values
	wanted.Speed = modelsettings.Speed{Mode: "standard"}
	if err := initializeBootstrapSettings(context.Background(), live, &wanted); err != nil || live.values != wanted || live.writes != 1 {
		t.Fatal("explicit speed lost during handoff", err)
	}
	if err := initializeBootstrapSettings(context.Background(), live, nil); err != nil || live.writes != 1 {
		t.Fatal("ordinary recovery rewrote settings")
	}
	live.values.Model = "native-changed"
	if err := initializeBootstrapSettings(context.Background(), live, &wanted); err == nil || live.writes != 1 {
		t.Fatal("concurrent native model change overwritten")
	}
}
