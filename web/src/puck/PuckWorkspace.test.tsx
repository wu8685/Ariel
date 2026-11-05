// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PuckWorkspace } from "./PuckWorkspace";

const accepted = (data: Record<string, unknown>) => ({ type: "response", v: 1, requestId: crypto.randomUUID(), outcome: "accepted", data } as const);
const rejected = (code: string) => ({ type: "response", v: 1, requestId: crypto.randomUUID(), outcome: "rejected", error: { code, message: code } } as const);
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); });

describe("PuckWorkspace", () => {
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
    fireEvent.click(screen.getByRole("radio", { name: "蓝" }));
    await waitFor(() => expect(composer.style.getPropertyValue("--puck-avatar-color")).toBe("#2588f7"));
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
    fireEvent.change(screen.getByLabelText("助理名称"), { target: { value: "小帕" } });
    fireEvent.click(screen.getByRole("button", { name: "创建" }));
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
    expect(screen.getByRole("group", { name: "任务卡片" })).toBeTruthy();
	expect(view.container.querySelectorAll(".puck-transcript .puck-avatar")).toHaveLength(0);
	expect(view.container.querySelectorAll(".puck-message.assistant.continued")).toHaveLength(2);
	await act(async () => { fireEvent.click(screen.getByRole("button", { name: "停止全部工作" })); });
    expect((await screen.findAllByText("已请求停止 1 项")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "打开会话" }));
    expect(onOpenThread).toHaveBeenCalledWith("exec-a", "thread-a");
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
    fireEvent.keyDown(textarea, { key: "Enter", shiftKey: false });
    expect(calls.filter(method => method === "puck.message.send")).toHaveLength(1);
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

	it("renders direct accessible avatar choices and persists click and keyboard selection", async () => {
		let assistant = { assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", avatar: JSON.stringify({ shape: "circle", color: "#8957f5" }) };
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
		const shapes = screen.getByRole("radiogroup", { name: "头像形状" });
		const colors = screen.getByRole("radiogroup", { name: "头像颜色" });
		expect(shapes.querySelectorAll('[role="radio"]')).toHaveLength(14);
		expect(colors.querySelectorAll('[role="radio"]')).toHaveLength(11);
		expect(screen.getByRole("radio", { name: "圆" }).getAttribute("aria-checked")).toBe("true");
		expect(screen.getByRole("radio", { name: "紫" }).getAttribute("aria-checked")).toBe("true");
		expect(screen.queryByRole("combobox", { name: "头像形状" })).toBeNull();
		expect(screen.queryByLabelText("头像颜色", { selector: 'input[type="color"]' })).toBeNull();

		fireEvent.click(screen.getByRole("radio", { name: "尖刺星" }));
		await waitFor(() => expect(screen.getByRole("radio", { name: "尖刺星" }).getAttribute("aria-checked")).toBe("true"));
		expect(document.querySelector(".puck-avatar-large")?.getAttribute("data-shape")).toBe("burst");
		expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-1", avatar: { shape: "burst", color: "#8957f5" } });
		fireEvent.keyDown(screen.getByRole("radio", { name: "尖刺星" }), { key: "ArrowRight" });
		await waitFor(() => expect(screen.getByRole("radio", { name: "圆角六边形" }).getAttribute("aria-checked")).toBe("true"));
		fireEvent.click(screen.getByRole("radio", { name: "蓝" }));
		await waitFor(() => expect(screen.getByRole("radio", { name: "蓝" }).getAttribute("aria-checked")).toBe("true"));
		expect((document.querySelector(".puck-avatar-large") as HTMLElement).style.getPropertyValue("--puck-avatar-color")).toBe("#2588f7");
		expect(request).toHaveBeenCalledWith("puck.update", "relay", { assistantId: "a-1", avatar: { shape: "hexagon", color: "#2588f7" } });
	});

	it("shows server unread counts, clears them on open, and persists editable instructions", async () => {
		let assistants = [
			{ assistantId: "a-1", name: "小帕", description: "开发助理", brainDeviceId: "brain-a", status: "ready", unreadCount: 0, lastMessagePreview: "已完成测试" },
			{ assistantId: "a-2", name: "罗宾", description: "", brainDeviceId: "brain-a", status: "ready", unreadCount: 3, lastMessagePreview: "需要你确认方案" },
		];
		const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
			if (method === "puck.list") return accepted({ assistants });
			if (method === "puck.history") return accepted({ messages: [] });
			if (method === "puck.read") { assistants = assistants.map(item => item.assistantId === params.assistantId ? { ...item, unreadCount: 0 } : item); return accepted({ unreadCount: 0 }); }
			if (method === "puck.update") { assistants = assistants.map(item => item.assistantId === params.assistantId ? { ...item, description: String(params.description) } : item); return accepted({ assistant: assistants.find(item => item.assistantId === params.assistantId) }); }
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
});
