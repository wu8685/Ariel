import { describe, expect, it, vi } from "vitest";
import { newRequestID } from "./ids";

describe("request IDs on LAN HTTP", () => {
  it("uses getRandomValues without requiring secure-context randomUUID", () => {
    const values = new Uint8Array(16);
    values.fill(0xff);
    const fill = vi.spyOn(crypto, "getRandomValues").mockImplementation(array => {
      (array as Uint8Array).set(values);
      return array;
    });
    const old = crypto.randomUUID;
    Object.defineProperty(crypto, "randomUUID", { configurable: true, value: undefined });
    try {
      expect(newRequestID()).toBe("ffffffff-ffff-4fff-bfff-ffffffffffff");
      expect(fill).toHaveBeenCalledTimes(1);
    } finally {
      Object.defineProperty(crypto, "randomUUID", { configurable: true, value: old });
      fill.mockRestore();
    }
  });
});
