import { describe, expect, it } from "vitest";
import { ComposerDraftStore, emptyComposerDraft, type ComposerDraftIdentity } from "./composer-drafts";
import type { FileQuote } from "./file-context";

const identity = (threadId: string, deviceId = "mac"): ComposerDraftIdentity => ({ deviceId, threadId });
const quote: FileQuote = { threadId: "one", turnId: "turn", itemId: "item", path: "src/app.ts", sha256: "a".repeat(64), modifiedAtMs: 1, startLine: 2, endLine: 3, text: "selected" };

describe("per-conversation composer drafts", () => {
  it("keeps independent text and revisions for each thread", () => {
    const store = new ComposerDraftStore();
    store.set(identity("one"), { ...emptyComposerDraft(), draft: "draft one", revision: 1 });
    store.set(identity("two"), { ...emptyComposerDraft(), draft: "draft two", revision: 4 });

    expect(store.get(identity("one"))).toMatchObject({ draft: "draft one", revision: 1 });
    expect(store.get(identity("two"))).toMatchObject({ draft: "draft two", revision: 4 });
  });

  it("separates the same thread id on different devices", () => {
    const store = new ComposerDraftStore();
    store.set(identity("same", "mac-a"), { ...emptyComposerDraft(), draft: "A", revision: 1 });
    store.set(identity("same", "mac-b"), { ...emptyComposerDraft(), draft: "B", revision: 1 });

    expect(store.get(identity("same", "mac-a"))?.draft).toBe("A");
    expect(store.get(identity("same", "mac-b"))?.draft).toBe("B");
  });

  it("stores text, screenshots, file quotes, and queue editing as one snapshot", () => {
    const store = new ComposerDraftStore();
    const screenshot = { name: "shot.png", bytes: 3, dataUri: "data:image/png;base64,AAA=" };
    store.set(identity("one"), { draft: "body", screenshots: [screenshot], fileQuotes: [quote], editingQueueId: "queue-1", revision: 7 });

    expect(store.get(identity("one"))).toEqual({ draft: "body", screenshots: [screenshot], fileQuotes: [quote], editingQueueId: "queue-1", revision: 7 });
  });

  it("clears only the source snapshot with the exact submitted revision", () => {
    const store = new ComposerDraftStore();
    store.set(identity("one"), { ...emptyComposerDraft(), draft: "sent", revision: 2 });
    store.set(identity("two"), { ...emptyComposerDraft(), draft: "current", revision: 8 });

    expect(store.clearIfRevision(identity("one"), 1)).toBe(false);
    expect(store.get(identity("one"))?.draft).toBe("sent");
    expect(store.clearIfRevision(identity("one"), 2)).toBe(true);
    expect(store.get(identity("one"))?.draft).toBe("");
    expect(store.get(identity("two"))?.draft).toBe("current");
  });

  it("drops file quotes that belong to another thread", () => {
    const store = new ComposerDraftStore();
    store.set(identity("two"), { ...emptyComposerDraft(), draft: "safe text", fileQuotes: [quote], revision: 3 });

    expect(store.get(identity("two"))).toMatchObject({ draft: "safe text", fileQuotes: [] });
  });

  it("clears every in-memory draft on logout", () => {
    const store = new ComposerDraftStore();
    store.set(identity("one"), { ...emptyComposerDraft(), draft: "private", revision: 1 });
    store.clear();
    expect(store.get(identity("one"))).toBeNull();
  });
});
