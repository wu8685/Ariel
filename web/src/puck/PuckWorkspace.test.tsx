// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PuckWorkspace } from "./PuckWorkspace";

const accepted = (data: Record<string, unknown>) => ({ type: "response", v: 1, requestId: crypto.randomUUID(), outcome: "accepted", data } as const);
afterEach(cleanup);

describe("PuckWorkspace", () => {
  it("creates an assistant and shows an immediate processing bubble when sending", async () => {
    let assistants: Record<string, unknown>[] = [];
    const request = vi.fn(async (method: string, _target: string, params: Record<string, unknown>) => {
      if (method === "puck.list") return accepted({ assistants });
      if (method === "puck.create") {
        const assistant = { assistantId: "a-1", name: params.name, description: "", brainDeviceId: "brain-a", status: "ready" };
        assistants = [assistant];
        return accepted({ assistant });
      }
      if (method === "puck.history") return accepted({ messages: [] });
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
    expect(await screen.findByText("正在处理…")).toBeTruthy();
    expect(screen.getByText("检查测试")).toBeTruthy();
  });

  it("renders question, authorization and task cards and can stop work", async () => {
    const messages = [
      { id: "q", role: "assistant", kind: "question", text: "部署到哪里？", cardJSON: JSON.stringify({ options: ["测试", "生产"] }) },
      { id: "z", role: "assistant", kind: "authorization", text: "需要授权", cardJSON: JSON.stringify({ risk: "会写入文件" }) },
      { id: "t", role: "assistant", kind: "task", text: "运行测试", cardJSON: JSON.stringify({ status: "running" }) },
    ];
    const request = vi.fn(async (method: string) => method === "puck.list"
      ? accepted({ assistants: [{ assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready" }] })
      : method === "puck.history" ? accepted({ messages }) : accepted({ summary: "已请求停止 1 项" }));
    render(<PuckWorkspace request={request} state="ready" devices={[]} />);
    expect(await screen.findByRole("group", { name: "问题卡片" })).toBeTruthy();
    expect(screen.getByRole("group", { name: "授权卡片" })).toBeTruthy();
    expect(screen.getByRole("group", { name: "任务卡片" })).toBeTruthy();
	await act(async () => { fireEvent.click(screen.getByRole("button", { name: "停止全部工作" })); });
    expect(await screen.findByText("已请求停止 1 项")).toBeTruthy();
  });

  it("explains unavailable deployment prerequisites", async () => {
    render(<PuckWorkspace request={vi.fn()} state="unavailable" reason="PUCK_REQUIRES_PASSKEY_HTTPS" devices={[]} />);
    expect(screen.getByText("Puck 仅支持 Passkey + HTTPS")).toBeTruthy();
  });
});
