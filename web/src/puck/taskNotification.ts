export const TASK_NOTIFICATION_KEY = "ariel.puck.taskNotifications";

export type NotificationPermissionState = NotificationPermission | "unsupported";

type NoticeStorage = Pick<Storage, "getItem" | "setItem">;

export function isProactiveTaskMessage(message: { kind?: string; cardJSON?: string }): boolean {
  if (!message.cardJSON || message.kind === "processing" || message.kind === "greeting_processing" || message.kind === "acknowledged") return false;
  try {
    const card = JSON.parse(message.cardJSON) as { proactive?: unknown; source?: unknown };
    return card.proactive === true && card.source === "task_event";
  } catch {
    return false;
  }
}

export function shouldSurfaceNotification(input: { proactive: boolean; focused: boolean; permission: NotificationPermissionState; enabled: boolean }): boolean {
  return input.proactive && !input.focused && input.enabled && input.permission === "granted";
}

export function taskNotificationsEnabled(storage: Pick<Storage, "getItem"> = localStorage): boolean {
  try {
    return storage.getItem(TASK_NOTIFICATION_KEY) === "granted";
  } catch {
    return false;
  }
}

export function notificationPermission(): NotificationPermissionState {
  if (typeof Notification === "undefined") return "unsupported";
  return Notification.permission;
}

export function pageFocused(): boolean {
  return document.visibilityState === "visible" && document.hasFocus();
}

export function registerTaskWorker(): void {
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) return;
  void navigator.serviceWorker.register("/sw.js");
}

export async function enableTaskNotifications(storage: NoticeStorage = localStorage): Promise<NotificationPermissionState> {
  if (typeof Notification === "undefined") return "unsupported";
  const permission = await Notification.requestPermission();
  try {
    if (permission === "granted" || permission === "denied") storage.setItem(TASK_NOTIFICATION_KEY, permission);
  } catch { /* private mode */ }
  if (permission === "granted") registerTaskWorker();
  return permission;
}

export async function showTaskNotification(title: string, body: string, tag: string): Promise<void> {
  const safeTitle = Array.from(title.trim() || "Puck").slice(0, 40).join("");
  const safeBody = Array.from(body.trim()).slice(0, 180).join("");
  const options: NotificationOptions = { body: safeBody, tag, data: { href: "/" } };
  if (typeof navigator !== "undefined" && "serviceWorker" in navigator) {
    try {
      const registration = await navigator.serviceWorker.getRegistration();
      if (registration) {
        await registration.showNotification(safeTitle, options);
        return;
      }
    } catch { /* the page Notification below is the fallback */ }
  }
  try {
    if (typeof Notification !== "undefined") new Notification(safeTitle, options);
  } catch { /* the browser refused or does not support notifications */ }
}
