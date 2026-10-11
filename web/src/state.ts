import type { Item, Thread, ThreadPatch, ThreadSnapshot, ThreadUpdate, Turn, TurnPatch } from "./generated/protocol";

export type ThreadView = {
  deviceId: string;
  threadId: string;
  subscriptionId: string;
  streamId: string;
  seq: number;
  thread: Thread;
};

export function belongsToSubscription<T extends { deviceId: string; threadId: string; subscriptionId: string }>(event: T, deviceId: string, threadId: string, subscriptionId: string): boolean {
  return Boolean(subscriptionId) && event.deviceId === deviceId && event.threadId === threadId && event.subscriptionId === subscriptionId;
}

export function applyThreadEvent(current: ThreadView | null, event: ThreadSnapshot | ThreadUpdate): ThreadView | null {
  if (event.event === "thread.snapshot") {
    if (event.seq !== 1) return null;
    return { deviceId: event.deviceId, threadId: event.threadId, subscriptionId: event.subscriptionId, streamId: event.streamId, seq: 1, thread: event.thread };
  }
  if (!current || current.deviceId !== event.deviceId || current.threadId !== event.threadId || current.subscriptionId !== event.subscriptionId || current.streamId !== event.streamId || event.baseSeq !== current.seq || event.seq !== current.seq + 1) return null;
  const full = event.thread;
  const patch = event.patch;
  if (full && !patch) return { ...current, seq: event.seq, thread: full };
  if (!patch || full) return null;
  const thread = applyThreadPatch(current.thread, patch);
  return thread ? { ...current, seq: event.seq, thread } : null;
}

function unique(values: readonly string[]): boolean {
  return new Set(values).size === values.length && values.every(Boolean);
}

function applyThreadPatch(current: Thread, patch: ThreadPatch): Thread | null {
  if (patch.threadState.threadId !== current.threadId || !unique(patch.turnOrder) || !unique(patch.removedTurnIds)) return null;
  const order = new Set(patch.turnOrder);
  const removed = new Set(patch.removedTurnIds);
  if (patch.removedTurnIds.some(id => order.has(id))) return null;

  const existing = new Map(current.turns.map(turn => [turn.turnId, turn]));
  const expectedRemoved = current.turns.filter(turn => !order.has(turn.turnId)).map(turn => turn.turnId);
  if (expectedRemoved.length !== removed.size || expectedRemoved.some(id => !removed.has(id))) return null;

  const patches = new Map<string, TurnPatch>();
  for (const turnPatch of patch.turnPatches) {
    if (!turnPatch.turnId || patches.has(turnPatch.turnId) || !order.has(turnPatch.turnId)) return null;
    patches.set(turnPatch.turnId, turnPatch);
  }

  const turns: Turn[] = [];
  for (const turnID of patch.turnOrder) {
    const before = existing.get(turnID);
    const turnPatch = patches.get(turnID);
    if (!turnPatch) {
      if (!before) return null;
      turns.push(before);
      continue;
    }
    const next = applyTurnPatch(before, turnPatch);
    if (!next) return null;
    turns.push(next);
  }
  return { ...patch.threadState, turns };
}

function applyTurnPatch(current: Turn | undefined, patch: TurnPatch): Turn | null {
  if (!unique(patch.itemOrder) || !unique(patch.removedItemIds)) return null;
  const order = new Set(patch.itemOrder);
  const removed = new Set(patch.removedItemIds);
  if (patch.removedItemIds.some(id => order.has(id))) return null;

  const items = new Map<string, Item>((current?.items || []).map(item => [item.itemId, item]));
  const expectedRemoved = (current?.items || []).filter(item => !order.has(item.itemId)).map(item => item.itemId);
  if (expectedRemoved.length !== removed.size || expectedRemoved.some(id => !removed.has(id))) return null;
  const upserted = new Set<string>();
  for (const item of patch.upsertItems) {
    if (!item.itemId || upserted.has(item.itemId) || !order.has(item.itemId)) return null;
    upserted.add(item.itemId);
    items.set(item.itemId, item);
  }
  const nextItems: Item[] = [];
  for (const itemID of patch.itemOrder) {
    const item = items.get(itemID);
    if (!item) return null;
    nextItems.push(item);
  }
  const next: Turn = { turnId: patch.turnId, status: patch.status, items: nextItems };
  if (patch.itemsComplete !== undefined) next.itemsComplete = patch.itemsComplete;
  if (patch.nextItemCursor !== undefined) next.nextItemCursor = patch.nextItemCursor;
  return next;
}

export function preserveDraftAfterSend(draft: string, outcome: "accepted" | "rejected" | "unknown" | "not_submitted"): string {
  return outcome === "accepted" ? "" : draft;
}

export function keepOfflineDevice<T extends { deviceId: string; agentOnline: boolean; codexReady: boolean }>(online: T[], previous: T[], selected: string): T[] {
  if (!selected || online.some(device => device.deviceId === selected)) return online;
  const remembered = previous.find(device => device.deviceId === selected);
  return remembered ? [...online, { ...remembered, agentOnline: false, codexReady: false }] : online;
}

export function recoveryTarget(deviceId: string, threadId: string, online: { deviceId: string }[], blockedSelection = ""): { deviceId: string; threadId: string } | null {
  return deviceId && threadId && blockedSelection !== `${deviceId}\u0000${threadId}` && online.some(device => device.deviceId === deviceId) ? { deviceId, threadId } : null;
}

export function permissionSummary(permissions?: { sandbox: string; approval: string }): { label: string; warning?: string } {
  if (!permissions || permissions.sandbox === "unknown") return { label: "当前 Desktop 权限未知" };
  const sandbox = ({ read_only: "Read Only", workspace_write: "Workspace Write", full_access: "Full Access" } as Record<string, string>)[permissions.sandbox];
  if (!sandbox) return { label: "当前 Desktop 权限未知" };
  const approval = ({ on_request: "on-request", never: "never" } as Record<string, string>)[permissions.approval] || "未知";
  const label = `当前 Desktop：${sandbox} · 审批策略 ${approval}`;
  return permissions.sandbox === "full_access" ? { label, warning: "Full Access 允许访问本机其他文件和网络；请确认这是当前 Desktop 允许的范围。" } : { label };
}

// Current Desktop permissions are visible context, not an additional send
// gate. The original owner still decides whether a turn or tool may run.
export function canSend(thread: { runtime: string; pendingInteractions: readonly unknown[] } | null, ready: boolean, working: boolean, draft: string, imageCount = 0, queueEnabled = false): boolean {
  if (!thread || !ready || working || (!draft.trim() && imageCount === 0)) return false;
  if (thread.runtime === "inProgress") return queueEnabled;
  return thread.runtime === "idle" && thread.pendingInteractions.length === 0;
}
