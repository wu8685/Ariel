// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App";
import type { Thread } from "./generated/protocol";

class BrowserSocket {
  static sockets: BrowserSocket[] = [];
  readyState = 1;
  sent: string[] = [];
  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  constructor(_url: string) { BrowserSocket.sockets.push(this); }
  send(value: string) { this.sent.push(value); }
  close(code = 1000) {
    this.readyState = 3;
    this.onclose?.({ code } as CloseEvent);
  }
  message(value: unknown) { this.onmessage?.({ data: JSON.stringify(value) } as MessageEvent); }
}

const relaySession = `s_${"a".repeat(64)}`;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
  originalWebSocket = globalThis.WebSocket;
  globalThis.WebSocket = BrowserSocket as unknown as typeof WebSocket;
  BrowserSocket.sockets = [];
  sessionStorage.clear();
});

afterEach(() => {
  cleanup();
  globalThis.WebSocket = originalWebSocket;
  vi.restoreAllMocks();
  delete (Element.prototype as unknown as Record<string, unknown>).scrollIntoView;
});

const fixtureThread: Thread = { threadId: "fixture", title: "Fixture", cwd: "/tmp/fixture", updatedAt: "2026-10-04T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] };

async function openFixture(thread: Thread = fixtureThread) {
  sessionStorage.setItem("ariel.web-session.v1", relaySession);
  render(<App />);
  const socket = BrowserSocket.sockets[0];
  const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
  socket.onopen?.(new Event("open"));
  act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
  await waitFor(() => expect(requests("device.list")).toHaveLength(1));
  await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
    { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } },
  ] } }));
  await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
  await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [thread] } }));
  fireEvent.click((await screen.findByText(thread.title)).closest("button")!);
  await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
  await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub" } }));
  act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread }));
  await waitFor(() => expect(screen.getByRole("heading", { name: thread.title })).toBeTruthy());
  return { socket, requests };
}

describe("Ariel app interactions", () => {
  it("dismisses the mobile sidebar on backdrop or Escape, but not inside the sidebar", () => {
    render(<App />);
    const sidebar = screen.getByLabelText("会话列表");
    expect(sidebar.classList.contains("open")).toBe(true);
    fireEvent.click(screen.getByLabelText("关闭会话列表遮罩"));
    expect(sidebar.classList.contains("open")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "☰ 会话" }));
    expect(sidebar.classList.contains("open")).toBe(true);
    fireEvent.click(sidebar);
    expect(sidebar.classList.contains("open")).toBe(true);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(sidebar.classList.contains("open")).toBe(false);
  });

  it("stores a Relay session after PIN login and reconnects after reload without storing the PIN", async () => {
    const first = render(<App />);
    fireEvent.change(screen.getByLabelText("6 位连接码"), { target: { value: "012345" } });
    fireEvent.click(screen.getByRole("button", { name: "连接" }));
    expect(BrowserSocket.sockets).toHaveLength(1);
    BrowserSocket.sockets[0].onopen?.(new Event("open"));
    expect(JSON.parse(BrowserSocket.sockets[0].sent[0]).token).toBe("012345");
    BrowserSocket.sockets[0].message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession });
    await waitFor(() => expect(sessionStorage.getItem("ariel.web-session.v1")).toBe(relaySession));
    expect(sessionStorage.getItem("ariel.web-session.v1")).not.toBe("012345");
    first.unmount();

    render(<App />);
    expect(BrowserSocket.sockets).toHaveLength(2);
    BrowserSocket.sockets[1].onopen?.(new Event("open"));
    expect(JSON.parse(BrowserSocket.sockets[1].sent[0]).token).toBe(relaySession);
  });

  it("clears a rejected saved session and explicit disconnect", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    expect(BrowserSocket.sockets).toHaveLength(1);
    BrowserSocket.sockets[0].close(1008);
    expect(await screen.findByText("保存的会话已失效，请重新输入连接码。")).toBeTruthy();
    expect(sessionStorage.getItem("ariel.web-session.v1")).toBeNull();

    fireEvent.change(screen.getByLabelText("6 位连接码"), { target: { value: "012345" } });
    fireEvent.click(screen.getByRole("button", { name: "连接" }));
    BrowserSocket.sockets[1].onopen?.(new Event("open"));
    BrowserSocket.sockets[1].message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession });
    await waitFor(() => expect(sessionStorage.getItem("ariel.web-session.v1")).toBe(relaySession));
    fireEvent.click(screen.getByRole("button", { name: "断开" }));
    expect(sessionStorage.getItem("ariel.web-session.v1")).toBeNull();
  });

  it("loads online devices and closes the drawer when a history thread is selected", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    const requestFor = (method: string) => socket.sent.map(value => JSON.parse(value)).find(value => value.method === method);
    await waitFor(() => expect(requestFor("device.list")).toBeTruthy());
    act(() => socket.message({ type: "response", v: 1, requestId: requestFor("device.list").requestId, outcome: "accepted", data: {
      devices: [{ deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, adapterVersion: "desktop", capabilities: { autoLoad: true } }],
    } }));
    expect(await screen.findByRole("option", { name: "昊天的 Mac" })).toBeTruthy();
    await waitFor(() => expect(requestFor("thread.list")).toBeTruthy());
    const thread = { threadId: "fixture", title: "Fixture", cwd: "/tmp/fixture", updatedAt: "2026-10-04T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] };
    act(() => socket.message({ type: "response", v: 1, requestId: requestFor("thread.list").requestId, outcome: "accepted", data: { threads: [thread] } }));
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    expect(screen.getByLabelText("会话列表").classList.contains("open")).toBe(false);
    await waitFor(() => expect(requestFor("thread.subscribe")).toBeTruthy());
    await act(async () => socket.message({ type: "response", v: 1, requestId: requestFor("thread.subscribe").requestId, outcome: "accepted", data: { subscriptionId: "sub" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", seq: 1, thread }));
    await waitFor(() => expect(screen.getAllByText("/tmp/fixture").length).toBeGreaterThan(1));
  });

  it("resubscribes the selected device after a stream sequence gap", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: {
      devices: [{ deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }],
    } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    const thread = { threadId: "fixture", title: "Fixture", cwd: "/tmp/fixture", updatedAt: "2026-10-04T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] };
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [thread] } }));
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    expect(requests("thread.subscribe")[0].deviceId).toBe("mac");
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub-1" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-1", streamId: "stream-1", seq: 1, thread }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Fixture" })).toBeTruthy());

    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-1", streamId: "stream-1", baseSeq: 1, seq: 3, thread }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    expect(requests("thread.subscribe")[1]).toMatchObject({ deviceId: "mac", params: { threadId: "fixture" } });
    expect(requests("thread.unsubscribe")).toContainEqual(expect.objectContaining({ deviceId: "mac", params: { subscriptionId: "sub-1" } }));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-2" } }));
    const recovered = { ...thread, title: "Recovered" };
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-2", streamId: "stream-2", seq: 1, thread: recovered }));
    expect(await screen.findByRole("heading", { name: "Recovered" })).toBeTruthy();
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-1", streamId: "stream-1", baseSeq: 1, seq: 2, thread }));
    expect(screen.getByRole("heading", { name: "Recovered" })).toBeTruthy();
    expect(requests("thread.subscribe")).toHaveLength(2);
  });

  it("does not replace the selected device's threads with a late response from the previous device", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac-a", deviceName: "Mac A", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } },
      { deviceId: "mac-b", deviceName: "Mac B", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } },
    ] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    expect(requests("thread.list")[0].deviceId).toBe("mac-a");
    fireEvent.change(screen.getByLabelText("设备"), { target: { value: "mac-b" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    expect(requests("thread.list")[1].deviceId).toBe("mac-b");
    const makeThread = (threadId: string, title: string) => ({ threadId, title, cwd: `/tmp/${threadId}`, updatedAt: "2026-10-04T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] });
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [makeThread("b", "Thread B")] } }));
    expect(await screen.findByText("Thread B")).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [makeThread("a", "Thread A")] } }));
    expect(screen.queryByText("Thread A")).toBeNull();
    expect(screen.getByText("Thread B")).toBeTruthy();
  });

  it("does not revive an offline device with an older device list response", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    const requests = socket.sent.map(value => JSON.parse(value)).filter(value => value.method === "device.list");
    expect(requests).toHaveLength(1);
    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentOnline: false, codexReady: false }));
    const updatedRequests = socket.sent.map(value => JSON.parse(value)).filter(value => value.method === "device.list");
    expect(updatedRequests).toHaveLength(2);
    const device = (agentOnline: boolean) => ({ deviceId: "mac", deviceName: "昊天的 Mac", agentOnline, codexReady: agentOnline, capabilities: { autoLoad: true } });
    await act(async () => socket.message({ type: "response", v: 1, requestId: updatedRequests[1].requestId, outcome: "accepted", data: { devices: [device(false)] } }));
    expect(screen.getByLabelText("会话列表").querySelector(".device-meta")?.textContent).toContain("Agent 离线");
    await act(async () => socket.message({ type: "response", v: 1, requestId: updatedRequests[0].requestId, outcome: "accepted", data: { devices: [device(true)] } }));
    expect(screen.getByLabelText("会话列表").querySelector(".device-meta")?.textContent).toContain("Agent 离线");
  });

  it("sends a message only after a selected owner snapshot and stops the exact active turn", async () => {
    const { socket, requests } = await openFixture();
    fireEvent.change(screen.getByLabelText("发送消息"), { target: { value: "fixture message" } });
    fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    expect(requests("turn.start")).toHaveLength(1);
    expect(requests("turn.start")[0]).toMatchObject({ deviceId: "mac", params: { threadId: "fixture", text: "fixture message" } });
    expect(requests("turn.start")[0].params.clientMessageId).toMatch(/^[0-9a-f-]{36}$/);
    expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).value).toBe("fixture message");
    expect((screen.getByRole("button", { name: /发送/ }) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "accepted", data: { turnId: "turn-1" } }));
    expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).value).toBe("");

    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "turn-1", status: "inProgress", items: [{ itemId: "reply", role: "assistant", text: "Working" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: running }));
    expect(await screen.findByText("Working")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /停止/ }));
    expect(requests("turn.interrupt")).toHaveLength(1);
    expect(requests("turn.interrupt")[0]).toMatchObject({ deviceId: "mac", params: { threadId: "fixture", expectedTurnId: "turn-1" } });
    expect(requests("turn.start")).toHaveLength(1);
  });

  it("sends an exact multi-question answer without optimistic card removal", async () => {
    const thread: Thread = { ...fixtureThread, runtime: "inProgress", pendingInteractions: [{ interactionId: "req-1", kind: "user_input", prompt: "Answer both", availableDecisions: ["answer"], questions: [
      { id: "choice", question: "选择颜色", options: ["Blue", "Green"] },
      { id: "note", question: "说明" },
    ] }] };
    const { socket, requests } = await openFixture(thread);
    expect((screen.getByRole("button", { name: "提交回答" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByPlaceholderText("选择建议或自行输入"), { target: { value: "Blue" } });
    fireEvent.change(screen.getByPlaceholderText("输入回答"), { target: { value: "自由文本" } });
    fireEvent.click(screen.getByRole("button", { name: "提交回答" }));
    expect(requests("interaction.respond")).toHaveLength(1);
    expect(requests("interaction.respond")[0]).toMatchObject({ deviceId: "mac", params: { threadId: "fixture", interactionId: "req-1", decision: "answer", answers: { choice: ["Blue"], note: ["自由文本"] } } });
    expect(screen.getByRole("heading", { name: "Codex 有一个问题" })).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("interaction.respond")[0].requestId, outcome: "accepted", data: {} }));
    expect(screen.getByRole("heading", { name: "Codex 有一个问题" })).toBeTruthy();
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: { ...thread, runtime: "idle", pendingInteractions: [] } }));
    expect(screen.queryByRole("heading", { name: "Codex 有一个问题" })).toBeNull();
  });

  it("routes only an offered command decision to the exact pending interaction", async () => {
    const thread: Thread = { ...fixtureThread, runtime: "inProgress", pendingInteractions: [{ interactionId: "command-1", kind: "command_approval", prompt: "在 /tmp/fixture 运行 /usr/bin/true", availableDecisions: ["accept_once", "deny_and_stop"] }] };
    const { requests } = await openFixture(thread);
    expect(screen.getByText("在 /tmp/fixture 运行 /usr/bin/true")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "拒绝" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "拒绝并停止" }));
    expect(requests("interaction.respond")).toHaveLength(1);
    expect(requests("interaction.respond")[0]).toMatchObject({ deviceId: "mac", params: { threadId: "fixture", interactionId: "command-1", decision: "deny_and_stop" } });
    expect(requests("interaction.respond")[0].params.answers).toBeUndefined();
    expect(requests("turn.interrupt")).toHaveLength(0);
  });

  it("keeps an unconfirmed send draft and never retries an unknown outcome", async () => {
    const { socket, requests } = await openFixture();
    fireEvent.change(screen.getByLabelText("发送消息"), { target: { value: "do not replay" } });
    fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    expect(requests("turn.start")).toHaveLength(1);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "owner receipt lost" } }));
    expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).value).toBe("do not replay");
    expect(screen.getByRole("alert").textContent).toContain("执行结果不确定");
    expect(requests("turn.start")).toHaveLength(1);
  });
});
