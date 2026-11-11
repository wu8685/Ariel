import { mkdirSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";

const token = `s_${"b".repeat(64)}`;
const shots = process.env.PUCK_CONTEXT_SHOTS;

test.use({ viewport: { width: 1280, height: 800 }, isMobile: false, hasTouch: false, deviceScaleFactor: 1 });

test("create form and config panel edit an isolated bot's custom context", async ({ page }) => {
  let assistants = [
    { assistantId: "puck-one", name: "小帕", description: "负责日常开发协助", customContext: "先给结论", brainDeviceId: "brain-a", status: "ready", permissionMode: "full", unreadCount: 0, lastMessagePreview: "测试已经开始" },
  ];
  const creates: Record<string, unknown>[] = [];
  const updates: Record<string, unknown>[] = [];
  await page.route("**/api/auth/status", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ mode: "passkey", authenticated: true, enrollmentRequired: false, puck: { state: "ready" } }) }));
  await page.route("http://127.0.0.1:4173/", async route => {
    const response = await route.fetch();
    await route.fulfill({ response, body: (await response.text()).replace("__ARIEL_WEB_AUTH__", "passkey") });
  });
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "puck-context", relayEpoch: "puck-context", sessionToken: token }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "brain-a", deviceName: "Mac mini", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, puckBrain: true, puckBrainReady: true } }] };
    if (message.method === "thread.list") data = { threads: [], nextCursor: "" };
    if (message.method === "puck.list") data = { assistants };
    if (message.method === "puck.history") data = { messages: [{ id: "hello", role: "assistant", kind: "text", text: "隔离 fixture 问候。", createdAt: "2026-11-11T02:00:00Z" }] };
    if (message.method === "puck.create") {
      creates.push(message.params);
      const assistant = { assistantId: "puck-new", name: message.params.name, customContext: message.params.customContext || "", description: "", brainDeviceId: "brain-a", status: "ready", unreadCount: 0, lastMessagePreview: "" };
      assistants = [...assistants, assistant];
      data = { assistant };
    }
    if (message.method === "puck.update") {
      updates.push(message.params);
      assistants = assistants.map(item => item.assistantId === message.params.assistantId ? { ...item, ...message.params } : item);
      data = { assistant: assistants.find(item => item.assistantId === message.params.assistantId) };
    }
    if (message.method === "puck.read") data = { unreadCount: 0 };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "打开 Puck" }).click();
  await expect(page.locator(".puck-transcript").getByText("隔离 fixture 问候。")).toBeVisible();
  await page.getByRole("button", { name: "新建" }).click();
  await page.getByRole("menuitem", { name: "新建助理" }).click();
  const dialog = page.locator(".puck-dialog");
  await expect(dialog.getByRole("heading", { name: "新建助理" })).toBeVisible();
  await dialog.getByLabel("助理名称").fill("家里的管家");
  await dialog.getByLabel("大脑主机").selectOption({ label: "Mac mini" });
  await dialog.getByRole("textbox", { name: "自定义上下文" }).fill("晚上十点后保持安静");
  await shot(page, "puck-create-custom-context");
  await dialog.getByRole("button", { name: "创建" }).click();
  await expect.poll(() => creates[0]?.customContext).toBe("晚上十点后保持安静");
  await expect.poll(() => creates[0]?.name).toBe("家里的管家");

  await page.locator(".puck-assistant-row").filter({ hasText: "小帕" }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "编辑" }).click();
  await expect(page.locator(".puck-workspace")).toHaveClass(/info-open/);
  const panel = page.locator(".puck-info");
  await expect(panel.getByText("先给结论")).toBeVisible();
  await panel.getByRole("button", { name: "编辑自定义上下文" }).click();
  await panel.getByRole("textbox", { name: "自定义上下文" }).fill("只在明确结果时告诉我");
  await shot(page, "puck-edit-custom-context");
  await panel.getByRole("button", { name: "保存自定义上下文" }).click();
  await expect(panel.getByText("只在明确结果时告诉我")).toBeVisible();
  await expect.poll(() => updates.at(-1)?.customContext).toBe("只在明确结果时告诉我");
  await expect.poll(() => updates.at(-1)?.assistantId).toBe("puck-one");
});

async function shot(page: Page, name: string) {
  if (!shots) return;
  mkdirSync(shots, { recursive: true });
  await page.screenshot({ path: `${shots}/${name}.png`, fullPage: false });
}
