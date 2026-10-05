import { describe, expect, it } from "vitest";
import { validateEnvelope } from "./protocol";

describe("shared Ariel v1 schema", () => {
  it("accepts a valid web hello and request", () => {
    expect(validateEnvelope({ type: "hello", v: 1, role: "web", token: "secret" })).toBe(true);
    expect(validateEnvelope({
      type: "request", v: 1, requestId: "00000000-0000-4000-8000-000000000001",
      deviceId: "mock-mac", method: "thread.list", params: { limit: 20 }
    })).toBe(true);
  });

  it("rejects unknown versions, extra fields and incomplete mutations", () => {
    expect(validateEnvelope({ type: "hello", v: 2, role: "web", token: "secret" })).toBe(false);
    expect(validateEnvelope({ type: "hello", v: 1, role: "web", token: "secret", deviceId: "claimed" })).toBe(false);
    expect(validateEnvelope({
      type: "request", v: 1, requestId: "00000000-0000-4000-8000-000000000001",
      deviceId: "mock-mac", method: "turn.start", params: { text: "hi" }
    })).toBe(false);
  });

  it("accepts bounded native search terms but rejects empty or oversized terms", () => {
    const request = (searchTerm: string) => ({
      type: "request", v: 1, requestId: "00000000-0000-4000-8000-000000000001",
      deviceId: "mock-mac", method: "thread.list", params: { limit: 50, searchTerm }
    });
    expect(validateEnvelope(request("Ariel"))).toBe(true);
    expect(validateEnvelope(request(""))).toBe(false);
    expect(validateEnvelope(request("x".repeat(129)))).toBe(false);
  });
});
