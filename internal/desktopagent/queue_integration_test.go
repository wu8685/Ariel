package desktopagent

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
)

// Opt-in mutation check against a newly created isolated fixture. It exercises
// only the durable queue and removes every item it creates before returning.
func TestRealFixtureNativeQueueLifecycle(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" {
		t.Skip("set an isolated queue fixture and explicit write enable")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("not a verified isolated fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	process, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	queue := appserver.QueueReader{RPC: process.Session}
	supported, err := queue.Supported(ctx)
	if err != nil || !supported {
		t.Fatalf("native queue unavailable: supported=%t err=%v", supported, err)
	}
	clientOne, clientTwo := "ariel-queue-"+rand.Text(), "ariel-queue-"+rand.Text()
	created := make([]string, 0, 2)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, id := range created {
			_, _ = queue.Delete(cleanupCtx, manifest.ThreadID, id)
		}
	}()
	one, err := queue.Add(ctx, manifest.ThreadID, clientOne, "first", nil)
	if err != nil || one.ID == "" || one.ClientUserMessageID != clientOne {
		t.Fatalf("queue add one: %+v %v", one, err)
	}
	created = append(created, one.ID)
	two, err := queue.Add(ctx, manifest.ThreadID, clientTwo, "second", nil)
	if err != nil || two.ID == "" || two.ClientUserMessageID != clientTwo {
		t.Fatalf("queue add two: %+v %v", two, err)
	}
	created = append(created, two.ID)
	updated, err := queue.Update(ctx, manifest.ThreadID, one.ID, "first edited", nil)
	if err != nil || updated.ID != one.ID || updated.ClientUserMessageID != clientOne {
		t.Fatalf("queue update: %+v %v", updated, err)
	}
	if err := queue.Reorder(ctx, manifest.ThreadID, []string{two.ID, one.ID}); err != nil {
		t.Fatal(err)
	}
	items, err := queue.List(ctx, manifest.ThreadID)
	if err != nil || len(items) != 2 || items[0].ID != two.ID || items[1].ID != one.ID {
		t.Fatalf("queue order: %+v %v", items, err)
	}
	for _, id := range []string{two.ID, one.ID} {
		deleted, err := queue.Delete(ctx, manifest.ThreadID, id)
		if err != nil || !deleted {
			t.Fatalf("queue delete %s: deleted=%t err=%v", id, deleted, err)
		}
	}
	created = created[:0]
	items, err = queue.List(ctx, manifest.ThreadID)
	if err != nil || len(items) != 0 {
		t.Fatalf("queue cleanup: %+v %v", items, err)
	}
	t.Log("native queue add, update, reorder, list, and delete verified on isolated fixture")
}

// Opt-in end-to-end Steer check. It starts a deliberately long no-tools turn
// in an isolated fixture, queues one follow-up, steers it through the Desktop
// owner, confirms queue deletion, and interrupts the test turn during cleanup.
func TestRealFixtureQueuedSteer(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" || os.Getenv("ARIEL_TEST_QUEUE_STEER") != "1" {
		t.Skip("requires an isolated no-tools fixture and explicit queue Steer enable")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Purpose != "" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("not a verified no-tools fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	process, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	follower, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	live := follower.(*desktopipc.Follower)
	cleanupFollower, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		live.Close()
		t.Fatal(err)
	}
	cleanupLive := cleanupFollower.(*desktopipc.Follower)
	defer cleanupLive.Close()
	queue := appserver.QueueReader{RPC: process.Session}
	service := NewService(singleFixtureHistory{thread: appserver.Thread{ID: manifest.ThreadID, Name: "isolated fixture", CWD: manifest.Workspace}}, func(context.Context, string, string) (Live, error) { return live, nil }, queue)
	defer service.Close()
	defer cleanupConcurrentFixture(t, cleanupLive, manifest.ThreadID, manifest.Workspace)
	items, err := service.QueueAdd(ctx, manifest.ThreadID, fixtureMessageID(t), "Stop listing and reply exactly ARIEL_QUEUE_STEER_OK.", nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("queue follow-up: %+v %v", items, err)
	}
	queueID := items[0]["queueId"].(string)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = service.QueueDelete(cleanupCtx, manifest.ThreadID, queueID)
	}()
	turnID, err := live.Start(ctx, fixtureMessageID(t), "Ariel isolated queue Steer check. Do not use tools or inspect files. Output the integers 1 through 2000, one per line.")
	if err != nil {
		t.Fatal(err)
	}
	items, err = service.QueueSteer(ctx, manifest.ThreadID, queueID, turnID)
	if err != nil || len(items) != 0 {
		t.Fatalf("owner Steer: %+v %v", items, err)
	}
	t.Log("queued follow-up was confirmed by the Desktop owner and removed from the durable queue")
}
