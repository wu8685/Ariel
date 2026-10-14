import { historyCacheKey, sanitizeHistoryCacheValue, type HistoryCacheIdentity, type HistoryCacheValue } from "./history-cache";

type DurableRecord = {
  key: string;
  identity: HistoryCacheIdentity;
  value: HistoryCacheValue;
  bytes: number;
  accessedAt: number;
};

type SelectionRecord = { identity: HistoryCacheIdentity; accessedAt: number };

export interface DurableHistoryBackend {
  get(key: string): Promise<unknown>;
  all(): Promise<unknown[]>;
  put(record: DurableRecord): Promise<void>;
  delete(key: string): Promise<void>;
  clear(): Promise<void>;
  getSelection(): Promise<unknown>;
  putSelection(record: SelectionRecord): Promise<void>;
  clearSelection(): Promise<void>;
}

type DurableHistoryCacheOptions = {
  backend?: DurableHistoryBackend;
  maxEntries?: number;
  maxEntryBytes?: number;
  maxBytes?: number;
  ttlMs?: number;
  now?: () => number;
};

const defaultMaxEntries = 20;
const defaultMaxEntryBytes = 2 << 20;
const defaultMaxBytes = 25 << 20;
const defaultTTL = 7 * 24 * 60 * 60_000;

function validIdentity(value: unknown): value is HistoryCacheIdentity {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<HistoryCacheIdentity>;
  return typeof candidate.deviceId === "string" && candidate.deviceId.length > 0
    && typeof candidate.agentEpoch === "string" && candidate.agentEpoch.length > 0
    && typeof candidate.threadId === "string" && candidate.threadId.length > 0;
}

function validRecord(value: unknown): DurableRecord | null {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Partial<DurableRecord>;
  if (typeof candidate.key !== "string" || !validIdentity(candidate.identity) || historyCacheKey(candidate.identity) !== candidate.key
    || typeof candidate.bytes !== "number" || !Number.isSafeInteger(candidate.bytes) || candidate.bytes <= 0
    || typeof candidate.accessedAt !== "number" || !Number.isFinite(candidate.accessedAt) || !candidate.value) return null;
  const safe = durableDisplayValue(candidate.value);
  return safe ? { key: candidate.key, identity: { ...candidate.identity }, value: safe, bytes: candidate.bytes, accessedAt: candidate.accessedAt } : null;
}

function durableDisplayValue(value: HistoryCacheValue): HistoryCacheValue | null {
  const safe = sanitizeHistoryCacheValue(value);
  if (!safe?.thread) return safe;
  const { permissions: _permissions, queuedMessages: _queuedMessages, ...display } = safe.thread;
  return { ...safe, thread: { ...display, runtime: "unknown", pendingInteractions: [] } };
}

function encodedBytes(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).length;
}

export class DurableHistoryCache {
  private readonly backend: DurableHistoryBackend;
  private readonly maxEntries: number;
  private readonly maxEntryBytes: number;
  private readonly maxBytes: number;
  private readonly ttlMs: number;
  private readonly now: () => number;
  private tail: Promise<void> = Promise.resolve();

  constructor(options: DurableHistoryCacheOptions = {}) {
    this.backend = options.backend ?? new IndexedDBDurableHistoryBackend();
    this.maxEntries = options.maxEntries ?? defaultMaxEntries;
    this.maxEntryBytes = options.maxEntryBytes ?? defaultMaxEntryBytes;
    this.maxBytes = options.maxBytes ?? defaultMaxBytes;
    this.ttlMs = options.ttlMs ?? defaultTTL;
    this.now = options.now ?? Date.now;
  }

  set(identity: HistoryCacheIdentity, value: HistoryCacheValue): Promise<boolean> {
    return this.serial(async () => {
      if (!validIdentity(identity)) return false;
      const safe = durableDisplayValue(value);
      if (!safe || (safe.history.older.length === 0 && !safe.thread) || (safe.thread && safe.thread.threadId !== identity.threadId)) return false;
      const key = historyCacheKey(identity);
      const accessedAt = this.now();
      const draft = { key, identity: { ...identity }, value: safe, accessedAt };
      const bytes = encodedBytes(draft);
      if (bytes > this.maxEntryBytes || bytes > this.maxBytes) return false;
      await this.backend.put({ ...draft, bytes });
      await this.prune(accessedAt);
      return true;
    }, false);
  }

  get(identity: HistoryCacheIdentity): Promise<HistoryCacheValue | null> {
    return this.serial(async () => {
      if (!validIdentity(identity)) return null;
      const key = historyCacheKey(identity);
      const raw = await this.backend.get(key);
      const record = validRecord(raw);
      const now = this.now();
      if (!record || record.accessedAt < now - this.ttlMs) {
        if (raw !== undefined) await this.backend.delete(key);
        return null;
      }
      record.accessedAt = now;
      await this.backend.put(record);
      return record.value;
    }, null);
  }

  rememberSelection(identity: HistoryCacheIdentity): Promise<void> {
    return this.serial(async () => {
      if (validIdentity(identity)) await this.backend.putSelection({ identity: { ...identity }, accessedAt: this.now() });
    }, undefined);
  }

  lastSelection(): Promise<HistoryCacheIdentity | null> {
    return this.serial(async () => {
      const raw = await this.backend.getSelection();
      if (!raw || typeof raw !== "object") return null;
      const record = raw as Partial<SelectionRecord>;
      if (!validIdentity(record.identity) || typeof record.accessedAt !== "number" || record.accessedAt < this.now() - this.ttlMs) {
        await this.backend.clearSelection();
        return null;
      }
      return { ...record.identity };
    }, null);
  }

  clearDevice(deviceId: string): Promise<void> {
    return this.serial(async () => {
      for (const raw of await this.backend.all()) {
        const record = validRecord(raw);
        if (!record || record.identity.deviceId === deviceId) await this.backend.delete(record?.key || String((raw as { key?: unknown })?.key || ""));
      }
      const selected = await this.backend.getSelection() as Partial<SelectionRecord> | undefined;
      if (selected?.identity?.deviceId === deviceId) await this.backend.clearSelection();
    }, undefined);
  }

  delete(identity: HistoryCacheIdentity): Promise<void> {
    return this.serial(async () => { if (validIdentity(identity)) await this.backend.delete(historyCacheKey(identity)); }, undefined);
  }

  clear(): Promise<void> {
    return this.serial(async () => { await this.backend.clear(); await this.backend.clearSelection(); }, undefined);
  }

  private async prune(now: number): Promise<void> {
    const valid: DurableRecord[] = [];
    for (const raw of await this.backend.all()) {
      const record = validRecord(raw);
      if (!record || record.accessedAt < now - this.ttlMs || record.bytes > this.maxEntryBytes) {
        const key = record?.key || String((raw as { key?: unknown })?.key || "");
        if (key) await this.backend.delete(key);
      } else valid.push(record);
    }
    valid.sort((a, b) => b.accessedAt - a.accessedAt);
    let retained = 0;
    for (let index = 0; index < valid.length; index++) {
      retained += valid[index].bytes;
      if (index >= this.maxEntries || retained > this.maxBytes) await this.backend.delete(valid[index].key);
    }
  }

  private serial<T>(operation: () => Promise<T>, fallback: T): Promise<T> {
    const result = this.tail.then(operation, operation).catch(() => fallback);
    this.tail = result.then(() => undefined, () => undefined);
    return result;
  }
}

const databaseName = "ariel-display-cache-v1";
const entryStore = "conversations";
const metadataStore = "metadata";
const selectionKey = "last-selection";

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error("IndexedDB request failed"));
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error || new Error("IndexedDB transaction failed"));
    transaction.onabort = () => reject(transaction.error || new Error("IndexedDB transaction aborted"));
  });
}

export class IndexedDBDurableHistoryBackend implements DurableHistoryBackend {
  private database: Promise<IDBDatabase> | null = null;

  private open(): Promise<IDBDatabase> {
    if (this.database) return this.database;
    this.database = new Promise((resolve, reject) => {
      if (!globalThis.indexedDB) { reject(new Error("IndexedDB unavailable")); return; }
      const request = indexedDB.open(databaseName, 1);
      request.onupgradeneeded = () => {
        if (!request.result.objectStoreNames.contains(entryStore)) request.result.createObjectStore(entryStore, { keyPath: "key" });
        if (!request.result.objectStoreNames.contains(metadataStore)) request.result.createObjectStore(metadataStore);
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error || new Error("IndexedDB open failed"));
    });
    return this.database;
  }

  async get(key: string): Promise<unknown> { const db = await this.open(); return requestResult(db.transaction(entryStore).objectStore(entryStore).get(key)); }
  async all(): Promise<unknown[]> { const db = await this.open(); return requestResult(db.transaction(entryStore).objectStore(entryStore).getAll()); }
  async put(record: DurableRecord): Promise<void> { const db = await this.open(); const tx = db.transaction(entryStore, "readwrite"); tx.objectStore(entryStore).put(record); await transactionDone(tx); }
  async delete(key: string): Promise<void> { if (!key) return; const db = await this.open(); const tx = db.transaction(entryStore, "readwrite"); tx.objectStore(entryStore).delete(key); await transactionDone(tx); }
  async clear(): Promise<void> { const db = await this.open(); const tx = db.transaction(entryStore, "readwrite"); tx.objectStore(entryStore).clear(); await transactionDone(tx); }
  async getSelection(): Promise<unknown> { const db = await this.open(); return requestResult(db.transaction(metadataStore).objectStore(metadataStore).get(selectionKey)); }
  async putSelection(record: SelectionRecord): Promise<void> { const db = await this.open(); const tx = db.transaction(metadataStore, "readwrite"); tx.objectStore(metadataStore).put(record, selectionKey); await transactionDone(tx); }
  async clearSelection(): Promise<void> { const db = await this.open(); const tx = db.transaction(metadataStore, "readwrite"); tx.objectStore(metadataStore).delete(selectionKey); await transactionDone(tx); }
}

export class MemoryDurableHistoryBackend implements DurableHistoryBackend {
  private records = new Map<string, unknown>();
  private selection: unknown;
  failWrites = false;
  async get(key: string): Promise<unknown> { return this.records.get(key); }
  async all(): Promise<unknown[]> { return [...this.records.values()]; }
  async put(record: DurableRecord): Promise<void> { if (this.failWrites) throw new Error("quota"); this.records.set(record.key, structuredClone(record)); }
  async delete(key: string): Promise<void> { this.records.delete(key); }
  async clear(): Promise<void> { this.records.clear(); }
  async getSelection(): Promise<unknown> { return this.selection; }
  async putSelection(record: SelectionRecord): Promise<void> { if (this.failWrites) throw new Error("quota"); this.selection = structuredClone(record); }
  async clearSelection(): Promise<void> { this.selection = undefined; }
  async injectCorrupt(threadId: string): Promise<void> { this.records.set(historyCacheKey({ deviceId: "mac", agentEpoch: "epoch-a", threadId }), { key: historyCacheKey({ deviceId: "mac", agentEpoch: "epoch-a", threadId }), broken: true }); }
}
