import { describe, expect, it, vi } from "vitest";
import { ArielSocket, isWebPIN, timeoutFor } from "./client";

class FakeSocket {
  readyState = 1;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  send(text: string) { this.sent.push(text); }
  close(code = 1000, reason = "") { this.readyState = 3; this.onclose?.({ code, reason }); }
  message(value: unknown) { this.onmessage?.({ data: JSON.stringify(value) }); }
}

describe("Ariel WebSocket client", () => {
  it("does not let a retired handshake confirm its replacement", () => {
    const sockets: FakeSocket[] = [];
    const client = new ArielSocket("ws://localhost/ws", () => {
      const socket = new FakeSocket(); sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    const ready = vi.fn(); client.onReady = ready;
    client.connect("cookie"); sockets[0].onopen?.();
    client.wake(); sockets[1].onopen?.();
    sockets[0].message({ type: "hello.ok", v: 1, connectionId: "retired", relayEpoch: "old" });
    expect(ready).not.toHaveBeenCalled();
    sockets[1].message({ type: "hello.ok", v: 1, connectionId: "current", relayEpoch: "new" });
    expect(ready).toHaveBeenCalledExactlyOnceWith("new", undefined);
    client.disconnect();
  });
  it("ignores a retired response and event even when their schema is valid", async () => {
    const sockets: FakeSocket[] = [];
    const client = new ArielSocket("ws://localhost/ws", () => {
      const socket = new FakeSocket(); sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    client.connect("cookie"); sockets[0].onopen?.();
    client.connect("cookie"); sockets[1].onopen?.();
    sockets[1].message({ type: "hello.ok", v: 1, connectionId: "new", relayEpoch: "new" });
    const event = vi.fn(); client.onEvent = event;
    const finished = vi.fn(); const pending = client.request("device.list", "relay", {}).then(finished);
    const requestId = JSON.parse(sockets[1].sent.at(-1)!).requestId;
    sockets[0].message({ type: "event", v: 1, event: "device.status", deviceId: "old", agentOnline: false, codexReady: false });
    sockets[0].message({ type: "response", v: 1, requestId, outcome: "accepted", data: { devices: ["retired"] } });
    await Promise.resolve();
    expect(event).not.toHaveBeenCalled();
    expect(finished).not.toHaveBeenCalled();
    sockets[1].message({ type: "response", v: 1, requestId, outcome: "accepted", data: { devices: [] } });
    await pending;
    expect(finished).toHaveBeenCalledWith(expect.objectContaining({ data: { devices: [] } }));
    client.disconnect();
  });
  it("ignores late open and handshake callbacks after explicit disconnect", () => {
    vi.useFakeTimers();
    try {
      const socket = new FakeSocket();
      const client = new ArielSocket("ws://localhost/ws", () => socket as unknown as WebSocket);
      const ready = vi.fn(), statuses = vi.fn(); client.onReady = ready; client.onStatus = statuses;
      client.connect("cookie"); client.disconnect();
      socket.onopen?.();
      socket.message({ type: "hello.ok", v: 1, connectionId: "late", relayEpoch: "late" });
      expect(socket.sent).toHaveLength(0);
      expect(ready).not.toHaveBeenCalled();
      expect(statuses).toHaveBeenLastCalledWith("disconnected");
      expect(vi.getTimerCount()).toBe(0);
    } finally { vi.useRealTimers(); }
  });
  it("a retired open cannot replace the active handshake deadline", () => {
    vi.useFakeTimers();
    try {
      const sockets: FakeSocket[] = [];
      const client = new ArielSocket("ws://localhost/ws", () => {
        const socket = new FakeSocket(); sockets.push(socket);
        return socket as unknown as WebSocket;
      });
      const statuses = vi.fn(); client.onStatus = statuses;
      client.connect("cookie"); client.connect("cookie");
      sockets[1].onopen?.(); sockets[0].onopen?.();
      sockets[1].message({ type: "hello.ok", v: 1, connectionId: "new", relayEpoch: "new" });
      vi.advanceTimersByTime(60_000);
      expect(sockets[0].sent).toHaveLength(0);
      expect(sockets).toHaveLength(2);
      expect(statuses).toHaveBeenLastCalledWith("ready");
      expect(vi.getTimerCount()).toBe(0);
      client.disconnect();
    } finally { vi.useRealTimers(); }
  });
  it("uses the Relay-issued session token on reconnect, not the PIN", () => {
    const sockets: FakeSocket[] = [];
    const client = new ArielSocket("ws://localhost/ws", () => {
      const socket = new FakeSocket(); sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    const ready: string[] = [];
    client.onReady = (_epoch, sessionToken) => ready.push(sessionToken || "");
    client.connect("012345");
    sockets[0].onopen?.();
    const sessionToken = `s_${"a".repeat(64)}`;
    sockets[0].message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e", sessionToken });
    expect(ready).toEqual([sessionToken]);
    sockets[0].close(1006);
    client.wake();
    sockets[1].onopen?.();
    expect(JSON.parse(sockets[1].sent[0]).token).toBe(sessionToken);
    client.disconnect();
  });
  it("accepts exactly six ASCII digits including a leading zero", () => {
    expect(isWebPIN("012345")).toBe(true);
    for (const value of ["", "12345", "1234567", "12a456", "１２３４５６"]) expect(isWebPIN(value)).toBe(false);
  });

  it("does not retry a rejected PIN and asks for correction", () => {
    vi.useFakeTimers();
    try {
      const sockets: FakeSocket[] = [];
      const client = new ArielSocket("ws://localhost/ws", () => {
        const socket = new FakeSocket(); sockets.push(socket);
        return socket as unknown as WebSocket;
      });
      const statuses: string[] = [];
      client.onStatus = status => statuses.push(status);
      client.connect("012345"); sockets[0].onopen?.();
      sockets[0].close(1008, "unauthorized");
      vi.advanceTimersByTime(60000);
      expect(sockets).toHaveLength(1);
      expect(statuses.at(-1)).toBe("invalid");
      client.wake();
      expect(sockets).toHaveLength(1);
      client.connect("654321");
      expect(sockets).toHaveLength(2);
      client.disconnect();
    } finally { vi.useRealTimers(); }
  });
  it("separates authentication rejection from protocol incompatibility", () => {
    vi.useFakeTimers();
    try {
      const sockets: FakeSocket[] = [];
      const client = new ArielSocket("ws://localhost/ws", () => {
        const socket = new FakeSocket(); sockets.push(socket);
        return socket as unknown as WebSocket;
      });
      const statuses: string[] = [];
      client.onStatus = status => statuses.push(status);

      client.connect("cookie"); sockets[0].onopen?.();
      sockets[0].close(1008, "invalid message");
      expect(statuses.at(-1)).toBe("incompatible");
      vi.advanceTimersByTime(60000);
      expect(sockets).toHaveLength(1);

      client.connect("cookie"); sockets[1].onopen?.();
      sockets[1].close(1008, "unexpected policy reason");
      expect(statuses.at(-1)).toBe("incompatible");
      vi.advanceTimersByTime(60000);
      expect(sockets).toHaveLength(2);
    } finally { vi.useRealTimers(); }
  });
  it("keeps local schema failures terminal and distinct from authentication", () => {
    vi.useFakeTimers();
    try {
      const socket = new FakeSocket();
      const client = new ArielSocket("ws://localhost/ws", () => socket as unknown as WebSocket);
      const statuses: string[] = [];
      client.onStatus = status => statuses.push(status);
      client.connect("cookie"); socket.onopen?.();
      socket.message({ type: "event", v: 1, event: "future.event" });
      expect(statuses.at(-1)).toBe("incompatible");
      vi.advanceTimersByTime(60000);
      expect(statuses.at(-1)).toBe("incompatible");
    } finally { vi.useRealTimers(); }
  });
  it("does not replay a one-time pairing credential after an uncertain disconnect", () => {
    vi.useFakeTimers();
    try {
      const sockets: FakeSocket[] = [];
      const client = new ArielSocket("ws://localhost/ws", () => {
        const socket = new FakeSocket(); sockets.push(socket);
        return socket as unknown as WebSocket;
      });
      const statuses: string[] = [];
      client.onStatus = status => statuses.push(status);
      client.connect(`p_${"a".repeat(64)}`);
      sockets[0].onopen?.();
      sockets[0].close(1006);
      vi.advanceTimersByTime(60000);
      expect(sockets).toHaveLength(1);
      expect(statuses.at(-1)).toBe("invalid");
      client.disconnect();
    } finally { vi.useRealTimers(); }
  });
  it("keeps Web read deadlines beyond the Relay read budget", () => {
    for (const method of ["thread.list", "thread.read", "thread.history", "thread.history.items", "thread.image"] as const) {
      expect(timeoutFor(method)).toBe(40000);
    }
    expect(timeoutFor("thread.subscribe")).toBe(60000);
    expect(timeoutFor("turn.start")).toBe(50000);
    expect(timeoutFor("queue.add")).toBe(timeoutFor("turn.start"));
    expect(timeoutFor("queue.reorder")).toBe(timeoutFor("turn.start"));
    expect(timeoutFor("thread.create")).toBe(timeoutFor("turn.start"));
    expect(timeoutFor("thread.bootstrap")).toBe(timeoutFor("turn.start"));
    expect(timeoutFor("thread.name.update")).toBe(timeoutFor("turn.start"));
    expect(timeoutFor("thread.pin.update")).toBe(timeoutFor("turn.start"));
    expect(timeoutFor("thread.list", { searchTerm: "Ariel" })).toBe(40000);
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

  it("classifies an interrupted subscription as safely recoverable", async () => {
    const socket = new FakeSocket();
    const client = new ArielSocket("ws://localhost/ws", () => socket as unknown as WebSocket);
    client.connect("secret"); socket.onopen?.();
    socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e" });
    const pending = client.request("thread.subscribe", "device", { threadId: "t" });
    socket.close();
    expect(await pending).toMatchObject({ outcome: "rejected", error: { code: "RESYNC_REQUIRED" } });
    client.disconnect();
  });

  it("classifies a local subscription deadline as a retryable timeout", async () => {
    vi.useFakeTimers();
    try {
      const socket = new FakeSocket();
      const client = new ArielSocket("ws://localhost/ws", () => socket as unknown as WebSocket);
      client.connect("secret"); socket.onopen?.();
      socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e" });
      const pending = client.request("thread.subscribe", "device", { threadId: "t" });
      await vi.advanceTimersByTimeAsync(timeoutFor("thread.subscribe"));
      expect(await pending).toMatchObject({ outcome: "rejected", error: { code: "TIMEOUT" } });
      client.disconnect();
    } finally { vi.useRealTimers(); }
  });

  it("classifies a local thread-list deadline as a retryable timeout", async () => {
    vi.useFakeTimers();
    try {
      const socket = new FakeSocket();
      const client = new ArielSocket("ws://localhost/ws", () => socket as unknown as WebSocket);
      client.connect("secret"); socket.onopen?.();
      socket.message({ type: "hello.ok", v: 1, connectionId: "c", relayEpoch: "e" });
      const pending = client.request("thread.list", "device", { limit: 50 });
      await vi.advanceTimersByTimeAsync(timeoutFor("thread.list"));
      expect(await pending).toMatchObject({ outcome: "rejected", error: { code: "TIMEOUT" } });
      client.disconnect();
    } finally { vi.useRealTimers(); }
  });

  it("keeps a healthy multiplexed connection and in-flight mutation on foreground wake", async () => {
    const sockets: FakeSocket[] = [];
    const client = new ArielSocket("ws://localhost/ws", () => {
      const socket = new FakeSocket(); sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    client.connect("secret"); sockets[0].onopen?.();
    sockets[0].message({ type: "hello.ok", v: 1, connectionId: "c1", relayEpoch: "e1" });
    const pending = client.request("turn.start", "device", { threadId: "t", clientMessageId: crypto.randomUUID(), text: "hi" });
    client.wake();
    expect(sockets).toHaveLength(1);
    expect(sockets[0].readyState).toBe(1);
    const outbound = JSON.parse(sockets[0].sent.at(-1)!);
    sockets[0].message({ type: "response", v: 1, requestId: outbound.requestId, outcome: "accepted", data: { turnId: "turn" } });
    expect((await pending).outcome).toBe("accepted");
    client.disconnect();
  });

  it("reconnects immediately on foreground wake when the socket is unavailable", () => {
    vi.useFakeTimers();
    const sockets: FakeSocket[] = [];
    try {
      const client = new ArielSocket("ws://localhost/ws", () => {
        const socket = new FakeSocket(); sockets.push(socket);
        return socket as unknown as WebSocket;
      });
      client.connect("secret"); sockets[0].onopen?.();
      sockets[0].message({ type: "hello.ok", v: 1, connectionId: "c1", relayEpoch: "e1" });
      sockets[0].close(1006);
      expect(sockets).toHaveLength(1);
      client.wake();
      expect(sockets).toHaveLength(2);
      sockets[1].onopen?.();
      expect(JSON.parse(sockets[1].sent[0]).token).toBe("secret");
      client.disconnect();
    } finally { vi.useRealTimers(); }
  });
});
