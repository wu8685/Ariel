// Bounded stream lineage per subscription. WebSocket ordering is reliable, but
// delayed async snapshot reads can complete after a replacement stream starts.
export class StreamFence {
  private readonly entries = new Map<string, { stream: string; seq: number; retired: Set<string> }>();

  // Status carries no new authoritative content. Check it without advancing
  // lineage, so a later snapshot at that seq can still recover the display.
  acceptStatus(key: string, stream: string | undefined, seq: number | undefined): boolean {
    const current = this.entries.get(key);
    if (!current || !stream) return true; // Cold promotion failure or cache removal.
    return !current.retired.has(stream) && (current.stream !== stream || seq === undefined || seq >= current.seq);
  }

  accept(key: string, stream: string, seq: number, allowCurrent = false): boolean {
    const current = this.entries.get(key);
    if (!stream || stream === "cache-pending" || !Number.isSafeInteger(seq) || seq < 1) return false;
    if (current && (current.retired.has(stream) || current.stream === stream && (seq < current.seq || seq === current.seq && !allowCurrent))) return false;
    const retired = current?.retired || new Set<string>();
    if (current && current.stream !== stream) retired.add(current.stream);
    while (retired.size > 16) retired.delete(retired.values().next().value!);
    this.entries.delete(key);
    this.entries.set(key, { stream, seq, retired });
    while (this.entries.size > 128) this.entries.delete(this.entries.keys().next().value!);
    return true;
  }
}
