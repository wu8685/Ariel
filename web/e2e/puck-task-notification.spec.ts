import { expect, test, type Page, type WebSocketRoute } from "@playwright/test";

const token = `s_${"b".repeat(64)}`;
const assistant = { assistantId: "puck-one", name: "小帕", description: "盯着发布", brainDeviceId: "brain-a", status: "ready", permissionMode: "full", unreadCount: 0, lastMessagePreview: "发布还在跑", avatar: JSON.stringify({ shape: "cloud", color: "#54b9a6" }) };
const initialMessages = [
  { id: "user-request", assistantId: "puck-one", role: "user", kind: "text", text: "发布完成后告诉我。", createdAt: "2026-11-11T12:00:00Z" },
  { id: "task-card:task-release", assistantId: "puck-one", role: "assistant", kind: "task", text: "发布", cardJSON: JSON.stringify({ taskId: "task-release", status: "running", deviceId: "exec-a", threadId: "thread-release" }), createdAt: "2026-11-11T12:00:01Z" },
];
const finishedMessages = [
  initialMessages[0],
  { id: "task-card:task-release", assistantId: "puck-one", role: "assistant", kind: "task", text: "发布", cardJSON: JSON.stringify({ taskId: "task-release", status: "review", summary: "发布已经完成。", deviceId: "exec-a", threadId: "thread-release" }), createdAt: "2026-11-11T12:06:00Z" },
  { id: "task-event-release", assistantId: "puck-one", role: "assistant", kind: "text", text: "发布已经完成。", cardJSON: JSON.stringify({ proactive: true, source: "task_event", taskId: "task-release", status: "review" }), createdAt: "2026-11-11T12:06:01Z" },
];

async function openPuck(page: Page) {
  let socket: WebSocketRoute | undefined;
  await page.addInitScript(() => {
    const notes: Array<{ title: string; body: string }> = [];
    const holder = window as unknown as { __puckNotes: typeof notes; __puckPermission: NotificationPermission | "" };
    holder.__puckNotes = notes;
    holder.__puckPermission = "";
    const record = (title: unknown, options?: { body?: string }) => notes.push({ title: String(title), body: options?.body || "" });
    const native = window.Notification;
    const RecordingNotification = function (this: Notification, title: string, options?: NotificationOptions) {
      record(title, options);
      return new native(title, options);
    } as unknown as typeof Notification;
    Object.defineProperty(RecordingNotification, "permission", { configurable: true, get: () => holder.__puckPermission || native.permission });
    RecordingNotification.requestPermission = (callback?: NotificationPermissionCallback) => native.requestPermission(callback);
    window.Notification = RecordingNotification;
    const original = ServiceWorkerRegistration.prototype.showNotification;
    ServiceWorkerRegistration.prototype.showNotification = function (title: string, options?: NotificationOptions) {
      record(title, options);
      return original.call(this, title, options);
    };
  });
  await page.route("**/api/auth/status", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ mode: "passkey", authenticated: true, enrollmentRequired: false, puck: { state: "ready" } }) }));
  await page.route("http://127.0.0.1:4173/", async route => {
    const response = await route.fetch();
    await route.fulfill({ response, body: (await response.text()).replace("__ARIEL_WEB_AUTH__", "passkey") });
  });
  await page.routeWebSocket("**/ws", next => {
    socket = next;
    next.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        next.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "puck-notify", relayEpoch: "puck-notify", sessionToken: token }));
        return;
      }
      if (message.type !== "request") return;
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "brain-a", deviceName: "Mac mini", agentOnline: true, codexReady: true, capabilities: { puckBrain: true, puckBrainReady: true } }] };
      if (message.method === "puck.list") data = { assistants: [assistant] };
      if (message.method === "puck.history") data = { messages: initialMessages };
      if (message.method === "puck.watch") data = { watchId: "watch-puck" };
      next.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "打开 Puck" }).click();
  await expect(page.locator(".puck-transcript").getByText("发布完成后告诉我。")).toBeVisible();
  return {
    finish() {
      socket?.send(JSON.stringify({ type: "event", v: 1, event: "puck.event", assistantId: "puck-one", seq: 1, kind: "history", payload: { messages: finishedMessages } }));
    },
  };
}

test("task completion appears in chat and as a background notification", async ({ page, context }) => {
  await context.grantPermissions(["notifications"]);
  const puck = await openPuck(page);
  await page.getByRole("button", { name: "查看助理信息" }).click();
  await page.getByRole("button", { name: "任务通知" }).click();
  await expect(page.getByRole("button", { name: "任务通知" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "关闭助理信息" }).click();
  await expect(page.locator(".puck-info")).toHaveAttribute("aria-hidden", "true");
  await page.evaluate(() => {
    document.hasFocus = () => false;
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
    (window as unknown as { __puckPermission: string }).__puckPermission = "granted";
  });
  puck.finish();
  await expect(page.locator(".puck-message.assistant.text").filter({ hasText: "发布已经完成。" })).toBeVisible();
  await expect(page.locator(".puck-task-card")).toContainText("这一轮已结束");
  const notice = page.getByRole("status", { name: "任务通知" });
  await expect(notice).toContainText("小帕");
  await expect(notice).toContainText("发布已经完成。");
  await expect.poll(() => page.evaluate(() => (window as unknown as { __puckNotes: Array<{ title: string; body: string }> }).__puckNotes.some(note => note.title === "小帕" && note.body === "发布已经完成。"))).toBe(true);
  await page.screenshot({ path: "/opt/cursor/artifacts/puck-task-notification.png", fullPage: true });
});
