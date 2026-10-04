import { describe, expect, it } from "vitest";
import { appendOlderPage, prependOlderItems, emptyHistoryState } from "./history";
import type { Turn } from "./generated/protocol";

const turn = (id: string): Turn => ({ turnId: id, status: "completed", items: [{ itemId: `${id}-item`, role: "assistant", text: id }] });

describe("bounded browser history", () => {
  it("walks past the overlapping recent window before appending older turns", () => {
    const live = [turn("8"), turn("9"), turn("10")];
    const overlap = appendOlderPage(emptyHistoryState(), live, { turns: [turn("10"), turn("9"), turn("8")], nextCursor: "after-8" });
    expect(overlap.older).toEqual([]);
    expect(overlap.anchorSeen).toBe(true);
    expect(overlap.cursor).toBe("after-8");
    const older = appendOlderPage(overlap, live, { turns: [turn("7"), turn("6")], nextCursor: "after-6" });
    expect(older.older.map(t => t.turnId)).toEqual(["6", "7"]);
    expect(older.cursor).toBe("after-6");
  });

  it("deduplicates turn and item IDs while prepending partial giant-turn history", () => {
    const live = [turn("10")];
    const anchor = appendOlderPage(emptyHistoryState(), live, { turns: [turn("10"), turn("9")], nextCursor: "after-9" });
    const overlap = appendOlderPage(anchor, live, { turns: [turn("9"), turn("8")], nextCursor: "" });
    expect(overlap.older.map(t => t.turnId)).toEqual(["8", "9"]);
    expect(overlap.exhausted).toBe(true);
    const partial: Turn = { turnId: "giant", status: "completed", items: [{ itemId: "b", role: "assistant", text: "new" }], itemsComplete: false, nextItemCursor: "before-b" };
    const completed = prependOlderItems(partial, [{ itemId: "a", role: "assistant", text: "old" }, { itemId: "b", role: "assistant", text: "duplicate" }], "", true);
    expect(completed.items.map(item => item.itemId)).toEqual(["a", "b"]);
    expect(completed.itemsComplete).toBe(true);
  });

  it("retains only a bounded older window and marks a gap next to live turns", () => {
    const live = [turn("latest")];
    const anchor = appendOlderPage(emptyHistoryState(), live, { turns: [turn("latest")], nextCursor: "next" });
    const first = appendOlderPage(anchor, live, { turns: Array.from({ length: 40 }, (_, i) => turn(`older-${40 - i}`)), nextCursor: "more" });
    const second = appendOlderPage(first, live, { turns: Array.from({ length: 40 }, (_, i) => turn(`oldest-${40 - i}`)), nextCursor: "" });
    expect(second.older).toHaveLength(50);
    expect(second.older[0].turnId).toBe("oldest-1");
    expect(second.gap).toBe(true);
  });

  it("also caps retained older message bytes on a phone", () => {
    const live = [turn("latest")];
    const anchor = appendOlderPage(emptyHistoryState(), live, { turns: live, nextCursor: "more" });
    const heavy = Array.from({ length: 40 }, (_, i): Turn => ({ turnId: `heavy-${i}`, status: "completed", items: [{ itemId: `item-${i}`, role: "assistant", text: "x".repeat(1 << 20) }] }));
    const page = appendOlderPage(anchor, live, { turns: heavy, nextCursor: "" });
    expect(page.older.length).toBeLessThan(40);
    expect(JSON.stringify(page.older).length).toBeLessThanOrEqual(32 << 20);
    expect(page.gap).toBe(true);
  });
});
