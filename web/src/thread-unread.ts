import type { Thread } from "./generated/protocol";

type Entry = { observed: number; seen: number; touched: number };
type PersistedState = { version: 1; entries: Record<string, Entry> };
type Options = { maxEntries?: number; maxBytes?: number; now?: () => number };

const storageKey = "ariel.thread-unread.v1";
const defaultMaxEntries = 1024;
const defaultMaxBytes = 256 << 10;

function entryKey(deviceId: string, threadId: string): string {
  return `${deviceId}\u0000${threadId}`;
}

function versionOf(updatedAt: string): number | null {
  const value = Date.parse(updatedAt);
  return Number.isFinite(value) ? value : null;
}

function validEntry(value: unknown): value is Entry {
  if (!value || typeof value !== "object") return false;
  const entry = value as Partial<Entry>;
  return Number.isFinite(entry.observed) && Number.isFinite(entry.seen) && Number.isFinite(entry.touched);
}

export class BrowserThreadUnreadStore {
  private readonly maxEntries: number;
  private readonly maxBytes: number;
  private readonly now: () => number;
  private disabled = false;
  private state: PersistedState;

  constructor(private readonly storage: Storage, options: Options = {}) {
    this.maxEntries = options.maxEntries ?? defaultMaxEntries;
    this.maxBytes = options.maxBytes ?? defaultMaxBytes;
    this.now = options.now ?? Date.now;
    this.state = this.load();
  }

  observe(deviceId: string, threads: Thread[]): Set<string> {
    if (this.disabled || !deviceId) return new Set();
    const unread = new Set<string>();
    for (const thread of threads) {
      const version = versionOf(thread.updatedAt);
      if (!thread.threadId || version === null) continue;
      const key = entryKey(deviceId, thread.threadId);
      const current = this.state.entries[key];
      if (!current) {
        this.state.entries[key] = { observed: version, seen: version, touched: this.now() };
        continue;
      }
      current.observed = Math.max(current.observed, version);
      current.touched = this.now();
      if (current.observed > current.seen) unread.add(thread.threadId);
    }
    if (!this.persist()) return new Set();
    return unread;
  }

  markRead(deviceId: string, threadId: string, updatedAt: string): boolean {
    if (this.disabled || !deviceId || !threadId) return false;
    const version = versionOf(updatedAt);
    if (version === null) return false;
    const key = entryKey(deviceId, threadId);
    const current = this.state.entries[key];
    if (!current) this.state.entries[key] = { observed: version, seen: version, touched: this.now() };
    else {
      current.observed = Math.max(current.observed, version);
      current.seen = Math.max(current.seen, version);
      current.touched = this.now();
    }
    return this.persist();
  }

  private load(): PersistedState {
    try {
      const raw = this.storage.getItem(storageKey);
      if (!raw) return { version: 1, entries: {} };
      const parsed = JSON.parse(raw) as Partial<PersistedState>;
      if (parsed.version !== 1 || !parsed.entries || typeof parsed.entries !== "object") return { version: 1, entries: {} };
      const entries: Record<string, Entry> = {};
      for (const [key, value] of Object.entries(parsed.entries)) if (validEntry(value)) entries[key] = value;
      return { version: 1, entries };
    } catch {
      return { version: 1, entries: {} };
    }
  }

  private persist(): boolean {
    try {
      this.pruneToLimits();
      this.storage.setItem(storageKey, JSON.stringify(this.state));
      return true;
    } catch {
      this.disabled = true;
      this.state = { version: 1, entries: {} };
      return false;
    }
  }

  private pruneToLimits(): void {
    const oldestFirst = () => Object.entries(this.state.entries).sort((a, b) => a[1].touched - b[1].touched);
    let entries = oldestFirst();
    while (entries.length > this.maxEntries) {
      delete this.state.entries[entries[0][0]];
      entries.shift();
    }
    while (new TextEncoder().encode(JSON.stringify(this.state)).length > this.maxBytes && entries.length) {
      delete this.state.entries[entries[0][0]];
      entries.shift();
    }
  }
}
