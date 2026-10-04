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
  it("renders every brand mark as a monochrome vector instead of an emoji glyph", async () => {
    await openFixture();
    const marks = [...document.querySelectorAll(".brand-mark")];
    expect(marks).toHaveLength(2);
    for (const mark of marks) {
      expect(mark.tagName.toLowerCase()).toBe("svg");
      expect(mark.querySelectorAll("path").length).toBeGreaterThan(0);
      expect(mark.textContent).toBe("");
    }
  });

  it("keeps connected brand, status and disconnect in the drawer while the compact menu exposes status", async () => {
    await openFixture();
    const shell = document.querySelector(".app-shell")!;
    expect(shell.classList.contains("connected")).toBe(true);
    const sidebar = screen.getByLabelText("会话列表");
    expect(sidebar.querySelector(".brand")?.textContent).toContain("Ariel");
    expect(sidebar.querySelector(".connection")?.textContent).toContain("Relay 已连接");
    expect(sidebar.querySelector("button[aria-label='断开']")).toBeTruthy();
    const menu = screen.getByRole("button", { name: /打开会话列表.*Relay 已连接/ });
    expect(menu.querySelector(".menu-glyph")?.textContent).toBe("☰");
    expect(menu.querySelector(".status-dot")).toBeTruthy();
    expect(menu.getAttribute("aria-controls")).toBe("session-sidebar");
    expect(sidebar.classList.contains("open")).toBe(false);
  });

  it("opens compact permissions details and closes them by toggling, outside click and Escape", async () => {
    const thread: Thread = { ...fixtureThread, permissions: { sandbox: "full_access", approval: "on_request" } };
    await openFixture(thread);
    const info = screen.getByRole("button", { name: /Full Access.*on-request/ });
    expect(info.classList.contains("danger")).toBe(true);
    expect(info.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(info);
    const details = screen.getByRole("dialog", { name: "当前 Desktop 权限详情" });
    expect(details.textContent).toContain("Full Access");
    expect(details.textContent).toContain("审批策略 on-request");
    expect(details.textContent).toContain("允许访问本机其他文件和网络");
    expect(info.getAttribute("aria-controls")).toBe(details.id);
    expect(info.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(info);
    expect(screen.queryByRole("dialog", { name: "当前 Desktop 权限详情" })).toBeNull();
    fireEvent.click(info);
    fireEvent.mouseDown(screen.getByRole("main"));
    expect(screen.queryByRole("dialog", { name: "当前 Desktop 权限详情" })).toBeNull();
    fireEvent.click(info);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "当前 Desktop 权限详情" })).toBeNull();
  });

  it("keeps an unknown Desktop permission visibly cautionary", async () => {
    await openFixture({ ...fixtureThread, permissions: { sandbox: "unknown", approval: "unknown" } });
    const info = screen.getByRole("button", { name: "当前 Desktop 权限未知" });
    expect(info.classList.contains("danger")).toBe(true);
    fireEvent.click(info);
    expect(screen.getByRole("dialog", { name: "当前 Desktop 权限详情" }).textContent).toContain("当前 Desktop 权限未知");
  });

  it("preserves approval cards, message text and an editable compact composer", async () => {
    const thread: Thread = { ...fixtureThread, permissions: { sandbox: "read_only", approval: "never" }, turns: [{ turnId: "t1", status: "completed", items: [{ itemId: "u1", role: "user", text: "test long/path/here" }] }], pendingInteractions: [{ interactionId: "a1", kind: "command_approval", prompt: "Approve test command", availableDecisions: ["deny"] }] };
    await openFixture(thread);
    expect(screen.getByText("test long/path/here")).toBeTruthy();
    expect(screen.getByText("Approve test command")).toBeTruthy();
    expect(screen.getByRole("button", { name: "拒绝" })).toBeTruthy();
    const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
    expect(input.rows).toBe(1);
    fireEvent.change(input, { target: { value: "draft\nsecond line" } });
    expect(input.value).toBe("draft\nsecond line");
    expect((screen.getByRole("button", { name: /发送/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("grows a mobile draft to four lines and clears the inline height on desktop resize", async () => {
    const originalWidth = window.innerWidth;
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "scrollHeight");
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", { configurable: true, get: () => 140 });
      await openFixture();
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      fireEvent.change(input, { target: { value: "one\ntwo\nthree\nfour\nfive" } });
      expect(input.style.height).toBe("116px");
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
      fireEvent(window, new Event("resize"));
      expect(input.style.height).toBe("");
    } finally {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: originalWidth });
      if (originalScrollHeight) Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", originalScrollHeight);
      else delete (HTMLTextAreaElement.prototype as unknown as Record<string, unknown>).scrollHeight;
    }
  });

  it("keeps the mobile composer inside a keyboard-shrunken visual viewport", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    const originalHeight = Object.getOwnPropertyDescriptor(window, "innerHeight");
    const originalViewport = Object.getOwnPropertyDescriptor(window, "visualViewport");
    const viewport = Object.assign(new EventTarget(), { height: 844, offsetTop: 0 });
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(window, "innerHeight", { configurable: true, value: 844 });
      Object.defineProperty(window, "visualViewport", { configurable: true, value: viewport });
      await openFixture();
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      const shell = document.querySelector(".app-shell") as HTMLElement;
      input.focus();
      viewport.height = 500;
      act(() => viewport.dispatchEvent(new Event("resize")));
      expect(shell.style.height).toBe("500px");
      viewport.offsetTop = 18;
      act(() => viewport.dispatchEvent(new Event("scroll")));
      expect(shell.style.height).toBe("518px");
      viewport.height = 844;
      viewport.offsetTop = 0;
      act(() => viewport.dispatchEvent(new Event("resize")));
      expect(shell.style.height).toBe("");
      viewport.height = 500;
      act(() => viewport.dispatchEvent(new Event("resize")));
      expect(shell.style.height).toBe("500px");
      act(() => input.blur());
      expect(shell.style.height).toBe("");
      viewport.height = 500;
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
      act(() => window.dispatchEvent(new Event("resize")));
      expect(shell.style.height).toBe("");
    } finally {
      if (originalWidth) Object.defineProperty(window, "innerWidth", originalWidth);
      if (originalHeight) Object.defineProperty(window, "innerHeight", originalHeight);
      if (originalViewport) Object.defineProperty(window, "visualViewport", originalViewport);
      else delete (window as unknown as Record<string, unknown>).visualViewport;
    }
  });
  it("dismisses the mobile sidebar on backdrop or Escape, but not inside the sidebar", () => {
    render(<App />);
    const sidebar = screen.getByLabelText("会话列表");
    expect(sidebar.classList.contains("open")).toBe(true);
    fireEvent.click(screen.getByLabelText("关闭会话列表遮罩"));
    expect(sidebar.classList.contains("open")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
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
    fireEvent.click(screen.getByLabelText("会话列表").querySelector("button[aria-label='断开']")!);
    expect(sessionStorage.getItem("ariel.web-session.v1")).toBeNull();
  });

  it("asks for a new PIN when a Relay restart revokes a session issued after login", () => {
    vi.useFakeTimers();
    try {
      render(<App />);
      fireEvent.change(screen.getByLabelText("6 位连接码"), { target: { value: "012345" } });
      fireEvent.click(screen.getByRole("button", { name: "连接" }));
      const first = BrowserSocket.sockets[0];
      first.onopen?.(new Event("open"));
      act(() => first.message({ type: "hello.ok", v: 1, connectionId: "c1", relayEpoch: "e1", sessionToken: relaySession }));
      expect(sessionStorage.getItem("ariel.web-session.v1")).toBe(relaySession);

      act(() => first.close(1006));
      act(() => vi.advanceTimersByTime(2000));
      expect(BrowserSocket.sockets).toHaveLength(2);
      const second = BrowserSocket.sockets[1];
      second.onopen?.(new Event("open"));
      expect(JSON.parse(second.sent[0]).token).toBe(relaySession);
      act(() => second.close(1008));

      expect(screen.getByText("保存的会话已失效，请重新输入连接码。")).toBeTruthy();
      expect(screen.queryByText(/连接码错误或 Relay 已锁定/)).toBeNull();
      expect(sessionStorage.getItem("ariel.web-session.v1")).toBeNull();
    } finally { vi.useRealTimers(); }
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
    expect(document.querySelector(".conversation-head .head-path")?.textContent).toBe("/tmp/fixture");
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

  it("shows a future interaction without offering an unverified decision", async () => {
    const thread: Thread = { ...fixtureThread, runtime: "inProgress", pendingInteractions: [{ interactionId: "future-1", kind: "unsupported", prompt: "Desktop 正在等待 Ariel 尚未支持的交互，请回电脑端处理。", availableDecisions: [] }] };
    const { requests } = await openFixture(thread);
    const card = screen.getByRole("heading", { name: "暂不支持的交互" }).closest("section");
    expect(card?.textContent).toContain("请回电脑端处理");
    expect(card?.querySelectorAll("button")).toHaveLength(0);
    expect(requests("interaction.respond")).toHaveLength(0);
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

  it("does not retry an approval with an unknown result and waits for the owner update", async () => {
    const thread: Thread = { ...fixtureThread, runtime: "inProgress", pendingInteractions: [{ interactionId: "command-1", kind: "command_approval", prompt: "在隔离目录运行 /usr/bin/true", availableDecisions: ["deny"] }] };
    const { socket, requests } = await openFixture(thread);
    fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
    expect(requests("interaction.respond")).toHaveLength(1);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("interaction.respond")[0].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "approval receipt lost" } }));
    expect(screen.getByRole("alert").textContent).toContain("执行结果不确定");
    expect(screen.getByRole("heading", { name: "等待命令审批" })).toBeTruthy();
    expect(requests("interaction.respond")).toHaveLength(1);
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: { ...thread, runtime: "idle", pendingInteractions: [] } }));
    expect(screen.queryByRole("heading", { name: "等待命令审批" })).toBeNull();
    expect(requests("interaction.respond")).toHaveLength(1);
  });
});
