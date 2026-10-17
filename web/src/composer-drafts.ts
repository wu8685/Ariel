import type { FileQuote } from "./file-context";
import type { ScreenshotDraft } from "./screenshots";

export type ComposerDraftIdentity = { deviceId: string; threadId: string };
export type ComposerDraftSnapshot = {
  draft: string;
  screenshots: ScreenshotDraft[];
  fileQuotes: FileQuote[];
  editingQueueId: string;
  revision: number;
};

export function emptyComposerDraft(revision = 0): ComposerDraftSnapshot {
  return { draft: "", screenshots: [], fileQuotes: [], editingQueueId: "", revision };
}

export function composerDraftKey(identity: ComposerDraftIdentity): string {
  return `${identity.deviceId}\u0000${identity.threadId}`;
}

function copy(snapshot: ComposerDraftSnapshot): ComposerDraftSnapshot {
  return { ...snapshot, screenshots: [...snapshot.screenshots], fileQuotes: [...snapshot.fileQuotes] };
}

export class ComposerDraftStore {
  private readonly entries = new Map<string, ComposerDraftSnapshot>();

  constructor(private readonly maxEntries = 100) {}

  get(identity: ComposerDraftIdentity): ComposerDraftSnapshot | null {
    const key = composerDraftKey(identity);
    const stored = this.entries.get(key);
    if (!stored) return null;
    this.entries.delete(key);
    this.entries.set(key, stored);
    return copy(stored);
  }

  set(identity: ComposerDraftIdentity, snapshot: ComposerDraftSnapshot): void {
    const key = composerDraftKey(identity);
    const safe = copy({
      ...snapshot,
      fileQuotes: snapshot.fileQuotes.filter(quote => quote.threadId === identity.threadId),
    });
    this.entries.delete(key);
    this.entries.set(key, safe);
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value as string | undefined;
      if (!oldest) break;
      this.entries.delete(oldest);
    }
  }

  clearIfRevision(identity: ComposerDraftIdentity, revision: number): boolean {
    const current = this.entries.get(composerDraftKey(identity));
    if (!current || current.revision !== revision) return false;
    this.set(identity, emptyComposerDraft(revision + 1));
    return true;
  }

  delete(identity: ComposerDraftIdentity): void {
    this.entries.delete(composerDraftKey(identity));
  }

  clear(): void {
    this.entries.clear();
  }
}
