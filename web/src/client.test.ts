import { describe, expect, it, vi } from "vitest";
import { ArielSocket, timeoutFor } from "./client";

class FakeSocket {
  readyState = 1;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  send(text: string) { this.sent.push(text); }
  close() { this.readyState = 3; this.onclose?.(); }
  message(value: unknown) { this.onmessage?.({ data: JSON.stringify(value) }); }
}

describe("Ariel WebSocket client", () => {
  it("waits longer for auto-load and owner mutations than ordinary reads", () => {
    expect(timeoutFor("thread.subscribe")).toBeGreaterThan(timeoutFor("thread.list"));
    expect(timeoutFor("turn.start")).toBeGreaterThan(timeoutFor("thread.list"));
  });
  it("sends token only in hello, not the URL, and correlates replies", async () => {
    const socket = new FakeSocket();
    const factory = vi.fn((_url: string) => socket as unknown as WebSocket);
    const client = new ArielSocket("ws://localhost/ws", factory);
    client.connect("secret");
    socket.onopen?.();
    expect(factory).toHaveBeenCalledWith("ws://localhost/ws");
    expect(JSON.parse(socket.sent[0])).toEqual({ type: "hello", v: 1, role: "web", token: "secret" });
    socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e" });
    const pending = client.request("device.list", "relay", {});
    const outbound = JSON.parse(socket.sent[1]);
    socket.message({ type: "response", v: 1, requestId: outbound.requestId, outcome: "accepted", data: { devices: [] } });
    expect((await pending).outcome).toBe("accepted");
    client.disconnect();
  });

  it("reports forwarded unresolved requests as unknown on disconnection", async () => {
    const socket = new FakeSocket();
    const client = new ArielSocket("ws://localhost/ws", () => socket as unknown as WebSocket);
    client.connect("secret"); socket.onopen?.();
    socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e" });
    const pending = client.request("thread.read", "device", { threadId: "t" });
    socket.close();
    expect((await pending).outcome).toBe("unknown");
    client.disconnect();
  });

  it("reconnects on foreground wake and marks in-flight mutation unknown", async () => {
    const sockets: FakeSocket[] = [];
    const client = new ArielSocket("ws://localhost/ws", () => {
      const socket = new FakeSocket(); sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    client.connect("secret"); sockets[0].onopen?.();
    sockets[0].message({ type: "hello.ok", v: 1, connectionId: "c1", relayEpoch: "e1" });
    const pending = client.request("turn.start", "device", { threadId: "t", clientMessageId: crypto.randomUUID(), text: "hi" });
    client.wake();
    expect((await pending).outcome).toBe("unknown");
    expect(sockets).toHaveLength(2);
    expect(sockets[0].readyState).toBe(3);
    sockets[1].onopen?.();
    expect(JSON.parse(sockets[1].sent[0]).token).toBe("secret");
    client.disconnect();
  });
});
