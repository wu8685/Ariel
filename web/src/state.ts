import type { Thread, ThreadSnapshot, ThreadUpdate } from "./generated/protocol";

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
  return { ...current, seq: event.seq, thread: event.thread };
}

export function preserveDraftAfterSend(draft: string, outcome: "accepted" | "rejected" | "unknown" | "not_submitted"): string {
  return outcome === "accepted" ? "" : draft;
}

export function keepOfflineDevice<T extends { deviceId: string; agentOnline: boolean; codexReady: boolean }>(online: T[], previous: T[], selected: string): T[] {
  if (!selected || online.some(device => device.deviceId === selected)) return online;
  const remembered = previous.find(device => device.deviceId === selected);
  return remembered ? [...online, { ...remembered, agentOnline: false, codexReady: false }] : online;
}

export function recoveryTarget(deviceId: string, threadId: string, online: { deviceId: string }[]): { deviceId: string; threadId: string } | null {
  return deviceId && threadId && online.some(device => device.deviceId === deviceId) ? { deviceId, threadId } : null;
}
