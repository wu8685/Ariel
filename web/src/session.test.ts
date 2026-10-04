import { describe, expect, it } from "vitest";
import { WebSession } from "./session";

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
    removeItem: (key: string) => { values.delete(key); },
  };
}

const sessionToken = `s_${"a".repeat(64)}`;

describe("Web session persistence", () => {
  it("keeps only the Relay-issued session for same-tab reload", () => {
    const storage = memoryStorage();
    const firstPage = new WebSession(() => storage);
    expect(firstPage.saved()).toBe("");
    firstPage.accepted(sessionToken);
    expect(new WebSession(() => storage).saved()).toBe(sessionToken);
    expect(storage.getItem("ariel.web-session.v1")).toBe(sessionToken);
  });

  it("clears a rejected session and explicit disconnect", () => {
    const storage = memoryStorage();
    const session = new WebSession(() => storage);
    session.accepted(sessionToken);
    session.rejected();
    expect(session.saved()).toBe("");
    session.accepted(sessionToken);
    session.disconnect();
    expect(session.saved()).toBe("");
  });

  it("ignores malformed stored credentials and storage access failures", () => {
    const storage = memoryStorage();
    storage.setItem("ariel.web-session.v1", "123456");
    expect(new WebSession(() => storage).saved()).toBe("");
    const unavailable = new WebSession(() => { throw new Error("storage denied"); });
    expect(unavailable.saved()).toBe("");
    expect(() => unavailable.accepted(sessionToken)).not.toThrow();
    expect(() => unavailable.disconnect()).not.toThrow();
  });
});
