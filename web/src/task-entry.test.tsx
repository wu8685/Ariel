// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

class FixtureSocket {
  static sockets: FixtureSocket[] = [];
  static readonly OPEN = 1;
  readyState = 1;
  sent: string[] = [];
  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  constructor(_url: string) { FixtureSocket.sockets.push(this); }
  send(raw: string) { this.sent.push(raw); }
  close() { this.readyState = 3; }
  receive(data: unknown) { this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent); }
}
const sessionToken = `s_${"d".repeat(64)}`;
let originalSocket: typeof WebSocket;
beforeEach(() => {
  originalSocket = globalThis.WebSocket;
  globalThis.WebSocket = FixtureSocket as unknown as typeof WebSocket;
  FixtureSocket.sockets = [];
  sessionStorage.clear(); localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => { cleanup(); globalThis.WebSocket = originalSocket; vi.restoreAllMocks(); document.querySelector('meta[name="ariel-enable-tasks"]')?.remove(); });
async function connect(tasksEnabled: boolean) {
  if (tasksEnabled) { const meta = document.createElement("meta"); meta.name = "ariel-enable-tasks"; meta.content = "true"; document.head.append(meta); }
  sessionStorage.setItem("ariel.web-session.v1", sessionToken);
  render(<App />);
  const socket = FixtureSocket.sockets[0];
  const requests = () => socket.sent.map(raw => JSON.parse(raw)).filter(message => message.type === "request");
  socket.onopen?.(new Event("open"));
  await act(async () => socket.receive({ type: "hello.ok", v: 1, connectionId: "task-entry", relayEpoch: "epoch", sessionToken }));
  const deviceRequest = requests().find(message => message.method === "device.list");
  await act(async () => socket.receive({ type: "response", v: 1, requestId: deviceRequest.requestId, outcome: "accepted", data: { devices: [
    { deviceId: "m", deviceName: "管理主机", agentEpoch: "m-1", agentOnline: true, codexReady: false, taskStoreId: "s", capabilities: { autoLoad: false, codexReady: false, taskManagement: true } },
    { deviceId: "e", deviceName: "执行主机", agentEpoch: "e-1", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, codexReady: true, taskExecution: true } },
  ] } }));
  return { socket, requests };
}
describe("Ariel task entry", () => {
  it("keeps the whole task path absent by default", async () => {
    const { requests } = await connect(false);
    expect(screen.queryByRole("button", { name: "任务看板" })).toBeNull();
    expect(requests().filter(message => message.method.startsWith("task."))).toHaveLength(0);
  });
  it("keeps the task path absent even when the backend task flag is enabled", async () => {
    const { requests } = await connect(true);
    expect(requests().filter(message => message.method.startsWith("task."))).toHaveLength(0);
    expect(screen.queryByRole("button", { name: "任务看板" })).toBeNull();
    expect(screen.queryByRole("button", { name: "进入任务看板" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "任务看板" })).toBeNull();
  });
});
