// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App";
import type { Thread } from "./generated/protocol";
import { normalizeProjectPath, projectName } from "./projects";
import { DurableHistoryCache, MemoryDurableHistoryBackend } from "./durable-history-cache";
import { emptyHistoryState } from "./history";
import * as passkeyAPI from "./auth";

class BrowserSocket {
  static sockets: BrowserSocket[] = [];
  static autoRejectCacheWatch = true;
  static readonly OPEN = 1;
  readyState = 1;
  sent: string[] = [];
  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  constructor(_url: string) { BrowserSocket.sockets.push(this); }
  send(value: string) {
    this.sent.push(value);
    const request = JSON.parse(value);
    if (BrowserSocket.autoRejectCacheWatch && request.method === "thread.cache.watch") {
      queueMicrotask(() => this.message({
        type: "response", v: 1, requestId: request.requestId, outcome: "rejected",
        error: { code: "PROTOCOL_UNSUPPORTED", message: "PROTOCOL_UNSUPPORTED" },
      }));
    }
  }
  close(code = 1000, reason = "") {
    this.readyState = 3;
    this.onclose?.({ code, reason } as CloseEvent);
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
  BrowserSocket.autoRejectCacheWatch = true;
  sessionStorage.clear();
  localStorage.clear();
  window.history.replaceState({}, "", "/");
});

afterEach(() => {
  cleanup();
  globalThis.WebSocket = originalWebSocket;
  vi.restoreAllMocks();
  delete (Element.prototype as unknown as Record<string, unknown>).scrollIntoView;
	 document.querySelector("meta[name='ariel-auth-mode']")?.remove();
	 document.querySelector("meta[name='ariel-chilling-file-context']")?.remove();
});

const fixtureThread: Thread = { threadId: "fixture", title: "Fixture", cwd: "/tmp/fixture", updatedAt: "2026-10-04T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] };

async function expandProject(cwd: string) {
  const path = normalizeProjectPath(cwd) || "路径未知";
  fireEvent.click(await screen.findByRole("button", { name: `展开项目 ${projectName(cwd)}，${path}` }));
}

async function openFixture(thread: Thread = fixtureThread, queue = false, threadCreate = false, listedThreads: Thread[] = [thread], threadArchive = false, threadMetadata = false, fileContext = false, workspaceBrowser = false) {
  sessionStorage.setItem("ariel.web-session.v1", relaySession);
  render(<App />);
  await waitFor(() => expect(BrowserSocket.sockets).toHaveLength(1));
  const socket = BrowserSocket.sockets[0];
  const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
  socket.onopen?.(new Event("open"));
  act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
  await waitFor(() => expect(requests("device.list")).toHaveLength(1));
  await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
    { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true, ...(queue ? { queue: true } : {}), ...(threadCreate ? { threadBootstrap: true } : {}), ...(threadArchive ? { threadArchive: true, threadArchiveUndo: true } : {}), ...(threadMetadata ? { threadRename: true, threadPin: true } : {}), ...(fileContext ? { chillingFileContext: true, chillingMessageReferences: true } : {}), ...(workspaceBrowser ? { chillingWorkspaceBrowser: true } : {}) } },
  ] } }));
  await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
  await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: listedThreads } }));
  await expandProject(thread.cwd);
  fireEvent.click((await screen.findByText(thread.title)).closest("button")!);
  await waitFor(() => expect(requests("thread.cache.watch")).toHaveLength(1));
  await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
  await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub" } }));
  act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread }));
  await waitFor(() => expect(screen.getByRole("heading", { name: thread.title })).toBeTruthy());
  return { socket, requests };
}

describe("Ariel app interactions", () => {
  it("keeps the transcript and editable draft on PIN expiry and resumes the same conversation", async () => {
    const thread: Thread = { ...fixtureThread, turns: [{ turnId: "t", status: "completed", items: [{ itemId: "a", role: "assistant", text: "失效之前的正文" }] }] };
    const { socket } = await openFixture(thread);
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "不应丢失的草稿" } });
    act(() => socket.close(1008, "unauthorized"));
    expect(screen.getByText("失效之前的正文")).toBeTruthy();
    expect(screen.queryByLabelText("连接 Relay")).toBeNull();
    expect(screen.getByRole("textbox", { name: "发送消息" })).toHaveProperty("value", "不应丢失的草稿");
    expect(screen.getByRole("textbox", { name: "发送消息" })).toHaveProperty("disabled", false);
    expect(screen.getByRole("button", { name: "正在同步会话，暂不可发送" })).toHaveProperty("disabled", true);
    fireEvent.change(screen.getByLabelText("6 位连接码"), { target: { value: "012345" } });
    fireEvent.click(screen.getByRole("button", { name: "重新连接" }));
    const resumed = BrowserSocket.sockets[1];
    resumed.onopen?.(new Event("open"));
    act(() => resumed.message({ type: "hello.ok", v: 1, connectionId: "c2", relayEpoch: "e", sessionToken: relaySession }));
    const requests = (method: string) => resumed.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => resumed.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    expect(requests("thread.subscribe")[0].params.threadId).toBe(thread.threadId);
    expect(requests("turn.start")).toHaveLength(0);
    expect(screen.getByText("失效之前的正文")).toBeTruthy();
    expect(screen.getByRole("button", { name: "正在同步会话，暂不可发送" })).toHaveProperty("disabled", true);
    await act(async () => resumed.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub2" } }));
    act(() => resumed.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub2", streamId: "s2", seq: 1, thread }));
    await waitFor(() => expect(screen.getByRole("button", { name: "发送" })).toHaveProperty("disabled", false));
    expect(screen.getByRole("textbox", { name: "发送消息" })).toHaveProperty("value", "不应丢失的草稿");
  });

  async function openPasskeyFixture(authenticated: () => boolean = () => true) {
    const meta = document.createElement("meta"); meta.name = "ariel-auth-mode"; meta.content = "passkey"; document.head.append(meta);
    const check = vi.spyOn(passkeyAPI, "loadAuthStatus").mockImplementation(async () => ({ mode: "passkey", authenticated: authenticated(), enrollmentRequired: false }));
    const result = await openFixture({ ...fixtureThread, turns: [{ turnId: "t", status: "completed", items: [{ itemId: "a", role: "assistant", text: "Passkey 原会话正文" }] }] });
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "Passkey 草稿" } });
    return { ...result, check };
  }

  it("refreshes a valid Cookie in place only once, even when the new socket is also unauthorized", async () => {
    const { socket, check } = await openPasskeyFixture();
    act(() => socket.close(1008, "unauthorized"));
    await waitFor(() => expect(BrowserSocket.sockets).toHaveLength(2));
    expect(check).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Passkey 原会话正文")).toBeTruthy();
    const retry = BrowserSocket.sockets[1]; retry.onopen?.(new Event("open"));
    act(() => retry.message({ type: "hello.ok", v: 1, connectionId: "c2", relayEpoch: "e" }));
    act(() => retry.close(1008, "unauthorized"));
    await waitFor(() => expect(screen.getByRole("button", { name: "重新登录" })).toHaveProperty("disabled", false));
    expect(check).toHaveBeenCalledTimes(2);
    expect(BrowserSocket.sockets).toHaveLength(2);
    expect(screen.queryByLabelText("Passkey 登录")).toBeNull();
    expect(screen.getByRole("textbox", { name: "发送消息" })).toHaveProperty("value", "Passkey 草稿");
  });

  it("keeps context after Passkey cancellation and retries only on a user gesture", async () => {
    let valid = true;
    const { socket } = await openPasskeyFixture(() => valid);
    valid = false;
    const login = vi.spyOn(passkeyAPI, "loginWithPasskey").mockRejectedValueOnce(new DOMException("cancel", "NotAllowedError")).mockResolvedValueOnce(undefined);
    act(() => socket.close(1008, "unauthorized"));
    const retry = await screen.findByRole("button", { name: "重新登录" });
    await waitFor(() => expect(retry).toHaveProperty("disabled", false));
    expect(login).not.toHaveBeenCalled();
    fireEvent.click(retry);
    expect(await screen.findByText("Passkey 操作已取消；你可以重新尝试。")).toBeTruthy();
    expect(screen.getByText("Passkey 原会话正文")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "发送消息" })).toHaveProperty("value", "Passkey 草稿");
    fireEvent.click(retry);
    await waitFor(() => expect(BrowserSocket.sockets).toHaveLength(2));
    expect(login).toHaveBeenCalledTimes(2);
  });

  it("fences a delayed Cookie refresh after explicit logout", async () => {
    const { socket, check } = await openPasskeyFixture();
    let finish!: (value: passkeyAPI.AuthStatus) => void;
    check.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    vi.spyOn(passkeyAPI, "logoutPasskey").mockResolvedValue(undefined);
    act(() => socket.close(1008, "unauthorized"));
    fireEvent.click(screen.getByRole("button", { name: "退出", hidden: true }));
    await act(async () => finish({ mode: "passkey", authenticated: true, enrollmentRequired: false }));
    expect(BrowserSocket.sockets).toHaveLength(1);
    expect(screen.queryByText("Passkey 原会话正文")).toBeNull();
    expect(await screen.findByRole("button", { name: "使用 Passkey 登录" })).toBeTruthy();
  });

  it("keeps context when the silent status check fails and allows manual login", async () => {
    const { socket, check } = await openPasskeyFixture();
    check.mockRejectedValueOnce(new Error("network"));
    act(() => socket.close(1008, "unauthorized"));
    expect(await screen.findByText("无法刷新登录状态，请重新登录。")).toBeTruthy();
    expect(screen.getByRole("button", { name: "重新登录" })).toHaveProperty("disabled", false);
    expect(screen.getByRole("textbox", { name: "发送消息" })).toHaveProperty("value", "Passkey 草稿");
  });

  it("does not revive a conversation from a delayed manual authentication after logout", async () => {
    let valid = true;
    const { socket } = await openPasskeyFixture(() => valid);
    valid = false;
    let finish!: () => void;
    vi.spyOn(passkeyAPI, "loginWithPasskey").mockImplementation(() => new Promise(resolve => { finish = resolve; }));
    vi.spyOn(passkeyAPI, "logoutPasskey").mockResolvedValue(undefined);
    act(() => socket.close(1008, "unauthorized"));
    const retry = await screen.findByRole("button", { name: "重新登录" });
    await waitFor(() => expect(retry).toHaveProperty("disabled", false));
    fireEvent.click(retry);
    fireEvent.click(screen.getByRole("button", { name: "退出", hidden: true }));
    await act(async () => finish());
    expect(BrowserSocket.sockets).toHaveLength(1);
    expect(screen.queryByText("Passkey 原会话正文")).toBeNull();
  });
	it("shows native execution options and disables only sending while saving", async () => {
		const {socket,requests}=await openFixture();
		await waitFor(()=>expect(requests("model.catalog.read")).toHaveLength(1));
		await act(async()=>socket.message({type:"response",v:1,requestId:requests("model.catalog.read")[0].requestId,outcome:"accepted",data:{agentEpoch:"agent-epoch",settingsSupported:true,catalog:{models:[{id:"native-model",name:"Native Model",description:"",efforts:[{id:"low",description:""},{id:"high",description:""}],defaultEffort:"low",tiers:[],defaultTier:null,inputModalities:["text"],isDefault:true}]}}}));
		await waitFor(()=>expect(requests("thread.settings.watch")).toHaveLength(1));
		await act(async()=>socket.message({type:"response",v:1,requestId:requests("thread.settings.watch")[0].requestId,outcome:"accepted",data:{agentEpoch:"agent-epoch",subscriptionId:"settings-sub",streamId:"settings-stream",seq:1,settings:{revision:"r",freshness:"fresh",writable:true,values:{model:"native-model",effort:"low",speed:{mode:"inherit"}}}}}));
		expect(screen.getByRole("button",{name:"模型与推理设置"}).closest(".composer-actions")).toBeTruthy();
		fireEvent.change(screen.getByRole("textbox",{name:"发送消息"}),{target:{value:"keep draft"}});
		fireEvent.click(screen.getByRole("button",{name:"模型与推理设置"}));fireEvent.change(screen.getByLabelText("推理强度"),{target:{value:"high"}});fireEvent.click(screen.getByRole("button",{name:"保存设置"}));
		await waitFor(()=>expect((screen.getByRole("button",{name:"发送"}) as HTMLButtonElement).disabled).toBe(true));
		expect((screen.getByRole("textbox",{name:"发送消息"}) as HTMLTextAreaElement).value).toBe("keep draft");expect(requests("thread.subscribe")).toHaveLength(1);expect(requests("turn.start")).toHaveLength(0);
		const file = new File([Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="), value => value.charCodeAt(0))], "shot.png", { type: "image/png" });
		fireEvent.change(screen.getByLabelText("附加截图"),{target:{files:[file]}});
		const warning=await screen.findByText("当前模型不支持图片，请更换模型或移除截图。");
		expect(warning.closest(".composer-actions")).toBeNull();expect(warning.closest(".composer")).toBeTruthy();
	});
	it("shows the workspace browser only when Web flag and Agent capability are ready", async () => {
		const meta=document.createElement("meta");meta.name="ariel-chilling-file-context";meta.content="true";document.head.append(meta);
		const {socket,requests}=await openFixture(fixtureThread,false,false,[fixtureThread],false,false,true,true);
		fireEvent.click(screen.getByRole("button",{name:"浏览工作区文件"}));
		await waitFor(()=>expect(requests("workspace.files.list")).toHaveLength(1));
		expect(requests("workspace.files.list")[0].params).toEqual({threadId:"fixture",path:"",limit:100});
		await act(async()=>socket.message({type:"response",v:1,requestId:requests("workspace.files.list")[0].requestId,outcome:"accepted",data:{path:"",revision:"r",entries:[]}}));
		expect(await screen.findByRole("dialog",{name:"工作区文件"})).toBeTruthy();
	});
	it("opens an inline workspace document reference through the controlled gateway", async () => {
		const meta = document.createElement("meta"); meta.name = "ariel-chilling-file-context"; meta.content = "true"; document.head.append(meta);
		const thread: Thread = { ...fixtureThread, turns: [{ turnId: "turn-doc", status: "completed", items: [{ itemId: "reply-doc", role: "assistant", text: "文件：[周报](ops-2026-10-25.md:2)" }] }] };
		const { socket, requests } = await openFixture(thread, false, false, [thread], false, false, true);
		await waitFor(() => expect(requests("file.context.changes")).toHaveLength(1));
		await act(async () => socket.message({ type:"response",v:1,requestId:requests("file.context.changes")[0].requestId,outcome:"accepted",data:{turns:[{turnId:"turn-doc",fileChanges:[]}]}}));
		fireEvent.click(screen.getByRole("button", { name: "打开文档：周报" }));
		await waitFor(() => expect(requests("file.context.reference.read")).toHaveLength(1));
		expect(requests("file.context.reference.read")[0].params).toMatchObject({threadId:"fixture",turnId:"turn-doc",itemId:"reply-doc",referenceKind:"markdown_link",rawReference:"ops-2026-10-25.md:2"});
		await act(async () => socket.message({type:"response",v:1,requestId:requests("file.context.reference.read")[0].requestId,outcome:"accepted",data:{path:"ops-2026-10-25.md",content:"first\nsecond",sha256:"a".repeat(64),modifiedAtMs:2,lineCount:2,language:"md",version:"current-workspace"}}));
		expect(await screen.findByRole("dialog", { name:"文件 ops-2026-10-25.md" })).toBeTruthy();
	});
	it("opens a fresh two-turn Relay cache without creating an Agent read subscription", async () => {
		BrowserSocket.autoRejectCacheWatch = false;
		sessionStorage.setItem("ariel.web-session.v1", relaySession);
		render(<App />);
		const socket = BrowserSocket.sockets[0];
		const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		socket.onopen?.(new Event("open"));
		act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(requests("device.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
		await expandProject(fixtureThread.cwd);
		fireEvent.click((await screen.findByText(fixtureThread.title)).closest("button")!);
		await waitFor(() => expect(requests("thread.cache.watch")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-a", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache-a", seq: 1, capturedAt: "2026-10-25T00:00:00Z", thread: fixtureThread } }));
		await waitFor(() => expect(screen.getByRole("heading", { name: fixtureThread.title })).toBeTruthy());
		expect(requests("thread.read")).toHaveLength(0);
		expect(requests("thread.subscribe")).toHaveLength(0);
	});
	it("re-establishes a Relay cache watcher after WebSocket reconnect and receives the terminal snapshot", async () => {
		BrowserSocket.autoRejectCacheWatch = false;
		sessionStorage.setItem("ariel.web-session.v1", relaySession);
		const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "working", role: "assistant", text: "重连前处理中" }] }] };
		render(<App />);
		const first = BrowserSocket.sockets[0];
		const firstRequests = (method: string) => first.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		first.onopen?.(new Event("open"));
		act(() => first.message({ type: "hello.ok", v: 1, connectionId: "c1", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(firstRequests("device.list")).toHaveLength(1));
		act(() => first.message({ type: "response", v: 1, requestId: firstRequests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(firstRequests("thread.list")).toHaveLength(1));
		act(() => first.message({ type: "response", v: 1, requestId: firstRequests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [running] } }));
		await expandProject(running.cwd);
		fireEvent.click((await screen.findByText(running.title)).closest("button")!);
		await waitFor(() => expect(firstRequests("thread.cache.watch")).toHaveLength(1));
		act(() => first.message({ type: "response", v: 1, requestId: firstRequests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-before", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache", seq: 1, capturedAt: running.updatedAt, thread: running } }));
		expect(await screen.findByText("重连前处理中")).toBeTruthy();

		vi.spyOn(Math, "random").mockReturnValue(0);
		act(() => first.close(1006));
		await act(async () => { await new Promise(resolve => window.setTimeout(resolve, 900)); });
		const second = BrowserSocket.sockets[1];
		const secondRequests = (method: string) => second.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		second.onopen?.(new Event("open"));
		act(() => second.message({ type: "hello.ok", v: 1, connectionId: "c2", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(secondRequests("device.list")).toHaveLength(1));
		act(() => second.message({ type: "response", v: 1, requestId: secondRequests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(secondRequests("thread.cache.watch")).toHaveLength(1));
		const terminal: Thread = { ...running, runtime: "idle", turns: [{ turnId: "active", status: "completed", items: [{ itemId: "working", role: "assistant", text: "重连后自动完成" }] }] };
		act(() => second.message({ type: "response", v: 1, requestId: secondRequests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-after", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache", seq: 2, capturedAt: terminal.updatedAt, thread: terminal } }));
		expect(await screen.findByText("重连后自动完成")).toBeTruthy();
		expect(secondRequests("thread.subscribe")).toHaveLength(0);
	});
	it("shows lightweight history while a missing Relay cache is promoted in the background", async () => {
		BrowserSocket.autoRejectCacheWatch = false;
		sessionStorage.setItem("ariel.web-session.v1", relaySession);
		render(<App />);
		const socket = BrowserSocket.sockets[0];
		const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		socket.onopen?.(new Event("open"));
		act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(requests("device.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
		await expandProject(fixtureThread.cwd);
		fireEvent.click((await screen.findByText(fixtureThread.title)).closest("button")!);
		await waitFor(() => expect(requests("thread.cache.watch")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-missing", freshness: "missing", agentOnline: true, agentEpoch: "epoch" } }));
		await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
		expect(requests("thread.cache.promote")).toHaveLength(1);
		expect(requests("thread.subscribe")).toHaveLength(0);
		const stored: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "stored", status: "completed", items: [{ itemId: "stored-item", role: "assistant", text: "轻量历史先显示" }] }] };
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stored } }));
		expect(await screen.findByText("轻量历史先显示")).toBeTruthy();
		expect((screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement).disabled).toBe(true);
		const fresh: Thread = { ...stored, runtime: "idle", turns: [...stored.turns, { turnId: "fresh", status: "completed", items: [{ itemId: "fresh-item", role: "assistant", text: "Owner 已接管" }] }] };
		act(() => socket.message({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: "fixture", watchId: "watch-missing", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache-fresh", seq: 1, capturedAt: "2026-10-25T00:00:01Z", thread: fresh }));
		expect(await screen.findByText("Owner 已接管")).toBeTruthy();
		expect(screen.queryByRole("button", { name: "正在同步会话，暂不可发送" })).toBeNull();
	});
	it("promotes the existing Relay watcher when reconnecting a failed cache without losing the draft", async () => {
		BrowserSocket.autoRejectCacheWatch = false;
		sessionStorage.setItem("ariel.web-session.v1", relaySession);
		render(<App />);
		const socket = BrowserSocket.sockets[0];
		const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		socket.onopen?.(new Event("open"));
		act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(requests("device.list")).toHaveLength(1));
		act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
		act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
		await expandProject(fixtureThread.cwd);
		fireEvent.click((await screen.findByText(fixtureThread.title)).closest("button")!);
		await waitFor(() => expect(requests("thread.cache.watch")).toHaveLength(1));
		const thread: Thread = { ...fixtureThread, turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old", role: "assistant", text: "失败期间仍可阅读" }] }] };
		act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-retry", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache", seq: 1, capturedAt: thread.updatedAt, thread } }));
		expect(await screen.findByText("失败期间仍可阅读")).toBeTruthy();
		fireEvent.change(screen.getByLabelText("发送消息"), { target: { value: "恢复期间保留的草稿" } });
		act(() => socket.message({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: thread.threadId, watchId: "watch-retry", freshness: "stale", staleReason: "RESYNC_REQUIRED", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache", seq: 1, capturedAt: thread.updatedAt, thread }));
		fireEvent.click(await screen.findByRole("button", { name: "重新连接" }));
		await waitFor(() => expect(requests("thread.cache.promote")).toHaveLength(1));
		expect(requests("thread.subscribe")).toHaveLength(0);
		expect(screen.getByText("失败期间仍可阅读")).toBeTruthy();
		expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).value).toBe("恢复期间保留的草稿");
		const recovered: Thread = { ...thread, turns: [{ turnId: "new", status: "completed", items: [{ itemId: "new", role: "assistant", text: "重新连接后自动更新" }] }] };
		act(() => socket.message({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: thread.threadId, watchId: "watch-retry", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "new-cache", seq: 1, capturedAt: thread.updatedAt, thread: recovered }));
		expect(await screen.findByText("重新连接后自动更新")).toBeTruthy();
		// A fresh native snapshot is authoritative even if the promotion's
		// request receipt subsequently times out or fails.
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.cache.promote")[0].requestId, outcome: "rejected", error: { code: "TIMEOUT", message: "TIMEOUT" } }));
		expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false);
	});
	it("keeps a fresh owner snapshot when the lightweight history response arrives late", async () => {
		BrowserSocket.autoRejectCacheWatch = false;
		sessionStorage.setItem("ariel.web-session.v1", relaySession);
		render(<App />);
		const socket = BrowserSocket.sockets[0];
		const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		socket.onopen?.(new Event("open"));
		act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(requests("device.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
		await expandProject(fixtureThread.cwd);
		fireEvent.click((await screen.findByText(fixtureThread.title)).closest("button")!);
		await waitFor(() => expect(requests("thread.cache.watch")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-race", freshness: "missing", agentOnline: true, agentEpoch: "epoch" } }));
		await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
		const fresh: Thread = { ...fixtureThread, turns: [{ turnId: "fresh", status: "completed", items: [{ itemId: "fresh-item", role: "assistant", text: "Owner 先完成" }] }] };
		act(() => socket.message({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: "fixture", watchId: "watch-race", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "cache-fresh", seq: 1, capturedAt: "2026-10-25T00:00:01Z", thread: fresh }));
		expect(await screen.findByText("Owner 先完成")).toBeTruthy();
		const late: Thread = { ...fixtureThread, turns: [{ turnId: "late", status: "completed", items: [{ itemId: "late-item", role: "assistant", text: "迟到的轻量历史" }] }] };
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: late } }));
		expect(screen.queryByText("迟到的轻量历史")).toBeNull();
		expect(screen.getByText("Owner 先完成")).toBeTruthy();
	});
	it("stops background syncing when an empty cache promotion ends in a terminal error", async () => {
		BrowserSocket.autoRejectCacheWatch = false;
		sessionStorage.setItem("ariel.web-session.v1", relaySession);
		render(<App />);
		const socket = BrowserSocket.sockets[0];
		const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
		socket.onopen?.(new Event("open"));
		act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
		await waitFor(() => expect(requests("device.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true } }] } }));
		await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
		await expandProject(fixtureThread.cwd);
		fireEvent.click((await screen.findByText(fixtureThread.title)).closest("button")!);
		await waitFor(() => expect(requests("thread.cache.watch")).toHaveLength(1));
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.cache.watch")[0].requestId, outcome: "accepted", data: { watchId: "watch-timeout", freshness: "missing", agentOnline: true, agentEpoch: "epoch" } }));
		await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
		act(() => socket.message({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: "fixture", watchId: "watch-timeout", freshness: "missing", agentOnline: true, agentEpoch: "epoch", staleReason: "TIMEOUT" }));
		const stored: Thread = { ...fixtureThread, turns: [{ turnId: "stored", status: "completed", items: [{ itemId: "stored-item", role: "assistant", text: "超时前已加载的历史" }] }] };
		await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stored } }));
		expect(await screen.findByText("超时前已加载的历史")).toBeTruthy();
		await waitFor(() => expect(screen.queryByRole("button", { name: "正在同步会话，暂不可发送" })).toBeNull());
		expect(screen.getByText(/历史只读 ·/)).toBeTruthy();
		expect(screen.getByText("超时前已加载的历史")).toBeTruthy();
	});
	it("uses Passkey mode without exposing a PIN fallback", async () => {
		const meta = document.createElement("meta");
		meta.name = "ariel-auth-mode";
		meta.content = "passkey";
		document.head.append(meta);
		vi.stubGlobal("fetch", vi.fn(async (url: string) => {
			if (url === "/api/auth/status") return new Response(JSON.stringify({ mode: "passkey", authenticated: false, enrollmentRequired: false }), { status: 200 });
			if (url.endsWith("/options")) return new Response(JSON.stringify({ publicKey: { challenge: "AQI", rpId: "ariel.example.com", allowCredentials: [], userVerification: "required" } }), { status: 200 });
			return new Response(JSON.stringify({ ok: true }), { status: 200 });
		}));
		Object.defineProperty(navigator, "credentials", {
			configurable: true,
			value: { get: vi.fn(async () => ({
				id: "credential", type: "public-key", rawId: new Uint8Array([1]).buffer,
				authenticatorAttachment: "platform", getClientExtensionResults: () => ({}),
				response: { clientDataJSON: new Uint8Array([2]).buffer, authenticatorData: new Uint8Array([3]).buffer, signature: new Uint8Array([4]).buffer, userHandle: null },
			} as unknown as PublicKeyCredential)) },
		});

		render(<App />);
		expect(await screen.findByRole("button", { name: "使用 Passkey 登录" })).toBeTruthy();
		expect(screen.queryByLabelText("6 位连接码")).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "使用 Passkey 登录" }));
		await waitFor(() => expect(BrowserSocket.sockets).toHaveLength(1));
		BrowserSocket.sockets[0].onopen?.(new Event("open"));
		expect(JSON.parse(BrowserSocket.sockets[0].sent[0]).token).toBe("cookie");
	});
	it("does not misreport a Passkey protocol mismatch as an expired login", async () => {
		const meta = document.createElement("meta");
		meta.name = "ariel-auth-mode";
		meta.content = "passkey";
		document.head.append(meta);
		vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ mode: "passkey", authenticated: true, enrollmentRequired: false }), { status: 200 })));
		render(<App />);
		await waitFor(() => expect(BrowserSocket.sockets).toHaveLength(1));
		const socket = BrowserSocket.sockets[0];
		socket.onopen?.(new Event("open"));
		act(() => socket.close(1008, "invalid message"));
		expect(await screen.findByRole("heading", { name: "页面版本不兼容" })).toBeTruthy();
		expect(screen.getByRole("button", { name: "刷新页面" })).toBeTruthy();
		expect(screen.queryByRole("button", { name: "使用 Passkey 登录" })).toBeNull();
		expect(screen.queryByText("登录会话已失效，请重新使用 Passkey。")).toBeNull();
	});
	it("keeps confirmed conversation content visible on a protocol mismatch", async () => {
		const content: Thread = { ...fixtureThread, turns: [{ turnId: "turn", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "已经确认的会话正文" }] }] };
		const { socket } = await openFixture(content);
		expect(await screen.findByText("已经确认的会话正文")).toBeTruthy();
		act(() => socket.close(1008, "invalid role or message"));
		expect(screen.getByText("已经确认的会话正文")).toBeTruthy();
		expect(screen.getByText("页面版本与 Relay 不兼容，操作已暂停。请刷新页面。" )).toBeTruthy();
		expect(screen.getByRole("button", { name: "正在同步会话，暂不可发送" }).hasAttribute("disabled")).toBe(true);
	});
  it("keeps every sidebar action in a compact header without repeated labels or status rows", async () => {
    await openFixture(fixtureThread, false, true);
    const sidebar = screen.getByLabelText("会话列表");
    expect(sidebar.querySelector(".sidebar-toolbar")).toBeTruthy();
    expect(sidebar.querySelector(".sidebar-identity")).toBeNull();
    expect(sidebar.querySelector(".sidebar-head")).toBeNull();
    expect(sidebar.querySelector(".device-label")).toBeNull();
    expect(sidebar.querySelector(".device-meta")).toBeNull();
    expect(sidebar.querySelector("#sidebar-device-status")?.textContent).toContain("Agent 在线，Codex 就绪");
    expect(screen.getByLabelText("设备").getAttribute("aria-describedby")).toBe("sidebar-device-status");
    for (const label of ["新建会话", "手机扫码登录", "刷新会话", "断开", "关闭会话列表"]) {
      expect(sidebar.querySelector(`button[aria-label='${label}']`)).toBeTruthy();
    }
  });

  it("shows a local unread marker after a batch refresh and clears it only after the selected snapshot renders", async () => {
    const background: Thread = { ...fixtureThread, threadId: "background", title: "Background", cwd: fixtureThread.cwd };
    const { socket, requests } = await openFixture(fixtureThread, false, false, [fixtureThread, background]);
    const backgroundRow = document.querySelector<HTMLElement>('.thread-row[data-thread-id="background"]')!;
    expect(backgroundRow.querySelector('[aria-label="有新结果"]')).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "刷新会话" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    const changed = { ...background, updatedAt: "2026-10-04T00:00:01Z" };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [fixtureThread, changed] } }));
    expect(document.querySelector('.thread-row[data-thread-id="background"] [aria-label="有新结果"]')).toBeTruthy();

    fireEvent.click(document.querySelector<HTMLElement>('.thread-row[data-thread-id="background"]')!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    expect(document.querySelector('.thread-row[data-thread-id="background"] [aria-label="有新结果"]')).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-background" } }));
    expect(document.querySelector('.thread-row[data-thread-id="background"] [aria-label="有新结果"]')).toBeTruthy();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "background", subscriptionId: "sub-background", streamId: "stream-background", seq: 1, thread: changed }));
    expect(document.querySelector('.thread-row[data-thread-id="background"] [aria-label="有新结果"]')).toBeTruthy();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await waitFor(() => expect(document.querySelector('.thread-row[data-thread-id="background"] [aria-label="有新结果"]')).toBeNull());
  });

  it("keeps a project unread marker until every unread session in that project is viewed", async () => {
    const alphaOne: Thread = { ...fixtureThread, threadId: "alpha-one", title: "Alpha one", cwd: "/work/alpha" };
    const alphaTwo: Thread = { ...fixtureThread, threadId: "alpha-two", title: "Alpha two", cwd: "/work/alpha/" };
    const { socket, requests } = await openFixture(fixtureThread, false, false, [fixtureThread, alphaOne, alphaTwo]);

    fireEvent.click(screen.getByRole("button", { name: "刷新会话" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    const changedOne = { ...alphaOne, updatedAt: "2026-10-04T00:00:01Z" };
    const changedTwo = { ...alphaTwo, updatedAt: "2026-10-04T00:00:02Z" };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [fixtureThread, changedOne, changedTwo] } }));

    const alphaGroup = await screen.findByRole("region", { name: "项目 alpha" });
    expect(alphaGroup.querySelector('[aria-label="项目有新结果"]')).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "展开项目 alpha，/work/alpha" }));
    expect(alphaGroup.querySelector('[aria-label="项目有新结果"]')).toBeTruthy();
    expect(requests("thread.list")).toHaveLength(2);

    fireEvent.click(screen.getByText("Alpha one").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-alpha-one" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "alpha-one", subscriptionId: "sub-alpha-one", streamId: "stream-alpha-one", seq: 1, thread: changedOne }));
    await waitFor(() => expect(document.querySelector('.thread-row[data-thread-id="alpha-one"] [aria-label="有新结果"]')).toBeNull());
    expect(alphaGroup.querySelector('[aria-label="项目有新结果"]')).toBeTruthy();

    fireEvent.click(screen.getByText("Alpha two").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(3));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[2].requestId, outcome: "accepted", data: { subscriptionId: "sub-alpha-two" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "alpha-two", subscriptionId: "sub-alpha-two", streamId: "stream-alpha-two", seq: 1, thread: changedTwo }));
    await waitFor(() => expect(alphaGroup.querySelector('[aria-label="项目有新结果"]')).toBeNull());
    expect(requests("thread.list")).toHaveLength(2);
  });

  it("uses one non-overlapping batch request for periodic unread refresh", async () => {
    let refresh: (() => void) | undefined;
    vi.spyOn(window, "setInterval").mockImplementation(((handler: TimerHandler, timeout?: number) => {
      if (timeout === 15_000 && typeof handler === "function") refresh = handler as unknown as () => void;
      return 91;
    }) as typeof window.setInterval);
    const { socket, requests } = await openFixture();
    expect(refresh).toBeTruthy();
    act(() => refresh!());
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    act(() => refresh!());
    expect(requests("thread.list")).toHaveLength(2);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    act(() => refresh!());
    await waitFor(() => expect(requests("thread.list")).toHaveLength(3));
  });

  it("opens the thread menu by secondary click and archives only a verified idle selection", async () => {
    const { socket, requests } = await openFixture(fixtureThread, false, false, [fixtureThread], true);
    const row = document.querySelector<HTMLElement>('.thread-row[data-thread-id="fixture"]')!;
    fireEvent.contextMenu(row, { clientX: 120, clientY: 180 });
    const archive = await screen.findByRole("menuitem", { name: "归档" });
    expect((archive as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(archive);
    expect(requests("thread.archive.update")).toHaveLength(1);
    expect(requests("thread.archive.update")[0].params).toEqual({ threadId: "fixture", archived: true });
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.archive.update")[0].requestId, outcome: "accepted", data: { operationId: "archive-one" } }));
    expect(screen.queryByText("Fixture")).toBeNull();
    expect(screen.getByRole("button", { name: "撤销归档" })).toBeTruthy();
  });

  it("renames a thread from the context menu without replacing the transcript or draft", async () => {
    const withMessage: Thread = { ...fixtureThread, turns: [{ turnId: "turn", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "保留的正文" }] }] };
    const { socket, requests } = await openFixture(withMessage, false, false, [withMessage], false, true);
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "未发送草稿" } });
    fireEvent.contextMenu(document.querySelector<HTMLElement>('.thread-row[data-thread-id="fixture"]')!, { clientX: 120, clientY: 180 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "重命名" }));
    const input = screen.getByRole("textbox", { name: "会话名称" });
    expect((input as HTMLInputElement).value).toBe("Fixture");
    expect((input as HTMLInputElement).selectionStart).toBe(0);
    expect((input as HTMLInputElement).selectionEnd).toBe("Fixture".length);
    fireEvent.change(input, { target: { value: "新" } });
    expect((input as HTMLInputElement).selectionStart).toBe(1);
    expect((input as HTMLInputElement).selectionEnd).toBe(1);
    fireEvent.change(input, { target: { value: "  新名称  " } });
    fireEvent.click(screen.getByRole("button", { name: "保存名称" }));
    await waitFor(() => expect(requests("thread.name.update")).toHaveLength(1));
    expect(requests("thread.name.update")[0].params).toEqual({ threadId: "fixture", name: "新名称" });
    const renamed = { ...withMessage, title: "新名称", updatedAt: "2026-10-04T00:00:01Z" };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.name.update")[0].requestId, outcome: "accepted", data: { thread: renamed } }));
    expect(await screen.findByRole("heading", { name: "新名称" })).toBeTruthy();
    expect(screen.getByText("保留的正文")).toBeTruthy();
    expect((screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement).value).toBe("未发送草稿");
    fireEvent.click(screen.getByRole("button", { name: "刷新会话" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [renamed] } }));
    expect(document.querySelector('[aria-label="有新结果"]')).toBeNull();
  });

  it("pins and unpins through one verified metadata mutation", async () => {
    const { socket, requests } = await openFixture(fixtureThread, false, false, [fixtureThread], false, true);
    const openMenu = () => fireEvent.contextMenu(document.querySelector<HTMLElement>('.thread-row[data-thread-id="fixture"]')!, { clientX: 120, clientY: 180 });
    openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "置顶" }));
    expect(requests("thread.pin.update")).toHaveLength(1);
    expect(requests("thread.pin.update")[0].params).toEqual({ threadId: "fixture", pinned: true });
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.pin.update")[0].requestId, outcome: "accepted", data: { thread: { ...fixtureThread, isPinned: true } } }));
    expect(await screen.findByRole("region", { name: "置顶会话" })).toBeTruthy();
    openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "取消置顶" }));
    expect(requests("thread.pin.update")).toHaveLength(2);
    expect(requests("thread.pin.update")[1].params).toEqual({ threadId: "fixture", pinned: false });
  });

  it("requires a 500ms stationary long press and suppresses its following click", async () => {
    const { requests } = await openFixture(fixtureThread, false, false, [fixtureThread], true);
    const row = document.querySelector<HTMLElement>('.thread-row[data-thread-id="fixture"]')!;
    vi.useFakeTimers();
    try {
      fireEvent.pointerDown(row, { pointerId: 7, clientX: 40, clientY: 80 });
      await act(async () => { await vi.advanceTimersByTimeAsync(499); });
      expect(screen.queryByRole("menu")).toBeNull();
      await act(async () => { await vi.advanceTimersByTimeAsync(1); });
      expect(screen.getByRole("menu")).toBeTruthy();
      fireEvent.pointerUp(row, { pointerId: 7, clientX: 40, clientY: 80 });
      fireEvent.click(row);
      expect(requests("thread.subscribe")).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("cancels a thread long press after movement and disables archive for an active session", async () => {
    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [] }] };
    await openFixture(running, false, false, [running], true);
    const row = document.querySelector<HTMLElement>('.thread-row[data-thread-id="fixture"]')!;
    vi.useFakeTimers();
    try {
      fireEvent.pointerDown(row, { pointerId: 8, clientX: 20, clientY: 20 });
      fireEvent.pointerMove(row, { pointerId: 8, clientX: 31, clientY: 20 });
      await act(async () => { await vi.advanceTimersByTimeAsync(500); });
      expect(screen.queryByRole("menu")).toBeNull();
      fireEvent.contextMenu(row, { clientX: 80, clientY: 120 });
      expect((screen.getByRole("menuitem", { name: "归档" }) as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByText("会话处理完成后可归档")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

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

  it("groups sessions by project and keeps every project collapsed until independently expanded", async () => {
    const { socket, requests } = await openFixture();
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索会话" }), { target: { value: "project" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    const alphaNew = { ...fixtureThread, threadId: "alpha-new", title: "Alpha new", cwd: "/work/alpha" };
    const beta = { ...fixtureThread, threadId: "beta", title: "Beta task", cwd: "/work/beta" };
    const alphaOld = { ...fixtureThread, threadId: "alpha-old", title: "Alpha old", cwd: "/work/alpha/" };
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [alphaNew, beta, alphaOld], nextCursor: "" } }));

    const sidebar = screen.getByLabelText("会话列表");
    await waitFor(() => expect(sidebar.querySelectorAll(".project-group")).toHaveLength(2));
    const groups = [...sidebar.querySelectorAll<HTMLElement>(".project-group")];
    expect(groups[0].querySelector(".project-name")?.textContent).toBe("alpha");
    expect(groups[0].querySelector(".project-path")?.textContent).toBe("/work/alpha");
    expect(groups[0].querySelector(".project-count")?.textContent).toBe("2");
    expect(groups[1].querySelector(".project-name")?.textContent).toBe("beta");
    expect(sidebar.querySelector(".list-caption")?.textContent).toContain("2 个项目");
    expect(sidebar.querySelector(".list-caption")?.textContent).toContain("3 个会话");
    const alphaToggle = screen.getByRole("button", { name: "展开项目 alpha，/work/alpha" });
    const betaToggle = screen.getByRole("button", { name: "展开项目 beta，/work/beta" });
    const alphaThreads = document.getElementById(alphaToggle.getAttribute("aria-controls")!);
    expect(alphaToggle.getAttribute("aria-expanded")).toBe("false");
    expect(betaToggle.getAttribute("aria-expanded")).toBe("false");
    expect(alphaThreads?.hidden).toBe(true);
    expect(screen.queryByText("Alpha new")).toBeNull();
    expect(screen.queryByText("Beta task")).toBeNull();

    fireEvent.click(alphaToggle);
    expect(screen.getByRole("button", { name: "收起项目 alpha，/work/alpha" }).getAttribute("aria-expanded")).toBe("true");
    expect(alphaThreads?.hidden).toBe(false);
    expect(screen.getByText("Alpha new")).toBeTruthy();
    expect(screen.getByText("Alpha old")).toBeTruthy();
    expect(screen.queryByText("Beta task")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "收起项目 alpha，/work/alpha" }));
    expect(screen.getByRole("button", { name: "展开项目 alpha，/work/alpha" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("Alpha new")).toBeNull();
  });

  it("shows native pinned sessions before projects without duplicating them in project groups", async () => {
    const { socket, requests } = await openFixture(fixtureThread, false, true);
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索会话" }), { target: { value: "pin" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    const pinnedAlpha: Thread = { ...fixtureThread, threadId: "pinned-alpha", title: "Pinned alpha", cwd: "/work/alpha", isPinned: true };
    const regularAlpha: Thread = { ...fixtureThread, threadId: "regular-alpha", title: "Regular alpha", cwd: "/work/alpha" };
    const pinnedOnly: Thread = { ...fixtureThread, threadId: "pinned-only", title: "Pinned only", cwd: "/work/pinned-only", isPinned: true };
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [pinnedAlpha, regularAlpha, pinnedOnly], nextCursor: "more" } }));

    const sidebar = screen.getByLabelText("会话列表");
    const pinned = await screen.findByRole("region", { name: "置顶会话" });
    const alphaGroup = await screen.findByRole("region", { name: "项目 alpha" });
    expect(pinned.compareDocumentPosition(alphaGroup) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(pinned.textContent).toContain("Pinned alpha");
    expect(pinned.textContent).toContain("Pinned only");
    expect(alphaGroup.querySelector(".project-count")?.textContent).toBe("1");
    expect(alphaGroup.textContent).not.toContain("Pinned alpha");
    expect(sidebar.querySelectorAll(".thread-row[data-thread-id='pinned-alpha']")).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "加载更多 →" }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(3));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[2].requestId, outcome: "accepted", data: { threads: [pinnedAlpha], nextCursor: "" } }));
    await waitFor(() => expect(sidebar.querySelectorAll(".thread-row[data-thread-id='pinned-alpha']")).toHaveLength(1));

    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    const projects = screen.getByRole("combobox", { name: "选择已有项目" }) as HTMLSelectElement;
    expect([...projects.options].some(option => option.value === "/work/pinned-only")).toBe(true);
  });

  it("creates only a local draft, then bootstraps exactly once on the first send", async () => {
    const { socket, requests } = await openFixture(fixtureThread, false, true);
    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    expect(screen.getByRole("dialog", { name: "新建会话" })).toBeTruthy();
    const cwd = screen.getByRole("textbox", { name: "项目目录" });
    expect((cwd as HTMLInputElement).value).toBe("/tmp/fixture");
    fireEvent.change(screen.getByRole("combobox", { name: "选择已有项目" }), { target: { value: "/tmp/fixture" } });
    expect((cwd as HTMLInputElement).value).toBe("/tmp/fixture");
    fireEvent.change(cwd, { target: { value: "/work/new-project" } });
    fireEvent.click(screen.getByRole("button", { name: "创建会话" }));
    expect(requests("thread.create")).toHaveLength(0);
    expect(requests("thread.bootstrap")).toHaveLength(0);
    expect(screen.queryByRole("dialog", { name: "新建会话" })).toBeNull();
    const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
    expect(input.disabled).toBe(false);
    expect(input.placeholder).toContain("第一条消息");
    fireEvent.change(input, { target: { value: "hello" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(requests("thread.bootstrap")).toHaveLength(1));
    expect(requests("thread.bootstrap")[0].params.cwd).toBe("/work/new-project");
    expect(requests("thread.bootstrap")[0].params.text).toBe("hello");
    const created: Thread = { ...fixtureThread, threadId: "00000000-0000-4000-8000-000000000099", title: "", cwd: "/work/new-project", updatedAt: "2026-10-13T00:00:00Z", runtime: "inProgress" };
    const request = requests("thread.bootstrap")[0];
    await act(async () => socket.message({ type: "response", v: 1, requestId: request.requestId, outcome: "accepted", data: { thread: created, threadId: created.threadId, turnId: "turn-first", clientMessageId: request.params.clientMessageId } }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    expect((screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement).value).toBe("");
  });

  it("sends selected draft settings with the first bootstrap and never writes another thread",async()=>{
    const {socket,requests}=await openFixture(fixtureThread,false,true);
    await waitFor(()=>expect(requests("model.catalog.read")).toHaveLength(1));
    await act(async()=>socket.message({type:"response",v:1,requestId:requests("model.catalog.read")[0].requestId,outcome:"accepted",data:{agentEpoch:"agent-epoch",settingsSupported:true,catalog:{models:[{id:"draft-model",name:"Draft Model",description:"",efforts:[{id:"low",description:""},{id:"high",description:""}],defaultEffort:"low",tiers:[],defaultTier:null,inputModalities:["text"],isDefault:true}]}}}));
    fireEvent.click(screen.getByRole("button",{name:"新建会话"}));fireEvent.click(screen.getByRole("button",{name:"创建会话"}));
    await waitFor(()=>expect(screen.getByRole("button",{name:"模型与推理设置"})).toBeTruthy());fireEvent.click(screen.getByRole("button",{name:"模型与推理设置"}));fireEvent.change(screen.getByLabelText("推理强度"),{target:{value:"high"}});fireEvent.change(screen.getByLabelText("速度"),{target:{value:"standard"}});fireEvent.click(screen.getByRole("button",{name:"保存设置"}));
    fireEvent.change(screen.getByRole("textbox",{name:"发送消息"}),{target:{value:"first"}});fireEvent.click(screen.getByRole("button",{name:"发送"}));await waitFor(()=>expect(requests("thread.bootstrap")).toHaveLength(1));
    expect(requests("thread.bootstrap")[0].params.settings).toEqual({model:"draft-model",effort:"high",speed:{mode:"standard"}});expect(requests("thread.settings.update")).toHaveLength(0);
  });

  it("keeps the local draft after an uncertain bootstrap and never retries automatically", async () => {
    const { socket, requests } = await openFixture(fixtureThread, false, true);
    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    fireEvent.change(screen.getByRole("textbox", { name: "项目目录" }), { target: { value: "/work/new-project" } });
    fireEvent.click(screen.getByRole("button", { name: "创建会话" }));
    const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "keep me" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(requests("thread.bootstrap")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.bootstrap")[0].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "unknown" } }));
    expect((screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement).value).toBe("keep me");
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 100)); });
    expect(requests("thread.bootstrap")).toHaveLength(1);
  });

  it("hides thread creation when the Desktop Agent does not advertise it", async () => {
    await openFixture();
    expect(screen.queryByRole("button", { name: "新建会话" })).toBeNull();
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

  it("maps a rejected conversation image to a safe actionable reason", async () => {
    const thread: Thread = { ...fixtureThread, turns: [{ turnId: "turn-image-failure", status: "completed", items: [{ itemId: "reply-image-failure", role: "assistant", text: "![large](large.png)", images: [{ index: 0, kind: "markdown", alt: "large", source: "large.png" }] }] }] };
    const { socket, requests } = await openFixture(thread);
    fireEvent.click(screen.getByRole("button", { name: "加载截图：large" }));
    await waitFor(() => expect(requests("thread.image")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.image")[0].requestId, outcome: "rejected", error: { code: "INVALID_ARGUMENT", message: "INVALID_ARGUMENT" } }));
    expect(await screen.findByText("截图未能加载：图片格式、尺寸或内容不受支持。点此重试")).toBeTruthy();
    expect(document.body.textContent).not.toContain("/Users/");
  });

  it("queues follow-ups while running and supports edit, reorder, delete, and steer", async () => {
    const first = { queueId: "queue-one", clientMessageId: "message-one", text: "第一条", images: [] as [], editable: true };
    const second = { queueId: "queue-two", clientMessageId: "message-two", text: "第二条", images: [] as [], editable: true };
    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active-turn", status: "inProgress", items: [] }], queuedMessages: [first] };
    const { socket, requests } = await openFixture(running, true);
    expect(screen.getByRole("button", { name: "加入队列" })).toBeTruthy();
    expect(document.querySelector(".send-loading-spinner")).toBeNull();

    const input = screen.getByRole("textbox", { name: "发送消息" });
    fireEvent.change(input, { target: { value: "第二条" } });
    fireEvent.click(screen.getByRole("button", { name: "加入队列" }));
    await waitFor(() => expect(requests("queue.add")).toHaveLength(1));
    expect(requests("queue.add")[0].params).toMatchObject({ threadId: "fixture", text: "第二条" });
    expect(requests("turn.start")).toHaveLength(0);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("queue.add")[0].requestId, outcome: "accepted", data: { queuedMessages: [first, second] } }));
    expect((input as HTMLTextAreaElement).value).toBe("");
    expect(screen.getByText(/共 2 条，最上方优先执行/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "更多选项：第一条" }));
    const queueMenu = screen.getByRole("menu", { name: "排队消息选项：第一条" });
    expect(queueMenu.parentElement).toBe(document.body);
    expect(queueMenu.closest(".queue-panel")).toBeNull();
    fireEvent.click(screen.getByRole("menuitem", { name: "编辑" }));
    expect((input as HTMLTextAreaElement).value).toBe("第一条");
    fireEvent.change(input, { target: { value: "第一条（已修改）" } });
    fireEvent.click(screen.getByRole("button", { name: "保存排队消息" }));
    await waitFor(() => expect(requests("queue.update")).toHaveLength(1));
    expect(requests("queue.update")[0].params).toMatchObject({ threadId: "fixture", queueId: "queue-one", text: "第一条（已修改）" });
    const edited = { ...first, text: "第一条（已修改）" };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("queue.update")[0].requestId, outcome: "accepted", data: { queuedMessages: [edited, second] } }));

    fireEvent.keyDown(screen.getByRole("button", { name: "拖拽排序：第一条（已修改）" }), { key: "ArrowDown" });
    await waitFor(() => expect(requests("queue.reorder")).toHaveLength(1));
    expect(requests("queue.reorder")[0].params.queueIds).toEqual(["queue-two", "queue-one"]);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("queue.reorder")[0].requestId, outcome: "accepted", data: { queuedMessages: [second, edited] } }));

    fireEvent.click(screen.getByRole("button", { name: "删除排队消息：第二条" }));
    await waitFor(() => expect(requests("queue.delete")).toHaveLength(1));
    expect(requests("queue.delete")[0].params.queueId).toBe("queue-two");
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("queue.delete")[0].requestId, outcome: "accepted", data: { queuedMessages: [edited] } }));

    fireEvent.click(screen.getByRole("button", { name: "引导：第一条（已修改）" }));
    await waitFor(() => expect(requests("queue.steer")).toHaveLength(1));
    expect(requests("queue.steer")[0].params).toMatchObject({ queueId: "queue-one", expectedTurnId: "active-turn" });
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("queue.steer")[0].requestId, outcome: "accepted", data: { queuedMessages: [] } }));
    expect(screen.queryByLabelText("排队的后续输入")).toBeNull();
  });

  it("uses icon-only functional controls with accessible hover labels", async () => {
    const queued = { queueId: "queue-one", clientMessageId: "message-one", text: "继续检查", images: [] as [], editable: true };
    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active-turn", status: "inProgress", items: [] }], queuedMessages: [queued] };
    await openFixture(running, true);

    for (const name of ["引导：继续检查", "删除排队消息：继续检查", "更多选项：继续检查", "选择截图", "停止", "加入队列"]) {
      const control = screen.getByRole("button", { name });
      expect(control.textContent).toBe("");
      expect(control.querySelector("svg[aria-hidden='true']")).toBeTruthy();
      expect(control.getAttribute("data-tooltip")).toBeTruthy();
    }
  });

  it("keeps dragging downward after the reordered row loses pointer capture", async () => {
    const first = { queueId: "queue-one", clientMessageId: "message-one", text: "第一条", images: [] as [], editable: true };
    const second = { queueId: "queue-two", clientMessageId: "message-two", text: "第二条", images: [] as [], editable: true };
    const third = { queueId: "queue-three", clientMessageId: "message-three", text: "第三条", images: [] as [], editable: true };
    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active-turn", status: "inProgress", items: [] }], queuedMessages: [first, second, third] };
    const { requests } = await openFixture(running, true);
    const handle = screen.getByRole("button", { name: "拖拽排序：第一条" });
    const rows = Array.from(document.querySelectorAll<HTMLElement>(".queue-item[data-queue-id]"));
    rows.forEach((row, index) => vi.spyOn(row, "getBoundingClientRect").mockReturnValue({ x: 0, y: index * 50, top: index * 50, right: 400, bottom: (index + 1) * 50, left: 0, width: 400, height: 50, toJSON: () => ({}) }));
    Object.defineProperty(document, "elementFromPoint", { configurable: true, value: vi.fn(() => handle) });

    fireEvent.pointerDown(handle, { pointerId: 7, clientX: 12, clientY: 25 });
    fireEvent.pointerMove(handle, { pointerId: 7, clientX: 12, clientY: 90 });
    fireEvent.lostPointerCapture(handle, { pointerId: 7, clientX: 12, clientY: 90 });
    fireEvent.pointerMove(document, { pointerId: 7, clientX: 12, clientY: 140 });
    fireEvent.pointerUp(document, { pointerId: 7, clientX: 12, clientY: 140 });
    delete (document as unknown as { elementFromPoint?: typeof document.elementFromPoint }).elementFromPoint;

    await waitFor(() => expect(requests("queue.reorder")).toHaveLength(1));
    expect(requests("queue.reorder")[0].params.queueIds).toEqual(["queue-two", "queue-three", "queue-one"]);
  });

  it("keeps a queued item and explains an unsupported guide without mentioning history pagination", async () => {
    const queued = { queueId: "queue-one", clientMessageId: "message-one", text: "继续检查", images: [] as [], editable: true };
    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active-turn", status: "inProgress", items: [] }], queuedMessages: [queued] };
    const { socket, requests } = await openFixture(running, true);

    fireEvent.click(screen.getByRole("button", { name: "引导：继续检查" }));
    await waitFor(() => expect(requests("queue.steer")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("queue.steer")[0].requestId, outcome: "rejected", error: { code: "PROTOCOL_UNSUPPORTED", message: "PROTOCOL_UNSUPPORTED" } }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("立即引导");
    expect(alert.textContent).toContain("排队项仍保留");
    expect(alert.textContent).not.toContain("历史分页");
    expect(screen.getByRole("button", { name: "引导：继续检查" })).toBeTruthy();
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
    await expandProject(fixtureThread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "rejected", error: { code: "HISTORY_TOO_LARGE", message: "HISTORY_TOO_LARGE" } }));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: { ...fixtureThread, runtime: "idle", historyComplete: false, turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "可读历史" }] }] } } }));
    expect(await screen.findByText("可读历史")).toBeTruthy();
    expect(screen.getByText(/历史只读.*超过 64 MiB 安全上限/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "重新连接" })).toBeNull();
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "do not send" } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    expect(document.querySelector(".send-loading-spinner")).toBeNull();
    expect(requests("turn.start")).toHaveLength(0);
  });

  it("explains temporary owner capacity and retries without reloading visible history", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    await expandProject(fixtureThread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    const stored: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "容量冲突时仍可阅读" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stored } }));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "rejected", error: { code: "OVERLOADED", message: "OVERLOADED" } }));

    expect(await screen.findByText("容量冲突时仍可阅读")).toBeTruthy();
    expect(screen.getByText(/实时连接资源繁忙.*发送暂时禁用/)).toBeTruthy();
    const retry = screen.getByRole("button", { name: "重新连接" });
    fireEvent.click(retry);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    expect(requests("thread.read")).toHaveLength(1);
    expect(screen.getByText("容量冲突时仍可阅读")).toBeTruthy();
    expect((screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement).disabled).toBe(true);

    const fresh: Thread = { ...stored, runtime: "idle", turns: [...stored.turns, { turnId: "live", status: "completed", items: [{ itemId: "live-item", role: "assistant", text: "实时连接已恢复" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-retry" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-retry", streamId: "stream-retry", seq: 1, thread: fresh }));
    expect(await screen.findByText("实时连接已恢复")).toBeTruthy();
    expect(screen.queryByText(/实时连接资源繁忙/)).toBeNull();
    expect(screen.queryByText(/历史只读/)).toBeNull();
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
    await expandProject(fixtureThread.cwd);
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
    expect(await screen.findByText(/历史只读.*超过 64 MiB 安全上限/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "重新连接" })).toBeNull();
    expect(screen.getByText("已可见内容")).toBeTruthy();
  });

  it("shows persisted history while the owner sync is still in flight", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    await expandProject(fixtureThread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    const stored: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "历史先可见" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stored } }));
    expect(await screen.findByText("历史先可见")).toBeTruthy();
    expect(screen.getByText(/正在同步最新状态.*操作暂时禁用/)).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "尚未可发" } });
    expect((screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement).disabled).toBe(true);
    expect(requests("turn.start")).toHaveLength(0);
    const fresh: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "历史先可见" }] }, { turnId: "live", status: "completed", items: [{ itemId: "live-item", role: "assistant", text: "实时已接管" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", seq: 1, thread: fresh }));
    expect(await screen.findByText("实时已接管")).toBeTruthy();
    expect(screen.getByText("历史先可见")).toBeTruthy();
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
    expect(screen.queryByText(/历史只读/)).toBeNull();
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "现在可发" } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false);
    expect(requests("thread.read")).toHaveLength(1);
  });

  it("keeps the progressively loaded history read-only when the owner sync fails", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    await expandProject(fixtureThread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    const stored: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "失败前已读到的历史" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stored } }));
    expect(await screen.findByText("失败前已读到的历史")).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "rejected", error: { code: "TIMEOUT", message: "TIMEOUT" } }));
    expect(screen.getByText("失败前已读到的历史")).toBeTruthy();
    expect(await screen.findByText(/历史只读.*Desktop 状态未确认/)).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toContain("重试");
    fireEvent.change(screen.getByRole("textbox", { name: "发送消息" }), { target: { value: "do not send" } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    expect(requests("turn.start")).toHaveLength(0);
  });

  it("accepts the in-flight history after the owner subscription times out first", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    await expandProject(fixtureThread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));

    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "rejected", error: { code: "TIMEOUT", message: "TIMEOUT" } }));
    expect(screen.getByText("正在同步会话…")).toBeTruthy();

    const stored: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "old", status: "completed", items: [{ itemId: "old-item", role: "assistant", text: "迟到但仍应展示的历史" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stored } }));
    expect(await screen.findByText("迟到但仍应展示的历史")).toBeTruthy();
    expect(screen.queryByText("正在同步会话…")).toBeNull();
    expect(await screen.findByText(/历史只读.*Desktop 状态未确认/)).toBeTruthy();
    expect(screen.getByRole("alert").textContent).not.toContain("不要直接重发");
    expect(requests("thread.read")).toHaveLength(1);
    expect(requests("turn.start")).toHaveLength(0);
  });

  it("ignores a late read fast path once the fresh owner snapshot arrived", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [fixtureThread] } }));
    await expandProject(fixtureThread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    await waitFor(() => expect(requests("thread.read")).toHaveLength(1));
    const fresh: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "live", status: "completed", items: [{ itemId: "live-item", role: "assistant", text: "owner 实时内容" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", seq: 1, thread: fresh }));
    expect(await screen.findByText("owner 实时内容")).toBeTruthy();
    const stale: Thread = { ...fixtureThread, runtime: "idle", turns: [{ turnId: "stale", status: "completed", items: [{ itemId: "stale-item", role: "assistant", text: "迟到的只读历史" }] }] };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.read")[0].requestId, outcome: "accepted", data: { thread: stale } }));
    expect(screen.getByText("owner 实时内容")).toBeTruthy();
    expect(screen.queryByText("迟到的只读历史")).toBeNull();
    expect(screen.queryByText(/历史只读/)).toBeNull();
  });

  it("automatically fills a segmented recent window without a tap", async () => {
    const recent: Thread = { ...fixtureThread, historyComplete: false, recentComplete: false, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "最新内容" }] }] };
    const { socket, requests } = await openFixture(recent);
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    const sent = requests("thread.history")[0];
    expect(sent.params).toMatchObject({ threadId: "fixture", limit: 1 });
    await act(async () => socket.message({ type: "response", v: 1, requestId: sent.requestId, outcome: "accepted", data: { turns: [recent.turns[0], { turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "补齐内容" }] }], nextCursor: "" } }));
    expect(await screen.findByText("补齐内容")).toBeTruthy();
    expect(screen.getByText("最新内容")).toBeTruthy();
  });

  it("loads older turns on demand after walking past the recent live overlap", async () => {
    const recent: Thread = { ...fixtureThread, historyComplete: false, recentComplete: true, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "最新内容" }] }] };
    const { socket, requests } = await openFixture(recent);
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    expect(requests("thread.history")[0].params).toMatchObject({ threadId: "fixture", limit: 3 });
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[0].requestId, outcome: "accepted", data: { turns: [recent.turns[0]], nextCursor: "after-latest" } }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[1].requestId, outcome: "accepted", data: { turns: [{ turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "更早内容" }] }], nextCursor: "" } }));
    expect(await screen.findByText("更早内容")).toBeTruthy();
    expect(screen.getByText("最新内容")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "加载更早消息" })).toBeNull();
  });

  it("rejects an oversized manual history page instead of silently skipping turns", async () => {
    const recent: Thread = { ...fixtureThread, historyComplete: false, recentComplete: true, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "最新内容" }] }] };
    const { socket, requests } = await openFixture(recent);
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    const turns = [recent.turns[0], ...[1, 2, 3, 4].map(index => ({ turnId: `older-${index}`, status: "completed" as const, items: [{ itemId: `older-item-${index}`, role: "assistant" as const, text: `更早内容 ${index}` }] }))];
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[0].requestId, outcome: "accepted", data: { turns, nextCursor: "more" } }));
    expect(screen.getByRole("alert").textContent).toContain("历史页返回数量超过请求范围");
    expect(screen.queryByText("更早内容 1")).toBeNull();
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

  it("restores downloaded older history after switching away and back within the same agent epoch", async () => {
    const first: Thread = { ...fixtureThread, title: "First", historyComplete: false, recentComplete: true, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "第一会话最新" }] }] };
    const second: Thread = { ...fixtureThread, threadId: "second", title: "Second", runtime: "inProgress", turns: [{ turnId: "second-active", status: "inProgress", items: [{ itemId: "second-item", role: "assistant", text: "第二会话运行中" }] }] };
    const { socket, requests } = await openFixture(first, false, false, [first, second]);
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[0].requestId, outcome: "accepted", data: { turns: [first.turns[0], { turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "已经下载的旧历史" }] }], nextCursor: "" } }));
    expect(await screen.findByText("已经下载的旧历史")).toBeTruthy();

    fireEvent.click(screen.getByText("Second").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-second" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "second", subscriptionId: "sub-second", streamId: "stream-second", seq: 1, thread: second }));
    expect(await screen.findByText("第二会话运行中")).toBeTruthy();

    fireEvent.click(screen.getByText("First").closest("button")!);
    expect(requests("thread.subscribe")).toHaveLength(2);
    expect(screen.getByText("第一会话最新")).toBeTruthy();
    expect(screen.getByText("已经下载的旧历史")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "正在同步会话…" })).toBeNull();
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
    expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).disabled).toBe(false);

    expect(await screen.findByText("已经下载的旧历史")).toBeTruthy();
    expect(screen.getByText("第一会话最新")).toBeTruthy();
    expect(screen.getByText("待命")).toBeTruthy();
    expect(requests("thread.history")).toHaveLength(1);
  });

  it("keeps a healthy socket and selected conversation stable after returning from background", async () => {
    const thread: Thread = { ...fixtureThread, turns: [{ turnId: "visible-turn", status: "completed", items: [{ itemId: "visible-item", role: "assistant", text: "不要重新同步的正文" }] }] };
    const { requests } = await openFixture(thread);
    const composer = screen.getByLabelText("发送消息") as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "后台切换期间保留的草稿" } });
    const now = vi.spyOn(Date, "now");
    now.mockReturnValueOnce(1_000);
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    now.mockReturnValue(7_000);
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    act(() => document.dispatchEvent(new Event("visibilitychange")));

    expect(BrowserSocket.sockets).toHaveLength(1);
    expect(requests("thread.subscribe")).toHaveLength(1);
    expect(screen.getByText("不要重新同步的正文")).toBeTruthy();
    expect(composer.value).toBe("后台切换期间保留的草稿");
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
  });

  it("keeps confirmed content and draft visible across a real socket reconnect", async () => {
    const thread: Thread = { ...fixtureThread, turns: [{ turnId: "visible-turn", status: "completed", items: [{ itemId: "visible-item", role: "assistant", text: "断线期间继续显示" }] }] };
    const { socket } = await openFixture(thread);
    const composer = screen.getByLabelText("发送消息") as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "未发送草稿" } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false);
    expect(document.querySelector(".send-loading-spinner")).toBeNull();
    vi.spyOn(Math, "random").mockReturnValue(0);

    act(() => socket.close(1006));
    expect(screen.getByText("断线期间继续显示")).toBeTruthy();
    expect(composer.value).toBe("未发送草稿");
    expect(screen.queryByRole("heading", { name: "正在同步会话…" })).toBeNull();
    const reconnectingSend = screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement;
    expect(reconnectingSend.disabled).toBe(true);
    expect(reconnectingSend.querySelector(".send-loading-spinner")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "发送" })).toBeNull();
    const sendsBeforeBlockedActions = socket.sent.filter(value => JSON.parse(value).method === "thread.send").length;
    fireEvent.click(reconnectingSend);
    fireEvent.keyDown(composer, { key: "Enter", shiftKey: false });
    expect(socket.sent.filter(value => JSON.parse(value).method === "thread.send")).toHaveLength(sendsBeforeBlockedActions);

    await act(async () => { await new Promise(resolve => window.setTimeout(resolve, 900)); });
    const reconnected = BrowserSocket.sockets[1];
    reconnected.onopen?.(new Event("open"));
    act(() => reconnected.message({ type: "hello.ok", v: 1, connectionId: "c2", relayEpoch: "e", sessionToken: relaySession }));
    const requests = (method: string) => reconnected.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    act(() => reconnected.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } },
    ] } }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    expect(await screen.findByText("断线期间继续显示")).toBeTruthy();
    expect(composer.value).toBe("未发送草稿");
    expect((screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => reconnected.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub-reconnected" } }));
    act(() => reconnected.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub-reconnected", streamId: "stream-reconnected", seq: 1, thread }));
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false);
    expect(document.querySelector(".send-loading-spinner")).toBeNull();
  });

  it("restores a locally persisted conversation before the fresh owner snapshot after reload", async () => {
    const backend = new MemoryDurableHistoryBackend();
    const durable = new DurableHistoryCache({ backend, now: () => 1_000 });
    const identity = { deviceId: "mac", agentEpoch: "agent-epoch", threadId: "cached" };
    const cached: Thread = { ...fixtureThread, threadId: "cached", title: "本机恢复会话", turns: [{ turnId: "cached-turn", status: "completed", items: [{ itemId: "cached-item", role: "assistant", text: "reload 后立即显示" }] }] };
    await durable.set(identity, { history: emptyHistoryState(), itemOverrides: {}, thread: cached });
    await durable.rememberSelection(identity);
    sessionStorage.setItem("ariel.web-session.v1", relaySession);

    render(<App durableHistoryCache={durable} />);
    const socket = BrowserSocket.sockets[0];
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } },
    ] } }));

    expect(await screen.findByText("reload 后立即显示")).toBeTruthy();
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    expect(requests("thread.subscribe")[0]).toMatchObject({ deviceId: "mac", params: { threadId: "cached" } });
    expect(screen.queryByRole("heading", { name: "正在同步会话…" })).toBeNull();
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
    const restoringSend = screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement;
    expect(restoringSend.disabled).toBe(true);
    expect(restoringSend.querySelector(".send-loading-spinner")).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub-cached" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "cached", subscriptionId: "sub-cached", streamId: "stream-cached", seq: 1, thread: cached }));
    expect(screen.getByRole("button", { name: "发送" })).toBeTruthy();
    expect(document.querySelector(".send-loading-spinner")).toBeNull();
  });

  it("multiplexes the current and previous conversation so A to B to A needs no resubscribe", async () => {
    const alpha: Thread = { ...fixtureThread, threadId: "alpha", title: "Alpha", turns: [{ turnId: "alpha-turn", status: "completed", items: [{ itemId: "alpha-item", role: "assistant", text: "Alpha 已确认内容" }] }] };
    const beta: Thread = { ...fixtureThread, threadId: "beta", title: "Beta", turns: [{ turnId: "beta-turn", status: "completed", items: [{ itemId: "beta-item", role: "assistant", text: "Beta 已确认内容" }] }] };
    const { socket, requests } = await openFixture(alpha, false, false, [alpha, beta]);

    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
    fireEvent.click(screen.getByText("Beta").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-beta" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "beta", subscriptionId: "sub-beta", streamId: "stream-beta", seq: 1, thread: beta }));
    expect(await screen.findByText("Beta 已确认内容")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
    fireEvent.click(screen.getByText("Alpha").closest("button")!);

    await waitFor(() => expect(screen.getByText("Alpha 已确认内容")).toBeTruthy());
    expect(requests("thread.subscribe")).toHaveLength(2);
    expect(requests("thread.unsubscribe")).toHaveLength(0);
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
  });

  it("merges a background delta into the warm conversation without disturbing the current DOM", async () => {
    const alpha: Thread = { ...fixtureThread, threadId: "alpha", title: "Alpha", turns: [{ turnId: "alpha-turn", status: "inProgress", items: [{ itemId: "alpha-item", role: "assistant", text: "Alpha 起点" }] }] };
    const beta: Thread = { ...fixtureThread, threadId: "beta", title: "Beta", turns: [{ turnId: "beta-turn", status: "completed", items: [{ itemId: "beta-item", role: "assistant", text: "Beta 保持不动" }] }] };
    const { socket, requests } = await openFixture(alpha, false, false, [alpha, beta]);
    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
    fireEvent.click(screen.getByText("Beta").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-beta" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "beta", subscriptionId: "sub-beta", streamId: "stream-beta", seq: 1, thread: beta }));
    const betaNode = await screen.findByText("Beta 保持不动");

    const alphaUpdated: Thread = { ...alpha, turns: [{ ...alpha.turns[0], items: [{ ...alpha.turns[0].items[0], text: "Alpha 后台增量" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "alpha", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: alphaUpdated }));
    expect(screen.getByText("Beta 保持不动")).toBe(betaNode);
    expect(screen.queryByText("Alpha 后台增量")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
    fireEvent.click(screen.getByText("Alpha").closest("button")!);
    expect(await screen.findByText("Alpha 后台增量")).toBeTruthy();
    expect(requests("thread.subscribe")).toHaveLength(2);
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
  });

  it("evicts the oldest warm subscription when selecting a third conversation", async () => {
    const alpha: Thread = { ...fixtureThread, threadId: "alpha", title: "Alpha" };
    const beta: Thread = { ...fixtureThread, threadId: "beta", title: "Beta" };
    const gamma: Thread = { ...fixtureThread, threadId: "gamma", title: "Gamma" };
    const { socket, requests } = await openFixture(alpha, false, false, [alpha, beta, gamma]);
    const open = async (thread: Thread, subscriptionId: string, streamId: string, expected: number) => {
      fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
      fireEvent.click(screen.getByText(thread.title).closest("button")!);
      await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(expected));
      await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[expected - 1].requestId, outcome: "accepted", data: { subscriptionId } }));
      act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId, streamId, seq: 1, thread }));
    };
    await open(beta, "sub-beta", "stream-beta", 2);
    await open(gamma, "sub-gamma", "stream-gamma", 3);
    expect(requests("thread.unsubscribe")).toContainEqual(expect.objectContaining({ params: { subscriptionId: "sub" } }));

    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
    fireEvent.click(screen.getByText("Alpha").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(4));
    expect(screen.getByText(/正在同步最新状态/)).toBeTruthy();
    expect(requests("thread.unsubscribe")).toContainEqual(expect.objectContaining({ params: { subscriptionId: "sub-beta" } }));
  });

  it("drops a restored cursor once and rebuilds pagination when the Agent rejects it", async () => {
    const first: Thread = { ...fixtureThread, title: "First", historyComplete: false, recentComplete: true, turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "第一会话最新" }] }] };
    const second: Thread = { ...fixtureThread, threadId: "second", title: "Second" };
    const { socket, requests } = await openFixture(first, false, false, [first, second]);
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[0].requestId, outcome: "accepted", data: { turns: [
      first.turns[0],
      { turnId: "older-1", status: "completed", items: [{ itemId: "older-item-1", role: "assistant", text: "缓存旧历史" }] },
      { turnId: "older-2", status: "completed", items: [{ itemId: "older-item-2", role: "assistant", text: "缓存旧历史二" }] },
      { turnId: "older-3", status: "completed", items: [{ itemId: "older-item-3", role: "assistant", text: "缓存旧历史三" }] },
    ], nextCursor: "cached-cursor" } }));

    fireEvent.click(screen.getByText("Second").closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-second" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "second", subscriptionId: "sub-second", streamId: "stream-second", seq: 1, thread: second }));
    fireEvent.click(screen.getByText("First").closest("button")!);
    expect(requests("thread.subscribe")).toHaveLength(2);
    expect(await screen.findByText("缓存旧历史")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(2));
    expect(requests("thread.history")[1].params.cursor).toBe("cached-cursor");
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[1].requestId, outcome: "rejected", error: { code: "INVALID_ARGUMENT", message: "stale cursor" } }));
    await waitFor(() => expect(requests("thread.history")).toHaveLength(3));
    expect(requests("thread.history")[2].params.cursor).toBeUndefined();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.history")[2].requestId, outcome: "accepted", data: { turns: [first.turns[0], { turnId: "rebuilt", status: "completed", items: [{ itemId: "rebuilt-item", role: "assistant", text: "重新确认的旧历史" }] }], nextCursor: "" } }));
    expect(await screen.findByText("重新确认的旧历史")).toBeTruthy();
    expect(screen.queryByText("缓存旧历史")).toBeNull();
  });

  it("keeps the reading position on live updates until the reader chooses to return to latest", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "partial" }] }] };
    const { socket } = await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    const conversationHead = document.querySelector(".conversation-head") as HTMLElement;
    const composerWrap = document.querySelector(".composer-wrap") as HTMLElement;
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);
    expect(conversationHead.dataset.historyCollapsed).toBe("true");
    expect(composerWrap.classList.contains("history-collapsed")).toBe(true);
    expect(composerWrap.dataset.historyCollapsed).toBe("true");
    const earlierScrollCalls = vi.mocked(Element.prototype.scrollIntoView).mock.calls.length;
    const updated: Thread = { ...recent, turns: [{ ...recent.turns[0], items: [{ itemId: "answer", role: "assistant", text: "partial and more" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: updated }));
    expect(transcript.scrollTop).toBe(300);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);
    expect(vi.mocked(Element.prototype.scrollIntoView).mock.calls.length).toBe(earlierScrollCalls);
    const returnToLatest = screen.getByRole("button", { name: "回到最新" });
    expect(returnToLatest.textContent).toBe("");
    expect(returnToLatest.querySelector('svg[aria-hidden="true"]')).toBeTruthy();
    fireEvent.click(returnToLatest);
    expect(transcript.scrollTop).toBe(800);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
    expect(conversationHead.dataset.historyCollapsed).toBe("false");
    expect(composerWrap.classList.contains("history-collapsed")).toBe(false);
    expect(composerWrap.dataset.historyCollapsed).toBe("false");
    expect(screen.queryByRole("button", { name: "回到最新" })).toBeNull();
  });

  it("uses hysteresis before expanding the collapsed history chrome near the latest messages", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "history" }] }] };
    const { socket } = await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    const conversationHead = document.querySelector(".conversation-head") as HTMLElement;
    const composerWrap = document.querySelector(".composer-wrap") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });

    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);
    expect(composerWrap.classList.contains("history-collapsed")).toBe(true);

    transcript.scrollTop = 730;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);
    const updated: Thread = { ...recent, turns: [{ ...recent.turns[0], items: [{ itemId: "answer", role: "assistant", text: "history with streamed update" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: updated }));
    expect(transcript.scrollTop).toBe(730);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);

    transcript.scrollTop = 780;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
    expect(composerWrap.classList.contains("history-collapsed")).toBe(false);
  });

  it("keeps the toward-latest intent when the final scroll step is only one pixel", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "history" }] }] };
    await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    const conversationHead = document.querySelector(".conversation-head") as HTMLElement;
    const composerWrap = document.querySelector(".composer-wrap") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });

    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);

    transcript.scrollTop = 767;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);

    transcript.scrollTop = 768;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
    expect(composerWrap.classList.contains("history-collapsed")).toBe(false);
  });

  it("restores collapsed chrome from an explicit toward-latest touch at the bottom", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "history" }] }] };
    await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    const conversationHead = document.querySelector(".conversation-head") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });

    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);

    transcript.scrollTop = 800;
    fireEvent.touchStart(transcript, { touches: [{ clientY: 250 }] });
    fireEvent.touchMove(transcript, { touches: [{ clientY: 300 }] });
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);
    fireEvent.touchEnd(transcript);
    fireEvent.touchStart(transcript, { touches: [{ clientY: 300 }] });
    fireEvent.touchMove(transcript, { touches: [{ clientY: 250 }] });
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
  });

  it("keeps the latest message anchored while the restored chrome shrinks the transcript", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "history" }] }] };
    await openFixture(recent);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    const conversationHead = document.querySelector(".conversation-head") as HTMLElement;
    const composerWrap = document.querySelector(".composer-wrap") as HTMLElement;
    let clientHeight = 200;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, get: () => clientHeight });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });

    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);

    transcript.scrollTop = 800;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
    expect(composerWrap.classList.contains("history-collapsed")).toBe(false);

    clientHeight = 100;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
    expect(composerWrap.classList.contains("history-collapsed")).toBe(false);
    expect(transcript.scrollTop).toBe(900);

    fireEvent.wheel(transcript, { deltaY: -100 });
    transcript.scrollTop = 700;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);
    expect(composerWrap.classList.contains("history-collapsed")).toBe(true);
  });

  it("expands the history header immediately when selecting another thread", async () => {
    const recent: Thread = { ...fixtureThread, turns: [{ turnId: "current", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "history" }] }] };
    const another: Thread = { ...fixtureThread, threadId: "another", title: "Another", cwd: "/tmp/another" };
    await openFixture(recent, false, false, [recent, another]);
    const transcript = document.querySelector(".transcript") as HTMLElement;
    const conversationHead = document.querySelector(".conversation-head") as HTMLElement;
    Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
    transcript.scrollTop = 300;
    fireEvent.scroll(transcript);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: /打开会话列表/ }));
    await expandProject(another.cwd);
    fireEvent.click(screen.getByText("Another").closest("button")!);
    expect(conversationHead.classList.contains("history-collapsed")).toBe(false);
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

  it("keeps formal monochrome product marks without resubscribing the selected conversation", async () => {
    const { requests } = await openFixture();
    fireEvent.click(document.querySelector(`.thread-row[data-thread-id="${fixtureThread.threadId}"]`)!);
    expect(requests("thread.subscribe")).toHaveLength(1);
    expect(screen.queryByText(/正在同步最新状态/)).toBeNull();
    const marks = [...document.querySelectorAll(".ariel-logo--micro")];
    expect(marks).toHaveLength(2);
    for (const mark of marks) {
      expect(mark.getAttribute("data-variant")).toBe("micro");
      expect(mark.getAttribute("aria-hidden")).toBe("true");
      expect(mark.textContent).toBe("");
    }
    const loading = document.querySelector(".empty-symbol");
    expect(loading).toBeNull();
    expect(document.querySelector("svg.brand-mark")).toBeNull();
  });

  it("keeps connected brand, status and disconnect in the drawer while the compact menu exposes status", async () => {
    await openFixture();
    const shell = document.querySelector(".app-shell")!;
    expect(shell.classList.contains("connected")).toBe(true);
    const sidebar = screen.getByLabelText("会话列表");
    expect(sidebar.querySelector(".brand")?.textContent).toContain("Ariel");
    expect(sidebar.querySelector(".connection")?.textContent).toContain("Relay 已连接");
    const disconnect = sidebar.querySelector("button[aria-label='断开']")!;
    expect(disconnect.textContent).toBe("");
    expect(disconnect.querySelector("svg[aria-hidden='true']")).toBeTruthy();
    expect(disconnect.getAttribute("data-tooltip")).toBe("断开");
    const menu = screen.getByRole("button", { name: /打开会话列表.*Relay 已连接/ });
    expect(menu.textContent).toBe("");
    expect(menu.querySelector("svg[aria-hidden='true']")).toBeTruthy();
    expect(menu.getAttribute("data-tooltip")).toBe("会话列表");
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
      { itemId: "tool", role: "system", text: "tool", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "completed", detailState: "available", detailRevision: "a".repeat(64), truncated: false } },
    ] }], pendingInteractions: [{ interactionId: "approval", kind: "command_approval", prompt: "**approval literal**", availableDecisions: ["deny"] }] } as unknown as Thread;
    const { socket, requests } = await openFixture(thread);
    expect(document.querySelector(".message.user strong")?.textContent).toBe("your emphasis");
    expect(document.querySelector(".message.assistant a")?.getAttribute("href")).toBe("https://example.test/");
    expect(document.querySelector(".message.system strong")).toBeNull();
    expect(screen.getByText("**system literal**")).toBeTruthy();
    expect(screen.getByText("**approval literal**")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /打开工具调用.*1 项/ }));
    fireEvent.click(screen.getByRole("button", { name: "查看详情" }));
    await waitFor(() => expect(requests("thread.activity.read")).toHaveLength(1));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.activity.read")[0].requestId, outcome: "accepted", data: { text: "**tool literal**", nextCursor: "", revision: "a".repeat(64), complete: true } }));
    expect(document.querySelector(".activity-details strong")).toBeNull();
    expect(screen.getByText("**tool literal**")).toBeTruthy();
  });

  it("discards a late lazy tool response after switching conversations", async () => {
    const thread = { ...fixtureThread, turns: [{ turnId: "turn-tool", status: "completed", items: [
      { itemId: "tool", role: "system", text: "tool", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "completed", detailState: "available", detailRevision: "a".repeat(64), truncated: false } },
    ] }] } as unknown as Thread;
    const background: Thread = { ...fixtureThread, threadId: "background", title: "Background" };
    const { socket, requests } = await openFixture(thread, false, false, [thread, background]);
    fireEvent.click(screen.getByRole("button", { name: /打开工具调用.*1 项/ }));
    fireEvent.click(screen.getByRole("button", { name: "查看详情" }));
    await waitFor(() => expect(requests("thread.activity.read")).toHaveLength(1));
    fireEvent.click(document.querySelector<HTMLElement>('.thread-row[data-thread-id="background"]')!);
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.activity.read")[0].requestId, outcome: "accepted", data: { text: "不应出现的迟到详情", nextCursor: "", revision: "a".repeat(64), complete: true } }));
    expect(screen.queryByText("不应出现的迟到详情")).toBeNull();
  });

  it("hides all verified calls behind one quiet text control per turn and restores their interleaved order", async () => {
    const thread = { ...fixtureThread, turns: [{ turnId: "tools", status: "completed", items: [
      { itemId: "u", role: "user", text: "question" },
      { itemId: "m", role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "completed", detailState: "available", detailRevision: "a".repeat(64), truncated: false } },
      { itemId: "c", role: "system", text: "命令执行", activity: { kind: "commandExecution", label: "/usr/bin/true", status: "completed", detailState: "available", detailRevision: "b".repeat(64), truncated: false } },
      { itemId: "a", role: "assistant", text: "answer" },
      { itemId: "f", role: "system", text: "文件变更", activity: { kind: "fileChange", label: "文件变更", status: "completed", detailState: "available", detailRevision: "c".repeat(64), truncated: false } },
      { itemId: "unknown", role: "system", text: "[futureTool 项目]" },
    ] }], pendingInteractions: [{ interactionId: "approval", kind: "command_approval", prompt: "Approve fixture only", availableDecisions: ["deny"] }] } as unknown as Thread;
    await openFixture(thread);
    const opener = screen.getByRole("button", { name: /打开工具调用.*3 项/ });
    expect(screen.getAllByRole("button", { name: /打开工具调用/ })).toHaveLength(1);
    expect(opener.getAttribute("aria-expanded")).toBe("false");
    expect(opener.textContent?.trim()).toBe("使用了 3 个工具");
    expect(opener.querySelector("svg")).toBeNull();
    expect(screen.getByLabelText("最近工具摘要").textContent).toBe("fixture/lookup（已完成） · /usr/bin/true（已完成） · 文件变更（已完成）");
    expect(document.querySelectorAll(".activity-item")).toHaveLength(0);
    expect(screen.getByText("answer")).toBeTruthy();
    expect(screen.getByText("[futureTool 项目]")).toBeTruthy();
    expect(screen.getByText("Approve fixture only")).toBeTruthy();
    fireEvent.click(opener);
    const closer = screen.getByRole("button", { name: /收起工具调用.*3 项/ });
    expect(closer.getAttribute("aria-expanded")).toBe("true");
    expect(closer.textContent?.trim()).toBe("收起工具调用（3 项）");
    expect(screen.getByText("fixture/lookup")).toBeTruthy();
    expect(screen.getByText("/usr/bin/true")).toBeTruthy();
    expect(document.querySelector(".activity-details img")).toBeNull();
    expect(document.querySelectorAll(".activity-item")).toHaveLength(3);
    const transcriptOrder = [...document.querySelectorAll(".turn .message, .turn .activity-item")].map(node => node.getAttribute("data-item-id"));
    expect(transcriptOrder).toEqual(["u", "m", "c", "a", "f", "unknown"]);
    fireEvent.click(screen.getByRole("button", { name: /收起工具调用/ }));
    expect(document.querySelectorAll(".activity-item")).toHaveLength(0);
    expect(screen.getByRole("button", { name: /打开工具调用/ }).textContent?.trim()).toBe("使用了 3 个工具");
  });

  it("keeps only the latest three tool summaries in collapsed mode without reading details", async () => {
    const activity = (itemId: string, status: "completed" | "inProgress" = "completed") => ({ itemId, role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: itemId, status, detailState: "available", detailRevision: itemId.repeat(64).slice(0, 64), truncated: false } });
    const thread = { ...fixtureThread, turns: [{ turnId: "many-tools", status: "inProgress", items: [activity("a"), activity("b"), activity("c"), activity("d"), activity("e", "inProgress")] }] } as unknown as Thread;
    await openFixture(thread);
    expect(screen.getByLabelText("最近工具摘要").textContent).toBe("c（已完成） · d（已完成） · e（运行中） · 另有 2 项");
    expect(screen.queryByText("a（已完成）")).toBeNull();
    expect(document.querySelectorAll(".activity-item")).toHaveLength(0);
  });

  it("keeps one tool opener expanded as live calls append to the same turn", async () => {
    const activity = (itemId: string) => ({ itemId, role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: itemId, status: "completed", detailState: "available", detailRevision: "a".repeat(64), truncated: false } });
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
    const activity = { itemId: "m", role: "system", text: "工具调用", activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "inProgress", detailState: "loading", truncated: false } };
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
      expect(input.style.height).toBe("44px");
    } finally {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: originalWidth });
      if (originalScrollHeight) Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", originalScrollHeight);
      else delete (HTMLTextAreaElement.prototype as unknown as Record<string, unknown>).scrollHeight;
    }
  });

  it("keeps an empty mobile composer at one line when the running placeholder wraps", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "scrollHeight");
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", { configurable: true, get: () => 68 });
      const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "user", role: "user", text: "work" }] }] };
      await openFixture(running);
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      expect(input.value).toBe("");
      expect(input.placeholder).toContain("Codex 正在运行");
      expect(input.style.height).toBe("44px");
      expect(input.scrollTop).toBe(0);
    } finally {
      if (originalWidth) Object.defineProperty(window, "innerWidth", originalWidth);
      if (originalScrollHeight) Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", originalScrollHeight);
      else delete (HTMLTextAreaElement.prototype as unknown as Record<string, unknown>).scrollHeight;
    }
  });

  it("does not remeasure an unchanged mobile draft for every streaming update", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "scrollHeight");
    let measurementReads = 0;
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", { configurable: true, get: () => { measurementReads++; return 68; } });
      const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "partial" }] }] };
      const { socket } = await openFixture(running);
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      fireEvent.change(input, { target: { value: "draft" } });
      expect(input.style.height).toBe("68px");
      const readsBeforeStreamChunk = measurementReads;
      const updated: Thread = { ...running, turns: [{ ...running.turns[0], items: [{ itemId: "answer", role: "assistant", text: "partial and more" }] }] };
      act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: updated }));
      expect(measurementReads).toBe(readsBeforeStreamChunk);
      expect(input.style.height).toBe("68px");
    } finally {
      if (originalWidth) Object.defineProperty(window, "innerWidth", originalWidth);
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

  it("keeps a focused running composer visible when the mobile keyboard resizes the transcript", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    const originalHeight = Object.getOwnPropertyDescriptor(window, "innerHeight");
    const originalViewport = Object.getOwnPropertyDescriptor(window, "visualViewport");
    const viewport = Object.assign(new EventTarget(), { height: 844, offsetTop: 0 });
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(window, "innerHeight", { configurable: true, value: 844 });
      Object.defineProperty(window, "visualViewport", { configurable: true, value: viewport });
      const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "partial" }] }] };
      await openFixture(running);
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      const transcript = document.querySelector(".transcript") as HTMLElement;
      const composerWrap = document.querySelector(".composer-wrap") as HTMLElement;
      let transcriptHeight = 600;
      Object.defineProperty(transcript, "clientHeight", { configurable: true, get: () => transcriptHeight });
      Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
      transcript.scrollTop = 400;

      input.focus();
      fireEvent.change(input, { target: { value: "处理中也要保留的草稿" } });
      viewport.height = 500;
      transcriptHeight = 500;
      act(() => viewport.dispatchEvent(new Event("resize")));
      fireEvent.scroll(transcript);

      expect(input.value).toBe("处理中也要保留的草稿");
      expect(document.activeElement).toBe(input);
      expect(composerWrap.dataset.historyCollapsed).toBe("false");
      expect(composerWrap.classList.contains("history-collapsed")).toBe(false);
    } finally {
      if (originalWidth) Object.defineProperty(window, "innerWidth", originalWidth);
      if (originalHeight) Object.defineProperty(window, "innerHeight", originalHeight);
      if (originalViewport) Object.defineProperty(window, "visualViewport", originalViewport);
      else delete (window as unknown as Record<string, unknown>).visualViewport;
    }
  });

  it("keeps focused draft input stable when Safari shrinks both viewport heights during streaming", async () => {
    const originalWidth = Object.getOwnPropertyDescriptor(window, "innerWidth");
    const originalHeight = Object.getOwnPropertyDescriptor(window, "innerHeight");
    const originalViewport = Object.getOwnPropertyDescriptor(window, "visualViewport");
    let innerHeight = 844;
    const viewport = Object.assign(new EventTarget(), { height: 844, offsetTop: 0 });
    try {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      Object.defineProperty(window, "innerHeight", { configurable: true, get: () => innerHeight });
      Object.defineProperty(window, "visualViewport", { configurable: true, value: viewport });
      const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "answer", role: "assistant", text: "partial" }] }] };
      const { socket } = await openFixture(running);
      const input = screen.getByRole("textbox", { name: "发送消息" }) as HTMLTextAreaElement;
      const transcript = document.querySelector(".transcript") as HTMLElement;
      const composerWrap = document.querySelector(".composer-wrap") as HTMLElement;
      let transcriptHeight = 600;
      Object.defineProperty(transcript, "clientHeight", { configurable: true, get: () => transcriptHeight });
      Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
      transcript.scrollTop = 400;

      input.focus();
      innerHeight = 500;
      viewport.height = 500;
      transcriptHeight = 500;
      act(() => viewport.dispatchEvent(new Event("resize")));

      for (const [index, value] of ["处", "处理", "处理中"].entries()) {
        fireEvent.change(input, { target: { value } });
        input.setSelectionRange(value.length, value.length);
        fireEvent.scroll(transcript);
        const updated: Thread = { ...running, turns: [{ ...running.turns[0], items: [{ ...running.turns[0].items[0], text: `partial-${index}` }] }] };
        act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: index + 1, seq: index + 2, thread: updated }));
        expect(composerWrap.dataset.historyCollapsed).toBe("false");
        expect(document.activeElement).toBe(input);
        expect(input.value).toBe(value);
        expect(input.selectionStart).toBe(value.length);
      }
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

  it("requires explicit mobile confirmation before exchanging a scrubbed pairing fragment", async () => {
    const pairing = `p_${"b".repeat(64)}`;
    window.history.replaceState({}, "", `/#pair=${pairing}`);
    render(<App />);
    expect(screen.getByRole("heading", { name: "确认登录 Ariel" })).toBeTruthy();
    expect(window.location.hash).toBe("");
    expect([...Array(sessionStorage.length)].map((_, index) => sessionStorage.getItem(sessionStorage.key(index)!))).not.toContain(pairing);
    expect(BrowserSocket.sockets).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "确认在此手机登录" }));
    expect(BrowserSocket.sockets).toHaveLength(1);
    BrowserSocket.sockets[0].onopen?.(new Event("open"));
    expect(JSON.parse(BrowserSocket.sockets[0].sent[0]).token).toBe(pairing);
    BrowserSocket.sockets[0].message({ type: "hello.ok", v: 1, connectionId: "paired", relayEpoch: "e", sessionToken: relaySession });
    await waitFor(() => expect(sessionStorage.getItem("ariel.web-session.v1")).toBe(relaySession));
    expect(document.body.textContent).not.toContain(pairing);
    expect(screen.queryByRole("heading", { name: "确认登录 Ariel" })).toBeNull();
  });

  it("lets a scanned phone cancel without making a connection", () => {
    const pairing = `p_${"c".repeat(64)}`;
    window.history.replaceState({}, "", `/#pair=${pairing}`);
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(BrowserSocket.sockets).toHaveLength(0);
    expect(screen.getByLabelText("6 位连接码")).toBeTruthy();
  });

  it("creates a local QR invitation and reports one-time consumption", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    fireEvent.click(await screen.findByRole("button", { name: "手机扫码登录" }));
    const pairRequests = () => socket.sent.map(value => JSON.parse(value)).filter(value => value.method?.startsWith("auth.pair."));
    await waitFor(() => expect(pairRequests()).toHaveLength(1));
    expect(pairRequests()[0].method).toBe("auth.pair.create");
    const pairing = `p_${"d".repeat(64)}`;
    act(() => socket.message({ type: "response", v: 1, requestId: pairRequests()[0].requestId, outcome: "accepted", data: { credential: pairing, expiresAt: new Date(Date.now() + 120_000).toISOString() } }));
    const image = await screen.findByRole("img", { name: "手机扫码登录二维码" });
    expect(image.getAttribute("src")).toMatch(/^data:image\/svg\+xml/);
    expect(image.getAttribute("src")).not.toContain(pairing);
    expect(document.body.textContent).not.toContain(pairing);
    act(() => socket.message({ type: "event", v: 1, event: "auth.pair.consumed" }));
    expect(await screen.findByText("手机已登录")).toBeTruthy();
  });

  it("clears a rejected saved session and explicit disconnect", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    expect(BrowserSocket.sockets).toHaveLength(1);
    BrowserSocket.sockets[0].close(1008, "unauthorized");
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
      act(() => second.close(1008, "unauthorized"));

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
    await expandProject(thread.cwd);
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
      devices: [{ deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch", capabilities: { autoLoad: true } }],
    } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    const thread = { threadId: "fixture", title: "Fixture", cwd: "/tmp/fixture", updatedAt: "2026-10-04T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] };
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [thread] } }));
    await expandProject(thread.cwd);
    fireEvent.click((await screen.findByText("Fixture")).closest("button")!);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(1));
    expect(requests("thread.subscribe")[0].deviceId).toBe("mac");
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[0].requestId, outcome: "accepted", data: { subscriptionId: "sub-1" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-1", streamId: "stream-1", seq: 1, thread }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Fixture" })).toBeTruthy());

    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-1", streamId: "stream-1", baseSeq: 1, seq: 3, thread }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    expect(screen.getByRole("heading", { name: "Fixture" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "正在同步会话…" })).toBeNull();
    expect(screen.getByText(/正在同步最新状态/)).toBeTruthy();
    expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "正在同步会话，暂不可发送" }) as HTMLButtonElement).disabled).toBe(true);
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

  it("fences the old subscription immediately when the current Agent goes offline", async () => {
    const { socket, requests } = await openFixture();
    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "agent-epoch", agentOnline: false, codexReady: false }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(2));

    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: { ...fixtureThread, runtime: "inProgress" } }));
    await act(async () => Promise.resolve());
    expect(requests("thread.subscribe")).toHaveLength(1);
    expect(screen.queryByText("事件顺序中断，正在重新同步会话…")).toBeNull();

    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[1].requestId, outcome: "accepted", data: { devices: [] } }));
    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "agent-next", agentOnline: true, codexReady: true }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(3));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[2].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-next", capabilities: { autoLoad: true } },
    ] } }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-next" } }));
    const recovered = { ...fixtureThread, title: "Recovered after reconnect" };
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-next", streamId: "stream-next", seq: 1, thread: recovered }));
    expect(await screen.findByRole("heading", { name: "Recovered after reconnect" })).toBeTruthy();
  });

  it("runs one follow-up recovery when a newer online epoch arrives during subscribe", async () => {
    const { socket, requests } = await openFixture();
    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "agent-epoch", agentOnline: false, codexReady: false }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[1].requestId, outcome: "accepted", data: { devices: [] } }));

    const comeOnline = async (epoch: string, expectedLists: number) => {
      act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: epoch, agentOnline: true, codexReady: true }));
      await waitFor(() => expect(requests("device.list")).toHaveLength(expectedLists));
      await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[expectedLists - 1].requestId, outcome: "accepted", data: { devices: [
        { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: epoch, capabilities: { autoLoad: true } },
      ] } }));
    };

    await comeOnline("agent-next", 3);
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await comeOnline("agent-newest", 4);
    expect(requests("thread.subscribe")).toHaveLength(2);

    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "rejected", error: { code: "DEVICE_OFFLINE", message: "replaced" } }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(3));
    expect(requests("thread.subscribe")[2]).toMatchObject({ deviceId: "mac", params: { threadId: "fixture" } });
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
    await expandProject("/tmp/b");
    expect(await screen.findByText("Thread B")).toBeTruthy();
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [makeThread("a", "Thread A")] } }));
    expect(screen.queryByText("Thread A")).toBeNull();
    expect(screen.getByText("Thread B")).toBeTruthy();
  });

  it("restarts a pending thread list read when the selected Agent epoch changes", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch-old", capabilities: { autoLoad: true } },
    ] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));

    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "epoch-new", agentOnline: true, codexReady: true }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(2));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[1].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch-new", capabilities: { autoLoad: true } },
    ] } }));

    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    expect(requests("thread.list")[1].deviceId).toBe("mac");
  });

  it("restores a fresh same-epoch thread list without another request when switching devices", async () => {
    sessionStorage.setItem("ariel.web-session.v1", relaySession);
    render(<App />);
    const socket = BrowserSocket.sockets[0];
    socket.onopen?.(new Event("open"));
    act(() => socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken: relaySession }));
    const requests = (method: string) => socket.sent.map(value => JSON.parse(value)).filter(value => value.method === method);
    await waitFor(() => expect(requests("device.list")).toHaveLength(1));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("device.list")[0].requestId, outcome: "accepted", data: { devices: [
      { deviceId: "mac-a", deviceName: "Mac A", agentOnline: true, codexReady: true, agentEpoch: "epoch-a", capabilities: { autoLoad: true } },
      { deviceId: "mac-b", deviceName: "Mac B", agentOnline: true, codexReady: true, agentEpoch: "epoch-b", capabilities: { autoLoad: true } },
    ] } }));
    await waitFor(() => expect(requests("thread.list")).toHaveLength(1));
    const makeThread = (threadId: string, title: string) => ({ threadId, title, cwd: `/tmp/${threadId}`, updatedAt: "2026-10-11T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] });
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "accepted", data: { threads: [makeThread("a", "Cached A")], nextCursor: "" } }));
    await expandProject("/tmp/a");
    expect(await screen.findByText("Cached A")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("设备"), { target: { value: "mac-b" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "accepted", data: { threads: [makeThread("b", "Thread B")], nextCursor: "" } }));
    await expandProject("/tmp/b");
    expect(await screen.findByText("Thread B")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("设备"), { target: { value: "mac-a" } });
    await expandProject("/tmp/a");
    expect(await screen.findByText("Cached A")).toBeTruthy();
    expect(requests("thread.list")).toHaveLength(2);
  });

  it("keeps device-switch read failures scoped and describes them as retryable loads", async () => {
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
    fireEvent.change(screen.getByLabelText("设备"), { target: { value: "mac-b" } });
    await waitFor(() => expect(requests("thread.list")).toHaveLength(2));
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[0].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "late A" } }));
    expect(screen.queryByRole("alert")).toBeNull();
    act(() => socket.message({ type: "response", v: 1, requestId: requests("thread.list")[1].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "B timed out" } }));
    expect((await screen.findByRole("alert")).textContent).toBe("加载会话超时，请重试。");
    expect(screen.queryByText(/不要直接重发/)).toBeNull();
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
    expect(screen.getByLabelText("会话列表").querySelector("#sidebar-device-status")?.textContent).toContain("Agent 离线");
    await act(async () => socket.message({ type: "response", v: 1, requestId: updatedRequests[0].requestId, outcome: "accepted", data: { devices: [device(true)] } }));
    expect(screen.getByLabelText("会话列表").querySelector("#sidebar-device-status")?.textContent).toContain("Agent 离线");
  });

  it("keeps the selected conversation visible across a transient Agent reconnect and resubscribes without a persistent alert", async () => {
    const offlineThread: Thread = { ...fixtureThread, turns: [{ turnId: "offline-turn", status: "completed", items: [{ itemId: "offline-message", role: "assistant", text: "Stable history content" }] }] };
    const { socket, requests } = await openFixture(offlineThread);
    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentOnline: false, codexReady: false }));
    expect(screen.getByText("Stable history content")).toBeTruthy();
    expect(screen.queryByText("Desktop Agent 暂时离线，恢复后会重新同步当前会话。")).toBeNull();
    const staleThread: Thread = { ...offlineThread, turns: [{ turnId: "offline-turn", status: "completed", items: [{ itemId: "stale-message", role: "assistant", text: "Stale stream content" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: staleThread }));
    expect(screen.queryByText("Stale stream content")).toBeNull();
    await waitFor(() => expect(requests("device.list")).toHaveLength(2));

    act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentOnline: true, codexReady: true }));
    await waitFor(() => expect(requests("device.list")).toHaveLength(3));
    const onlineDevice = { deviceId: "mac", deviceName: "昊天的 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-epoch-2", capabilities: { autoLoad: true } };
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("device.list")[2].requestId, outcome: "accepted", data: { devices: [onlineDevice] } }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("thread.subscribe")[1].requestId, outcome: "accepted", data: { subscriptionId: "sub-recovered" } }));
    act(() => socket.message({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub-recovered", streamId: "stream-recovered", seq: 1, thread: offlineThread }));
    expect(await screen.findByText("Stable history content")).toBeTruthy();
    expect(screen.queryByText("Desktop Agent 暂时离线，恢复后会重新同步当前会话。")).toBeNull();
  });

  it("shows the offline state only after the Agent remains unavailable beyond the reconnect grace period", async () => {
    const offlineThread: Thread = { ...fixtureThread, turns: [{ turnId: "offline-turn", status: "completed", items: [{ itemId: "offline-message", role: "assistant", text: "Stable history content" }] }] };
    const { socket } = await openFixture(offlineThread);
    vi.useFakeTimers();
    try {
      act(() => socket.message({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentOnline: false, codexReady: false }));
      expect(screen.getByText("Stable history content")).toBeTruthy();
      await act(async () => { await vi.advanceTimersByTimeAsync(8_000); });
      expect(screen.getByText("Stable history content")).toBeTruthy();
      expect(screen.getByText("Desktop Agent 暂时离线，恢复后会重新同步当前会话。")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
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
    expect(screen.getByLabelText("你（发送中）").textContent).toContain("fixture message");
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "accepted", data: { turnId: "turn-1" } }));
    expect((screen.getByLabelText("发送消息") as HTMLTextAreaElement).value).toBe("");
    expect(screen.getByLabelText("你（等待同步）").textContent).toContain("fixture message");

    const running: Thread = { ...fixtureThread, runtime: "inProgress", turns: [{ turnId: "turn-1", status: "inProgress", items: [{ itemId: "reply", role: "assistant", text: "Working" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: running }));
    expect(await screen.findByText("Working")).toBeTruthy();
    const stop = screen.getByRole("button", { name: "停止" });
    expect(stop.textContent).toBe("");
    expect(stop.querySelector("svg[aria-hidden='true']")).toBeTruthy();
    expect(stop.getAttribute("data-tooltip")).toBe("停止");
    fireEvent.click(stop);
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
    fireEvent.click(screen.getByRole("radio", { name: "Blue" }));
    fireEvent.change(screen.getByRole("textbox", { name: "说明" }), { target: { value: "自由文本" } });
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
    expect(screen.getByLabelText("你（结果待确认）").textContent).toContain("do not replay");
    expect(screen.getByRole("alert").textContent).toContain("执行结果不确定");
    expect(requests("turn.start")).toHaveLength(1);
  });

  it("clears an accepted send after a same-thread stream resubscription", async () => {
    const { socket, requests } = await openFixture();
    const composer = screen.getByLabelText("发送消息") as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "survive resync" } });
    fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    expect(requests("turn.start")).toHaveLength(1);

    act(() => socket.message({ type: "event", v: 1, event: "thread.error", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", code: "RESYNC_REQUIRED" }));
    await waitFor(() => expect(requests("thread.subscribe")).toHaveLength(2));
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "accepted", data: { turnId: "turn-resynced" } }));

    expect(composer.value).toBe("");
    expect(requests("turn.start")).toHaveLength(1);
  });

  it("clears an uncertain send only when the exact client message is observed", async () => {
    const { socket, requests } = await openFixture();
    const composer = screen.getByLabelText("发送消息") as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "uncertain message" } });
    fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    const clientMessageId = requests("turn.start")[0].params.clientMessageId;
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "owner receipt lost" } }));

    const wrongIdentity: Thread = { ...fixtureThread, turns: [{ turnId: "wrong", status: "inProgress", items: [{ itemId: "wrong-user", role: "user", clientMessageId: "11111111-1111-4111-8111-111111111111", text: "uncertain message" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: wrongIdentity }));
    expect(composer.value).toBe("uncertain message");

    const confirmed: Thread = { ...wrongIdentity, turns: [{ turnId: "confirmed", status: "inProgress", items: [{ itemId: "confirmed-user", role: "user", clientMessageId, text: "uncertain message" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 2, seq: 3, thread: confirmed }));
    expect(composer.value).toBe("");
    expect(screen.queryByLabelText("你（结果待确认）")).toBeNull();
    expect(requests("turn.start")).toHaveLength(1);
  });

  it("does not clear newer composer edits when an uncertain send is later confirmed", async () => {
    const { socket, requests } = await openFixture();
    const composer = screen.getByLabelText("发送消息") as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "first message" } });
    fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    const clientMessageId = requests("turn.start")[0].params.clientMessageId;
    await act(async () => socket.message({ type: "response", v: 1, requestId: requests("turn.start")[0].requestId, outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "owner receipt lost" } }));
    fireEvent.change(composer, { target: { value: "new draft" } });

    const confirmed: Thread = { ...fixtureThread, turns: [{ turnId: "confirmed", status: "inProgress", items: [{ itemId: "confirmed-user", role: "user", clientMessageId, text: "first message" }] }] };
    act(() => socket.message({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", baseSeq: 1, seq: 2, thread: confirmed }));
    expect(composer.value).toBe("new draft");
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
