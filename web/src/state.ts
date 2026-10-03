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

export function recoveryTarget(deviceId: string, threadId: string, online: { deviceId: string }[], blockedSelection = ""): { deviceId: string; threadId: string } | null {
  return deviceId && threadId && blockedSelection !== `${deviceId}\u0000${threadId}` && online.some(device => device.deviceId === deviceId) ? { deviceId, threadId } : null;
}

export function permissionSummary(permissions?: { sandbox: string; approval: string }): { label: string; warning?: string } {
  if (!permissions || permissions.sandbox === "unknown") return { label: "当前 Desktop 权限未知" };
  const sandbox = ({ read_only: "Read Only", workspace_write: "Workspace Write", full_access: "Full Access" } as Record<string, string>)[permissions.sandbox];
  if (!sandbox) return { label: "当前 Desktop 权限未知" };
  const approval = ({ on_request: "on-request", never: "never" } as Record<string, string>)[permissions.approval] || "未知";
  const label = `当前 Desktop：${sandbox} · 审批策略 ${approval}`;
  return permissions.sandbox === "full_access" ? { label, warning: "可访问本机更多文件和网络；on-request 不代表受 sandbox 限制。" } : { label };
}
