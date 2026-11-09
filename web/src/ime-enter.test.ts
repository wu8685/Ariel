import { describe, expect, it } from "vitest";
import { createImeEnterGuard } from "./ime-enter";

function key(overrides: Record<string, unknown> = {}) {
  return { key: "Enter", shiftKey: false, nativeEvent: {}, ...overrides };
}

describe("IME Enter guard", () => {
  it("rejects every active-composition signal without treating Shift+Enter as submit", () => {
    let now = 1_000;
    const guard = createImeEnterGuard({ now: () => now });
    expect(guard.classify(key({ isComposing: true }))).toBe("composition");
    expect(guard.classify(key({ nativeEvent: { isComposing: true } }))).toBe("composition");
    expect(guard.classify(key({ keyCode: 229 }))).toBe("composition");
    expect(guard.blocksFormSubmit()).toBe(true);
    expect(guard.classify(key({ nativeEvent: { keyCode: 229 } }))).toBe("composition");
    guard.compositionStart();
    expect(guard.classify(key())).toBe("composition");
    expect(guard.classify(key({ shiftKey: true }))).toBe("other");
    now += 1;
  });

  it("suppresses Safari's compositionend tail Enter, then restores ordinary Enter", () => {
    let now = 2_000;
    const guard = createImeEnterGuard({ now: () => now, postCompositionMs: 50 });
    guard.compositionStart();
    expect(guard.blocksFormSubmit()).toBe(true);
    guard.compositionEnd();
    expect(guard.classify(key())).toBe("post-composition");
    expect(guard.blocksFormSubmit()).toBe(true);
    guard.allowExplicitFormSubmit();
    expect(guard.blocksFormSubmit()).toBe(false);
    expect(guard.blocksFormSubmit()).toBe(true);
    now += 51;
    expect(guard.classify(key())).toBe("submit");
    expect(guard.blocksFormSubmit()).toBe(false);
    expect(guard.classify({ key: "a", nativeEvent: {} })).toBe("other");
  });

  it("ends active composition on blur without discarding the Safari tail guard", () => {
    let now = 3_000;
    const guard = createImeEnterGuard({ now: () => now, postCompositionMs: 40 });
    guard.compositionStart();
    guard.compositionEnd();
    guard.blur();
    expect(guard.classify(key())).toBe("post-composition");
    now += 41;
    expect(guard.classify(key())).toBe("submit");
  });
});
