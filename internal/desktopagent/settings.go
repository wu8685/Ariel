package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/modelsettings"
)

type ModelReader interface {
	Models(context.Context) (modelsettings.Catalog, error)
}
type executionSettingsLive interface {
	ExecutionSettings() (modelsettings.Values, error)
	UpdateExecutionSettings(context.Context, modelsettings.Values) error
}

func setControllerLiveLocked(c *threadController, live Live) {
	c.live = live
	c.settingsGeneration = rand.Text()
	c.settingsVersion = 0
	c.settingsKnown = false
}

// This is the one-time completion of an explicitly configured bootstrap,
// not replay/reconciliation of browser preferences during ordinary reconnect.
// Desktop resume applies its global speed even when the bootstrap turn used
// an explicit tier. Initialize the new native owner before enabling mutations.
func initializeBootstrapSettings(ctx context.Context, live Live, wanted *modelsettings.Values) error {
	if wanted == nil {
		return nil
	}
	capable, ok := live.(executionSettingsLive)
	if !ok {
		return appserver.ErrMethodUnavailable
	}
	current, err := capable.ExecutionSettings()
	if err != nil {
		return err
	}
	if current.Model != wanted.Model || wanted.Effort != "" && current.Effort != wanted.Effort {
		return desktopipc.ErrNativeStateUncertain
	}
	if current == *wanted {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = capable.UpdateExecutionSettings(writeCtx, *wanted); err != nil {
		return err
	}
	current, err = capable.ExecutionSettings()
	if err != nil || current != *wanted {
		return appserver.ErrOutcomeUnknown
	}
	return nil
}

func (s *Service) SetModelReader(r ModelReader) { s.models = r }
func (s *Service) SettingsEnabled() bool        { return s.models != nil }

func (s *Service) validateImageModel(ctx context.Context, live Live, images []string) error {
	if len(images) == 0 || !s.SettingsEnabled() {
		return nil
	}
	capable, ok := live.(executionSettingsLive)
	if !ok {
		return appserver.ErrMethodUnavailable
	}
	values, err := capable.ExecutionSettings()
	if err != nil {
		return err
	}
	catalog, err := s.ModelCatalog(ctx, false)
	if err != nil {
		return err
	}
	return catalog.Validate(values, true)
}
func (s *Service) ModelCatalog(ctx context.Context, refresh bool) (modelsettings.Catalog, error) {
	if s.models == nil {
		return modelsettings.Catalog{}, appserver.ErrMethodUnavailable
	}
	for {
		s.modelsMu.Lock()
		if loading := s.modelsLoading; loading != nil {
			s.modelsMu.Unlock()
			select {
			case <-loading:
				refresh = false
				continue
			case <-ctx.Done():
				return modelsettings.Catalog{}, ctx.Err()
			}
		}
		if !refresh && time.Now().Before(s.modelsExpires) {
			out := s.modelsCatalog
			s.modelsMu.Unlock()
			return out, nil
		}
		loading := make(chan struct{})
		s.modelsLoading = loading
		s.modelsMu.Unlock()
		catalog, err := s.models.Models(ctx)
		if err == nil {
			err = catalog.CheckBounds()
		}
		s.modelsMu.Lock()
		if err == nil {
			s.modelsCatalog = catalog
			s.modelsExpires = time.Now().Add(5 * time.Minute)
		}
		s.modelsLoading = nil
		close(loading)
		s.modelsMu.Unlock()
		return catalog, err
	}
}

func settingsSnapshotLocked(c *threadController, _ json.RawMessage) (modelsettings.Snapshot, error) {
	capable, ok := c.live.(executionSettingsLive)
	if !ok {
		return modelsettings.Snapshot{}, appserver.ErrMethodUnavailable
	}
	values, err := capable.ExecutionSettings()
	if err != nil {
		return modelsettings.Snapshot{}, err
	}
	if c.settingsGeneration == "" {
		c.settingsGeneration = rand.Text()
	}
	if !c.settingsKnown || c.settingsLast != values {
		c.settingsVersion++
		c.settingsLast = values
		c.settingsKnown = true
	}
	fresh := c.current != nil
	return modelsettings.Snapshot{Values: values, Revision: c.settingsGeneration + "-" + strconv.FormatUint(c.settingsVersion, 10), Freshness: map[bool]string{true: "fresh", false: "stale"}[fresh], Writable: fresh && !c.bootstrapHold && !c.migrating}, nil
}
func (s *Service) ReadSettings(ctx context.Context, id string) (modelsettings.Snapshot, error) {
	if !s.SettingsEnabled() {
		return modelsettings.Snapshot{}, appserver.ErrMethodUnavailable
	}
	c, err := s.controller(id)
	if err != nil {
		return modelsettings.Snapshot{}, err
	}
	defer s.releaseController(c)
	c.mu.Lock()
	defer c.mu.Unlock()
	return settingsSnapshotLocked(c, nil)
}
func (s *Service) UpdateSettings(ctx context.Context, id, expected string, p modelsettings.Patch) (modelsettings.Snapshot, error) {
	if !s.SettingsEnabled() {
		return modelsettings.Snapshot{}, appserver.ErrMethodUnavailable
	}
	if expected == "" || (p.Model == nil && p.Effort == nil && p.Speed == nil) {
		return modelsettings.Snapshot{}, modelsettings.ErrInvalid
	}
	catalog, err := s.ModelCatalog(ctx, false)
	if err != nil {
		return modelsettings.Snapshot{}, err
	}
	c, err := s.controller(id)
	if err != nil {
		return modelsettings.Snapshot{}, err
	}
	defer s.releaseController(c)
	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	before, err := settingsSnapshotLocked(c, nil)
	live := c.live
	c.mu.Unlock()
	if err != nil {
		return before, err
	}
	if !before.Writable {
		return before, desktopipc.ErrResyncRequired
	}
	if before.Revision != expected {
		return before, modelsettings.ErrConflict
	}
	desired := p.Apply(before.Values)
	if err = catalog.Validate(desired, false); err != nil {
		return before, err
	}
	if s.queue != nil {
		items, e := s.queue.List(ctx, id)
		if e != nil {
			return before, e
		}
		if len(items) > 0 {
			return before, errors.New("QUEUE_NOT_EMPTY")
		}
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = live.(executionSettingsLive).UpdateExecutionSettings(writeCtx, desired); err != nil {
		return before, err
	}
	c.mu.Lock()
	if c.live != live {
		c.mu.Unlock()
		return before, appserver.ErrOutcomeUnknown
	}
	after, err := settingsSnapshotLocked(c, nil)
	c.mu.Unlock()
	if err != nil || after.Values != desired {
		return before, appserver.ErrOutcomeUnknown
	}
	s.publishSettings(c, live, nil)
	return after, nil
}

func settingsEvent(c *threadController, sub *subscription, snapshot modelsettings.Snapshot) map[string]any {
	sub.seq++
	sub.settings = &snapshot
	return map[string]any{"event": "thread.settings.update", "threadId": c.id, "subscriptionId": sub.id, "streamId": sub.streamID, "seq": sub.seq, "settings": snapshot}
}
func (s *Service) publishSettings(c *threadController, live Live, raw json.RawMessage) {
	c.mu.Lock()
	if c.live != live {
		c.mu.Unlock()
		return
	}
	interested := false
	for _, sub := range c.subs {
		if sub.active && sub.settings != nil {
			interested = true
			break
		}
	}
	if !interested {
		c.mu.Unlock()
		return
	}
	snapshot, err := settingsSnapshotLocked(c, raw)
	if err != nil {
		c.mu.Unlock()
		return
	}
	type delivery struct {
		emit  func(map[string]any)
		event map[string]any
	}
	var out []delivery
	for _, sub := range c.subs {
		if sub.active && sub.settings != nil && *sub.settings != snapshot {
			out = append(out, delivery{sub.emit, settingsEvent(c, sub, snapshot)})
		}
	}
	c.mu.Unlock()
	for _, d := range out {
		d.emit(d.event)
	}
}
func (s *Service) WatchSettings(ctx context.Context, id string, emit func(map[string]any)) (string, string, modelsettings.Snapshot, func(), error) {
	if !s.SettingsEnabled() {
		return "", "", modelsettings.Snapshot{}, nil, appserver.ErrMethodUnavailable
	}
	if emit == nil {
		return "", "", modelsettings.Snapshot{}, nil, modelsettings.ErrInvalid
	}
	c, err := s.controller(id)
	if err != nil {
		return "", "", modelsettings.Snapshot{}, nil, err
	}
	defer s.releaseController(c)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.subs) >= maxSubscriptionsPerThread {
		return "", "", modelsettings.Snapshot{}, nil, modelsettings.ErrCapacity
	}
	before, err := settingsSnapshotLocked(c, nil)
	if err != nil {
		return "", "", before, nil, err
	}
	sub := &subscription{id: "settings-" + rand.Text(), streamID: "settings-stream-" + rand.Text(), seq: 1, settings: &before, emit: emit}
	c.subs[sub.id] = sub
	stopWarmRetireLocked(c)
	activate := func() {
		c.mu.Lock()
		if c.subs[sub.id] != sub {
			c.mu.Unlock()
			return
		}
		sub.active = true
		live := c.live
		c.mu.Unlock()
		s.publishSettings(c, live, nil)
	}
	return sub.id, sub.streamID, before, activate, nil
}
