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
  it("searches all native sessions with a debounced query, shows snippets, pages, and restores recents", async () => {
    const { socket, requests } = await openFixture();
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索会话" }), { target: { value: "Ariel" } });
    expect(requests("thread.list")).toHaveLength(1);
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    expect(requests("thread.list")[1].params).toMatchObject({ searchTerm: "Ariel", limit: 50 });
    const result = { ...fixtureThread, threadId: "found", title: "Found title", searchSnippet: "命中的消息内容" };
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [result], nextCursor: "page-two" } }));
    expect(await screen.findByText("命中的消息内容")).toBeTruthy();
    expect(screen.getByLabelText("会话列表").textContent).not.toContain("Fixture");
    fireEvent.click(screen.getByRole("button", { name: "加载更多 →" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(3));
    expect(requests("thread.list")[2].params).toMatchObject({ searchTerm: "Ariel", cursor: "page-two" });
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[2].requestId, outcome: "accepted", data: { threads: [{ ...fixtureThread, threadId: "older", title: "Older hit" }], nextCursor: "" } }));
    expect(await screen.findByText("Older hit")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "清空搜索" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(4));
    expect(requests("thread.list")[3].params.searchTerm).toBeUndefined();
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[3].requestId, outcome: "accepted", data: { threads: [fixtureThread], nextCursor: "" } }));
    expect(await screen.findByText("Fixture")).toBeTruthy();
    expect(screen.queryByText("Older hit")).toBeNull();
  });

  it("isolates stale search responses and keeps search errors local to the sidebar", async () => {
    const { socket, requests } = await openFixture();
    const search = screen.getByRole("searchbox", { name: "搜索会话" });
    fireEvent.change(search, { target: { value: "first" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    fireEvent.change(search, { target: { value: "second" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(3));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[2].requestId, outcome: "accepted", data: { threads: [], nextCursor: "" } }));
    expect(await screen.findByText("没有找到匹配会话")).toBeTruthy();
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [{ ...fixtureThread, threadId: "stale", title: "Stale result" }] } }));
    expect(screen.queryByText("Stale result")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "刷新会话" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(4));
    expect(requests("thread.list")[3].params.searchTerm).toBe("second");
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[3].requestId, outcome: "rejected", error: { code: "PROTOCOL_UNSUPPORTED", message: "unsupported" } }));
    expect(await screen.findByText("当前 Codex 版本不支持会话内容搜索")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Fixture" })).toBeTruthy();
  });

  it("refreshes the list when edited input trims to the same query", async () => {
    const { socket, requests } = await openFixture();
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索会话" }), { target: { value: " " } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    expect(requests("thread.list")[1].params.searchTerm).toBeUndefined();
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [fixtureThread], nextCursor: "" } }));
    await waitFor(() => expect(screen.getByLabelText("会话列表").textContent).toContain("Fixture"));
  });

  it("keeps a search snippet visible when the selected session receives a live update", async () => {
    const { socket, requests } = await openFixture();
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索会话" }), { target: { value: "Ariel" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [{ ...fixtureThread, searchSnippet: "匹配摘要" }], nextCursor: "" } }));
    expect(await screen.findByText("匹配摘要")).toBeTruthy();
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: { ...fixtureThread, runtime: "inProgress" } }));
    expect(screen.getByText("匹配摘要")).toBeTruthy();
  });

  it("sends an attached screenshot without text and renders a native reply image on demand", async () => {
    const thread: Thread = { ...fixtureThread, turns: [{ turnId: "turn-image", status: "completed", items: [{ itemId: "reply-image", role: "assistant", text: "Codex 生成的图片", images: [{ index: 0, kind: "native", alt: "Codex 图片", source: "" }] }] }] };
    const { socket, requests } = await openFixture(thread);
    fireEvent.click(screen.getByRole("button", { name: "加载截图：Codex 图片" }));
    await waitFor(() => expect(requests("thread.image")).toHaveLength(1));
    expect(requests("thread.image")[0].params).toEqual({ threadId: "fixture", turnId: "turn-image", itemId: "reply-image", imageIndex: 0 });
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.image")[0].requestId, outcome: "accepted", data: { dataUri: "data:image/png;base64,AAAA" } }));
    expect(await screen.findByRole("img", { name: "Codex 图片" })).toBeTruthy();
    const file = new File([Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="), value => value.charCodeAt(0))], "shot.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("附加截图"), { target: { files: [file] } });
    await waitFor(() => expect(screen.getByRole("button", { name: "移除截图：shot.png" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(requests("turn.start")).toHaveLength(1));
    expect(requests("turn.start")[0].params.text).toBe("");
    expect(requests("turn.start")[0].params.images[0]).toMatch(/^data:image\/png;base64,/);
  });
  it("falls back to explicitly read-only paged history when owner snapshot is oversized", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "rejected", error: { code: "HISTORY_TOO_LARGE", message: "HISTORY_TOO_LARGE" } }));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: { ...fixtureThread, runtime: "idle", historyComplete: false, turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "可读历史" }] }] } } }));
    expect(await screen.findByText("可读历史")).toBeTruthy();
    expect(screen.getByText(/历史只读.*Desktop 状态未确认/)).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "do not send" } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    expect(requests("turn.start")).toHaveLength(0);
  });

  it("stops the sync spinner if both owner and stored history cannot be loaded", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "rejected", error: { code: "HISTORY_TOO_LARGE", message: "HISTORY_TOO_LARGE" } }));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "rejected", error: { code: "PROTOCOL_UNSUPPORTED", message: "PROTOCOL_UNSUPPORTED" } }));
    expect(screen.getByRole("heading", { name: "会话状态无法确认" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "正在同步会话…" })).toBeNull();
    expect(screen.getByRole("alert").textContent).toContain("历史分页");
  });

  it("keeps visible history and becomes read-only when a live update exceeds the frame limit", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "one", status: "completed", items: [{ itemId: "item", role: "assistant", text: "已可见内容" }] }] };
    const { socket, requests } = await openFixture(recent);
    act(() => socket.message({ type: "event", v: 1, event: "thread.error", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", code: "HISTORY_TOO_LARGE" }));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    expect(screen.getByText("已可见内容")).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: recent } }));
    expect(await screen.findByText(/历史只读.*Desktop 状态未确认/)).toBeTruthy();
    expect(screen.getByText("已可见内容")).toBeTruthy();
  });

  it("automatically fills a segmented recent window without a tap", async () => {
    const recent: Thread = { ...fixtureThread, historyComplete: false, recentComplete: false, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "最新内容" }] }] };
    const { socket, requests } = await openFixture(recent);
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    const sent = requests("thread.history")[0];
    await act(async () => socket.message({ type: "response", v: 1, requestId: sent.requestId, outcome: "accepted", data: { turns: [recent.turns[0], { turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "补齐内容" }] }], nextCursor: "" } }));
    expect(await screen.findByText("补齐内容")).toBeTruthy();
    expect(screen.getByText("最新内容")).toBeTruthy();
  });

  it("loads older turns on demand after walking past the ten-turn live overlap", async () => {
    const recent: Thread = { ...fixtureThread, historyComplete: false, recentComplete: true, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "最新内容" }] }] };
    const { socket, requests } = await openFixture(recent);
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[0].requestId, outcome: "accepted", data: { turns: [recent.turns[0]], nextCursor: "after-latest" } }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[1].requestId, outcome: "accepted", data: { turns: [{ turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "更早内容" }] }], nextCursor: "" } }));
    expect(await screen.findByText("更早内容")).toBeTruthy();
    expect(screen.getByText("最新内容")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "加载更早消息" })).toBeNull();
  });

  it("loads older items inside a giant turn without replacing visible newest items", async () => {
    const partial: Thread = { ...fixtureThread, turns: [{ turnId: "giant", status: "completed", items: [{ itemId: "new-item", role: "assistant", text: "本回合最新" }], itemsComplete: false, nextItemCursor: "before-new" }] };
    const { socket, requests } = await openFixture(partial);
    fireEvent.click(screen.getByRole("button", { name: "加载此回合更早内容" }));
    await waitFor(() => expect(requests("thread.history.items")).toHaveLength(1));
    const sent = requests("thread.history.items")[0];
    expect(sent.params).toMatchObject({ threadId: "fixture", turnId: "giant", cursor: "before-new" });
    await act(async () => socket.message({ type: "response", v: 1, requestId: sent.requestId, outcome: "accepted", data: { turnId: "giant", items: [{ itemId: "old-item", role: "assistant", text: "本回合更早" }], nextItemCursor: "", itemsComplete: true } }));
    expect(await screen.findByText("本回合更早")).toBeTruthy();
    expect(screen.getByText("本回合最新")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "加载此回合更早内容" })).toBeNull();
  });

  it("keeps the reading position on live updates until the reader chooses to return to latest", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "partial" }] }] };
    const { socket } = await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    const earlierScrollCalls = vi.mocked(Element.prototype.scrollIntoView).mock.calls.length;
    const updated: Thread = { ...recent, turns: [{ ...recent.turns[0], items: [{ itemId: "answer", role: "assistant", text: "partial and more" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: updated }));
    expect(transcript.scrollTop).toBe(300);
    expect(vi.mocked(Element.prototype.scrollIntoView).mock.calls.length).toBe(earlierScrollCalls);
    expect(screen.getByRole("button", { name: "回到最新" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "回到最新" }));
    expect(transcript.scrollTop).toBe(800);
    expect(screen.queryByRole("button", { name: "回到最新" })).toBeNull();
  });

  it("does not announce a new message for a status-only update while reading older content", async () => {
    const recent: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "current", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "same text" }] }] };
    const { socket } = await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    const stopped: Thread = { ...recent, runtime: "idle", turns: [{ ...recent.turns[0], status: "completed" }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: stopped }));
    expect(screen.queryByRole("button", { name: "回到最新" })).toBeNull();
    const more: Thread = { ...stopped, turns: [...stopped.turns, { turnId: "next", status: "completed", items: [{ itemId: "new", role: "assistant", text: "new content" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 2, seq: 3, thread: more }));
    expect(screen.getByRole("button", { name: "回到最新" })).toBeTruthy();
    expect(transcript.scrollTop).toBe(300);
  });

  it("restores the same-thread reading anchor after a stream resubscription", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "completed", items: [{ itemId: "anchor", role: "assistant", text: "read this" }] }] };
    const { socket, requests } = await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      return this.dataset.itemId === "anchor" ? { top: 120, bottom: 160 } as DOMRect : { top: 0, bottom: 0 } as DOMRect;
    });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    act(() => socket.message({ type: "event", v: 1, event: "thread.error", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", code: "RESYNC_REQUIRED" }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    transcript.scrollTop = 0;
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-again" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-again", streamId: "stream-again", seq: 1, thread: recent }));
    expect(transcript.scrollTop).toBe(300);
  });

  it("preserves an existing message's screen position when older turns are prepended", async () => {
    const recent: Thread = { ...fixtureThread, historyComplete: false, recentComplete: true, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "anchor", role: "assistant", text: "正在阅读" }] }] };
    const { socket, requests } = await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    const anchor = document.querySelector('[data-item-id="anchor"]') as HTMLElement;
    vi.spyOn(anchor, "getBoundingClientRect").mockImplementation(() => {
      const top = document.querySelector('[data-item-id="older"]') ? 220 : 120;
      return { top, bottom: top + 40 } as DOMRect;
    });
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[0].requestId, outcome: "accepted", data: { turns: [recent.turns[0]], nextCursor: "older" } }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[1].requestId, outcome: "accepted", data: { turns: [{ turnId: "previous", status: "completed", items: [{ itemId: "older", role: "user", text: "更早内容" }] }], nextCursor: "" } }));
    expect(screen.getByText("更早内容")).toBeTruthy();
    expect(transcript.scrollTop).toBe(400);
  });

  it("preserves an existing message's screen position when older items are prepended", async () => {
    const partial: Thread = { ...fixtureThread, turns: [{ turnId: "giant", status: "completed", items: [{ itemId: "anchor", role: "assistant", text: "正在阅读" }], itemsComplete: false, nextItemCursor: "before-anchor" }] };
    const { socket, requests } = await openFixture(partial);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    const anchor = document.querySelector('[data-item-id="anchor"]') as HTMLElement;
    vi.spyOn(anchor, "getBoundingClientRect").mockImplementation(() => {
      const top = document.querySelector('[data-item-id="older"]') ? 220 : 120;
      return { top, bottom: top + 40 } as DOMRect;
    });
    fireEvent.click(screen.getByRole("button", { name: "加载此回合更早内容" }));
    await waitFor(() => expect(requests("thread.history.items")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history.items")[0].requestId, outcome: "accepted", data: { turnId: "giant", items: [{ itemId: "older", role: "assistant", text: "更早内容" }], nextItemCursor: "", itemsComplete: true } }));
    expect(screen.getByText("更早内容")).toBeTruthy();
    expect(transcript.scrollTop).toBe(400);
  });

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

  it("shows user and Codex bubbles on separate sides without visible names or avatars", async () => {
    const thread: Thread = { ...fixtureThread, turns: [{ turnId: "bubble", status: "completed", items: [
      { itemId: "user", role: "user", text: "first\nsecond" },
      { itemId: "assistant", role: "assistant", text: "answer" },
      { itemId: "system", role: "system", text: "compatibility notice" },
    ] }], pendingInteractions: [{ interactionId: "approval", kind: "command_approval", prompt: "Approve fixture only", availableDecisions: ["deny"] }] };
    await openFixture(thread);
    const user = document.querySelector(".message.user")!;
    const assistant = document.querySelector(".message.assistant")!;
    const system = document.querySelector(".message.system")!;
    expect(user.getAttribute("aria-label")).toBe("你");
    expect(assistant.getAttribute("aria-label")).toBe("Codex");
    expect(user.textContent).toBe("first\nsecond");
    expect(assistant.textContent).toBe("answer");
    expect(system.textContent).toContain("compatibility notice");
    expect(document.querySelectorAll(".message .avatar, .message .message-role")).toHaveLength(0);
    expect(screen.getByText("Approve fixture only")).toBeTruthy();
  });

  it("formats only user and Codex bodies while keeping system, tool and approval text literal", async () => {
    const thread = { ...fixtureThread, turns: [{ turnId: "markdown", status: "completed", items: [
      { itemId: "user", role: "user", text: "**your emphasis**" },
      { itemId: "assistant", role: "assistant", text: "[source](https://example.test/)" },
      { itemId: "system", role: "system", text: "**system literal**" },
      { itemId: "tool", role: "system", text: "tool", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "completed", details: "**tool literal**", truncated: false } },
    ] }], pendingInteractions: [{ interactionId: "approval", kind: "command_approval", prompt: "**approval literal**", availableDecisions: ["deny"] }] } as unknown as Thread;
    await openFixture(thread);
    expect(document.querySelector(".message.user strong")?.textContent).toBe("your emphasis");
    expect(document.querySelector(".message.assistant a")?.getAttribute("href")).toBe("https://example.test/");
    expect(document.querySelector(".message.system strong")).toBeNull();
    expect(screen.getByText("**system literal**")).toBeTruthy();
    expect(screen.getByText("**approval literal**")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /打开工具调用.*1 项/ }));
    expect(document.querySelector(".activity-details strong")).toBeNull();
    expect(screen.getByText("**tool literal**")).toBeTruthy();
  });

  it("hides all verified calls behind one icon per turn and restores their interleaved order", async () => {
    const thread = { ...fixtureThread, turns: [{ turnId: "tools", status: "completed", items: [
      { itemId: "u", role: "user", text: "question" },
      { itemId: "m", role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "completed", details: "<img src=x onerror=alert(1)>", truncated: false } },
      { itemId: "c", role: "system", text: "命令执行", activity: { kind: "commandExecution", label: "/usr/bin/true", status: "completed", details: "退出 0", truncated: false } },
      { itemId: "a", role: "assistant", text: "answer" },
      { itemId: "f", role: "system", text: "文件变更", activity: { kind: "fileChange", label: "文件变更", status: "completed", details: "+safe", truncated: false } },
      { itemId: "unknown", role: "system", text: "[futureTool 项目]" },
    ] }], pendingInteractions: [{ interactionId: "approval", kind: "command_approval", prompt: "Approve fixture only", availableDecisions: ["deny"] }] } as unknown as Thread;
    await openFixture(thread);
    const opener = screen.getByRole("button", { name: /打开工具调用.*3 项/ });
    expect(screen.getAllByRole("button", { name: /打开工具调用/ })).toHaveLength(1);
    expect(opener.getAttribute("aria-expanded")).toBe("false");
    expect(opener.textContent?.trim()).toHaveLength(0);
    expect(screen.queryByText("fixture/lookup")).toBeNull();
    expect(screen.queryByText("/usr/bin/true")).toBeNull();
    expect(screen.queryByText("文件变更")).toBeNull();
    expect(document.querySelectorAll(".activity-item")).toHaveLength(0);
    expect(screen.getByText("answer")).toBeTruthy();
    expect(screen.getByText("[futureTool 项目]")).toBeTruthy();
    expect(screen.getByText("Approve fixture only")).toBeTruthy();
    fireEvent.click(opener);
    expect(screen.getByRole("button", { name: /收起工具调用.*3 项/ }).getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("fixture/lookup")).toBeTruthy();
    expect(screen.getByText("/usr/bin/true")).toBeTruthy();
    expect(document.querySelector(".activity-details img")).toBeNull();
    expect(document.querySelectorAll(".activity-item")).toHaveLength(3);
    const transcriptOrder = [...document.querySelectorAll(".turn .message, .turn .activity-item")].map(node => node.getAttribute("data-item-id"));
    expect(transcriptOrder).toEqual(["u", "m", "c", "a", "f", "unknown"]);
    fireEvent.click(screen.getByRole("button", { name: /收起工具调用/ }));
    expect(document.querySelectorAll(".activity-item")).toHaveLength(0);
    expect(screen.getAllByRole("button", { name: /打开工具调用/ })).toHaveLength(1);
  });

  it("keeps one tool opener expanded as live calls append to the same turn", async () => {
    const activity = (itemId: string) => ({ itemId, role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: itemId, status: "completed", details: itemId, truncated: false } });
    const starting = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "live-tools", status: "inProgress", items: [activity("first")] }] } as unknown as Thread;
    const { socket } = await openFixture(starting);
    const controlAnchor = document.querySelector(".activity-group")?.getAttribute("data-item-id");
    fireEvent.click(screen.getByRole("button", { name: /打开工具调用.*1 项/ }));
    const updated = { ...starting, turns: [{ ...starting.turns[0], items: [activity("first"), activity("second"), { itemId: "note", role: "assistant", text: "progress" }, activity("third")] }] } as unknown as Thread;
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: updated }));
    expect(screen.getAllByRole("button", { name: /收起工具调用.*3 项/ })).toHaveLength(1);
    expect(document.querySelectorAll(".activity-item")).toHaveLength(3);
    expect(document.querySelector(".activity-group")?.getAttribute("data-item-id")).toBe(controlAnchor);
    expect(screen.getByText("progress")).toBeTruthy();
  });

  it("shows one thinking or processing line only while the active turn can run", async () => {
    const starting: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "u", role: "user", text: "work" }] }] };
    const { socket } = await openFixture(starting);
    expect(screen.getAllByText("思考中…")).toHaveLength(1);
    const activity = { itemId: "m", role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "inProgress", details: "", truncated: false } };
    const running = { ...starting, turns: [{ ...starting.turns[0], items: [...starting.turns[0].items, activity] }] } as unknown as Thread;
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: running }));
    expect(screen.queryByText("思考中…")).toBeNull();
    expect(screen.getAllByText(/处理中…/)).toHaveLength(1);
    expect(screen.getByRole("button", { name: /打开工具调用.*1 项/ })).toBeTruthy();
    const completed = { ...running, runtime: "idle", turns: [{ ...running.turns[0], status: "completed" }] } as unknown as Thread;
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 2, seq: 3, thread: completed }));
    expect(screen.queryByText(/处理中…/)).toBeNull();
    expect(screen.getByRole("button", { name: /打开工具调用.*1 项/ })).toBeTruthy();
  });

  it("uses Enter only for mobile newlines and requires the send button", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      const { socket, requests } = await openFixture();
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      const send = screen.getByRole("button", { name: "发送" }) as HTMLButtonElement;
      expect(input.getAttribute("enterkeyhint")).toBe("enter");
      fireEvent.change(input, { target: { value: "first" } });
      expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(true);
      expect(fireEvent.keyDown(input, { key: "Enter", shiftKey: true })).toBe(true);
      expect(fireEvent.keyDown(input, { key: "Enter", isComposing: true })).toBe(true);
      expect(requests("turn.start")).toHaveLength(0);
      fireEvent.change(input, { target: { value: "first\nsecond" } });
      expect(input.value).toBe("first\nsecond");
      fireEvent.click(send);
      await waitFor(() => expect(requests("turn.start")).toHaveLength(1));
      expect(requests("turn.start")[0].params.text).toBe("first\nsecond");
      expect(send.disabled).toBe(true);
      fireEvent.click(send);
      expect(requests("turn.start")).toHaveLength(1);
      await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "rejected", error: { code: "TURN_BUSY", message: "busy" } }));
      expect(input.value).toBe("first\nsecond");
      fireEvent.click(send);
      await waitFor(() => expect(requests("turn.start")).toHaveLength(2));
      await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[1].requestId, outcome: "accepted", data: { turnId: "new" } }));
      expect(input.value).toBe("");
    } finally {
      if (originalWidth) Object.defineProperty(window, "innerWidth", originalWidth);
    }
  });

  it("retains desktop Enter to send and Shift+Enter to insert a newline", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
      const { requests } = await openFixture();
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      fireEvent.change(input, { target: { value: "desktop draft" } });
      expect(fireEvent.keyDown(input, { key: "Enter", shiftKey: true })).toBe(true);
      expect(fireEvent.keyDown(input, { key: "Enter", isComposing: true })).toBe(true);
      expect(requests("turn.start")).toHaveLength(0);
      expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
      await waitFor(() => expect(requests("turn.start")).toHaveLength(1));
    } finally {
      if (originalWidth) Object.defineProperty(window, "innerWidth", originalWidth);
    }
  });

  it("grows a mobile draft from one to eight visual lines, then scrolls internally and shrinks", async () => {
    const originalWidth = window.innerWidth;
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "scrollHeight");
    let measuredHeight = 44;
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", { configurable: true, get: () => measuredHeight });
      await openFixture();
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      expect(input.rows).toBe(1);
      expect(input.style.height).toBe("44px");
      measuredHeight = 92;
      fireEvent.change(input, { target: { value: "one\ntwo\nthree" } });
      expect(input.style.height).toBe("92px");
      measuredHeight = 140;
      fireEvent.change(input, { target: { value: "one long line that wraps automatically inside the narrow composer" } });
      expect(input.style.height).toBe("140px");
      measuredHeight = 212;
      fireEvent.change(input, { target: { value: "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight" } });
      expect(input.style.height).toBe("212px");
      measuredHeight = 260;
      fireEvent.change(input, { target: { value: "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten" } });
      expect(input.style.height).toBe("212px");
      expect(input.value).toContain("nine\nten");
      expect(input.scrollTop).toBeGreaterThan(0);
      measuredHeight = 44;
      fireEvent.change(input, { target: { value: "short" } });
      expect(input.style.height).toBe("44px");
      expect(input.scrollTop).toBe(0);
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
