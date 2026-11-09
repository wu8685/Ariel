import { afterEach, describe, expect, it, vi } from "vitest";
import { approveRemoteLogin, remoteLoginURL, startRemoteLogin, takeRemoteLoginID } from "./remote-login";

afterEach(() => vi.restoreAllMocks());

describe("Passkey remote login client", () => {
  it("removes the challenge id from the address bar and never accepts a poll token as an id", () => {
    const id = "AbCdEfGhIjKlMnOpQrStUv";
    const pollToken = "p".repeat(43);
    let current = `/?remoteLogin=${id}&from=qr#keep`;
    const location = {
      pathname: "/",
      get search() { return current.slice(current.indexOf("?"), current.indexOf("#")); },
      hash: "#keep",
    };
    const history = { replaceState: (_data: unknown, _unused: string, url?: string | URL | null) => { current = String(url); } };
    expect(takeRemoteLoginID(location, history)).toBe(id);
    expect(current).toBe("/?from=qr#keep");
    expect(remoteLoginURL("https://www.kan.chat/", id)).toBe(`https://www.kan.chat/?remoteLogin=${id}`);
    expect(remoteLoginURL("https://www.kan.chat/", id)).not.toContain(pollToken);
    expect(() => remoteLoginURL("https://www.kan.chat", pollToken)).toThrow(/invalid remote login/);
  });

  it("drops an invalid challenge id after stripping it", () => {
    let url = "/?remoteLogin=short";
    const location = { pathname: "/", search: "?remoteLogin=short", hash: "" };
    const history = { replaceState: (_data: unknown, _unused: string, next?: string | URL | null) => { url = String(next); } };
    expect(takeRemoteLoginID(location, history)).toBe("");
    expect(url).toBe("/");
  });

  it("posts a start and an explicit approve without putting the poll token in the approve body", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("fetch", vi.fn(async (_url: string, init?: RequestInit) => {
      bodies.push(JSON.parse(String(init?.body)));
      return new Response(JSON.stringify(bodies.length === 1
        ? { id: "AbCdEfGhIjKlMnOpQrStUv", pollToken: "t".repeat(43), expiresAt: "2026-11-09T12:03:00Z", code: "K7NQ2M" }
        : { status: "approved" }), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
    const challenge = await startRemoteLogin();
    await approveRemoteLogin(challenge.id);
    expect(bodies[0]).toEqual({});
    expect(bodies[1]).toEqual({ id: challenge.id });
    expect(JSON.stringify(bodies[1])).not.toContain(challenge.pollToken);
  });
});
