import { describe, expect, it } from "vitest";
import { BrowserHistoryCache, mergeCachedHistory, type HistoryCacheIdentity } from "./history-cache";
import { emptyHistoryState } from "./history";
import type { Turn } from "./generated/protocol";

const identity = (threadId: string, agentEpoch = "epoch-a"): HistoryCacheIdentity => ({ deviceId: "mac", agentEpoch, threadId });
const turn = (id: string, text = id): Turn => ({ turnId: id, status: "completed", items: [{ itemId: `${id}-item`, role: "assistant", text }] });
const value = (id: string, text = id) => ({ history: { ...emptyHistoryState(), older: [turn(id, text)], cursor: `after-${id}`, anchorSeen: true }, itemOverrides: {} });

describe("bounded browser history cache", () => {
  it("isolates entries by agent epoch and expires them after five minutes", () => {
    let now = 1_000;
    const cache = new BrowserHistoryCache({ now: () => now });
    expect(cache.set(identity("one"), value("old"))).toBe(true);
    expect(cache.get(identity("one"))?.history.older[0].turnId).toBe("old");
    expect(cache.get(identity("one", "epoch-b"))).toBeNull();
    now += 5 * 60_000 + 1;
    expect(cache.get(identity("one"))).toBeNull();
  });

  it("updates an existing key and evicts the least recently used entry", () => {
    let now = 1_000;
    const cache = new BrowserHistoryCache({ maxEntries: 2, maxBytes: 1 << 20, now: () => now });
    cache.set(identity("one"), value("one-old"));
    now++;
    cache.set(identity("two"), value("two-old"));
    now++;
    expect(cache.get(identity("one"))).not.toBeNull();
    now++;
    cache.set(identity("three"), value("three-old"));
    expect(cache.get(identity("two"))).toBeNull();
    expect(cache.get(identity("one"))).not.toBeNull();
    expect(cache.get(identity("three"))).not.toBeNull();

    cache.set(identity("one"), value("one-new"));
    expect(cache.get(identity("one"))?.history.older[0].turnId).toBe("one-new");
  });

  it("counts UTF-8 bytes, rejects oversized entries, and never truncates content", () => {
    const cache = new BrowserHistoryCache({ maxEntries: 8, maxBytes: 512, now: () => 1_000 });
    expect(cache.set(identity("small"), value("small", "短文本"))).toBe(true);
    expect(cache.set(identity("large"), value("large", "界".repeat(200)))).toBe(false);
    expect(cache.get(identity("large"))).toBeNull();
    expect(cache.get(identity("small"))?.history.older[0].items[0].text).toBe("短文本");
    expect(cache.bytes()).toBeLessThanOrEqual(512);
  });

  it("fails closed for incomplete or duplicate turns and keeps only matching completed item overrides", () => {
    const cache = new BrowserHistoryCache({ now: () => 1_000 });
    const incomplete: Turn = { ...turn("running"), status: "inProgress" };
    expect(cache.set(identity("running"), { history: { ...emptyHistoryState(), older: [incomplete] }, itemOverrides: {} })).toBe(false);
    expect(cache.set(identity("duplicate"), { history: { ...emptyHistoryState(), older: [turn("same"), turn("same")] }, itemOverrides: {} })).toBe(false);

    const overridden = { ...turn("old"), items: [turn("old").items[0], { itemId: "older-item", role: "assistant" as const, text: "older" }] };
    expect(cache.set(identity("valid"), { history: { ...emptyHistoryState(), older: [turn("old")] }, itemOverrides: { old: overridden, unrelated: turn("unrelated") } })).toBe(true);
    expect(cache.get(identity("valid"))?.itemOverrides).toEqual({ old: overridden });
  });

  it("clears one device or the full connection without retaining body text", () => {
    const cache = new BrowserHistoryCache({ now: () => 1_000 });
    cache.set(identity("one"), value("one"));
    cache.set({ deviceId: "other", agentEpoch: "epoch-a", threadId: "two" }, value("two"));
    cache.clearDevice("mac");
    expect(cache.get(identity("one"))).toBeNull();
    expect(cache.get({ deviceId: "other", agentEpoch: "epoch-a", threadId: "two" })).not.toBeNull();
    cache.clear();
    expect(cache.bytes()).toBe(0);
  });

  it("lets a fresh live snapshot replace overlapping cached turns and items", () => {
    const cached = {
      history: { ...emptyHistoryState(), older: [turn("old"), turn("overlap")], cursor: "after-old", anchorSeen: true },
      itemOverrides: { old: turn("old", "expanded old"), overlap: turn("overlap", "stale overlap") },
    };
    const merged = mergeCachedHistory(cached, [turn("overlap", "fresh overlap"), turn("latest")]);
    expect(merged.history.older.map(item => item.turnId)).toEqual(["old"]);
    expect(Object.keys(merged.itemOverrides)).toEqual(["old"]);
  });
});
