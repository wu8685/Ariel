// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import { avatarShapes, deriveAvatar } from "./PuckAvatar";
import { PuckWorkspace } from "./PuckWorkspace";

const accepted = (data: Record<string, unknown>) => ({ type: "response", v: 1, requestId: crypto.randomUUID(), outcome: "accepted", data } as const);
const rejected = (code: string) => ({ type: "response", v: 1, requestId: crypto.randomUUID(), outcome: "rejected", error: { code, message: code } } as const);
afterEach(() => { cleanup(); localStorage.clear(); vi.useRealTimers(); vi.restoreAllMocks(); });

describe("PuckWorkspace", () => {
	it("reports the server assistant capacity and disables creation at the limit", async () => {
		const assistants = Array.from({ length: 50 }, (_, index) => ({ assistantId: `a-${index}`, name: `Bot ${index}`, brainDeviceId: "brain-a", status: "ready" }));
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants, maxActiveAssistants: 50 })
			: method === "puck.history" ? accepted({ messages: [] }) : accepted({}));
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("Bot 0");
		expect(screen.getByLabelText("助理数量 50/50")).toBeTruthy();
		expect(screen.getByRole("button", { name: "新建助理" }).hasAttribute("disabled")).toBe(true);
	});

	it("restores the last used assistant and falls back when the saved assistant no longer exists", async () => {
		localStorage.setItem("ariel.puck.lastAssistantId.v1", "a-2");
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [
				{ assistantId: "a-1", name: "第一个", brainDeviceId: "brain-a", status: "ready" },
				{ assistantId: "a-2", name: "最近使用", brainDeviceId: "brain-a", status: "ready" },
			] });
			if (method === "puck.history") return accepted({ messages: [{ id: `history-${params.assistantId}`, assistantId: params.assistantId, role: "assistant", kind: "text", text: `history-${params.assistantId}` }] });
			return accepted({});
		});
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("history-a-2");
		expect(view.container.querySelector(".puck-assistant-row.active")?.textContent).toContain("最近使用");
		expect(request).toHaveBeenCalledWith("puck.history", "relay", { assistantId: "a-2" });

		cleanup();
		localStorage.setItem("ariel.puck.lastAssistantId.v1", "deleted");
		const fallback = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("history-a-1");
		expect(fallback.container.querySelector(".puck-assistant-row.active")?.textContent).toContain("第一个");
		expect(localStorage.getItem("ariel.puck.lastAssistantId.v1")).toBe("a-1");
	});

	it("confirms deletion, removes the bot, selects a survivor, and reports failures", async () => {
		let assistants = [
			{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" },
			{ assistantId: "a-2", name: "罗宾", brainDeviceId: "brain-a", status: "ready" },
		];
		localStorage.setItem("ariel.puck.lastAssistantId.v1", "a-2");
		let failDelete = false;
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.delete") {
				if (failDelete) return rejected("BRAIN_UNAVAILABLE");
				assistants = assistants.filter(item => item.assistantId !== params.assistantId);
				return accepted({});
			}
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("罗宾");
		fireEvent.click(screen.getByRole("button", { name: "删除 Bot" }));
		expect(screen.getByRole("dialog", { name: "删除 Bot" }).textContent).toContain("停止该 Bot 的后台大脑");
		expect(screen.getByRole("dialog", { name: "删除 Bot" }).textContent).toContain("历史不再显示");
		fireEvent.click(screen.getByRole("button", { name: "确认删除罗宾" }));
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.delete", "relay", { assistantId: "a-2" }));
		await waitFor(() => expect(screen.queryByText("罗宾")).toBeNull());
		expect(document.querySelector(".puck-assistant-row.active")?.textContent).toContain("小帕");
		expect(localStorage.getItem("ariel.puck.lastAssistantId.v1")).toBe("a-1");

		failDelete = true;
		fireEvent.click(screen.getByRole("button", { name: "删除 Bot" }));
		fireEvent.click(screen.getByRole("button", { name: "确认删除小帕" }));
		expect((await screen.findByRole("alert")).textContent).toContain("助理暂时无法思考");
		expect(screen.getAllByText("小帕").length).toBeGreaterThan(0);
	});

	it("scrolls switched assistants to the latest message and preserves manual history reading", async () => {
		let eventListener: ((event: Record<string, unknown>) => void) | undefined;
		const subscribeEvents = vi.fn((listener: (event: Record<string, unknown>) => void) => { eventListener = listener; return () => undefined; });
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [
				{ assistantId: "a-1", name: "助理一", brainDeviceId: "brain-a", status: "ready" },
				{ assistantId: "a-2", name: "助理二", brainDeviceId: "brain-a", status: "ready" },
			] });
			if (method === "puck.history") return accepted({ messages: [{ id: `latest-${params.assistantId}`, assistantId: params.assistantId, role: "assistant", kind: "text", text: `latest-${params.assistantId}` }] });
			if (method === "puck.watch") return accepted({ watchId: `watch-${params.assistantId}` });
			return accepted({});
		});
		const view = render(<PuckWorkspace request={request} subscribeEvents={subscribeEvents} state="ready" devices={[]} />);
		await screen.findAllByText("latest-a-1");
		const transcript = view.container.querySelector<HTMLElement>(".puck-transcript")!;
		Object.defineProperties(transcript, { scrollHeight: { configurable: true, get: () => 1000 }, clientHeight: { configurable: true, get: () => 200 } });
		fireEvent.click(screen.getAllByText("助理二")[0].closest("button")!);
		await screen.findAllByText("latest-a-2");
		await waitFor(() => expect(transcript.scrollTop).toBe(800));

		transcript.scrollTop = 300;
		fireEvent.scroll(transcript);
		await act(async () => eventListener?.({ event: "puck.event", assistantId: "a-2", kind: "history", payload: { messages: [
			{ id: "latest-a-2", assistantId: "a-2", role: "assistant", kind: "text", text: "latest-a-2" },
			{ id: "new-a-2", assistantId: "a-2", role: "assistant", kind: "text", text: "new-a-2" },
		] } }));
		await screen.findAllByText("new-a-2");
		expect(transcript.scrollTop).toBe(300);
	});

  it("uses only the selected avatar color for the accessible composer send control and updates it immediately", async () => {
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => method === "puck.list"
      ? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", avatar: JSON.stringify({ shape: "cloud", color: "#ff2f92" }) }] })
      : method === "puck.history" ? accepted({ messages: [] })
      : method === "puck.update" ? accepted({ assistant: { assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", avatar: JSON.stringify(params.avatar) } }) : accepted({}));
    const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    await screen.findAllByText("小帕");
    const send = screen.getByRole("button", { name: "发送" });
    expect(send.querySelector("svg")).toBeTruthy();
    const composer = view.container.querySelector<HTMLElement>(".puck-composer")!;
    expect(composer.style.getPropertyValue("--puck-avatar-color")).toBe("#ff2f92");
    fireEvent.click(screen.getByRole("button", { name: "编辑头像" }));
    fireEvent.click(screen.getByRole("radio", { name: "蓝" }));
    await waitFor(() => expect(composer.style.getPropertyValue("--puck-avatar-color")).toBe("#3c82f6"));
    const css = readFileSync("src/puck/puck.css", "utf8");
    expect(css).toMatch(/\.puck-workspace \.puck-send\.primary\s*\{[^}]*display:\s*grid;[^}]*place-items:\s*center;[^}]*background:\s*var\(--puck-avatar-color\);[^}]*color:\s*var\(--puck-send-foreground\)/s);
    expect(css).toMatch(/\.puck-workspace \.puck-send\.primary:disabled\s*\{[^}]*background:\s*[^;]+;[^}]*color:\s*[^;]+;/s);
    expect(css).not.toMatch(/\.puck-workspace \.puck-btn\.primary\s*\{[^}]*background:\s*var\(--puck-avatar-color\)/s);
    expect(view.container.querySelector(".puck-send svg")).toBeTruthy();
  });

  it("adds, removes and sends screenshot-only drafts through puck.message.send", async () => {
    const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
      calls.push({ method, params });
      if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] });
      if (method === "puck.history") return accepted({ messages: [] });
      return accepted({ state: "processing" });
    });
    render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    await screen.findAllByText("小帕");
    expect(screen.getByRole("button", { name: "选择截图" })).toBeTruthy();
    const input = screen.getByLabelText("附加截图") as HTMLInputElement;
    const image = new File([new Uint8Array([137, 80, 78, 71])], "puck.png", { type: "image/png" });
    fireEvent.change(input, { target: { files: [image] } });
    expect(await screen.findByRole("img", { name: "puck.png" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "移除截图：puck.png" }));
    expect(screen.queryByRole("img", { name: "puck.png" })).toBeNull();
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(input, { target: { files: [image] } });
    await screen.findByRole("img", { name: "puck.png" });
    const send = screen.getByRole("button", { name: "发送" });
    expect((send as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(send);
    await waitFor(() => expect(calls.filter(call => call.method === "puck.message.send")).toHaveLength(1));
    const sent = calls.find(call => call.method === "puck.message.send")!.params;
    expect(sent.text).toBe("");
    expect(sent.images).toEqual([expect.stringMatching(/^data:image\/png;base64,/) ]);
    await waitFor(() => expect(screen.queryByLabelText("待发送截图")).toBeNull());
  });

	it("replies to a specific message, sends replyTo, renders the quote and jumps to the source", async () => {
		const messages = [
			{ id: "assistant-source", assistantId: "a-1", role: "assistant", kind: "text", text: "先检查失败测试", createdAt: "2026-11-08T08:00:00Z" },
			{ id: "task-card", assistantId: "a-1", role: "assistant", kind: "task", text: "Ariel 测试", cardJSON: JSON.stringify({ taskId: "task-a", status: "running" }) },
		];
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages })
			: method === "puck.message.send" ? accepted({ state: "processing", startedAt: "2026-11-08T08:01:00Z" }) : accepted({}));
		const scrollIntoView = vi.fn();
		Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scrollIntoView });
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findByText("先检查失败测试");
		fireEvent.click(screen.getByRole("button", { name: "回复助理消息：先检查失败测试" }));
		expect(screen.getByLabelText("正在回复助理消息").textContent).toContain("先检查失败测试");
		fireEvent.change(screen.getByLabelText("给 Puck 发消息"), { target: { value: "继续" } });
		fireEvent.click(screen.getByRole("button", { name: "发送" }));
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.message.send", "relay", expect.objectContaining({ replyTo: "assistant-source", text: "继续" })));
		const quote = await screen.findByRole("button", { name: "跳到被引用的消息" });
		expect(quote.textContent).toContain("先检查失败测试");
		fireEvent.click(quote);
		expect(scrollIntoView).toHaveBeenCalled();
		expect(view.container.querySelector('[data-message-id="assistant-source"]')?.classList.contains("reply-highlight")).toBe(true);
	});

	it("cancels a reply, clears it on assistant switch, supports touch swipe, and loads a missing source on demand", async () => {
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [
				{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" },
				{ assistantId: "a-2", name: "罗宾", brainDeviceId: "brain-a", status: "ready" },
			] });
			if (method === "puck.history") return accepted({ messages: params.assistantId === "a-1" ? [
				{ id: "swipe-source", assistantId: "a-1", role: "user", kind: "text", text: "移动端原消息" },
				{ id: "reply-with-missing-source", assistantId: "a-1", role: "assistant", kind: "text", text: "已有回复", replyTo: "not-loaded" },
			] : [] });
			if (method === "puck.message.get") return accepted({ message: { id: "not-loaded", assistantId: "a-1", role: "assistant", kind: "text", text: "按需加载的原消息" } });
			return accepted({});
		});
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findByText("移动端原消息");
		const source = view.container.querySelector<HTMLElement>('[data-message-id="swipe-source"]')!;
		fireEvent.pointerDown(source, { pointerType: "touch", clientX: 220, clientY: 20 });
		fireEvent.pointerUp(source, { pointerType: "touch", clientX: 130, clientY: 22 });
		expect(screen.getByLabelText("正在回复你的消息").textContent).toContain("移动端原消息");
		fireEvent.click(screen.getByRole("button", { name: "取消回复" }));
		expect(screen.queryByLabelText("正在回复你的消息")).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "回复你的消息：移动端原消息" }));
		fireEvent.click(screen.getAllByText("罗宾")[0].closest("button")!);
		await waitFor(() => expect(screen.queryByLabelText("正在回复你的消息")).toBeNull());
		fireEvent.click(screen.getAllByText("小帕")[0].closest("button")!);
		const missingQuote = await screen.findByRole("button", { name: "加载并跳到被引用的消息" });
		fireEvent.click(missingQuote);
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.message.get", "relay", { assistantId: "a-1", messageId: "not-loaded" }));
		expect((await screen.findAllByText("按需加载的原消息")).length).toBeGreaterThan(0);
	});

  it("never displays a raw error code and restores a failed send for explicit retry", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [] })
			: method === "puck.message.send" ? rejected("RESYNC_REQUIRED") : accepted({}));
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("小帕");
		const composer = screen.getByLabelText("给 Puck 发消息") as HTMLTextAreaElement;
		fireEvent.change(composer, { target: { value: "继续刚才的问题" } });
		fireEvent.click(screen.getByRole("button", { name: "发送" }));
		expect((await screen.findAllByText("实时连接暂时失效，请重试。")).length).toBeGreaterThan(0);
		expect(screen.queryByText("RESYNC_REQUIRED")).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "重试发送" }));
		expect(composer.value).toBe("继续刚才的问题");
	});

  it("opens an assistant whose question card contains structured options without crashing the workspace", async () => {
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
      if (method === "puck.list") return accepted({ assistants: [
        { assistantId: "safe", name: "小帕", brainDeviceId: "brain-a", status: "ready" },
        { assistantId: "mindpowers", name: "MindPowers 研发", brainDeviceId: "brain-a", status: "ready" },
      ] });
      if (method === "puck.history") return accepted({ messages: params.assistantId === "mindpowers" ? [{
        id: "question-card:rag-scope",
        assistantId: "mindpowers",
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
      }] : [] });
      return accepted({});
    });

    render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    await screen.findAllByText("小帕");
    fireEvent.click(screen.getAllByText("MindPowers 研发")[0].closest("button")!);

    expect(await screen.findByRole("button", { name: /RAG 全链路（推荐）/ })).toBeTruthy();
    expect(screen.getByText("覆盖 RAG/VDB、知识库导入/分析、检索验证和相关部署。")).toBeTruthy();
    expect(document.querySelector(".puck-assistants")).toBeTruthy();
  });

  it("keeps the assistant list visible and shows a local error when one conversation cannot render", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const request = vi.fn(async (method: string) => method === "puck.list"
      ? accepted({ assistants: [
          { assistantId: "broken", name: "损坏历史", brainDeviceId: "brain-a", status: "ready" },
          { assistantId: "safe", name: "正常助理", brainDeviceId: "brain-a", status: "ready" },
        ] })
      : method === "puck.history" ? accepted({ messages: [{
          id: "bad-reaction", role: "assistant", kind: "text", text: "仍应保留导航", reaction: JSON.stringify({ assistant: { invalid: true } }),
        }] }) : accepted({}));

    render(<PuckWorkspace request={request} state="ready" devices={[]} />);

    expect((await screen.findByRole("alert")).textContent).toContain("这段对话暂时无法显示");
    expect(screen.getAllByText("损坏历史").length).toBeGreaterThan(0);
    expect(screen.getAllByText("正常助理").length).toBeGreaterThan(0);
    expect(document.querySelector(".puck-workspace")).toBeTruthy();
  });

  it("ignores a previous assistant's late polling history after switching assistants", async () => {
    vi.useFakeTimers();
    let resolveLate: ((value: ReturnType<typeof accepted>) => void) | undefined;
    const calls = new Map<string, number>();
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
      if (method === "puck.list") return accepted({ assistants: [
        { assistantId: "a-1", name: "旧助理", brainDeviceId: "brain-a", status: "ready" },
        { assistantId: "a-2", name: "新助理", brainDeviceId: "brain-a", status: "ready" },
      ] });
      if (method === "puck.history") {
        const id = String(params.assistantId);
        const count = (calls.get(id) || 0) + 1;
        calls.set(id, count);
        if (id === "a-1" && count > 1) return new Promise<ReturnType<typeof accepted>>(resolve => { resolveLate = resolve; });
        return accepted({ messages: id === "a-1"
          ? [
              { id: "old", assistantId: "a-1", role: "assistant", kind: "text", text: "旧助理历史" },
              { id: "old-user", assistantId: "a-1", role: "user", kind: "text", text: "旧助理用户消息" },
            ]
          : [{ id: "new", assistantId: "a-2", role: "assistant", kind: "text", text: "新助理问候" }] });
      }
      return accepted({});
    });
    render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(screen.getAllByText("旧助理历史").length).toBeGreaterThan(0);
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    fireEvent.click(screen.getAllByText("新助理")[0].closest("button")!);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(screen.getAllByText("新助理问候").length).toBeGreaterThan(0);
    expect(document.querySelector(".puck-transcript")?.textContent).not.toContain("旧助理用户消息");
    await act(async () => { resolveLate?.(accepted({ messages: [
      { id: "leaked-1", assistantId: "a-1", role: "user", kind: "text", text: "看下目前你能看到哪几个主机？" },
      { id: "leaked-2", assistantId: "a-1", role: "user", kind: "text", text: "看下那本书的总结完成了吗" },
    ] })); await Promise.resolve(); });
    expect(screen.queryByText("看下目前你能看到哪几个主机？")).toBeNull();
    expect(screen.queryByText("看下那本书的总结完成了吗")).toBeNull();
    expect(screen.getAllByText("新助理问候").length).toBeGreaterThan(0);
  });

  it("starts a newly created assistant with an isolated conversation and drops the previous assistant's late history", async () => {
    vi.useFakeTimers();
    let assistants: Record<string, unknown>[] = [{ assistantId: "old", name: "旧助理", brainDeviceId: "brain-a", status: "ready" }];
    let oldHistoryCalls = 0;
    let resolveLate: ((value: ReturnType<typeof accepted>) => void) | undefined;
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
      if (method === "puck.list") return accepted({ assistants });
      if (method === "puck.history") {
        if (params.assistantId === "old") {
          oldHistoryCalls += 1;
          if (oldHistoryCalls > 1) return new Promise<ReturnType<typeof accepted>>(resolve => { resolveLate = resolve; });
          return accepted({ messages: [{ id: "old-user", assistantId: "old", role: "user", kind: "text", text: "只属于旧助理" }] });
        }
        return accepted({ messages: [] });
      }
      if (method === "puck.create") {
        const assistant = { assistantId: "new", name: params.name, brainDeviceId: "brain-a", status: "ready" };
        assistants = [...assistants, assistant];
        return accepted({ assistant, greeting: { state: "processing", messageId: "new-greeting", startedAt: "2026-11-05T00:00:00Z" } });
      }
      return accepted({});
    });

    render(<PuckWorkspace request={request} state="ready" devices={[{ deviceId: "brain-a", name: "Mac mini", capabilities: { puckBrainReady: true } }]} />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(document.querySelector(".puck-transcript")?.textContent).toContain("只属于旧助理");
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    fireEvent.click(screen.getByRole("button", { name: "新建助理" }));
    fireEvent.change(screen.getByLabelText("助理名称"), { target: { value: "个人心得" } });
    fireEvent.click(screen.getByRole("button", { name: "创建" }));
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });

    expect(document.querySelector(".puck-transcript")?.textContent).not.toContain("只属于旧助理");
    expect(screen.getAllByText("正在准备…").length).toBeGreaterThan(0);
    await act(async () => {
      resolveLate?.(accepted({ messages: [{ id: "late-old", role: "user", kind: "text", text: "旧轮询晚到的用户消息" }] }));
      await Promise.resolve();
    });
    expect(screen.queryByText("旧轮询晚到的用户消息")).toBeNull();
    expect(screen.getAllByText("正在准备…").length).toBeGreaterThan(0);
  });

  it("shows an avatar only on an active processing row, never on completed assistant history", async () => {
    const request = vi.fn(async (method: string) => method === "puck.list"
      ? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
      : method === "puck.history" ? accepted({ messages: [
        { id: "done", role: "assistant", kind: "text", text: "已经完成的答复" },
        { id: "working", role: "assistant", kind: "processing", text: "正在处理…" },
      ] }) : accepted({}));
    const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    await screen.findByText("已经完成的答复");
    const completed = screen.getByText("已经完成的答复").closest(".puck-message");
    const processing = screen.getAllByText("正在处理…").map(node => node.closest(".puck-message")).find(Boolean);
    expect(completed).toBeTruthy();
    expect(processing).toBeTruthy();
    expect(completed!.querySelector(".puck-avatar")).toBeNull();
    expect(processing!.querySelector(".puck-avatar")).toBeTruthy();
  });

  it("creates an assistant and shows an immediate processing bubble when sending", async () => {
    let assistants: Record<string, unknown>[] = [];
    let resolveHistory: (() => void) | undefined;
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
      if (method === "puck.list") return accepted({ assistants });
      if (method === "puck.create") {
        const assistant = { assistantId: "a-1", name: params.name, description: "", brainDeviceId: "brain-a", status: "ready" };
        assistants = [assistant];
        return accepted({ assistant });
      }
      if (method === "puck.history") return new Promise<ReturnType<typeof accepted>>(resolve => {
        resolveHistory = () => resolve(accepted({ messages: [] }));
      });
      if (method === "puck.message.send") return accepted({ state: "processing" });
      return accepted({});
    });
    render(<PuckWorkspace request={request} state="ready" devices={[{ deviceId: "brain-a", name: "Mac mini", capabilities: { puckBrainReady: true } }]} />);
	await screen.findByText("创建你的第一个助理");
	fireEvent.click(screen.getAllByRole("button", { name: "新建助理" }).at(-1)!);
    const nameInput = screen.getByLabelText("助理名称");
    fireEvent.change(nameInput, { target: { value: "小帕" } });
    fireEvent.compositionStart(nameInput);
    fireEvent.keyDown(nameInput, { key: "Enter" });
    fireEvent.compositionEnd(nameInput);
    fireEvent.submit(nameInput.closest("form")!);
    expect(request.mock.calls.filter(([method]) => method === "puck.create")).toHaveLength(0);
    await act(async () => { await new Promise(resolve => window.setTimeout(resolve, 60)); });
    fireEvent.submit(nameInput.closest("form")!);
	await screen.findAllByText("小帕");
    fireEvent.change(screen.getByLabelText("给 Puck 发消息"), { target: { value: "检查测试" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    expect((await screen.findAllByText("正在处理…")).length).toBeGreaterThan(0);
    expect(screen.getByText("检查测试")).toBeTruthy();
    await act(async () => { resolveHistory?.(); });
    expect(screen.getByText("检查测试")).toBeTruthy();
    expect(screen.getAllByText("正在处理…").length).toBeGreaterThan(0);
  });

  it("renders question, authorization and task cards and can stop work", async () => {
    const messages = [
      { id: "q", role: "assistant", kind: "question", text: "部署到哪里？", cardJSON: JSON.stringify({ options: ["测试", "生产"] }) },
      { id: "z", role: "assistant", kind: "authorization", text: "需要授权", cardJSON: JSON.stringify({ risk: "会写入文件" }) },
      { id: "t", role: "assistant", kind: "task", text: "运行测试", cardJSON: JSON.stringify({ status: "running", deviceId: "exec-a", threadId: "thread-a" }) },
    ];
    const request = vi.fn(async (method: string) => method === "puck.list"
      ? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
      : method === "puck.history" ? accepted({ messages }) : accepted({ summary: "已请求停止 1 项", message: { id: "stop", role: "assistant", kind: "stop_summary", text: "已请求停止 1 项" } }));
    const onOpenThread = vi.fn();
    const view = render(<PuckWorkspace request={request} state="ready" devices={[]} onOpenThread={onOpenThread} />);
    expect(await screen.findByRole("group", { name: "问题卡片" })).toBeTruthy();
    expect(screen.getByRole("group", { name: "授权卡片" })).toBeTruthy();
		expect(screen.getByRole("button", { name: "以后都允许" })).toBeTruthy();
		expect(screen.getByRole("group", { name: "授权卡片" }).textContent).not.toContain("未说明");
    expect(screen.getByRole("group", { name: "任务卡片" })).toBeTruthy();
	expect(view.container.querySelectorAll(".puck-transcript .puck-avatar")).toHaveLength(0);
	expect(view.container.querySelectorAll(".puck-message.assistant.continued")).toHaveLength(2);
	await act(async () => { fireEvent.click(screen.getByRole("button", { name: "停止全部工作" })); });
    expect((await screen.findAllByText("已请求停止 1 项")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "打开会话" }));
    expect(onOpenThread).toHaveBeenCalledWith("exec-a", "thread-a");
  });

	it("persists approve-always decisions, renders their terminal label, and revokes saved rules", async () => {
		let messages = [{ id: "z", role: "assistant", kind: "authorization", text: "运行测试", cardJSON: JSON.stringify({ authorizationId: "auth-a", cardState: "pending", deviceId: "exec-a", cwd: "/work" }) }];
		let rules = [{ id: "rule-a", assistantId: "a-1", actionType: "start_task", deviceId: "exec-a", cwd: "/work", createdAt: "2026-11-08T10:00:00Z" }];
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", permissionMode: "ask" }] });
			if (method === "puck.history") return accepted({ messages });
			if (method === "puck.authorization.rule.list") return accepted({ rules });
			if (method === "puck.authorization.decide") { messages = messages.map(message => ({ ...message, cardJSON: JSON.stringify({ ...JSON.parse(message.cardJSON), cardState: "consumed", decision: params.decision }) })); return accepted({ state: "consumed" }); }
			if (method === "puck.authorization.rule.revoke") { rules = rules.filter(rule => rule.id !== params.ruleId); return accepted({}); }
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findByText("start_task");
		fireEvent.click(screen.getByRole("button", { name: "以后都允许" }));
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.authorization.decide", "relay", { assistantId: "a-1", authorizationId: "auth-a", decision: "approve_always" }));
		expect(await screen.findByText("已允许（以后都允许）")).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: "撤销规则 start_task /work" }));
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.authorization.rule.revoke", "relay", { assistantId: "a-1", ruleId: "rule-a" }));
		expect(await screen.findByText("尚未保存规则")).toBeTruthy();
	});

  it("explains unavailable deployment prerequisites", async () => {
    render(<PuckWorkspace request={vi.fn()} state="unavailable" reason="PUCK_REQUIRES_PASSKEY_HTTPS" devices={[]} />);
    expect(screen.getByText("Puck 仅支持 Passkey + HTTPS")).toBeTruthy();
  });

  it("submits a card once, updates it in place, renders Markdown and sends with Enter", async () => {
    const calls: string[] = [];
    const request = vi.fn(async (method: string) => {
      calls.push(method);
      if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] });
      if (method === "puck.history") return accepted({ messages: [
        { id: "md", role: "assistant", kind: "text", text: "**测试通过**" },
        { id: "q", role: "assistant", kind: "question", text: "选一个", cardJSON: JSON.stringify({ questionId: "q-1", options: ["A"], cardState: "pending" }) },
      ] });
      return accepted({ state: "processing" });
    });
    render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    expect(await screen.findByText("测试通过", { selector: "strong" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "A" }));
    expect(await screen.findByText("已回答")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "A" })).toBeNull();
    expect(calls.filter(method => method === "puck.question.answer")).toHaveLength(1);
    const textarea = screen.getByLabelText("给 Puck 发消息");
    fireEvent.change(textarea, { target: { value: "继续" } });
    fireEvent.keyDown(textarea, { key: "Enter", keyCode: 229, which: 229 });
    fireEvent.compositionStart(textarea);
    fireEvent.keyDown(textarea, { key: "Enter" });
    fireEvent.compositionEnd(textarea);
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(calls.filter(method => method === "puck.message.send")).toHaveLength(0);
    await act(async () => { await new Promise(resolve => window.setTimeout(resolve, 60)); });
    fireEvent.keyDown(textarea, { key: "Enter", shiftKey: false });
    expect(calls.filter(method => method === "puck.message.send")).toHaveLength(1);
  });

	it("normalizes historical questions JSON, supports multi-select and custom answers, and keeps ids", async () => {
		let answerParams: Record<string, unknown> | undefined;
		const raw = JSON.stringify({ questions: [
			{ id: "entry", question: "从哪里进入？", options: [{ label: "Codex Desktop", description: "桌面应用", value: "desktop" }] },
			{ id: "features", question: "需要哪些能力？", options: [{ label: "浏览器", value: "browser" }, { label: "文件", value: "files" }], multiSelect: true, allowCustom: true },
		] });
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] });
			if (method === "puck.history") return accepted({ messages: [{ id: "question-card:legacy", role: "assistant", kind: "question", text: raw, cardJSON: raw }] });
			if (method === "puck.question.answer") answerParams = params;
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		expect(await screen.findByText("从哪里进入？")).toBeTruthy();
		expect(screen.queryByText(raw)).toBeNull();
		expect(screen.getByText("桌面应用")).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: /Codex Desktop/ }));
		fireEvent.click(screen.getByRole("button", { name: "浏览器" }));
		fireEvent.click(screen.getByRole("button", { name: "文件" }));
		const custom = screen.getByRole("textbox", { name: "自定义回答：需要哪些能力？" });
		fireEvent.change(custom, { target: { value: "终端" } });
		fireEvent.compositionStart(custom);
		fireEvent.keyDown(custom, { key: "Enter" });
		fireEvent.compositionEnd(custom);
		fireEvent.keyDown(custom, { key: "Enter" });
		expect(answerParams).toBeUndefined();
		fireEvent.click(screen.getByRole("button", { name: "提交回答" }));
		await waitFor(() => expect(answerParams).toBeTruthy());
		expect(answerParams).toMatchObject({ questionId: "legacy", answers: [
			{ questionId: "entry", values: ["desktop"] },
			{ questionId: "features", values: ["browser", "files"], custom: "终端" },
		] });
		expect(await screen.findByText("已回答")).toBeTruthy();
	});

	it("never renders malformed structured JSON as question text", async () => {
		const raw = `{"questions":[}`;
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [{ id: "bad", role: "assistant", kind: "question", text: raw, cardJSON: raw }] }) : accepted({}));
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		expect(await screen.findByText("助手发来了一个无法识别的问题")).toBeTruthy();
		expect(screen.queryByText(raw)).toBeNull();
	});

	it("renders second-rejection fallback cards as friendly terminal notices", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [
				{ id: "q", role: "assistant", kind: "question", text: "暂时无法生成问题卡，请直接发送消息回答。", cardJSON: JSON.stringify({ cardState: "invalid", tool: "ask_user" }) },
				{ id: "a", role: "assistant", kind: "authorization", text: "暂时无法生成授权卡；尚未授权任何操作，请直接说明是否继续。", cardJSON: JSON.stringify({ cardState: "invalid", tool: "request_authorization" }) },
				{ id: "r", role: "assistant", kind: "share_result", text: "暂时无法生成结果卡，请让助理重新整理结果。", cardJSON: JSON.stringify({ cardState: "invalid", tool: "share_result" }) },
			] }) : accepted({}));
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		expect(await screen.findByText("暂时无法生成问题卡，请直接发送消息回答。")).toBeTruthy();
		expect(screen.getByText("暂时无法生成授权卡；尚未授权任何操作，请直接说明是否继续。")).toBeTruthy();
		expect(screen.getAllByText("暂时无法生成结果卡，请让助理重新整理结果。").length).toBeGreaterThan(0);
		expect(screen.queryByRole("button", { name: "仅这一次允许" })).toBeNull();
		expect(screen.queryByText(/"cardState"/)).toBeNull();
	});

	it("isolates Puck message typography from Ariel globals while retaining the Ariel bubble colors", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [
				{ id: "u", role: "user", kind: "text", text: "用户消息", cardJSON: JSON.stringify({ images: ["data:image/png;base64,AAAA"] }) },
				{ id: "a", role: "assistant", kind: "text", text: "助理消息" },
			] }) : accepted({}));
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findByText("用户消息");
		expect(view.container.querySelector(".puck-message.user > .puck-message-content")).toBeTruthy();
		expect(view.container.querySelector(".puck-message.assistant > .puck-message-content")).toBeTruthy();
		expect(view.container.querySelector(".puck-message.user .puck-markdown")).toBeTruthy();
		expect(view.container.querySelector<HTMLImageElement>(".puck-message.user .puck-message-image")?.src).toContain("data:image/png;base64,AAAA");
		expect(view.container.querySelector(".puck-message.message, .puck-message .message-body, .puck-message .message-text, .puck-message .markdown-body")).toBeNull();
	});

	it("collapses terminal card actions and omits attention badges from the information hero", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [
				{ id: "q", role: "assistant", kind: "question", text: "选一个", cardJSON: JSON.stringify({ options: ["A", "B"], cardState: "answered", answer: "A" }) },
				{ id: "z", role: "assistant", kind: "authorization", text: "运行测试", cardJSON: JSON.stringify({ cardState: "consumed", decision: "approve_once", decidedAt: "2026-11-04T01:32:00Z" }) },
			] }) : accepted({}));
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		expect(await screen.findByText("已回答")).toBeTruthy();
		expect(screen.queryByRole("button", { name: "A" })).toBeNull();
		expect(screen.queryByRole("button", { name: "B" })).toBeNull();
		expect(screen.queryByRole("button", { name: "拒绝" })).toBeNull();
		expect(screen.queryByRole("button", { name: "仅这一次允许" })).toBeNull();
		expect(screen.getByText(/你选择了/).textContent).toContain("A");
		expect(screen.getByText(/已允许一次/)).toBeTruthy();
		expect(view.container.querySelector(".puck-info .puck-avatar-large .puck-avatar-badge")).toBeNull();
	});

	it("collapses an expired pending authorization into a result without decision buttons", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [
				{ id: "z", role: "assistant", kind: "authorization", text: "运行测试", cardJSON: JSON.stringify({ authorizationId: "expired", cardState: "pending", expiresAt: "2020-01-01T00:00:00Z" }) },
			] }) : accepted({}));
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		expect(await screen.findByText("已过期")).toBeTruthy();
		expect(screen.queryByRole("button", { name: "拒绝" })).toBeNull();
		expect(screen.queryByRole("button", { name: "仅这一次允许" })).toBeNull();
	});

	it("keeps a pending authorization expiry fresh while no work is running", async () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-11-09T00:00:00Z"));
		const expiresAt = new Date(Date.now() + 2_000).toISOString();
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [{ id: "auth", role: "assistant", kind: "authorization", text: "运行测试", cardJSON: JSON.stringify({ authorizationId: "auth", cardState: "pending", expiresAt }) }] })
			: method === "puck.watch" ? accepted({ watchId: "watch-a" }) : accepted({}));
		render(<PuckWorkspace request={request} subscribeEvents={() => () => undefined} state="ready" devices={[]} />);
		await act(async () => { await vi.advanceTimersByTimeAsync(0); });
		expect(screen.getByText("1 分钟后过期")).toBeTruthy();
		await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
		expect(screen.getByText("授权已过期")).toBeTruthy();
		expect(screen.queryByRole("button", { name: "仅这一次允许" })).toBeNull();
	});

  it("uses puck.watch events for durable conversation updates and unwatches on cleanup", async () => {
    let listener: ((event: Record<string, unknown>) => void) | undefined;
    const subscribeEvents = vi.fn((next: (event: Record<string, unknown>) => void) => { listener = next; return vi.fn(); });
    const request = vi.fn(async (method: string) => {
      if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] });
      if (method === "puck.history") return accepted({ messages: [] });
      if (method === "puck.watch") return accepted({ watchId: "watch-a" });
      return accepted({});
    });
    const view = render(<PuckWorkspace request={request} subscribeEvents={subscribeEvents} state="ready" devices={[]} />);
    await screen.findAllByText("小帕");
    await waitFor(() => expect(subscribeEvents).toHaveBeenCalledTimes(1));
    await act(async () => {
      listener?.({ type: "event", v: 1, event: "puck.event", assistantId: "a-1", seq: 1, kind: "history", payload: { messages: [{ id: "durable-a", role: "assistant", kind: "text", text: "增量更新已落库" }] } });
    });
    expect(screen.getAllByText("增量更新已落库").length).toBeGreaterThan(0);
    view.unmount();
    await act(async () => undefined);
    expect(request).toHaveBeenCalledWith("puck.unwatch", "relay", { watchId: "watch-a" });
  });

  it("reduces full polling while puck.watch is healthy and restores fallback intervals on failure", async () => {
    vi.useFakeTimers();
    let watchHealthy = true;
    const subscribeEvents = vi.fn(() => vi.fn());
    const request = vi.fn(async (method: string) => {
      if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] });
      if (method === "puck.history") return accepted({ messages: [] });
      if (method === "puck.watch") return watchHealthy ? accepted({ watchId: "watch-a" }) : rejected("PUCK_UNAVAILABLE");
      return accepted({});
    });
    const view = render(<PuckWorkspace request={request} subscribeEvents={subscribeEvents} state="ready" devices={[]} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(request.mock.calls.filter(call => call[0] === "puck.watch")).toHaveLength(1);
    const initialLists = request.mock.calls.filter(call => call[0] === "puck.list").length;
    const initialHistory = request.mock.calls.filter(call => call[0] === "puck.history").length;
    await act(async () => { await vi.advanceTimersByTimeAsync(59_000); });
    expect(request.mock.calls.filter(call => call[0] === "puck.list")).toHaveLength(initialLists);
    expect(request.mock.calls.filter(call => call[0] === "puck.history")).toHaveLength(initialHistory);
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000); });
    expect(request.mock.calls.filter(call => call[0] === "puck.list").length).toBeGreaterThan(initialLists);
    expect(request.mock.calls.filter(call => call[0] === "puck.history").length).toBeGreaterThan(initialHistory);

    view.unmount();
    watchHealthy = false;
    request.mockClear();
    render(<PuckWorkspace request={request} subscribeEvents={subscribeEvents} state="ready" devices={[]} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    const failedLists = request.mock.calls.filter(call => call[0] === "puck.list").length;
    const failedHistory = request.mock.calls.filter(call => call[0] === "puck.history").length;
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000); });
    expect(request.mock.calls.filter(call => call[0] === "puck.list").length).toBeGreaterThanOrEqual(failedLists + 2);
    expect(request.mock.calls.filter(call => call[0] === "puck.history").length).toBeGreaterThan(failedHistory);
  });

	it("renders direct accessible avatar choices and persists click and keyboard selection", async () => {
		let assistant = { assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", avatar: JSON.stringify({ shape: "circle", color: "#8957f5", image: "https://cdn.example.test/bot.png" }) };
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [assistant] });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.update") {
				assistant = { ...assistant, avatar: JSON.stringify(params.avatar) };
				return accepted({ assistant });
			}
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("小帕");
		fireEvent.click(screen.getByRole("button", { name: "编辑头像" }));
		const shapes = screen.getByRole("radiogroup", { name: "头像形状" });
		const colors = screen.getByRole("radiogroup", { name: "头像颜色" });
		expect(shapes.querySelectorAll('[role="radio"]')).toHaveLength(13);
		expect(colors.querySelectorAll('[role="radio"]')).toHaveLength(11);
		expect(Array.from(shapes.querySelectorAll(".puck-shape-row")).map(row => row.querySelectorAll('[role="radio"]').length)).toEqual([4, 4, 5]);
		expect(Array.from(colors.querySelectorAll(".puck-color-row")).map(row => row.querySelectorAll('[role="radio"]').length)).toEqual([6, 5]);
		expect(screen.getByRole("tab", { name: "生成" })).toBeTruthy();
		expect(screen.getByRole("tab", { name: "上传" })).toBeTruthy();
		expect(screen.getByRole("button", { name: "重置为默认" })).toBeTruthy();
		expect(screen.getByRole("radio", { name: "团块" }).getAttribute("aria-checked")).toBe("true");
		expect(screen.getByRole("radio", { name: "紫" }).getAttribute("aria-checked")).toBe("true");
		expect(document.querySelector(".puck-avatar-large img")?.getAttribute("src")).toBe("https://cdn.example.test/bot.png");
		expect(screen.queryByRole("combobox", { name: "头像形状" })).toBeNull();
		expect(screen.queryByLabelText("头像颜色", { selector: 'input[type="color"]' })).toBeNull();

		fireEvent.click(screen.getByRole("radio", { name: "星" }));
		await waitFor(() => expect(screen.getByRole("radio", { name: "星" }).getAttribute("aria-checked")).toBe("true"));
		expect(document.querySelector(".puck-avatar-large")?.getAttribute("data-shape")).toBe("star");
		expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-1", avatar: { shape: "star", color: "violet", image: "https://cdn.example.test/bot.png" } });
		fireEvent.keyDown(screen.getByRole("radio", { name: "星" }), { key: "ArrowRight" });
		await waitFor(() => expect(screen.getByRole("radio", { name: "六边形" }).getAttribute("aria-checked")).toBe("true"));
		fireEvent.keyDown(screen.getByRole("radio", { name: "六边形" }), { key: "ArrowRight" });
		await waitFor(() => expect(screen.getByRole("radio", { name: "团块" }).getAttribute("aria-checked")).toBe("true"));
		fireEvent.click(screen.getByRole("radio", { name: "蓝" }));
		await waitFor(() => expect(screen.getByRole("radio", { name: "蓝" }).getAttribute("aria-checked")).toBe("true"));
		expect((document.querySelector(".puck-avatar-large") as HTMLElement).style.getPropertyValue("--puck-avatar-color")).toBe("#3c82f6");
		expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-1", avatar: { shape: "blob", color: "blue", image: "https://cdn.example.test/bot.png" } });
	});

	it("resets to the derived avatar, uploads a photo, and does not generate one", async () => {
		let assistant = { assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", avatar: JSON.stringify({ shape: "star", color: "green", image: "https://cdn.example.test/bot.png" }) };
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [assistant] });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.update") {
				assistant = { ...assistant, avatar: JSON.stringify(params.avatar) };
				return accepted({ assistant });
			}
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("小帕");
		fireEvent.click(screen.getByRole("button", { name: "编辑头像" }));
		const fallback = deriveAvatar("a-1");
		fireEvent.click(screen.getByRole("button", { name: "重置为默认" }));
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-1", avatar: { shape: fallback.shape, color: fallback.color } }));
		expect(document.querySelector(".puck-avatar-large img")).toBeNull();
		const fallbackLabel = avatarShapes.find(option => option.value === fallback.shape)?.label || "";
		expect(screen.getByRole("radio", { name: fallbackLabel }).getAttribute("aria-checked")).toBe("true");

		const updatesBeforeGenerate = request.mock.calls.filter(call => call[0] === "puck.update").length;
		fireEvent.click(screen.getByRole("tab", { name: "生成" }));
		expect(screen.getByText("头像生成还没有接到 Puck。这里不会发起生成请求。")).toBeTruthy();
		expect(request.mock.calls.filter(call => call[0] === "puck.update")).toHaveLength(updatesBeforeGenerate);

		fireEvent.click(screen.getByRole("tab", { name: "上传" }));
		const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="), char => char.charCodeAt(0));
		fireEvent.change(screen.getByLabelText("上传头像图片"), { target: { files: [new File([png], "avatar.png", { type: "image/png" })] } });
		await waitFor(() => expect(request.mock.calls.some(call => call[0] === "puck.update" && String((call[2] as { avatar?: { image?: string } }).avatar?.image || "").startsWith("data:image/png;base64,"))).toBe(true));
		expect(document.querySelector(".puck-avatar-large img")?.getAttribute("src")).toMatch(/^data:image\/png;base64,/);
		fireEvent.change(screen.getByLabelText("上传头像图片"), { target: { files: [new File(["<svg/>"], "avatar.svg", { type: "image/svg+xml" })] } });
		expect((await screen.findByRole("alert")).textContent).toContain("仅支持 PNG、JPEG 或 WebP。");
		fireEvent.keyDown(window, { key: "Escape" });
		await waitFor(() => expect(screen.queryByRole("dialog", { name: "头像" })).toBeNull());
	});

	it("shows server unread counts, clears them on open, and persists editable instructions", async () => {
		let assistants = [
			{ assistantId: "a-1", name: "小帕", description: "开发助理", brainDeviceId: "brain-a", status: "ready", permissionMode: "full", unreadCount: 0, lastMessagePreview: "已完成测试" },
			{ assistantId: "a-2", name: "罗宾", description: "", brainDeviceId: "brain-a", status: "ready", permissionMode: "full", unreadCount: 3, lastMessagePreview: "需要你确认方案" },
		];
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.read") { assistants = assistants.map(item => item.assistantId === params.assistantId ? { ...item, unreadCount: 0 } : item); return accepted({ unreadCount: 0 }); }
			if (method === "puck.update") { assistants = assistants.map(item => item.assistantId === params.assistantId ? { ...item, ...params } : item); return accepted({ assistant: assistants.find(item => item.assistantId === params.assistantId) }); }
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findAllByText("小帕");
		expect(screen.getByLabelText("罗宾有 3 条未读消息").textContent).toBe("3");
		expect(screen.getByText("需要你确认方案")).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: /罗宾/ }));
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.read", "relay", { assistantId: "a-2" }));
		expect(screen.queryByLabelText("罗宾有 3 条未读消息")).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "编辑说明" }));
		fireEvent.change(screen.getByLabelText("助理说明"), { target: { value: "先给结论，再列风险" } });
		fireEvent.click(screen.getByRole("button", { name: "保存说明" }));
		await screen.findByText("先给结论，再列风险");
		expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-2", description: "先给结论，再列风险" });
		expect((screen.getByLabelText("运行权限") as HTMLSelectElement).value).toBe("full");
		fireEvent.change(screen.getByLabelText("运行权限"), { target: { value: "ask" } });
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-2", permissionMode: "ask" }));
		expect((screen.getByLabelText("运行权限") as HTMLSelectElement).value).toBe("ask");
	});

	it("renders live real elapsed time, final work summary and actor reaction pills", async () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-11-04T02:00:05Z"));
		let messages: Array<{ id:string; role:string; kind:string; text:string; reaction?:string; workSummary?:string }> = [
			{ id: "user-a", role: "user", kind: "text", text: "检查测试", reaction: JSON.stringify({ assistant: "👀" }) },
			{ id: "processing-a", role: "assistant", kind: "greeting_processing", text: "正在准备…", workSummary: JSON.stringify({ startedAt: "2026-11-04T02:00:00Z", state: "running" }) },
			{ id: "done-a", role: "assistant", kind: "text", text: "已经完成", workSummary: JSON.stringify({ elapsedMs: 12500, steps: 3 }) },
		];
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", unreadCount: 0 }] });
			if (method === "puck.history") return accepted({ messages });
			if (method === "puck.message.react") { messages = messages.map(message => message.id === params.messageId ? { ...message, reaction: JSON.stringify({ assistant: "👀", user: params.emoji }) } : message); return accepted({ reaction: JSON.stringify({ assistant: "👀", user: params.emoji }) }); }
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await act(async () => { await vi.advanceTimersByTimeAsync(0); });
		expect(screen.getByText("工作了 5 秒")).toBeTruthy();
		expect(screen.getByRole("button", { name: "停止全部工作" })).toBeTruthy();
		expect(screen.getByText("工作了 13 秒 · 执行 3 步")).toBeTruthy();
		expect(screen.getByRole("button", { name: "助理回应 👀，1 个" })).toBeTruthy();
		fireEvent.click(screen.getAllByRole("button", { name: "给消息添加回应" })[0]);
		fireEvent.click(screen.getByRole("button", { name: "回应 👍" }));
		await act(async () => { await vi.advanceTimersByTimeAsync(0); });
		expect(request).toHaveBeenCalledWith("puck.message.react", "relay", { assistantId: "a-1", messageId: "user-a", emoji: "👍" });
		expect(screen.getByRole("button", { name: "你回应 👍，1 个" })).toBeTruthy();
	});

	it("collapses consecutive handled authorization cards and explains an empty result", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [
				{ id: "a", role: "assistant", kind: "authorization", text: "第一次", cardJSON: JSON.stringify({ cardState: "approved", decision: "approve_once" }) },
				{ id: "b", role: "assistant", kind: "authorization", text: "第二次", cardJSON: JSON.stringify({ cardState: "pending", expiresAt: "2020-01-01T00:00:00Z" }) },
				{ id: "c", role: "assistant", kind: "share_result", text: "完成了", cardJSON: JSON.stringify({ summary: "完成了", items: [] }) },
			] }) : accepted({}));
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		const archive = await screen.findByText(/已收起 2 条已处理或过期的授权/);
		expect((archive.closest("details") as HTMLDetailsElement).open).toBe(false);
		expect(screen.getByText(/没有更多详情/)).toBeTruthy();
		expect(view.container.textContent).not.toContain("结果详情暂时无法显示");
	});

	it("creates a group from ready assistants and shows each member reply", async () => {
		const assistants = [
			{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" },
			{ assistantId: "a-2", name: "罗宾", brainDeviceId: "brain-a", status: "ready" },
		];
		const group = { groupId: "group-1", name: "研究组", members: [{ assistantId: "a-1", name: "小帕" }, { assistantId: "a-2", name: "罗宾" }] };
		let history: Array<Record<string, unknown>> = [];
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants });
			if (method === "puck.group.list") return accepted({ groups: request.mock.calls.some(call => call[0] === "puck.group.create") ? [group] : [] });
			if (method === "puck.group.create") return accepted({ group });
			if (method === "puck.group.history") return accepted({ messages: history });
			if (method === "puck.group.message.send") {
				history = [
					{ id: params.clientMessageId, role: "user", kind: "text", text: params.text },
					{ id: `${params.clientMessageId}-a-1`, role: "assistant", kind: "processing", text: "正在处理…", authorAssistantId: "a-1", authorName: "小帕" },
					{ id: `${params.clientMessageId}-a-2`, role: "assistant", kind: "text", text: "我先看范围。", authorAssistantId: "a-2", authorName: "罗宾" },
				];
				return accepted({ messages: history });
			}
			if (method === "puck.history") return accepted({ messages: [] });
			return accepted({});
		});
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		fireEvent.click(await screen.findByRole("button", { name: "建组聊天" }));
		fireEvent.change(screen.getByLabelText("组名"), { target: { value: "研究组" } });
		fireEvent.click(screen.getByRole("checkbox", { name: "小帕" }));
		fireEvent.click(screen.getByRole("checkbox", { name: "罗宾" }));
		fireEvent.click(screen.getByRole("button", { name: "创建" }));
		expect(await screen.findByPlaceholderText("给小组发消息…")).toBeTruthy();
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.group.history", "relay", { groupId: "group-1" }));
		fireEvent.change(screen.getByLabelText("给 Puck 发消息"), { target: { value: "一起看一下" } });
		fireEvent.click(screen.getByRole("button", { name: "发送" }));
		expect(await screen.findAllByText("我先看范围。")).toHaveLength(2);
		const groupRow = screen.getByRole("button", { name: /研究组/ });
		expect(groupRow.querySelector(".puck-group-cluster")).toBeTruthy();
		expect(groupRow.querySelectorAll(".puck-avatar")).toHaveLength(2);
		expect(groupRow.querySelector(".puck-group-mark")).toBeNull();
		expect(groupRow.textContent).toContain("我先看范围。");
		expect(screen.getAllByText("罗宾").length).toBeGreaterThan(0);
		expect(request).toHaveBeenCalledWith("puck.group.message.send", "relay", expect.objectContaining({ groupId: "group-1", text: "一起看一下" }));
	});

	it("creates, renames, moves and collapses sidebar sections, then restores them without deleting chats", async () => {
		const assistants = [
			{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", unreadCount: 0, lastMessagePreview: "早" },
			{ assistantId: "a-2", name: "罗宾", brainDeviceId: "brain-a", status: "ready", unreadCount: 3, lastMessagePreview: "需要你确认方案" },
		];
		const group = { groupId: "group-1", name: "研究组", members: [{ assistantId: "a-1", name: "小帕" }, { assistantId: "a-2", name: "罗宾" }], lastMessagePreview: "一起看一下" };
		let sections: Array<{ sectionId: string; name: string; collapsed: boolean }> = [];
		let placements: Array<{ kind: string; entryId: string; sectionId: string; position: number }> = [];
		let ungroupedCollapsed = false;
		const snapshot = () => ({ sections: sections.map(item => ({ ...item })), placements: placements.map(item => ({ ...item })), ungroupedCollapsed });
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants });
			if (method === "puck.group.list") return accepted({ groups: [group] });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.section.list") return accepted(snapshot());
			if (method === "puck.section.create") { sections = [...sections, { sectionId: "section-1", name: String(params.name), collapsed: false }]; return accepted(snapshot()); }
			if (method === "puck.section.rename") { sections = sections.map(item => item.sectionId === params.sectionId ? { ...item, name: String(params.name) } : item); return accepted(snapshot()); }
			if (method === "puck.section.move") {
				placements = [...placements.filter(item => !(item.kind === params.kind && item.entryId === params.entryId)), { kind: String(params.kind), entryId: String(params.entryId), sectionId: String(params.sectionId || ""), position: Number(params.position) || 0 }];
				return accepted(snapshot());
			}
			if (method === "puck.section.collapse") {
				if (params.sectionId) sections = sections.map(item => item.sectionId === params.sectionId ? { ...item, collapsed: params.collapsed === true } : item);
				else ungroupedCollapsed = params.collapsed === true;
				return accepted(snapshot());
			}
			if (method === "puck.section.delete") {
				placements = placements.map(item => item.sectionId === params.sectionId ? { ...item, sectionId: "" } : item);
				sections = sections.filter(item => item.sectionId !== params.sectionId);
				return accepted(snapshot());
			}
			return accepted({});
		});
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findByRole("button", { name: "收起未分组" });
		expect(document.querySelector(".puck-conversation-title strong")?.textContent).toBe("小帕");
		expect(screen.getByLabelText("罗宾有 3 条未读消息").textContent).toBe("3");
		fireEvent.click(screen.getByRole("button", { name: "新建分组" }));
		fireEvent.change(screen.getByLabelText("分组名称"), { target: { value: "工作" } });
		fireEvent.click(screen.getByRole("button", { name: "创建" }));
		expect(await screen.findByRole("button", { name: "收起工作" })).toBeTruthy();
		expect(document.querySelector(".puck-assistant-row select")).toBeNull();
		expect(screen.queryByRole("menuitem", { name: "移动到分组" })).toBeNull();
		expect(screen.queryByRole("button", { name: "重命名分组 工作" })).toBeNull();
		fireEvent.contextMenu(screen.getByRole("button", { name: "收起工作" }));
		fireEvent.click(screen.getByRole("menuitem", { name: "重命名" }));
		fireEvent.change(screen.getByLabelText("新的分组名称"), { target: { value: "工作机" } });
		fireEvent.click(screen.getByRole("button", { name: "保存" }));
		expect(await screen.findByRole("button", { name: "收起工作机" })).toBeTruthy();
		const header = screen.getByRole("button", { name: "收起工作机" });
		Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => header });
		const hit = vi.spyOn(document, "elementFromPoint").mockReturnValue(header);
		for (const name of [/罗宾/, /研究组/]) {
			const row = screen.getByRole("button", { name });
			fireEvent.pointerDown(row, { pointerId: 1, pointerType: "mouse", clientX: 20, clientY: 20, button: 0 });
			await act(async () => { await new Promise(resolve => setTimeout(resolve, 460)); });
			fireEvent.pointerMove(row, { pointerId: 1, clientX: 24, clientY: 40 });
			fireEvent.pointerUp(row, { pointerId: 1, clientX: 24, clientY: 40 });
		}
		hit.mockRestore();
		await waitFor(() => expect(request).toHaveBeenCalledWith("puck.section.move", "relay", expect.objectContaining({ kind: "group", entryId: "group-1", sectionId: "section-1" })));
		const section = screen.getByRole("button", { name: "收起工作机" }).closest("section");
		expect(section?.textContent).toContain("罗宾");
		expect(section?.textContent).toContain("研究组");
		expect(section?.textContent).toContain("一起看一下");
		expect(section?.querySelector(".puck-group-cluster")).toBeTruthy();
		expect(section?.querySelector(".puck-group-mark")).toBeNull();
		expect(screen.getByLabelText("罗宾有 3 条未读消息").textContent).toBe("3");
		expect(document.querySelector(".puck-conversation-title strong")?.textContent).toBe("小帕");
		const collapse = screen.getByRole("button", { name: "收起工作机" });
		fireEvent.pointerDown(collapse, { pointerId: 9, button: 0, clientX: 8, clientY: 8 });
		fireEvent.pointerUp(collapse, { pointerId: 9, button: 0, clientX: 8, clientY: 8 });
		fireEvent.click(collapse);
		await waitFor(() => expect(screen.queryByRole("button", { name: /罗宾/ })).toBeNull());
		expect(screen.getByRole("button", { name: "展开工作机" })).toBeTruthy();
		expect(screen.getByRole("button", { name: /小帕/ })).toBeTruthy();
		view.unmount();
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		expect(await screen.findByRole("button", { name: "展开工作机" })).toBeTruthy();
		expect(screen.queryByRole("button", { name: /罗宾/ })).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "展开工作机" }));
		expect(await screen.findByLabelText("罗宾有 3 条未读消息")).toBeTruthy();
		expect(screen.getByRole("button", { name: /研究组/ })).toBeTruthy();
		fireEvent.contextMenu(screen.getByRole("button", { name: "收起工作机" }));
		fireEvent.click(screen.getByRole("menuitem", { name: "删除分组" }));
		expect(screen.getByText(/不会删除 Bot、群聊、会话或历史/)).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: "确认删除分组 工作机" }));
		expect(await screen.findByRole("button", { name: /罗宾/ })).toBeTruthy();
		expect(screen.queryByRole("button", { name: "收起工作机" })).toBeNull();
		expect(screen.getByRole("button", { name: /研究组/ })).toBeTruthy();
		expect(request.mock.calls.some(call => call[0] === "puck.delete")).toBe(false);
	});

	it("long-presses then drags a bot into a section, and ignores a drop outside", async () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-11-10T02:00:00Z"));
		const assistants = [
			{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", lastActivityAt: "2026-11-10T01:57:00Z", lastMessagePreview: "刚刚" },
			{ assistantId: "a-2", name: "罗宾", brainDeviceId: "brain-a", status: "ready", lastActivityAt: "2026-11-07T02:00:00Z", lastMessagePreview: "周六的结论" },
		];
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants });
			if (method === "puck.group.list") return accepted({ groups: [] });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.section.list") return accepted({ sections: [{ sectionId: "section-1", name: "工作机", collapsed: true }], placements: [], ungroupedCollapsed: false });
			if (method === "puck.section.collapse") return accepted({ sections: [{ sectionId: "section-1", name: "工作机", collapsed: false }], placements: [], ungroupedCollapsed: false });
			if (method === "puck.section.move") return accepted({ sections: [{ sectionId: "section-1", name: "工作机", collapsed: false }], placements: [{ kind: "assistant", entryId: params.entryId, sectionId: "section-1", position: 0 }], ungroupedCollapsed: false });
			return accepted({});
		});
		Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => null });
		const hit = vi.spyOn(document, "elementFromPoint").mockReturnValue(null);
		const frames: FrameRequestCallback[] = [];
		vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback: FrameRequestCallback) => {
			frames.push(callback);
			return frames.length;
		});
		vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
		render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await act(async () => { await vi.advanceTimersByTimeAsync(0); });
		const row = screen.getByRole("button", { name: /罗宾/ });
		expect(row.textContent).toContain("星期六");
		expect(screen.getByRole("button", { name: /小帕/ }).textContent).toContain("09:57");
		expect(screen.queryByRole("menuitem", { name: "移动到分组" })).toBeNull();
		fireEvent.pointerDown(row, { pointerId: 3, pointerType: "touch", clientX: 20, clientY: 20, button: 0 });
		fireEvent.pointerMove(row, { pointerId: 3, pointerType: "touch", clientX: 40, clientY: 20 });
		await act(async () => { await vi.advanceTimersByTimeAsync(500); });
		expect(document.querySelector(".puck-drag-ghost")).toBeNull();
		fireEvent.pointerUp(row, { pointerId: 3, clientX: 40, clientY: 20 });
		fireEvent.pointerDown(row, { pointerId: 4, pointerType: "touch", clientX: 20, clientY: 20, button: 0 });
		await act(async () => { await vi.advanceTimersByTimeAsync(450); });
		expect(document.querySelector(".puck-drag-ghost")?.textContent).toBe("罗宾");
		expect(row.className).toContain("lifting");
		const list = document.querySelector<HTMLElement>(".puck-assistant-list");
		expect(list).toBeTruthy();
		list!.getBoundingClientRect = () => ({ top: 0, bottom: 400, left: 0, right: 280, width: 280, height: 400, x: 0, y: 0, toJSON() { return {}; } }) as DOMRect;
		list!.scrollTop = 80;
		fireEvent.pointerMove(row, { pointerId: 4, clientX: 30, clientY: 10 });
		await act(async () => { frames.splice(0).forEach(callback => callback(0)); });
		expect(list!.scrollTop).toBeLessThan(80);
		hit.mockReturnValue(document.body);
		fireEvent.pointerMove(row, { pointerId: 4, clientX: 20, clientY: 80 });
		fireEvent.pointerUp(row, { pointerId: 4, clientX: 20, clientY: 80 });
		expect(request.mock.calls.some(call => call[0] === "puck.section.move")).toBe(false);
		hit.mockReturnValue(screen.getByRole("button", { name: "展开工作机" }));
		fireEvent.pointerDown(row, { pointerId: 5, pointerType: "mouse", clientX: 20, clientY: 20, button: 0 });
		await act(async () => { await vi.advanceTimersByTimeAsync(450); });
		fireEvent.pointerMove(row, { pointerId: 5, clientX: 20, clientY: 90 });
		expect(screen.getByRole("button", { name: "展开工作机" }).closest("section")?.className).toContain("drop-target");
		await act(async () => { await vi.advanceTimersByTimeAsync(400); });
		expect(request).toHaveBeenCalledWith("puck.section.collapse", "relay", { sectionId: "section-1", collapsed: false });
		await act(async () => { fireEvent.pointerUp(row, { pointerId: 5, clientX: 20, clientY: 90 }); });
		expect(request).toHaveBeenCalledWith("puck.section.move", "relay", expect.objectContaining({ kind: "assistant", entryId: "a-2", sectionId: "section-1" }));
		expect(screen.getByRole("button", { name: "收起工作机" }).closest("section")?.textContent).toContain("罗宾");
		hit.mockRestore();
	});
});
