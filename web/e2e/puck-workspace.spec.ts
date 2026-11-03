import { expect, test, type Page } from "@playwright/test";

const token = `s_${"b".repeat(64)}`;
const initialAssistants = [{ assistantId: "puck-one", name: "小帕", description: "负责日常开发协助", brainDeviceId: "brain-a", status: "ready", avatar: JSON.stringify({ shape: "cloud", color: "#ff2d8d" }) }];
let assistants = initialAssistants;
let avatarUpdates: Record<string, unknown>[] = [];
const initialMessages = [
	{ id: "user-request", role: "user", kind: "text", text: "帮我检查 Ariel 的测试状态。", createdAt: "2026-11-03T11:59:59Z" },
  { id: "hello", role: "assistant", kind: "text", text: "晚上好。测试已经开始，我会在有结果时告诉你。", createdAt: "2026-11-03T12:00:00Z" },
  { id: "question", role: "assistant", kind: "question", text: "先运行哪组测试？", cardJSON: JSON.stringify({ questionId: "question-a", options: ["Go", "Web"], cardState: "pending" }), createdAt: "2026-11-03T12:00:01Z" },
  { id: "authorization", role: "assistant", kind: "authorization", text: "这次操作需要你的确认。", cardJSON: JSON.stringify({ authorizationId: "authorization-a", action: "start_task", deviceId: "brain-a", cwd: "/work/ariel", expiresAt: "2026-11-03T12:30:00Z", risk: "目标主机无法证明审批策略", cardState: "pending" }), createdAt: "2026-11-03T12:00:02Z" },
  { id: "task", role: "assistant", kind: "task", text: "运行 Ariel 测试", cardJSON: JSON.stringify({ status: "running", deviceId: "brain-a", threadId: "thread-a" }), createdAt: "2026-11-03T12:00:03Z" },
];
let messages = initialMessages;

async function openPuck(page: Page) {
  assistants = initialAssistants;
	avatarUpdates = [];
  messages = initialMessages;
  await page.route("**/api/auth/status", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ mode: "passkey", authenticated: true, enrollmentRequired: false, puck: { state: "ready" } }) }));
  await page.route("http://127.0.0.1:4173/", async route => {
    const response = await route.fetch();
    await route.fulfill({ response, body: (await response.text()).replace('__ARIEL_WEB_AUTH__', 'passkey') });
  });
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") { socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "puck-e2e", relayEpoch: "puck-e2e", sessionToken: token })); return; }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "brain-a", deviceName: "Mac mini", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, puckBrain: true, puckBrainReady: true } }] };
    if (message.method === "thread.list") data = { threads: [], nextCursor: "" };
    if (message.method === "puck.list") data = { assistants };
    if (message.method === "puck.history") data = { messages };
		if (message.method === "puck.update") {
			avatarUpdates.push(message.params);
			assistants = assistants.map(item => item.assistantId === message.params.assistantId ? { ...item, avatar: JSON.stringify(message.params.avatar) } : item);
			data = { assistant: assistants.find(item => item.assistantId === message.params.assistantId) };
		}
    if (message.method === "puck.question.answer") {
      messages = messages.map(item => item.id === "question" ? { ...item, cardJSON: JSON.stringify({ questionId: "question-a", options: ["Go", "Web"], cardState: "answered", answer: "Go" }) } : item);
      data = {};
    }
    if (message.method === "puck.stop") data = { summary: "已请求停止 1 项", message: { id: "stop", role: "assistant", kind: "stop_summary", text: "已请求停止 1 项" } };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));
  await page.goto("/");
  await expect(page.getByRole("button", { name: "打开 Puck" })).toBeVisible();
  await page.getByRole("button", { name: "打开 Puck" }).click();
  await expect(page.getByText("晚上好。测试已经开始，我会在有结果时告诉你。")).toBeVisible();
}

for (const width of [320, 390, 1280]) for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`Puck workspace ${width}px ${colorScheme}`, () => {
    test.use({ viewport: { width, height: 844 }, isMobile: width < 700, hasTouch: width < 700, colorScheme });
    test("keeps the conversation usable without horizontal overflow", async ({ page }) => {
      await page.emulateMedia({ reducedMotion: "reduce", colorScheme });
      await openPuck(page);
			await expect(page.locator(".puck-workspace")).toHaveCSS("background-color", "rgb(5, 5, 5)");
      expect(await page.locator(".puck-workspace").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
			await expect(page.locator(".puck-message.assistant.text .puck-markdown").first()).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
			await expect(page.locator(".puck-message.user .puck-markdown")).toHaveCSS("background-color", "rgb(28, 28, 30)");
      await expect(page.getByRole("group", { name: "问题卡片" })).toBeVisible();
      await expect(page.getByRole("group", { name: "授权卡片" })).toBeVisible();
      await expect(page.getByRole("group", { name: "任务卡片" })).toContainText("进行中");
      await expect(page.getByRole("button", { name: "打开会话" })).toBeVisible();
			const avatar = page.locator(".puck-conversation > header .puck-avatar");
      await expect(avatar).toHaveAttribute("data-expression", "waiting");
			await expect(avatar.locator(".puck-avatar-ear")).toHaveCount(2);
			await expect(avatar.locator(".puck-avatar-leaf")).toHaveCount(2);
			await expect(avatar.locator(".puck-avatar-brows")).toHaveCount(0);
      await expect(avatar.locator("svg")).toHaveCSS("animation-name", "none");
			await avatar.click();
			await expect(avatar).toHaveAttribute("data-reacting", "false");
			await expect(avatar.locator(".puck-avatar-particles")).toHaveCount(0);
      await page.getByRole("button", { name: "Go" }).click();
      await expect(page.getByRole("group", { name: "问题卡片" })).toContainText("已回答");
      await expect(page.getByRole("button", { name: "Go" })).toBeDisabled();
      if (width < 700) {
        await page.getByRole("button", { name: "打开助理列表" }).click();
        await expect(page.locator(".puck-assistants")).toHaveClass(/mobile-open/);
        await page.mouse.click(width - 20, 400);
        await expect(page.locator(".puck-assistants")).not.toHaveClass(/mobile-open/);
				await page.getByRole("button", { name: "查看助理信息" }).click();
				await expect(page.getByRole("radiogroup", { name: "头像形状" })).toBeVisible();
				expect(await page.locator(".puck-avatar-picker").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
      } else {
        await expect(page.locator(".puck-assistants")).toBeVisible();
        await expect(page.locator(".puck-info")).toBeVisible();
				await expect(page.getByRole("radiogroup", { name: "头像形状" })).toBeVisible();
      }
      await expect(page).toHaveScreenshot(`puck-${width}-${colorScheme}.png`, { animations: "disabled", caret: "hide" });
    });
  });
}

test.describe("Puck forest-sprite avatar interaction", () => {
	test.use({ viewport: { width: 1280, height: 844 }, isMobile: false, hasTouch: false, colorScheme: "dark", reducedMotion: "no-preference" });
	test("follows the pointer and clears playful petals after click", async ({ page }) => {
		await page.emulateMedia({ reducedMotion: "no-preference", colorScheme: "dark" });
		await openPuck(page);
		const avatar = page.locator(".puck-conversation > header .puck-avatar");
		const box = await avatar.boundingBox();
		expect(box).toBeTruthy();
		await page.mouse.move(box!.x + box!.width - 2, box!.y + box!.height / 2);
		await expect(avatar).not.toHaveCSS("--puck-eye-x", "0px");
		await avatar.click();
		await expect(avatar.locator(".puck-avatar-particles")).toBeVisible();
		await page.mouse.move(1000, 800);
		await expect(avatar.locator(".puck-avatar-particles")).toHaveCount(0, { timeout: 1000 });
		await expect(avatar).toHaveAttribute("draggable", "false");
	});

	test("selects every avatar property directly and persists mouse and keyboard changes", async ({ page }) => {
		await openPuck(page);
		const shapeGroup = page.getByRole("radiogroup", { name: "头像形状" });
		const colorGroup = page.getByRole("radiogroup", { name: "头像颜色" });
		await expect(shapeGroup.getByRole("radio")).toHaveCount(4);
		await expect(colorGroup.getByRole("radio")).toHaveCount(7);
		await page.getByRole("radio", { name: "星形" }).click();
		await expect(page.getByRole("radio", { name: "星形" })).toHaveAttribute("aria-checked", "true");
		await expect(page.locator(".puck-avatar-large")).toHaveAttribute("data-shape", "star");
		await page.getByRole("radio", { name: "星形" }).press("ArrowRight");
		await expect(page.getByRole("radio", { name: "圆角三角" })).toHaveAttribute("aria-checked", "true");
		await page.getByRole("radio", { name: "亮蓝" }).press("Enter");
		await expect(page.getByRole("radio", { name: "亮蓝" })).toHaveAttribute("aria-checked", "true");
		await expect.poll(() => avatarUpdates.at(-1)?.avatar).toEqual({ shape: "triangle", color: "#1687ff" });
	});
});
