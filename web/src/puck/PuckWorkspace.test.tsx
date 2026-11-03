// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PuckWorkspace } from "./PuckWorkspace";

const accepted = (data: Record<string, unknown>) => ({ type: "response", v: 1, requestId: crypto.randomUUID(), outcome: "accepted", data } as const);
afterEach(cleanup);

describe("PuckWorkspace", () => {
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
	expect(view.container.querySelectorAll(".puck-transcript .puck-avatar")).toHaveLength(1);
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
    expect(screen.getByRole("button", { name: "A" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "A" }));
    expect(calls.filter(method => method === "puck.question.answer")).toHaveLength(1);
    const textarea = screen.getByLabelText("给 Puck 发消息");
    fireEvent.change(textarea, { target: { value: "继续" } });
    fireEvent.keyDown(textarea, { key: "Enter", shiftKey: false });
    expect(calls.filter(method => method === "puck.message.send")).toHaveLength(1);
  });

	it("reuses Ariel message and bubble classes for user and assistant content", async () => {
		const request = vi.fn(async (method: string) => method === "puck.list"
			? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
			: method === "puck.history" ? accepted({ messages: [
				{ id: "u", role: "user", kind: "text", text: "用户消息" },
				{ id: "a", role: "assistant", kind: "text", text: "助理消息" },
			] }) : accepted({}));
		const view = render(<PuckWorkspace request={request} state="ready" devices={[]} />);
		await screen.findByText("用户消息");
		expect(view.container.querySelector(".puck-message.message.user > .puck-message-content.message-body")).toBeTruthy();
		expect(view.container.querySelector(".puck-message.message.assistant > .puck-message-content.message-body")).toBeTruthy();
		expect(view.container.querySelector(".puck-message.user .puck-markdown.markdown-body")).toBeTruthy();
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
});
