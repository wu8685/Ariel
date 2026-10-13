import { describe, expect, it } from "vitest";
import type { Thread } from "./generated/protocol";
import { BrowserThreadUnreadStore } from "./thread-unread";

class MemoryStorage implements Storage {
  private values = new Map<string, string>();
  get length() { return this.values.size; }
  clear() { this.values.clear(); }
  getItem(key: string) { return this.values.get(key) ?? null; }
  key(index: number) { return [...this.values.keys()][index] ?? null; }
  removeItem(key: string) { this.values.delete(key); }
  setItem(key: string, value: string) { this.values.set(key, value); }
}

const thread = (threadId: string, updatedAt: string): Thread => ({
  threadId, title: threadId, cwd: `/tmp/${threadId}`, updatedAt, runtime: "idle", turns: [], pendingInteractions: [],
});

describe("BrowserThreadUnreadStore", () => {
  it("seeds first observation as read, then marks only a later version unread", () => {
    const store = new BrowserThreadUnreadStore(new MemoryStorage(), { now: () => 10 });
    expect(store.observe("mac", [thread("a", "2026-10-13T00:00:00Z")])).toEqual(new Set());
    expect(store.observe("mac", [thread("a", "2026-10-13T00:00:01Z")])).toEqual(new Set(["a"]));
    expect(store.observe("other", [thread("a", "2026-10-13T00:00:01Z")])).toEqual(new Set());
  });

  it("clears unread at the rendered version without accepting invalid or older timestamps", () => {
    const store = new BrowserThreadUnreadStore(new MemoryStorage());
    store.observe("mac", [thread("a", "2026-10-13T00:00:00Z")]);
    store.observe("mac", [thread("a", "2026-10-13T00:00:02Z")]);
    expect(store.markRead("mac", "a", "2026-10-13T00:00:02Z")).toBe(true);
    expect(store.observe("mac", [thread("a", "2026-10-13T00:00:01Z")])).toEqual(new Set());
    expect(store.markRead("mac", "a", "not-a-date")).toBe(false);
    expect(store.observe("mac", [thread("bad", "not-a-date")])).toEqual(new Set());
  });

  it("bounds persisted entries and recovers from damaged or unavailable storage", () => {
    const storage = new MemoryStorage();
    storage.setItem("ariel.thread-unread.v1", "broken");
    let now = 0;
    const store = new BrowserThreadUnreadStore(storage, { maxEntries: 2, now: () => ++now });
    store.observe("mac", [thread("a", "2026-10-13T00:00:00Z"), thread("b", "2026-10-13T00:00:00Z"), thread("c", "2026-10-13T00:00:00Z")]);
    const saved = JSON.parse(storage.getItem("ariel.thread-unread.v1")!);
    expect(Object.keys(saved.entries)).toHaveLength(2);

    const unavailable = new MemoryStorage();
    unavailable.setItem = () => { throw new DOMException("quota", "QuotaExceededError"); };
    expect(new BrowserThreadUnreadStore(unavailable).observe("mac", [thread("a", "2026-10-13T00:00:00Z")])).toEqual(new Set());
  });
});
