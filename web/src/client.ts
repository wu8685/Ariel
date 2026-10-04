import { validateEnvelope } from "./protocol";
import { newRequestID } from "./ids";
import type { ArielProtocolV1Envelope, Response } from "./generated/protocol";

export type ConnectionStatus = "disconnected" | "connecting" | "ready" | "invalid";
export function isWebPIN(value: string): boolean { return /^[0-9]{6}$/.test(value); }
type Method = "device.list" | "thread.list" | "thread.read" | "thread.history" | "thread.history.items" | "thread.subscribe" | "thread.unsubscribe" | "turn.start" | "turn.interrupt" | "interaction.respond";
type Pending = { finish: (response: Response) => void; timer: ReturnType<typeof setTimeout> };

export function timeoutFor(method: Method): number {
  if (method === "thread.subscribe" || method === "thread.history" || method === "thread.history.items") return 35000;
  if (method === "turn.start" || method === "turn.interrupt" || method === "interaction.respond") return 50000;
  return 12000;
}

export class ArielSocket {
  onStatus: (status: ConnectionStatus) => void = () => {};
  onEvent: (event: ArielProtocolV1Envelope) => void = () => {};
  onReady: (relayEpoch: string, sessionToken?: string) => void = () => {};
  private ws: WebSocket | null = null;
  private token = "";
  private ready = false;
  private retry = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private helloTimer: ReturnType<typeof setTimeout> | null = null;
  private pending = new Map<string, Pending>();

  constructor(private readonly url: string, private readonly factory: (url: string) => WebSocket = url => new WebSocket(url)) {}

  connect(token: string) {
    this.disconnect();
    this.token = token;
    if (token) this.open();
  }

  disconnect() {
    this.token = "";
    this.ready = false;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    if (this.helloTimer) clearTimeout(this.helloTimer);
    this.retryTimer = null;
    this.helloTimer = null;
    const ws = this.ws;
    this.ws = null;
    ws?.close();
    this.failPending();
    this.onStatus("disconnected");
  }

  wake() {
    if (!this.token) return;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    if (this.helloTimer) clearTimeout(this.helloTimer);
    this.retryTimer = null;
    this.helloTimer = null;
    const old = this.ws;
    this.ws = null;
    this.ready = false;
    old?.close();
    this.failPending();
    this.open();
  }

  request(method: Method, deviceId: string, params: Record<string, unknown>): Promise<Response> {
    const requestId = newRequestID();
    if (!this.ready || !this.ws || this.ws.readyState !== 1) return Promise.resolve(this.localFailure(requestId, "not_submitted", "DEVICE_OFFLINE"));
    if (this.pending.size >= 32) return Promise.resolve(this.localFailure(requestId, "not_submitted", "OVERLOADED"));
    const message = { type: "request", v: 1, requestId, deviceId, method, params };
    if (!validateEnvelope(message)) return Promise.resolve(this.localFailure(requestId, "not_submitted", "INVALID_ARGUMENT"));
    return new Promise(resolve => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        resolve(this.localFailure(requestId, "unknown", "OUTCOME_UNKNOWN"));
      }, timeoutFor(method));
      this.pending.set(requestId, { finish: resolve, timer });
      try { this.ws?.send(JSON.stringify(message)); }
      catch {
        clearTimeout(timer);
        this.pending.delete(requestId);
        resolve(this.localFailure(requestId, "unknown", "OUTCOME_UNKNOWN"));
      }
    });
  }

  private open() {
    if (!this.token) return;
    this.onStatus("connecting");
    let ws: WebSocket;
    try { ws = this.factory(this.url); }
    catch { this.scheduleReconnect(); return; }
    this.ws = ws;
    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "hello", v: 1, role: "web", token: this.token }));
      this.helloTimer = setTimeout(() => ws.close(), 5000);
    };
    ws.onmessage = event => {
      let value: unknown;
      try { value = JSON.parse(String(event.data)); } catch { this.onStatus("invalid"); ws.close(); return; }
      if (!validateEnvelope(value)) { this.onStatus("invalid"); ws.close(); return; }
      if (value.type === "hello.ok") {
        if (this.ready) { this.onStatus("invalid"); ws.close(); return; }
        if (this.helloTimer) clearTimeout(this.helloTimer);
        this.ready = true;
        this.retry = 0;
        if (value.sessionToken) this.token = value.sessionToken;
        this.onStatus("ready");
        this.onReady(value.relayEpoch, value.sessionToken);
        return;
      }
      if (!this.ready) { this.onStatus("invalid"); ws.close(); return; }
      if (value.type === "response") {
        const pending = this.pending.get(value.requestId);
        if (pending) { clearTimeout(pending.timer); this.pending.delete(value.requestId); pending.finish(value); }
      } else if (value.type === "event") this.onEvent(value);
      else { this.onStatus("invalid"); ws.close(); }
    };
    ws.onclose = event => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.ready = false;
      if (this.helloTimer) clearTimeout(this.helloTimer);
      this.failPending();
      if (event.code === 1008) {
        this.token = "";
        this.onStatus("invalid");
        return;
      }
      this.onStatus("disconnected");
      if (this.token) this.scheduleReconnect();
    };
  }

  private scheduleReconnect() {
    const base = Math.min(15000, 1000 * 2 ** Math.min(this.retry++, 4));
    this.retryTimer = setTimeout(() => this.open(), base * (0.8 + Math.random() * 0.4));
  }

  private failPending() {
    for (const [id, pending] of this.pending) { clearTimeout(pending.timer); pending.finish(this.localFailure(id, "unknown", "OUTCOME_UNKNOWN")); }
    this.pending.clear();
  }

  private localFailure(requestId: string, outcome: "unknown" | "not_submitted", code: "DEVICE_OFFLINE" | "OVERLOADED" | "INVALID_ARGUMENT" | "OUTCOME_UNKNOWN"): Response {
    return { type: "response", v: 1, requestId, outcome, error: { code, message: code } };
  }
}
