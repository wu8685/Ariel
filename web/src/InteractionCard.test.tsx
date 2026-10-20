// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { InteractionCard } from "./App";
import type { Interaction } from "./generated/protocol";

afterEach(cleanup);

function card(kind: Interaction["kind"], decisions: Interaction["availableDecisions"]): Interaction {
  return { interactionId: "interaction-1", kind, prompt: "仅隔离 fixture", availableDecisions: decisions };
}

describe("interaction cards", () => {
  it("only offers owner-provided command and file decisions", () => {
    const onRespond = vi.fn();
    const { rerender } = render(<InteractionCard card={card("command_approval", ["accept_once", "deny_and_stop"])} values={{}} onChange={vi.fn()} onRespond={onRespond} disabled={false} />);
    expect(screen.getByRole("heading", { name: "等待命令审批" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "拒绝" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "拒绝并停止" }));
    expect(onRespond).toHaveBeenCalledWith("deny_and_stop");
    rerender(<InteractionCard card={card("file_approval", ["deny"])} values={{}} onChange={vi.fn()} onRespond={onRespond} disabled={false} />);
    expect(screen.getByRole("heading", { name: "等待文件变更审批" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
    expect(onRespond).toHaveBeenLastCalledWith("deny");
  });

  it("shows a one-turn permission grant without inventing broader options", () => {
    const onRespond = vi.fn();
    render(<InteractionCard card={card("permission_request", ["accept_once", "deny"])} values={{}} onChange={vi.fn()} onRespond={onRespond} disabled={false} />);
    expect(screen.getByRole("button", { name: "仅本轮按原请求授权" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "仅本轮按原请求授权" }));
    expect(onRespond).toHaveBeenCalledWith("accept_once");
  });

  it("requires answers to all questions before submission", () => {
    const onRespond = vi.fn();
    const question = { ...card("user_input", ["answer"]), questions: [{ id: "choice", question: "选择颜色", options: ["Blue"] }, { id: "note", question: "说明" }] } as Interaction;
    const { rerender } = render(<InteractionCard card={question} values={{}} onChange={vi.fn()} onRespond={onRespond} disabled={false} />);
    expect((screen.getByRole("button", { name: "提交回答" }) as HTMLButtonElement).disabled).toBe(true);
    rerender(<InteractionCard card={question} values={{ choice: "Blue", note: "测试说明" }} onChange={vi.fn()} onRespond={onRespond} disabled={false} />);
    fireEvent.click(screen.getByRole("button", { name: "提交回答" }));
    expect(onRespond).toHaveBeenCalledWith("answer");
  });

  it("renders touch options with descriptions and reveals an explicit custom answer", () => {
    const onChange = vi.fn();
    const question = { ...card("user_input", ["answer"]), questions: [{ id: "choice", header: "方式", question: "请选择处理方式", options: [{ label: "继续", description: "使用当前设置继续" }, { label: "暂停" }] }] } as unknown as Interaction;
    render(<InteractionCard card={question} values={{}} onChange={onChange} onRespond={vi.fn()} disabled={false} />);

    expect(screen.getByText("方式")).toBeTruthy();
    expect(screen.getByText("使用当前设置继续")).toBeTruthy();
    expect(screen.queryByRole("combobox")).toBeNull();
    fireEvent.click(screen.getByRole("radio", { name: /继续/ }));
    expect(onChange).toHaveBeenCalledWith("choice", "继续");

    fireEvent.click(screen.getByRole("radio", { name: "其他" }));
    expect(screen.getByRole("textbox", { name: "自定义回答" })).toBeTruthy();
  });
});
