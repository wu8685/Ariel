import type { Thread } from "./generated/protocol";

export type ThreadListCacheIdentity = { deviceId: string; agentEpoch: string; query: string };
export type ThreadListCacheValue = { threads: Thread[]; cursor: string };
export type ThreadListCacheHit = { value: ThreadListCacheValue; fresh: boolean };

type Entry = {
  identity: ThreadListCacheIdentity;
  value: ThreadListCacheValue;
  bytes: number;
  storedAt: number;
  accessedAt: number;
};

type Options = {
  maxBytes?: number;
  maxEntries?: number;
  freshMs?: number;
  retainMs?: number;
  now?: () => number;
};

const defaultMaxBytes = 4 << 20;
const defaultMaxEntries = 16;
const defaultFreshMs = 60_000;
const defaultRetainMs = 5 * 60_000;

function normalized(identity: ThreadListCacheIdentity): ThreadListCacheIdentity {
  return { ...identity, query: identity.query.trim().toLocaleLowerCase() };
}

function keyFor(identity: ThreadListCacheIdentity): string {
  const value = normalized(identity);
  return `${value.deviceId}\u0000${value.agentEpoch}\u0000${value.query}`;
}

function cloneValue(value: ThreadListCacheValue): ThreadListCacheValue {
  return { threads: value.threads.map(thread => ({ ...thread })), cursor: value.cursor };
}

export class BrowserThreadListCache {
  private readonly maxBytes: number;
  private readonly maxEntries: number;
  private readonly freshMs: number;
  private readonly retainMs: number;
  private readonly now: () => number;
  private readonly entries = new Map<string, Entry>();
  private retainedBytes = 0;

  constructor(options: Options = {}) {
    this.maxBytes = options.maxBytes ?? defaultMaxBytes;
    this.maxEntries = options.maxEntries ?? defaultMaxEntries;
    this.freshMs = options.freshMs ?? defaultFreshMs;
    this.retainMs = options.retainMs ?? defaultRetainMs;
    this.now = options.now ?? Date.now;
  }

  set(identity: ThreadListCacheIdentity, value: ThreadListCacheValue): boolean {
    const safeIdentity = normalized(identity);
    const key = keyFor(safeIdentity);
    this.remove(key);
    this.pruneExpired();
    if (!safeIdentity.deviceId || !safeIdentity.agentEpoch || !Array.isArray(value.threads)) return false;
    const safeValue = cloneValue(value);
    const bytes = new TextEncoder().encode(JSON.stringify({ identity: safeIdentity, value: safeValue })).length;
    if (bytes > this.maxBytes) return false;
    const now = this.now();
    this.entries.set(key, { identity: safeIdentity, value: safeValue, bytes, storedAt: now, accessedAt: now });
    this.retainedBytes += bytes;
    this.evict();
    return this.entries.has(key);
  }

  get(identity: ThreadListCacheIdentity): ThreadListCacheHit | null {
    this.pruneExpired();
    const key = keyFor(identity);
    const entry = this.entries.get(key);
    if (!entry) return null;
    const now = this.now();
    entry.accessedAt = now;
    this.entries.delete(key);
    this.entries.set(key, entry);
    return { value: cloneValue(entry.value), fresh: now-entry.storedAt <= this.freshMs };
  }

  clearDevice(deviceId: string): void {
    for (const [key, entry] of this.entries) if (entry.identity.deviceId === deviceId) this.remove(key);
  }

  clear(): void {
    this.entries.clear();
    this.retainedBytes = 0;
  }

  private remove(key: string): void {
    const entry = this.entries.get(key);
    if (!entry) return;
    this.entries.delete(key);
    this.retainedBytes -= entry.bytes;
  }

  private pruneExpired(): void {
    const cutoff = this.now()-this.retainMs;
    for (const [key, entry] of this.entries) if (entry.accessedAt < cutoff) this.remove(key);
  }

  private evict(): void {
    while (this.entries.size > this.maxEntries || this.retainedBytes > this.maxBytes) {
      const oldest = this.entries.keys().next().value as string | undefined;
      if (!oldest) return;
      this.remove(oldest);
    }
  }
}
