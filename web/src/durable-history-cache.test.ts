import { describe, expect, it } from "vitest";
import { DurableHistoryCache, MemoryDurableHistoryBackend } from "./durable-history-cache";
import { emptyHistoryState } from "./history";
import type { Thread, Turn } from "./generated/protocol";

const identity = (threadId: string, agentEpoch = "epoch-a") => ({ deviceId: "mac", agentEpoch, threadId });
const turn = (id: string, text = id): Turn => ({ turnId: id, status: "completed", items: [{ itemId: `${id}-item`, role: "assistant", text }] });
const value = (threadId: string, text = threadId) => ({
  history: emptyHistoryState(),
  itemOverrides: {},
  thread: { threadId, title: threadId, cwd: "/fixture", updatedAt: "2026-10-15T00:00:00Z", runtime: "idle", turns: [turn(`${threadId}-turn`, text)], pendingInteractions: [{ interactionId: "pending", kind: "command_approval", prompt: "stale", availableDecisions: ["accept_once"] }], queuedMessages: [{ queueId: "queued", clientMessageId: "client", text: "stale", editable: true }], permissions: { sandbox: "full_access", approval: "never" } } as Thread,
});

describe("durable local conversation cache", () => {
  it("persists a validated display view and last selection across cache instances", async () => {
    const backend = new MemoryDurableHistoryBackend();
    const first = new DurableHistoryCache({ backend, now: () => 1_000 });
    expect(await first.set(identity("one"), value("one", "本机缓存正文"))).toBe(true);
    await first.rememberSelection(identity("one"));

    const restored = new DurableHistoryCache({ backend, now: () => 2_000 });
    expect((await restored.get(identity("one")))?.thread?.turns[0].items[0].text).toBe("本机缓存正文");
    expect((await restored.get(identity("one")))?.thread?.runtime).toBe("unknown");
    expect((await restored.get(identity("one")))?.thread?.pendingInteractions).toEqual([]);
    expect((await restored.get(identity("one")))?.thread?.queuedMessages).toBeUndefined();
    expect((await restored.get(identity("one")))?.thread?.permissions).toBeUndefined();
    expect(await restored.lastSelection()).toEqual(identity("one"));
    expect(await restored.get(identity("one", "other-epoch"))).toBeNull();
    expect((await restored.latest("mac", "one"))?.value.thread?.turns[0].items[0].text).toBe("本机缓存正文");
    expect((await restored.latest("mac", "one"))?.identity.agentEpoch).toBe("epoch-a");
  });

  it("selects the newest safe display cache across Agent epochs", async () => {
    let now = 1_000;
    const backend = new MemoryDurableHistoryBackend();
    const cache = new DurableHistoryCache({ backend, now: () => now });
    await cache.set(identity("one", "epoch-a"), value("one", "旧内容"));
    now = 2_000;
    await cache.set(identity("one", "epoch-b"), value("one", "新内容"));
    const restored = await cache.latest("mac", "one");
    expect(restored?.identity.agentEpoch).toBe("epoch-b");
    expect(restored?.value.thread?.runtime).toBe("unknown");
    expect(restored?.value.thread?.turns[0].items[0].text).toBe("新内容");
  });

  it("enforces entry, byte and TTL bounds without breaking when storage rejects", async () => {
    let now = 1_000;
    const backend = new MemoryDurableHistoryBackend();
    const cache = new DurableHistoryCache({ backend, maxEntries: 2, maxEntryBytes: 700, maxBytes: 1_200, ttlMs: 7_000, now: () => now });
    expect(await cache.set(identity("one"), value("one"))).toBe(true);
    now++;
    expect(await cache.set(identity("two"), value("two"))).toBe(true);
    await cache.get(identity("one"));
    now++;
    expect(await cache.set(identity("three"), value("three"))).toBe(true);
    expect(await cache.get(identity("two"))).toBeNull();
    expect(await cache.set(identity("huge"), value("huge", "界".repeat(500)))).toBe(false);
    now += 7_001;
    expect(await cache.get(identity("one"))).toBeNull();

    backend.failWrites = true;
    await expect(cache.set(identity("safe"), value("safe"))).resolves.toBe(false);
  });

  it("clears one device and drops corrupt records instead of exposing them", async () => {
    const backend = new MemoryDurableHistoryBackend();
    const cache = new DurableHistoryCache({ backend, now: () => 1_000 });
    await cache.set(identity("one"), value("one"));
    await cache.set({ deviceId: "other", agentEpoch: "epoch-a", threadId: "two" }, value("two"));
    await backend.injectCorrupt("corrupt");
    expect(await cache.get(identity("corrupt"))).toBeNull();
    await cache.clearDevice("mac");
    expect(await cache.get(identity("one"))).toBeNull();
    expect(await cache.get({ deviceId: "other", agentEpoch: "epoch-a", threadId: "two" })).not.toBeNull();
  });
});
