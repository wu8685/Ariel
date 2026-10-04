import type { Item, Turn } from "./generated/protocol";

export type HistoryState = {
  older: Turn[];
  cursor: string;
  anchorSeen: boolean;
  exhausted: boolean;
  gap: boolean;
};

export type HistoryPage = { turns: Turn[]; nextCursor: string };

const maxOlderTurns = 50;
const maxOlderBytes = 32 << 20;

export function emptyHistoryState(): HistoryState {
  return { older: [], cursor: "", anchorSeen: false, exhausted: false, gap: false };
}

export function appendOlderPage(current: HistoryState, live: Turn[], page: HistoryPage): HistoryState {
  const liveIDs = new Set(live.map(turn => turn.turnId));
  const seen = new Set(current.older.map(turn => turn.turnId));
  const additions: Turn[] = [];
  let anchorSeen = current.anchorSeen;
  for (const turn of page.turns) {
    if (!anchorSeen) {
      if (liveIDs.has(turn.turnId)) anchorSeen = true;
      continue;
    }
    if (liveIDs.has(turn.turnId) || seen.has(turn.turnId)) continue;
    seen.add(turn.turnId);
    additions.push(turn);
  }
  const merged = [...additions.reverse(), ...current.older];
  const kept: Turn[] = [];
  let retainedBytes = 2;
  const encoder = new TextEncoder();
  for (const turn of merged) {
    const turnBytes = encoder.encode(JSON.stringify(turn)).length + (kept.length ? 1 : 0);
    if (kept.length >= maxOlderTurns || retainedBytes + turnBytes > maxOlderBytes) break;
    kept.push(turn);
    retainedBytes += turnBytes;
  }
  return {
    older: kept,
    cursor: page.nextCursor,
    anchorSeen,
    exhausted: !page.nextCursor,
    gap: current.gap || kept.length < merged.length,
  };
}

export function prependOlderItems(turn: Turn, items: Item[], nextItemCursor: string, itemsComplete: boolean): Turn {
  const existing = new Set(turn.items.map(item => item.itemId));
  return {
    ...turn,
    items: [...items.filter(item => !existing.has(item.itemId)), ...turn.items],
    itemsComplete,
    nextItemCursor,
  };
}
