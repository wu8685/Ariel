import { describe, expect, it, vi } from "vitest";
import { pairingURL, takePairingCredential } from "./pairing";

const credential = `p_${"a".repeat(64)}`;

describe("pairing URL handling", () => {
  it("keeps the one-time credential in a fragment and removes it immediately", () => {
    const replaceState = vi.fn();
    const found = takePairingCredential(
      { hash: `#pair=${credential}`, pathname: "/", search: "?source=camera" },
      { replaceState },
    );
    expect(found).toBe(credential);
    expect(replaceState).toHaveBeenCalledWith(null, "", "/?source=camera");
    expect(pairingURL("http://192.168.1.8:8080", credential)).toBe(`http://192.168.1.8:8080/#pair=${credential}`);
  });

  it("rejects malformed credentials after clearing only pairing fragments", () => {
    const replaceState = vi.fn();
    expect(takePairingCredential({ hash: "#pair=012345", pathname: "/", search: "" }, { replaceState })).toBe("");
    expect(replaceState).toHaveBeenCalled();
    replaceState.mockClear();
    expect(takePairingCredential({ hash: "#section", pathname: "/", search: "" }, { replaceState })).toBe("");
    expect(replaceState).not.toHaveBeenCalled();
  });
});
