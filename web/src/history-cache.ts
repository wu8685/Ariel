import type { Turn } from "./generated/protocol";
import type { HistoryState } from "./history";

export type HistoryCacheIdentity = { deviceId: string; agentEpoch: string; threadId: string };
export type HistoryCacheValue = { history: HistoryState; itemOverrides: Record<string, Turn> };

type CacheEntry = {
  identity: HistoryCacheIdentity;
  value: HistoryCacheValue;
  bytes: number;
  accessedAt: number;
};

type HistoryCacheOptions = {
  maxBytes?: number;
  maxEntries?: number;
  ttlMs?: number;
  now?: () => number;
};

const defaultMaxBytes = 32 << 20;
const defaultMaxEntries = 8;
const defaultTTL = 5 * 60_000;

function cacheKey(identity: HistoryCacheIdentity): string {
  return `${identity.deviceId}\u0000${identity.agentEpoch}\u0000${identity.threadId}`;
}

function validItems(turn: Turn): boolean {
  const seen = new Set<string>();
  for (const item of turn.items) {
    if (!item.itemId || seen.has(item.itemId)) return false;
    seen.add(item.itemId);
  }
  return true;
}

function sanitize(value: HistoryCacheValue): HistoryCacheValue | null {
  const turnIDs = new Set<string>();
  for (const turn of value.history.older) {
    if (!turn.turnId || turn.status !== "completed" || turnIDs.has(turn.turnId) || !validItems(turn)) return null;
    turnIDs.add(turn.turnId);
  }
  const itemOverrides: Record<string, Turn> = {};
  for (const [turnID, turn] of Object.entries(value.itemOverrides)) {
    if (!turnIDs.has(turnID)) continue;
    if (turn.turnId !== turnID || turn.status !== "completed" || !validItems(turn)) return null;
    itemOverrides[turnID] = turn;
  }
  return { history: { ...value.history, older: [...value.history.older] }, itemOverrides };
}

export class BrowserHistoryCache {
  private readonly maxBytes: number;
  private readonly maxEntries: number;
  private readonly ttlMs: number;
  private readonly now: () => number;
  private readonly entries = new Map<string, CacheEntry>();
  private retainedBytes = 0;

  constructor(options: HistoryCacheOptions = {}) {
    this.maxBytes = options.maxBytes ?? defaultMaxBytes;
    this.maxEntries = options.maxEntries ?? defaultMaxEntries;
    this.ttlMs = options.ttlMs ?? defaultTTL;
    this.now = options.now ?? Date.now;
  }

  set(identity: HistoryCacheIdentity, value: HistoryCacheValue): boolean {
    const key = cacheKey(identity);
    this.remove(key);
    this.pruneExpired();
    if (!identity.deviceId || !identity.agentEpoch || !identity.threadId) return false;
    const safe = sanitize(value);
    if (!safe || safe.history.older.length === 0) return false;
    const bytes = new TextEncoder().encode(JSON.stringify({ identity, value: safe })).length;
    if (bytes > this.maxBytes) return false;
    const entry: CacheEntry = { identity: { ...identity }, value: safe, bytes, accessedAt: this.now() };
    this.entries.set(key, entry);
    this.retainedBytes += bytes;
    this.evict();
    return this.entries.has(key);
  }

  get(identity: HistoryCacheIdentity): HistoryCacheValue | null {
    this.pruneExpired();
    const key = cacheKey(identity);
    const entry = this.entries.get(key);
    if (!entry) return null;
    entry.accessedAt = this.now();
    this.entries.delete(key);
    this.entries.set(key, entry);
    return entry.value;
  }

  delete(identity: HistoryCacheIdentity): void {
    this.remove(cacheKey(identity));
  }

  clearDevice(deviceId: string): void {
    for (const [key, entry] of this.entries) {
      if (entry.identity.deviceId === deviceId) this.remove(key);
    }
  }

  clear(): void {
    this.entries.clear();
    this.retainedBytes = 0;
  }

  bytes(): number {
    this.pruneExpired();
    return this.retainedBytes;
  }

  private remove(key: string): void {
    const entry = this.entries.get(key);
    if (!entry) return;
    this.entries.delete(key);
    this.retainedBytes -= entry.bytes;
  }

  private pruneExpired(): void {
    const cutoff = this.now() - this.ttlMs;
    for (const [key, entry] of this.entries) {
      if (entry.accessedAt < cutoff) this.remove(key);
    }
  }

  private evict(): void {
    while (this.entries.size > this.maxEntries || this.retainedBytes > this.maxBytes) {
      const oldest = this.entries.keys().next().value as string | undefined;
      if (!oldest) return;
      this.remove(oldest);
    }
  }
}

export function mergeCachedHistory(value: HistoryCacheValue, liveTurns: Turn[]): HistoryCacheValue {
  const liveIDs = new Set(liveTurns.map(turn => turn.turnId));
  const older = value.history.older.filter(turn => !liveIDs.has(turn.turnId));
  const olderIDs = new Set(older.map(turn => turn.turnId));
  const itemOverrides = Object.fromEntries(Object.entries(value.itemOverrides).filter(([turnID]) => olderIDs.has(turnID)));
  return { history: { ...value.history, older }, itemOverrides };
}
