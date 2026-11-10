import { expect, test, type Page } from "@playwright/test";

const token = `s_${"b".repeat(64)}`;
const initialAssistants = [
	{ assistantId: "puck-one", name: "小帕", description: "负责日常开发协助", brainDeviceId: "brain-a", status: "ready", permissionMode: "full", unreadCount: 0, lastMessagePreview: "测试已经开始", avatar: JSON.stringify({ shape: "cloud", color: "#ff2d8d" }) },
	{ assistantId: "puck-two", name: "罗宾", description: "整理研究资料", brainDeviceId: "brain-a", status: "ready", permissionMode: "full", unreadCount: 2, lastMessagePreview: "需要你确认研究范围", avatar: JSON.stringify({ shape: "drop", color: "#16c96a" }) },
];
let assistants = initialAssistants;
let avatarUpdates: Record<string, unknown>[] = [];
let sentMessages: Record<string, unknown>[] = [];
let authorizationDecisions: Record<string, unknown>[] = [];
const initialMessages = [
	{ id: "user-request", role: "user", kind: "text", text: "帮我检查 Ariel 的测试状态。", createdAt: "2026-11-03T11:59:59Z" },
	{ id: "hello", role: "assistant", kind: "text", text: "晚上好。测试已经开始，我会在有结果时告诉你。", workSummary: JSON.stringify({ elapsedMs: 12400, steps: 3 }), createdAt: "2026-11-03T12:00:00Z" },
  { id: "question", role: "assistant", kind: "question", text: "先运行哪组测试？", cardJSON: JSON.stringify({ questionId: "question-a", options: ["Go", "Web"], cardState: "pending" }), createdAt: "2026-11-03T12:00:01Z" },
  { id: "authorization", role: "assistant", kind: "authorization", text: "这次操作需要你的确认。", cardJSON: JSON.stringify({ authorizationId: "authorization-a", action: "start_task", deviceId: "brain-a", cwd: "/work/ariel", expiresAt: "2026-11-03T12:30:00Z", risk: "目标主机无法证明审批策略", cardState: "pending" }), createdAt: "2026-11-03T12:00:02Z" },
  { id: "task", role: "assistant", kind: "task", text: "运行 Ariel 测试", cardJSON: JSON.stringify({ taskId: "task-a", status: "running", deviceId: "brain-a", threadId: "thread-a" }), createdAt: "2026-11-03T12:00:03Z" },
];
let messages = initialMessages;

async function openPuck(page: Page, history: typeof initialMessages = initialMessages, assistantFixture = initialAssistants, historyByAssistant?: Record<string, typeof initialMessages>) {
  assistants = assistantFixture;
	avatarUpdates = [];
  sentMessages = [];
	authorizationDecisions = [];
  messages = history;
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
    if (message.method === "puck.history") data = { messages: historyByAssistant?.[String(message.params.assistantId)] ?? messages };
		if (message.method === "puck.message.get") data = { message: messages.find(item => item.id === message.params.messageId) };
		if (message.method === "puck.update") {
			avatarUpdates.push(message.params);
			assistants = assistants.map(item => item.assistantId === message.params.assistantId ? { ...item, ...(message.params.avatar ? { avatar: JSON.stringify(message.params.avatar) } : {}), ...(typeof message.params.description === "string" ? { description: message.params.description } : {}), ...(typeof message.params.permissionMode === "string" ? { permissionMode: message.params.permissionMode } : {}) } : item);
			data = { assistant: assistants.find(item => item.assistantId === message.params.assistantId) };
		}
		if (message.method === "puck.read") {
			assistants = assistants.map(item => item.assistantId === message.params.assistantId ? { ...item, unreadCount: 0 } : item);
			data = { unreadCount: 0 };
		}
		if (message.method === "puck.delete") {
			assistants = assistants.filter(item => item.assistantId !== message.params.assistantId);
			data = {};
		}
		if (message.method === "puck.message.react") {
			const reaction = JSON.stringify({ user: message.params.emoji });
			messages = messages.map(item => item.id === message.params.messageId ? { ...item, reaction } : item);
			data = { reaction };
		}
    if (message.method === "puck.message.send") {
      sentMessages.push(message.params);
      data = { state: "processing", startedAt: "2026-11-03T12:00:04Z" };
    }
    if (message.method === "puck.question.answer") {
      messages = messages.map(item => item.id === "question" ? { ...item, cardJSON: JSON.stringify({ questionId: "question-a", options: ["Go", "Web"], cardState: "answered", answer: "Go" }) } : item);
      data = {};
    }
		if (message.method === "puck.authorization.decide") {
			authorizationDecisions.push(message.params);
			messages = messages.map(item => item.id === "authorization" ? { ...item, cardJSON: JSON.stringify({ ...JSON.parse(item.cardJSON || "{}"), cardState: "consumed", decision: message.params.decision }) } : item);
			data = { state: "consumed" };
		}
    if (message.method === "puck.stop") data = { summary: "已请求停止 1 项", message: { id: "stop", role: "assistant", kind: "stop_summary", text: "已请求停止 1 项" } };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));
  await page.goto("/");
  await expect(page.getByRole("button", { name: "打开 Puck" })).toBeVisible();
  await page.getByRole("button", { name: "打开 Puck" }).click();
  await expect(page.locator(".puck-transcript").getByText("晚上好。测试已经开始，我会在有结果时告诉你。")).toBeVisible();
}

test.describe("Puck reply to message desktop", () => {
	test.use({ viewport: { width: 1280, height: 844 }, isMobile: false, hasTouch: false, colorScheme: "dark" });
	test("reveals reply on hover, sends replyTo and jumps back to the quoted message", async ({ page }) => {
		await openPuck(page);
		const source = page.locator('[data-message-id="hello"]');
		await source.hover();
		const reply = page.getByRole("button", { name: /回复助理消息：晚上好/ });
		await expect(reply).toHaveCSS("opacity", "1");
		await reply.click();
		await expect(page.getByLabel("正在回复助理消息")).toContainText("晚上好");
		const composer = page.getByRole("textbox", { name: "给 Puck 发消息" });
		await composer.fill("继续检查失败项");
		await composer.dispatchEvent("keydown", { key: "Enter", code: "Enter", keyCode: 229, which: 229, bubbles: true, cancelable: true });
		await composer.dispatchEvent("compositionstart", { data: "继续" });
		await composer.press("Enter");
		await composer.dispatchEvent("compositionend", { data: "继续" });
		await composer.press("Enter");
		expect(sentMessages).toHaveLength(0);
		await page.waitForTimeout(60);
		await composer.press("Enter");
		await expect.poll(() => sentMessages.at(-1)?.replyTo).toBe("hello");
		const quote = page.getByRole("button", { name: "跳到被引用的消息" });
		await quote.click();
		await expect(source).toHaveClass(/reply-highlight/);
	});
});

test.describe("Puck reply to message mobile", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
	test("starts a reply with a left swipe and keeps the frosted preview clear at 390px", async ({ page }) => {
		await openPuck(page);
		const source = page.locator('[data-message-id="task"]');
		await source.dispatchEvent("pointerdown", { pointerType: "touch", clientX: 330, clientY: 320 });
		await source.dispatchEvent("pointerup", { pointerType: "touch", clientX: 245, clientY: 322 });
		const preview = page.getByLabel("正在回复助理消息");
		await expect(preview).toContainText("运行 Ariel 测试");
		await expect(preview).toHaveCSS("backdrop-filter", /blur\(20px\)/);
		expect(await page.locator(".puck-workspace").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
		await expect(page).toHaveScreenshot("puck-reply-preview-390-dark.png", { animations: "disabled", caret: "hide" });
		await page.getByRole("textbox", { name: "给 Puck 发消息" }).fill("继续跑失败项");
		await page.getByRole("button", { name: "发送" }).click();
		await expect.poll(() => sentMessages.at(-1)?.replyTo).toBe("task");
		await expect(page.getByRole("button", { name: "跳到被引用的消息" })).toContainText("运行 Ariel 测试");
	});
});

test.describe("Puck structured question cards", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
	test("renders single, multi-question, described and answered mobile cards", async ({ page }) => {
		const history = [
			...initialMessages.slice(0, 2),
			{ id: "question-card:single", role: "assistant", kind: "question", text: "从哪里进入？", cardJSON: JSON.stringify({ questionId:"single", questions:[{ id:"entry", question:"从哪里进入？", options:[{ label:"Codex Desktop", value:"desktop" }] }], cardState:"pending" }), createdAt:"2026-11-03T12:00:01Z" },
			{ id: "question-card:described", role: "assistant", kind: "question", text: "选择工作方式", cardJSON: JSON.stringify({ questionId:"described", questions:[{ id:"mode", question:"选择工作方式", options:[{ label:"Puck / Ariel", description:"适合从手机远程操作，说明文字可以自然换行", value:"puck" }] }], cardState:"pending" }), createdAt:"2026-11-03T12:00:02Z" },
			{ id: "question-card:multiple", role: "assistant", kind: "question", text: "请回答以下 2 个问题", cardJSON: JSON.stringify({ questionId:"multiple", questions:[{ id:"entry", question:"入口？", options:[{ label:"Desktop", value:"desktop" }] },{ id:"features", question:"能力？", options:[{ label:"浏览器", value:"browser" },{ label:"文件", value:"files" }], multiSelect:true, allowCustom:true }], cardState:"pending" }), createdAt:"2026-11-03T12:00:03Z" },
			{ id: "question-card:answered", role: "assistant", kind: "question", text: "选择入口", cardJSON: JSON.stringify({ questionId:"answered", questions:[{ id:"entry", question:"选择入口", options:[{ label:"Codex CLI", value:"cli" }] }], cardState:"answered", answers:[{ questionId:"entry", values:["cli"] }] }), createdAt:"2026-11-03T12:00:04Z" },
		] as typeof initialMessages;
		await openPuck(page, history);
		for (const id of ["single","described","multiple","answered"]) {
			const row = page.locator(`[data-message-id="question-card:${id}"]`);
			expect(await row.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
			await expect(row).toHaveScreenshot(`puck-question-${id}-390-dark.png`, { animations:"disabled", caret:"hide" });
		}
		expect(await page.locator(".puck-workspace").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
	});
});

test.describe("Puck persistent authorization card", () => {
	test.use({ viewport: { width: 320, height: 720 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
	test("keeps three decisions inside a narrow card and renders approve-always terminal state", async ({ page }) => {
		const history = [
			...initialMessages.slice(0, 2),
			{ id: "authorization", role: "assistant", kind: "authorization", text: "运行隔离测试需要确认。", cardJSON: JSON.stringify({ authorizationId: "authorization-a", action: "start_task", deviceId: "brain-a", cwd: "/work/ariel", expiresAt: "2099-11-03T12:30:00Z", cardState: "pending" }), createdAt: "2026-11-03T12:00:02Z" },
		] as typeof initialMessages;
		await openPuck(page, history);
		const card = page.getByRole("group", { name: "授权卡片" });
		for (const name of ["拒绝", "仅这一次允许", "以后都允许"]) await expect(card.getByRole("button", { name })).toBeVisible();
		expect(await card.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
		await card.getByRole("button", { name: "以后都允许" }).click();
		await expect(card.getByText("已允许（以后都允许）")).toBeVisible();
		expect(authorizationDecisions).toContainEqual({ assistantId: "puck-one", authorizationId: "authorization-a", decision: "approve_always" });
	});
});

test.describe("Puck recent assistant and latest-message navigation", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });

	test("restores the recent assistant, scrolls switches to the bottom, and preserves manual reading", async ({ page }) => {
		const firstHistory = [
			...initialMessages,
			...Array.from({ length: 30 }, (_, index) => ({ id: `first-${index}`, role: index % 2 ? "user" : "assistant", kind: "text", text: `一号历史消息 ${index + 1} ${"较长内容用于形成真实滚动区域。".repeat(4)}`, createdAt: `2026-11-03T13:${String(index).padStart(2, "0")}:00Z` })),
		] as typeof initialMessages;
		const secondHistory = [
			...initialMessages,
			...Array.from({ length: 30 }, (_, index) => ({ id: `second-${index}`, role: index % 2 ? "user" : "assistant", kind: "text", text: `二号历史消息 ${index + 1} ${"较长内容用于形成真实滚动区域。".repeat(4)}`, createdAt: `2026-11-03T14:${String(index).padStart(2, "0")}:00Z` })),
		] as typeof initialMessages;
		const histories = { "puck-one": firstHistory, "puck-two": secondHistory };
		await page.addInitScript(() => localStorage.setItem("ariel.puck.lastAssistantId.v1", "puck-two"));
		await openPuck(page, firstHistory, initialAssistants, histories);

		const transcript = page.locator(".puck-transcript");
		await expect(page.locator(".puck-conversation-title")).toHaveText("罗宾");
		await expect(transcript.getByText("二号历史消息 30", { exact: false })).toHaveCount(1);
		expect(await transcript.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeLessThanOrEqual(1);

		await page.getByRole("button", { name: "打开助理列表" }).click();
		await page.getByRole("button", { name: /小帕/ }).click();
		await expect(page.locator(".puck-conversation-title")).toHaveText("小帕");
		await expect(transcript.getByText("一号历史消息 30", { exact: false })).toHaveCount(1);
		expect(await transcript.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeLessThanOrEqual(1);
		const lastMessageBox = await page.locator(".puck-message").last().boundingBox();
		const composerBox = await page.locator(".puck-composer").boundingBox();
		expect(lastMessageBox!.y + lastMessageBox!.height).toBeLessThanOrEqual(composerBox!.y - 8);

		await transcript.evaluate(element => { element.scrollTop = 120; element.dispatchEvent(new Event("scroll")); });
		firstHistory.push({ id: "first-new", role: "assistant", kind: "text", text: "一号轮询新增消息", createdAt: "2026-11-03T13:31:00Z" });
		await expect(transcript.getByText("一号轮询新增消息")).toBeVisible({ timeout: 12_000 });
		expect(await transcript.evaluate(element => element.scrollTop)).toBe(120);
	});
});

test.describe("Puck delete assistant", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });

	test("confirms the destructive action and switches to a surviving Bot", async ({ page }) => {
		await openPuck(page);
		await page.getByRole("button", { name: "查看助理信息" }).click();
		await page.getByRole("button", { name: "删除 Bot" }).click();
		const dialog = page.getByRole("dialog", { name: "删除 Bot" });
		await expect(dialog).toContainText("停止该 Bot 的后台大脑");
		await expect(dialog).toContainText("历史不再显示");
		await expect(dialog).toHaveCSS("backdrop-filter", /blur\(20px\)/);
		await expect(page).toHaveScreenshot("puck-delete-confirm-390-dark.png", { animations: "disabled", caret: "hide" });
		await page.getByRole("button", { name: "确认删除小帕" }).click();
		await expect(page.locator(".puck-conversation-title")).toHaveText("罗宾");
		await expect(page.getByRole("button", { name: /小帕/ })).toHaveCount(0);
		await expect.poll(() => page.evaluate(() => localStorage.getItem("ariel.puck.lastAssistantId.v1"))).toBe("puck-two");
	});
});

for (const width of [320, 390, 1280]) {
  test.describe(`Puck fixed workspace ${width}px`, () => {
    test.use({ viewport: { width, height: 844 }, isMobile: width < 700, hasTouch: width < 700, colorScheme: "dark" });
    test("gives the processing row a large avatar without overflowing the conversation", async ({ page }) => {
      await openPuck(page, [
        ...initialMessages,
        { id: "working", role: "assistant", kind: "processing", text: "正在处理一项需要持续关注的工作…", createdAt: "2026-11-03T12:00:04Z" },
      ]);

      const processingRow = page.locator('[data-message-id="working"]');
      const avatarBox = await processingRow.locator(":scope > .puck-avatar").boundingBox();
      expect(avatarBox).toBeTruthy();
      expect(avatarBox!.width).toBeGreaterThanOrEqual(56);
      expect(avatarBox!.height).toBeGreaterThanOrEqual(56);
      expect(await processingRow.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
      expect(await page.locator(".puck-workspace").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
      await expect(page.locator(".puck-conversation > header .puck-avatar")).toHaveCSS("width", "32px");
      const listAvatarBox = await page.locator(".puck-assistant-row .puck-avatar").first().boundingBox();
      expect(listAvatarBox).toBeTruthy();
      expect(listAvatarBox!.width).toBeGreaterThanOrEqual(78);
      expect(listAvatarBox!.width).toBeLessThanOrEqual(82);
      expect(listAvatarBox!.height).toBeGreaterThanOrEqual(78);
      expect(listAvatarBox!.height).toBeLessThanOrEqual(82);
      const listRowBox = await page.locator(".puck-assistant-row").first().boundingBox();
      expect(listRowBox!.height).toBe(96);
      await expect(page.locator(".puck-shape-option .puck-avatar").first()).toHaveCSS("width", "28px");
      await expect(page.locator(".puck-info .puck-avatar-large")).toHaveCSS("width", "96px");
      await expect(processingRow).toHaveScreenshot(`puck-processing-avatar-${width}.png`, { animations: "disabled", caret: "hide" });
      const assistantList = page.locator(".puck-assistants");
      if (width < 700) {
        await page.getByRole("button", { name: "打开助理列表" }).click();
        await expect(assistantList).toHaveClass(/mobile-open/);
      }
      await expect(assistantList).toHaveScreenshot(`puck-assistant-list-${width}.png`, { animations: "disabled", caret: "hide" });
    });

    test("keeps the header and desktop side panels fixed while only the transcript scrolls", async ({ page }) => {
      const longHistory = [
        ...initialMessages,
        ...Array.from({ length: 24 }, (_, index) => ({
          id: `scroll-${index}`,
          role: index % 2 ? "user" : "assistant",
          kind: "text",
          text: `滚动固定布局验证 ${index + 1}。${"只有中间消息列表应滚动，顶部和侧栏保持原位。".repeat(3)}`,
          createdAt: `2026-11-03T12:${String(index + 5).padStart(2, "0")}:00Z`,
        })),
      ];
      await openPuck(page, longHistory);
      const transcript = page.locator(".puck-transcript");
      const header = page.locator(".puck-conversation > header");
      const menu = page.getByRole("button", { name: "打开助理列表" });
      const before = width >= 700 ? await Promise.all([
        page.locator(".puck-assistants").boundingBox(),
        page.locator(".puck-info").boundingBox(),
      ]) : [];
			const initialHeaderBox = await header.boundingBox();
			const conversationBox = await page.locator(".puck-conversation").boundingBox();
			const transcriptBox = await transcript.boundingBox();
			await expect(header).toHaveCSS("position", "absolute");
			expect(transcriptBox!.y).toBe(conversationBox!.y);
			expect(initialHeaderBox!.y).toBe(conversationBox!.y);
			await transcript.evaluate(element => { element.scrollTop = 0; });
			const initialMessageBox = await page.locator(".puck-message").first().boundingBox();
			expect(initialMessageBox!.y).toBeGreaterThanOrEqual(initialHeaderBox!.y + initialHeaderBox!.height);
			await transcript.evaluate(element => { element.scrollTop = 48; });
			const scrolledMessageBox = await page.locator(".puck-message").first().boundingBox();
			expect(scrolledMessageBox!.y).toBeLessThan(initialHeaderBox!.y + initialHeaderBox!.height);
			expect(scrolledMessageBox!.y + scrolledMessageBox!.height).toBeGreaterThan(initialHeaderBox!.y);

      await transcript.evaluate(element => {
        element.scrollTop = element.scrollHeight;
        element.dispatchEvent(new Event("scroll"));
      });

      await expect(header).toBeInViewport();
			await expect(header).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
			await expect(header).toHaveCSS("border-bottom-width", "0px");
			const titleChip = header.locator(".puck-header-title");
			const actionChip = header.locator(".puck-header-actions");
			await expect(titleChip).toHaveCSS("backdrop-filter", /blur\(20px\)/);
			await expect(titleChip).toHaveCSS("background-color", "rgba(32, 32, 36, 0.48)");
			await expect(titleChip).toHaveCSS("border-radius", "999px");
			await expect(actionChip).toHaveCSS("backdrop-filter", /blur\(20px\)/);
			const chipGeometry = await header.locator(":scope > .puck-glass-chip").evaluateAll(chips => chips.map(chip => {
				const box = chip.getBoundingClientRect();
				return { left: box.left, right: box.right, top: box.top, bottom: box.bottom };
			}).filter(chip => chip.right > chip.left));
			expect(chipGeometry).toHaveLength(3);
			for (let index = 1; index < chipGeometry.length; index++) expect(chipGeometry[index].left).toBeGreaterThanOrEqual(chipGeometry[index - 1].right);
			const headerBox = await header.boundingBox();
			expect(chipGeometry.every(chip => chip.top >= headerBox!.y && chip.bottom <= headerBox!.y + headerBox!.height)).toBe(true);
			const headerPadding = width < 700 ? 12 : 16;
			expect(Math.abs(chipGeometry[0].left - (headerBox!.x + headerPadding))).toBeLessThanOrEqual(1);
			expect(Math.abs(chipGeometry[2].right - (headerBox!.x + headerBox!.width - headerPadding))).toBeLessThanOrEqual(1);
			expect(chipGeometry[0].right - chipGeometry[0].left).toBe(chipGeometry[2].right - chipGeometry[2].left);
			expect(Math.abs((chipGeometry[1].left + chipGeometry[1].right) / 2 - (headerBox!.x + headerBox!.width / 2))).toBeLessThanOrEqual(1);
			expect(chipGeometry[1].left - chipGeometry[0].right).toBeGreaterThan(width < 700 ? 8 : 32);
			expect(chipGeometry[2].left - chipGeometry[1].right).toBeGreaterThan(width < 700 ? 8 : 32);

			const footer = page.locator(".puck-conversation > footer");
			const composer = page.locator(".puck-composer");
			await expect(footer).toHaveCSS("position", "absolute");
			await expect(footer).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
			await expect(footer).toHaveCSS("pointer-events", "none");
			await expect(composer).toHaveCSS("backdrop-filter", /blur\(20px\)/);
			await expect(composer).toHaveCSS("background-color", "rgba(32, 32, 36, 0.48)");
			const footerBox = await footer.boundingBox();
			const composerBox = await composer.boundingBox();
			expect(Math.abs(footerBox!.y + footerBox!.height - (conversationBox!.y + conversationBox!.height))).toBeLessThanOrEqual(1);
			expect(transcriptBox!.y + transcriptBox!.height).toBe(conversationBox!.y + conversationBox!.height);
			const lastMessageBox = await page.locator(".puck-message").last().boundingBox();
			expect(lastMessageBox!.y + lastMessageBox!.height).toBeLessThanOrEqual(composerBox!.y - 8);
			await expect(page).toHaveScreenshot(`puck-floating-header-${width}.png`, { animations: "disabled", caret: "hide" });
      expect(await page.evaluate(() => document.scrollingElement?.scrollTop || 0)).toBe(0);
      if (width < 700) {
        await expect(menu).toBeVisible();
        await menu.click();
        await expect(page.locator(".puck-assistants")).toHaveClass(/mobile-open/);
      } else {
        const after = await Promise.all([
          page.locator(".puck-assistants").boundingBox(),
          page.locator(".puck-info").boundingBox(),
        ]);
        expect(after).toEqual(before);
      }
      await expect(page).toHaveScreenshot(`sticky-puck-${width}-after-scroll.png`, { animations: "disabled", caret: "hide" });
    });
  });
}

test.describe("Puck HTTP availability normalization", () => {
	for (const fixture of [
		{ state: "off", opensPuck: false },
		{ state: "degraded", reason: "PUCK_BRAIN_UNAVAILABLE", opensPuck: true },
	] as const) {
		test(`${fixture.state} does not masquerade as ready`, async ({ page }) => {
			await page.route("**/api/auth/status", route => route.fulfill({
				contentType: "application/json",
				body: JSON.stringify({ mode: "passkey", authenticated: true, enrollmentRequired: false, puck: fixture }),
			}));
			await page.route("http://127.0.0.1:4173/", async route => {
				const response = await route.fetch();
				await route.fulfill({ response, body: (await response.text()).replace("__ARIEL_WEB_AUTH__", "passkey") });
			});
			await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
				const message = JSON.parse(String(raw));
				if (message.type === "hello") socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "puck-status-e2e", relayEpoch: "puck-status-e2e", sessionToken: token }));
			}));
			await page.goto("/");
			const button = page.getByRole("button", { name: "打开 Puck" });
			if (!fixture.opensPuck) {
				await expect(button).toHaveCount(0);
				return;
			}
			await expect(button).toBeVisible();
			await button.click();
			await expect(page.locator(".puck-unavailable")).toContainText("Puck 不可用");
			await expect(page.getByRole("textbox", { name: "给 Puck 发消息" })).toHaveCount(0);
		});
	}
});

test.describe("Puck malformed assistant data isolation", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });

	test("opens MindPowers structured question options without a black screen", async ({ page }) => {
		const pageErrors: string[] = [];
		page.on("pageerror", error => pageErrors.push(error.message));
		const history = [
			...initialMessages,
			{
				id: "question-card:rag-scope",
				role: "assistant",
				kind: "question",
				text: "RAG 进度关注范围要覆盖到哪一层？",
				cardJSON: JSON.stringify({
					questionId: "rag-scope",
					cardState: "pending",
					options: [
						{ label: "RAG 全链路（推荐）", description: "覆盖 RAG/VDB、知识库导入/分析、检索验证和相关部署。" },
						{ label: "仅 RAG - VDB 线程", description: "只跟踪当前研发线程。" },
					],
				}),
			},
		] as typeof initialMessages;
		await openPuck(page, initialMessages, [
			initialAssistants[0],
			{ ...initialAssistants[1], assistantId: "mindpowers", name: "MindPowers 研发" },
		], { "puck-one": initialMessages, mindpowers: history });
		await page.getByRole("button", { name: "打开助理列表" }).click();
		await page.getByRole("button", { name: /MindPowers 研发/ }).click();

		await expect(page.locator(".puck-workspace")).toBeVisible();
		await expect(page.getByRole("button", { name: /RAG 全链路（推荐）/ })).toBeVisible();
		await page.getByRole("button", { name: "打开助理列表" }).click();
		await expect(page.locator(".puck-assistants")).toBeVisible();
		expect(pageErrors).toEqual([]);
	});
});

test.describe("Puck touch reactions", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
	test("hides the persistent add control and opens the picker on long press", async ({ page }) => {
		await openPuck(page);
		await expect(page.getByRole("button", { name: "给消息添加回应" }).first()).toBeHidden();
		const message = page.locator(".puck-message").first();
		await message.dispatchEvent("pointerdown", { pointerType: "touch", isPrimary: true, clientX: 50, clientY: 100 });
		await page.waitForTimeout(550);
		await expect(page.getByRole("button", { name: "回应 👍" })).toBeVisible();
		await message.dispatchEvent("pointerup", { pointerType: "touch", isPrimary: true, clientX: 50, clientY: 100 });
	});
});

for (const width of [320, 390, 1280]) for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`Puck workspace ${width}px ${colorScheme}`, () => {
    test.use({ viewport: { width, height: 844 }, isMobile: width < 700, hasTouch: width < 700, colorScheme });
    test("keeps the conversation usable without horizontal overflow", async ({ page }) => {
      await page.emulateMedia({ reducedMotion: "reduce", colorScheme });
      await openPuck(page);
			await expect(page.locator(".puck-workspace")).toHaveCSS("background-color", "rgb(5, 5, 5)");
      expect(await page.locator(".puck-workspace").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
			const assistantContent = page.locator(".puck-message.assistant.text .puck-message-content").first();
			await expect(assistantContent).toHaveCSS("background-color", "rgb(28, 28, 30)");
			await expect(assistantContent).toHaveCSS("padding", "11px 15px");
			await expect(assistantContent).toHaveCSS("border-radius", "18px 18px 18px 5px");
			await expect(page.locator(".puck-message.question .puck-message-content")).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
			await expect(page.locator(".puck-message.question .puck-message-content")).toHaveCSS("padding", "0px");
			await expect(page.locator(".puck-message.user .puck-message-content")).toHaveCSS("background-color", "rgb(29, 93, 198)");
			await expect(page.locator(".puck-message.message, .puck-message .message-body, .puck-message .message-text, .puck-message .markdown-body")).toHaveCount(0);
			await expect(page.locator(".puck-transcript .puck-message.assistant .puck-avatar")).toHaveCount(0);
      await expect(page.getByRole("group", { name: "问题卡片" })).toBeVisible();
      await expect(page.getByRole("group", { name: "授权卡片" })).toBeVisible();
			await expect(page.getByRole("group", { name: "授权卡片" })).toContainText("已过期");
			await expect(page.getByRole("button", { name: "拒绝" })).toHaveCount(0);
			await expect(page.getByRole("button", { name: "仅这一次允许" })).toHaveCount(0);
      await expect(page.getByRole("group", { name: "任务卡片" })).toContainText("进行中");
      await expect(page.getByRole("button", { name: "打开会话" })).toBeVisible();
			await expect(page.getByRole("button", { name: "打开会话" })).toHaveCSS("height", "28px");
			const sendButton = page.getByRole("button", { name: "发送" });
			await expect(sendButton).toHaveCSS("height", "32px");
			await expect(sendButton).not.toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
			await expect(page.getByRole("button", { name: "停止全部工作" })).toHaveCSS("height", "32px");
			const avatar = page.locator(".puck-conversation > header .puck-avatar");
      await expect(avatar).toHaveAttribute("data-expression", "waiting");
			await expect(avatar.locator(".puck-avatar-ear")).toHaveCount(2);
			await expect(avatar.locator(".puck-avatar-crown, .puck-avatar-leaf, .puck-avatar-flower, .puck-avatar-mouth")).toHaveCount(0);
			await expect(avatar.locator(".puck-avatar-brows")).toHaveCount(0);
			await expect(avatar.locator(".puck-avatar-eye-shape")).toHaveCount(2);
			await expect(avatar.locator(".puck-avatar-eye-white, .puck-avatar-pupil, clipPath")).toHaveCount(0);
      await expect(avatar.locator("svg")).toHaveCSS("animation-name", "none");
			await avatar.click();
			await expect(avatar).toHaveAttribute("data-reacting", "false");
			await expect(avatar.locator(".puck-avatar-sparkle")).toHaveCount(5);
      await page.getByRole("button", { name: "Go" }).click();
      await expect(page.getByRole("group", { name: "问题卡片" })).toContainText("已回答");
			await expect(page.getByRole("button", { name: "Go" })).toHaveCount(0);
			await expect(page.getByRole("group", { name: "问题卡片" })).toContainText("你选择了 Go");
      if (width < 700) {
        await page.getByRole("button", { name: "打开助理列表" }).click();
        await expect(page.locator(".puck-assistants")).toHaveClass(/mobile-open/);
        await page.mouse.click(width - 20, 400);
        await expect(page.locator(".puck-assistants")).not.toHaveClass(/mobile-open/);
				await page.getByRole("button", { name: "查看助理信息" }).click();
				await expect(page.getByRole("radiogroup", { name: "头像形状" })).toBeVisible();
				expect(await page.locator(".puck-avatar-picker").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
				if (width === 390 && colorScheme === "dark") await expect(page).toHaveScreenshot("puck-390-info-drawer-dark.png", { animations: "disabled", caret: "hide" });
				await page.mouse.click(10, 400);
				await expect(page.locator(".puck-info")).not.toHaveClass(/mobile-open/);
      } else {
        await expect(page.locator(".puck-assistants")).toBeVisible();
        await expect(page.locator(".puck-info")).toBeVisible();
				await expect(page.getByRole("radiogroup", { name: "头像形状" })).toBeVisible();
				const headerBottoms = await Promise.all([".puck-assistants > header", ".puck-conversation > header", ".puck-info > header"].map(selector => page.locator(selector).evaluate(element => element.getBoundingClientRect().bottom)));
				expect(Math.max(...headerBottoms) - Math.min(...headerBottoms)).toBeLessThanOrEqual(1);
      }
			await expect(page.locator(".puck-conversation > header")).toHaveCSS("height", "52px");
			await expect(page.locator(".puck-assistants")).toHaveCSS("backdrop-filter", "none");
			if (width < 700) {
				await expect(page.locator(".puck-info")).toHaveCSS("backdrop-filter", /blur\(20px\)/);
			}
			if (width >= 700) {
				await expect(page.locator(".puck-assistants")).toHaveCSS("width", "280px");
				await expect(page.locator(".puck-assistant-row").first()).toHaveCSS("height", "96px");
			}
			const gaps = await page.locator(".puck-message").evaluateAll(elements => ({
				group: elements[1].getBoundingClientRect().top - elements[0].getBoundingClientRect().bottom,
				inside: elements[2].getBoundingClientRect().top - elements[1].getBoundingClientRect().bottom,
			}));
			expect(gaps.group).toBeGreaterThanOrEqual(19);
			expect(gaps.group).toBeLessThanOrEqual(21);
			expect(gaps.inside).toBeGreaterThanOrEqual(5);
			expect(gaps.inside).toBeLessThanOrEqual(7);
			const cardWidths = await page.locator(".puck-card").evaluateAll(cards => cards.map(card => card.getBoundingClientRect().width));
			expect(Math.max(...cardWidths) - Math.min(...cardWidths)).toBeLessThanOrEqual(1);
			const composerGeometry = await page.locator(".puck-composer").evaluate(composer => {
				const outer = composer.getBoundingClientRect();
				const send = composer.querySelector(".puck-send")!.getBoundingClientRect();
				return { height: outer.height, centerDelta: Math.abs((outer.top + outer.height / 2) - (send.top + send.height / 2)) };
			});
			expect(composerGeometry.height).toBeGreaterThanOrEqual(44);
			expect(composerGeometry.height).toBeLessThanOrEqual(48);
			expect(composerGeometry.centerDelta).toBeLessThanOrEqual(1);
			await expect(page.locator(".puck-info .puck-avatar-large .puck-avatar-badge")).toHaveCount(0);
      await expect(page).toHaveScreenshot(`puck-${width}-${colorScheme}.png`, { animations: "disabled", caret: "hide" });
			await page.getByRole("textbox", { name: "给 Puck 发消息" }).fill("第一行\n第二行\n第三行");
			expect(await page.locator(".puck-composer").evaluate(composer => composer.getBoundingClientRect().height)).toBeGreaterThan(48);
			await expect(sendButton).toBeEnabled();
			await expect(sendButton).toHaveCSS("background-color", "rgb(255, 45, 141)");
			await expect(sendButton).toHaveCSS("color", "rgb(11, 11, 12)");
			const arrowCenter = await sendButton.evaluate(button => {
				const outer = button.getBoundingClientRect();
				const icon = button.querySelector("svg")!.getBoundingClientRect();
				return {
					x: Math.abs(outer.left + outer.width / 2 - (icon.left + icon.width / 2)),
					y: Math.abs(outer.top + outer.height / 2 - (icon.top + icon.height / 2)),
				};
			});
			expect(arrowCenter.x).toBeLessThanOrEqual(.5);
			expect(arrowCenter.y).toBeLessThanOrEqual(.5);
			if (width === 390 && colorScheme === "dark") await expect(page).toHaveScreenshot("puck-send-enabled-390-dark.png", { animations: "disabled", caret: "hide" });
    });
  });
}

test.describe("Puck frosted dialog", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
	test("uses the shared glass surface without blurring the transcript", async ({ page }) => {
		await openPuck(page);
		await page.getByRole("button", { name: "打开助理列表" }).click();
		await page.getByRole("button", { name: "新建助理" }).click();
		const dialog = page.locator(".puck-dialog");
		await expect(dialog).toBeVisible();
		await expect(dialog).toHaveCSS("backdrop-filter", /blur\(20px\)/);
		await expect(dialog).toHaveCSS("background-color", "rgba(32, 32, 36, 0.48)");
		await expect(dialog).toHaveCSS("border-radius", "20px");
		await expect(page.locator(".puck-transcript")).toHaveCSS("backdrop-filter", "none");
		await expect(dialog).toHaveScreenshot("puck-dialog-390-dark.png", { animations: "disabled", caret: "hide" });
	});
});

test.describe("Puck idle floating header", () => {
	test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
	test("keeps a balanced right glass chip when there is no stop action", async ({ page }) => {
		await openPuck(page, initialMessages.slice(0, 2));
		const header = page.locator(".puck-conversation > header");
		await expect(header.locator(".puck-glass-chip")).toHaveCount(3);
		await expect(page.getByRole("button", { name: "更多助理操作" })).toBeVisible();
		await expect(page.getByRole("button", { name: "停止全部工作" })).toHaveCount(0);
		await page.getByRole("button", { name: "更多助理操作" }).click();
		await expect(page.locator(".puck-info")).toHaveClass(/mobile-open/);
	});
});

test.describe("Puck composer attachments", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" });
  test("keeps the plus target visible and sends a removable image-only draft", async ({ page }) => {
    await openPuck(page);
    const attach = page.getByRole("button", { name: "选择截图" });
    await expect(attach).toBeVisible();
    const attachBox = await attach.boundingBox();
    expect(attachBox?.width).toBeGreaterThanOrEqual(32);
    expect(attachBox?.height).toBeGreaterThanOrEqual(32);
    const input = page.getByLabel("附加截图");
    await input.setInputFiles({ name: "fixture.png", mimeType: "image/png", buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64") });
    await expect(page.getByRole("img", { name: "fixture.png" })).toBeVisible();
    await page.getByRole("button", { name: "移除截图：fixture.png" }).click();
    await expect(page.getByRole("img", { name: "fixture.png" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "发送" })).toBeDisabled();
    await input.setInputFiles({ name: "fixture.png", mimeType: "image/png", buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64") });
    await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
    await page.getByRole("button", { name: "发送" }).click();
    await expect.poll(() => sentMessages.length).toBe(1);
    expect(sentMessages[0].text).toBe("");
    expect(sentMessages[0].images).toEqual([expect.stringMatching(/^data:image\/png;base64,/)]);
    await expect(page.getByLabel("待发送截图")).toHaveCount(0);
    await expect(page.locator(".puck-message.user .puck-message-image")).toHaveCount(1);
  });
});

test.describe("Puck forest-sprite avatar interaction", () => {
	test.use({ viewport: { width: 1280, height: 844 }, isMobile: false, hasTouch: false, colorScheme: "dark", reducedMotion: "no-preference" });
	test("follows the pointer and finishes its compositor-only click reaction", async ({ page }) => {
		await page.emulateMedia({ reducedMotion: "no-preference", colorScheme: "dark" });
		await openPuck(page);
		const avatar = page.locator(".puck-conversation > header .puck-avatar");
		const box = await avatar.boundingBox();
		expect(box).toBeTruthy();
		await page.mouse.move(box!.x + box!.width - 2, box!.y + box!.height / 2);
		await expect(avatar).not.toHaveCSS("--puck-face-x", "0px");
		await expect(avatar).not.toHaveCSS("--puck-head-rotate", "0deg");
		await avatar.click();
		await expect(avatar).toHaveAttribute("data-reacting", "true");
		await expect(avatar.locator(".puck-avatar-sparkle")).toHaveCount(5);
		await expect(avatar.locator(".puck-avatar-particle")).toHaveCount(0);
		await page.mouse.move(1000, 800);
		await expect(avatar).toHaveAttribute("data-reacting", "false", { timeout: 1000 });
		await expect(avatar).toHaveAttribute("draggable", "false");
	});

	test("captures the same avatar at rest, larger turns, orbit poses and a blink", async ({ page }) => {
		await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "dark" });
		await openPuck(page);
		const board = page.locator(".puck-motion-storyboard");
		await page.locator(".puck-conversation > header .puck-avatar").evaluate(source => {
			const frames = [
				{ label: "静止", head: [0, 0, 0], face: [4, -4, -24], eyes: [1, .72, 1], blink: "open" },
				{ label: "左上扫视", head: [-7.5, -6.8, -24], face: [-11.5, -10, -32], eyes: [.55, 1, 1.7], blink: "open" },
				{ label: "环绕顶部", head: [0, -6.8, -20], face: [0, -10, -24], eyes: [1, 1, 1.7], blink: "open" },
				{ label: "右下扫视", head: [7.5, 6.8, 24], face: [11.5, 10, 32], eyes: [1, .55, 1.7], blink: "open" },
				{ label: "环绕底部", head: [0, 6.8, 20], face: [0, 10, -24], eyes: [1, 1, 1.7], blink: "open" },
				{ label: "眨眼", head: [0, 0, 0], face: [4, -4, -24], eyes: [1, .72, 1], blink: "closed" },
			];
			const storyboard = document.createElement("section");
			storyboard.className = "puck-motion-storyboard";
			Object.assign(storyboard.style, { position: "fixed", inset: "80px auto auto 80px", zIndex: "99", display: "flex", gap: "24px", padding: "24px", borderRadius: "20px", background: "#0d0d0e", color: "white", font: "14px -apple-system, system-ui" });
			for (const frame of frames) {
				const cell = document.createElement("div"); Object.assign(cell.style, { display: "grid", justifyItems: "center", gap: "10px" });
				const avatar = source.cloneNode(true) as HTMLElement; avatar.querySelector(".puck-avatar-badge")?.remove();
				Object.assign(avatar.style, { width: "112px", height: "112px" });
				const properties = { "--puck-head-x": `${frame.head[0]}%`, "--puck-head-y": `${frame.head[1]}%`, "--puck-head-rotate": `${frame.head[2]}deg`, "--puck-face-x": `${frame.face[0]}px`, "--puck-face-y": `${frame.face[1]}px`, "--puck-face-rotate": `${frame.face[2]}deg`, "--puck-left-eye-xscale": String(frame.eyes[0]), "--puck-right-eye-xscale": String(frame.eyes[1]), "--puck-eye-roundness": String(frame.eyes[2]) };
				Object.entries(properties).forEach(([name, value]) => avatar.style.setProperty(name, value));
				avatar.dataset.blinkPhase = frame.blink;
				cell.append(avatar, Object.assign(document.createElement("span"), { textContent: frame.label })); storyboard.append(cell);
			}
			document.querySelector(".puck-workspace")?.append(storyboard);
		});
		await expect(board).toHaveScreenshot("puck-avatar-motion-storyboard.png", { animations: "disabled" });
	});

	test("keeps head, face and eyes on one smooth 720ms compositor timeline", async ({ page }) => {
		await page.emulateMedia({ reducedMotion: "no-preference", colorScheme: "dark" });
		await openPuck(page);
		await page.locator(".puck-conversation > header .puck-avatar").evaluate(source => {
			const stage = document.createElement("section");
			stage.className = "puck-motion-capture-stage";
			Object.assign(stage.style, { position: "fixed", inset: "0", zIndex: "100", display: "grid", placeItems: "center", background: "#0d0d0e" });
			const avatar = source.cloneNode(true) as HTMLElement;
			avatar.classList.add("puck-motion-capture");
			avatar.querySelector(".puck-avatar-badge")?.remove();
			Object.assign(avatar.style, { width: "128px", height: "128px" });
			const properties = { "--puck-motion-duration": "720ms", "--puck-head-x": "0%", "--puck-head-y": "0%", "--puck-head-rotate": "0deg", "--puck-face-x": "4px", "--puck-face-y": "-4px", "--puck-face-rotate": "-24deg", "--puck-left-eye-xscale": "1", "--puck-right-eye-xscale": ".72", "--puck-eye-roundness": "1" };
			Object.entries(properties).forEach(([name, value]) => avatar.style.setProperty(name, value));
			stage.append(avatar); document.querySelector(".puck-workspace")!.append(stage); void avatar.offsetWidth;
		});
		const capture = page.locator(".puck-motion-capture");
		await page.waitForTimeout(32);
		await capture.evaluate(avatar => {
			const properties = { "--puck-head-x": "7.5%", "--puck-head-y": "6.8%", "--puck-head-rotate": "24deg", "--puck-face-x": "11.5px", "--puck-face-y": "10px", "--puck-face-rotate": "32deg", "--puck-left-eye-xscale": "1", "--puck-right-eye-xscale": ".55", "--puck-eye-roundness": "1.7" };
			Object.entries(properties).forEach(([name, value]) => (avatar as HTMLElement).style.setProperty(name, value));
		});
		await page.waitForTimeout(32);
		const timeline = await capture.evaluate(element => {
			const animated = [".puck-avatar-head", ".puck-avatar-face", ".puck-avatar-eye-left", ".puck-avatar-eye-right", ".puck-avatar-eye-shape"];
			const animations = animated.flatMap(selector => Array.from(element.querySelectorAll(selector))).flatMap(node => node.getAnimations());
			const result = animations.map(animation => {
				const timing = animation.effect?.getTiming();
				return { duration: Number(timing?.duration), easing: timing?.easing };
			});
			animations.forEach(animation => animation.pause());
			return result;
		});
		expect(timeline.length).toBeGreaterThanOrEqual(5);
		expect(new Set(timeline.map(item => item.duration))).toEqual(new Set([720]));
		expect(new Set(timeline.map(item => item.easing))).toEqual(new Set(["cubic-bezier(0.45, 0, 0.25, 1)"]));

		const frames: string[] = [];
		for (let time = 0; time <= 720; time += 16) {
			await capture.evaluate((element, currentTime) => element.getAnimations({ subtree: true }).forEach(animation => { animation.pause(); animation.currentTime = currentTime; }), time);
			frames.push(`data:image/png;base64,${(await capture.screenshot({ animations: "allow" })).toString("base64")}`);
		}
		await page.evaluate(frameURLs => {
			document.querySelector(".puck-motion-capture-stage")?.remove();
			const strip = document.createElement("section"); strip.className = "puck-motion-filmstrip";
			Object.assign(strip.style, { position: "fixed", inset: "12px", zIndex: "100", display: "grid", gridTemplateColumns: "repeat(8, 64px)", gap: "8px", width: "fit-content", padding: "16px", borderRadius: "16px", background: "#0d0d0e" });
			frameURLs.forEach((src, index) => { const image = new Image(); image.src = src; image.width = 64; image.height = 64; image.alt = `${index * 16}ms`; strip.append(image); });
			document.querySelector(".puck-workspace")!.append(strip);
		}, frames);
		await expect(page.locator(".puck-motion-filmstrip")).toHaveScreenshot("puck-avatar-motion-frames.png", { animations: "allow" });
	});

	test("selects every avatar property directly and persists mouse and keyboard changes", async ({ page }) => {
		await openPuck(page);
		const shapeGroup = page.getByRole("radiogroup", { name: "头像形状" });
		const colorGroup = page.getByRole("radiogroup", { name: "头像颜色" });
		await expect(shapeGroup.getByRole("radio")).toHaveCount(25);
		await expect(colorGroup.getByRole("radio")).toHaveCount(11);
		expect(await shapeGroup.getByRole("radio").evaluateAll(radios => radios.map(radio => radio.getAttribute("aria-label")))).toEqual(["团块", "卵石", "豆形", "蛋形", "超椭圆", "平板", "胶囊", "圆柱", "六边形", "宝石", "晶体", "楔形", "盾形", "圆顶", "拱形", "云", "水滴", "叶", "方形", "闪光", "四叶", "花", "心", "房子", "星"]);
		await page.getByRole("radio", { name: "星" }).click();
		await expect(page.getByRole("radio", { name: "星" })).toHaveAttribute("aria-checked", "true");
		await expect(page.locator(".puck-avatar-large")).toHaveAttribute("data-shape", "star");
		await page.getByRole("radio", { name: "星" }).press("ArrowRight");
		await expect(page.getByRole("radio", { name: "团块" })).toHaveAttribute("aria-checked", "true");
		await page.getByRole("radio", { name: "蓝" }).press("Enter");
		await expect(page.getByRole("radio", { name: "蓝" })).toHaveAttribute("aria-checked", "true");
		await expect.poll(() => avatarUpdates.at(-1)?.avatar).toEqual({ shape: "blob", color: "blue" });
	});

	test("syncs unread, reactions, editable instructions and a selected-assistant PNG favicon", async ({ page }) => {
		await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "dark" });
		await openPuck(page);
		await expect(page.getByLabel("罗宾有 2 条未读消息")).toHaveText("2");
		await expect(page.getByText("需要你确认研究范围")).toBeVisible();
		const puckIcon = page.locator('link[rel="icon"]');
		await expect(puckIcon).toHaveAttribute("type", "image/png");
		await expect(puckIcon).toHaveAttribute("href", /^data:image\/png;base64,/);
		const firstIcon = await puckIcon.getAttribute("href");
		await page.getByRole("button", { name: /罗宾/ }).click();
		await expect(page.getByLabel("罗宾有 2 条未读消息")).toHaveCount(0);
		await expect.poll(() => puckIcon.getAttribute("href")).not.toBe(firstIcon);
		await page.getByRole("button", { name: "编辑说明" }).click();
		await page.getByLabel("助理说明").fill("先整理来源，再给结论");
		await page.getByRole("button", { name: "保存说明" }).click();
		await expect(page.getByText("先整理来源，再给结论")).toBeVisible();
		await page.getByRole("button", { name: /小帕/ }).click();
		await expect(page.getByText("工作了 13 秒 · 执行 3 步")).toBeVisible();
		await page.getByRole("button", { name: "给消息添加回应" }).first().click();
		await page.getByRole("button", { name: "回应 🎉" }).click();
		await expect(page.getByRole("button", { name: "你回应 🎉，1 个" })).toBeVisible();
		await expect(page).toHaveScreenshot("puck-feature-parity-1280-dark.png", { animations: "disabled", caret: "hide" });
		await page.getByRole("button", { name: "Codex 会话" }).click();
		await expect(puckIcon).toHaveAttribute("type", "image/svg+xml");
		await expect(puckIcon).toHaveAttribute("href", "/brand/ariel-logo-wind-messenger-micro.svg");
	});

	test("shows full access by default and persists switching back to ask", async ({ page }) => {
		await openPuck(page);
		const permission = page.getByLabel("运行权限");
		await expect(permission).toHaveValue("full");
		await expect(page.getByText("可执行任意命令并访问主机文件，不再询问")).toBeVisible();
		await permission.selectOption("ask");
		await expect(permission).toHaveValue("ask");
		await expect(page.getByText("高风险操作会请求确认")).toBeVisible();
		await expect.poll(() => avatarUpdates.at(-1)).toMatchObject({ assistantId: "puck-one", permissionMode: "ask" });
	});
});
