import { expect, test, type Page } from "@playwright/test";

const token = `s_${"b".repeat(64)}`;
const initialAssistants = [
	{ assistantId: "puck-one", name: "小帕", description: "负责日常开发协助", brainDeviceId: "brain-a", status: "ready", unreadCount: 0, lastMessagePreview: "测试已经开始", avatar: JSON.stringify({ shape: "cloud", color: "#ff2d8d" }) },
	{ assistantId: "puck-two", name: "罗宾", description: "整理研究资料", brainDeviceId: "brain-a", status: "ready", unreadCount: 2, lastMessagePreview: "需要你确认研究范围", avatar: JSON.stringify({ shape: "drop", color: "#16c96a" }) },
];
let assistants = initialAssistants;
let avatarUpdates: Record<string, unknown>[] = [];
const initialMessages = [
	{ id: "user-request", role: "user", kind: "text", text: "帮我检查 Ariel 的测试状态。", createdAt: "2026-11-03T11:59:59Z" },
	{ id: "hello", role: "assistant", kind: "text", text: "晚上好。测试已经开始，我会在有结果时告诉你。", workSummary: JSON.stringify({ elapsedMs: 12400, steps: 3 }), createdAt: "2026-11-03T12:00:00Z" },
  { id: "question", role: "assistant", kind: "question", text: "先运行哪组测试？", cardJSON: JSON.stringify({ questionId: "question-a", options: ["Go", "Web"], cardState: "pending" }), createdAt: "2026-11-03T12:00:01Z" },
  { id: "authorization", role: "assistant", kind: "authorization", text: "这次操作需要你的确认。", cardJSON: JSON.stringify({ authorizationId: "authorization-a", action: "start_task", deviceId: "brain-a", cwd: "/work/ariel", expiresAt: "2026-11-03T12:30:00Z", risk: "目标主机无法证明审批策略", cardState: "pending" }), createdAt: "2026-11-03T12:00:02Z" },
  { id: "task", role: "assistant", kind: "task", text: "运行 Ariel 测试", cardJSON: JSON.stringify({ status: "running", deviceId: "brain-a", threadId: "thread-a" }), createdAt: "2026-11-03T12:00:03Z" },
];
let messages = initialMessages;

async function openPuck(page: Page, history: typeof initialMessages = initialMessages) {
  assistants = initialAssistants;
	avatarUpdates = [];
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
    if (message.method === "puck.history") data = { messages };
		if (message.method === "puck.update") {
			avatarUpdates.push(message.params);
			assistants = assistants.map(item => item.assistantId === message.params.assistantId ? { ...item, ...(message.params.avatar ? { avatar: JSON.stringify(message.params.avatar) } : {}), ...(typeof message.params.description === "string" ? { description: message.params.description } : {}) } : item);
			data = { assistant: assistants.find(item => item.assistantId === message.params.assistantId) };
		}
		if (message.method === "puck.read") {
			assistants = assistants.map(item => item.assistantId === message.params.assistantId ? { ...item, unreadCount: 0 } : item);
			data = { unreadCount: 0 };
		}
		if (message.method === "puck.message.react") {
			const reaction = JSON.stringify({ user: message.params.emoji });
			messages = messages.map(item => item.id === message.params.messageId ? { ...item, reaction } : item);
			data = { reaction };
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

for (const width of [390, 1280]) {
  test.describe(`Puck fixed workspace ${width}px`, () => {
    test.use({ viewport: { width, height: 844 }, isMobile: width < 700, hasTouch: width < 700, colorScheme: "dark" });
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

      await transcript.evaluate(element => {
        element.scrollTop = element.scrollHeight;
        element.dispatchEvent(new Event("scroll"));
      });

      await expect(header).toBeInViewport();
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

for (const width of [320, 390, 1280]) for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`Puck workspace ${width}px ${colorScheme}`, () => {
    test.use({ viewport: { width, height: 844 }, isMobile: width < 700, hasTouch: width < 700, colorScheme });
    test("keeps the conversation usable without horizontal overflow", async ({ page }) => {
      await page.emulateMedia({ reducedMotion: "reduce", colorScheme });
      await openPuck(page);
			await expect(page.locator(".puck-workspace")).toHaveCSS("background-color", "rgb(5, 5, 5)");
      expect(await page.locator(".puck-workspace").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
			const assistantContent = page.locator(".puck-message.assistant.text .puck-message-content").first();
			await expect(assistantContent).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
			await expect(assistantContent).toHaveCSS("padding", "0px");
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
			if (width >= 700) await expect(page.locator(".puck-assistant-row").first()).toHaveCSS("height", "56px");
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
			await expect(sendButton).toHaveCSS("background-color", "rgb(255, 255, 255)");
			await expect(sendButton).toHaveCSS("color", "rgb(10, 10, 10)");
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
		await expect(shapeGroup.getByRole("radio")).toHaveCount(14);
		await expect(colorGroup.getByRole("radio")).toHaveCount(11);
		expect(await shapeGroup.getByRole("radio").evaluateAll(radios => radios.map(radio => radio.getAttribute("aria-label")))).toEqual(["圆", "云团", "倾斜圆角方块", "四角星", "四叶花", "心形团", "波浪花", "梨形水滴", "胶囊", "圆角三角", "倾斜圆角矩形", "尖刺星", "圆角六边形", "南瓜"]);
		await page.getByRole("radio", { name: "尖刺星" }).click();
		await expect(page.getByRole("radio", { name: "尖刺星" })).toHaveAttribute("aria-checked", "true");
		await expect(page.locator(".puck-avatar-large")).toHaveAttribute("data-shape", "burst");
		await page.getByRole("radio", { name: "尖刺星" }).press("ArrowRight");
		await expect(page.getByRole("radio", { name: "圆角六边形" })).toHaveAttribute("aria-checked", "true");
		await page.getByRole("radio", { name: "蓝" }).press("Enter");
		await expect(page.getByRole("radio", { name: "蓝" })).toHaveAttribute("aria-checked", "true");
		await expect.poll(() => avatarUpdates.at(-1)?.avatar).toEqual({ shape: "hexagon", color: "#2588f7" });
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
});
