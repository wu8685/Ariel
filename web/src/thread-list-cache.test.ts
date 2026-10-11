import { describe, expect, it } from "vitest";
import type { Thread } from "./generated/protocol";
import { BrowserThreadListCache } from "./thread-list-cache";

const thread = (threadId: string): Thread => ({ threadId, title: threadId, cwd: `/tmp/${threadId}`, updatedAt: "2026-10-11T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] });

describe("BrowserThreadListCache", () => {
  it("isolates lists by device, agent epoch, and normalized query", () => {
    let now = 1_000;
    const cache = new BrowserThreadListCache({ now: () => now });
    cache.set({ deviceId: "mac-a", agentEpoch: "epoch-1", query: " Ariel " }, { threads: [thread("a")], cursor: "next" });
    expect(cache.get({ deviceId: "mac-a", agentEpoch: "epoch-1", query: "ariel" })?.value.threads[0].threadId).toBe("a");
    expect(cache.get({ deviceId: "mac-b", agentEpoch: "epoch-1", query: "ariel" })).toBeNull();
    expect(cache.get({ deviceId: "mac-a", agentEpoch: "epoch-2", query: "ariel" })).toBeNull();
    now += 61_000;
    expect(cache.get({ deviceId: "mac-a", agentEpoch: "epoch-1", query: "ariel" })?.fresh).toBe(false);
  });

  it("uses bounded LRU eviction and can clear one device", () => {
    let now = 1_000;
    const cache = new BrowserThreadListCache({ maxEntries: 2, now: () => ++now });
    cache.set({ deviceId: "a", agentEpoch: "1", query: "" }, { threads: [thread("a")], cursor: "" });
    cache.set({ deviceId: "b", agentEpoch: "1", query: "" }, { threads: [thread("b")], cursor: "" });
    cache.get({ deviceId: "a", agentEpoch: "1", query: "" });
    cache.set({ deviceId: "c", agentEpoch: "1", query: "" }, { threads: [thread("c")], cursor: "" });
    expect(cache.get({ deviceId: "b", agentEpoch: "1", query: "" })).toBeNull();
    cache.clearDevice("a");
    expect(cache.get({ deviceId: "a", agentEpoch: "1", query: "" })).toBeNull();
    expect(cache.get({ deviceId: "c", agentEpoch: "1", query: "" })?.value.threads[0].threadId).toBe("c");
  });
});
