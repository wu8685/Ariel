import { afterEach, describe, expect, it, vi } from "vitest";
import { base64URLToBuffer, bufferToBase64URL, loginWithPasskey, registerPasskey } from "./auth";

afterEach(() => vi.restoreAllMocks());

describe("Passkey Web client", () => {
  it("round-trips unpadded base64url values", () => {
    const bytes = new Uint8Array([0, 1, 2, 250, 255]);
    const encoded = bufferToBase64URL(bytes);
    expect(encoded).toBe("AAEC-v8");
    expect([...new Uint8Array(base64URLToBuffer(encoded))]).toEqual([...bytes]);
  });

  it("converts assertion options and posts only the credential response", async () => {
    const requests: Array<{ url: string; body: unknown }> = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      requests.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (url.endsWith("/options")) return new Response(JSON.stringify({ publicKey: { challenge: "AQI", rpId: "ariel.example.com", allowCredentials: [{ type: "public-key", id: "AwQ" }], userVerification: "required" } }), { status: 200, headers: { "Content-Type": "application/json" } });
      return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
    const get = vi.fn(async (options: CredentialRequestOptions) => {
      expect([...new Uint8Array(options.publicKey!.challenge as ArrayBuffer)]).toEqual([1, 2]);
      expect([...new Uint8Array(options.publicKey!.allowCredentials![0].id as ArrayBuffer)]).toEqual([3, 4]);
      return {
        id: "credential", type: "public-key", rawId: new Uint8Array([5]).buffer,
        authenticatorAttachment: "platform", getClientExtensionResults: () => ({}),
        response: { clientDataJSON: new Uint8Array([6]).buffer, authenticatorData: new Uint8Array([7]).buffer, signature: new Uint8Array([8]).buffer, userHandle: null },
      } as unknown as PublicKeyCredential;
    });
    Object.defineProperty(navigator, "credentials", { configurable: true, value: { get } });

    await loginWithPasskey();
    expect(get).toHaveBeenCalledOnce();
    expect(requests.map(request => request.url)).toEqual(["/api/auth/passkey/login/options", "/api/auth/passkey/login/verify"]);
    expect(requests[1].body).toMatchObject({ id: "credential", rawId: "BQ", response: { clientDataJSON: "Bg", authenticatorData: "Bw", signature: "CA", userHandle: null } });
  });

  it("converts registration options and carries the setup token only to the options endpoint", async () => {
    const requests: Array<{ url: string; body: unknown }> = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      requests.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (url.endsWith("/options")) return new Response(JSON.stringify({ publicKey: { challenge: "AQI", rp: { id: "ariel.example.com", name: "Ariel" }, user: { id: "AwQ", name: "owner", displayName: "Ariel Owner" }, pubKeyCredParams: [{ type: "public-key", alg: -7 }] } }), { status: 200, headers: { "Content-Type": "application/json" } });
      return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
    Object.defineProperty(navigator, "credentials", { configurable: true, value: { create: vi.fn(async () => ({
      id: "new-credential", type: "public-key", rawId: new Uint8Array([5]).buffer,
      authenticatorAttachment: "platform", getClientExtensionResults: () => ({}),
      response: { clientDataJSON: new Uint8Array([6]).buffer, attestationObject: new Uint8Array([7]).buffer, getTransports: () => ["internal"] },
    } as unknown as PublicKeyCredential)) } });

    await registerPasskey("bootstrap-secret");
    expect(requests[0].body).toEqual({ setupToken: "bootstrap-secret" });
    expect(requests[1].body).toMatchObject({ id: "new-credential", response: { clientDataJSON: "Bg", attestationObject: "Bw", transports: ["internal"] } });
    expect(JSON.stringify(requests[1].body)).not.toContain("bootstrap-secret");
  });
});
