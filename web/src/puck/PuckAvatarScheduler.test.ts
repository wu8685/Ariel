// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getAvatarMotionSchedulerDebug,
  registerAvatarMotion,
  resetAvatarMotionSchedulerForTests,
  type AvatarMotionTarget,
} from "./PuckAvatarScheduler";

afterEach(() => {
  resetAvatarMotionSchedulerForTests();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function target(): AvatarMotionTarget {
  return { setBlinkPhase: vi.fn(), setGaze: vi.fn() };
}

describe("Puck avatar global motion scheduler", () => {
  it("shares exactly one timeout across independent avatars without a RAF loop", async () => {
    vi.useFakeTimers();
    const raf = vi.spyOn(window, "requestAnimationFrame");
    const first = target();
    const second = target();
    const a = registerAvatarMotion("assistant-a:one", first);
    const b = registerAvatarMotion("assistant-b:two", second);

    expect(getAvatarMotionSchedulerDebug()).toMatchObject({ entries: 2, pendingTimers: 1, paused: false });
    expect(vi.getTimerCount()).toBe(1);
    await vi.advanceTimersByTimeAsync(20_000);
    expect(first.setBlinkPhase).toHaveBeenCalled();
    expect(first.setGaze).toHaveBeenCalled();
    expect(second.setBlinkPhase).toHaveBeenCalled();
    expect(second.setGaze).toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(1);
    expect(raf).not.toHaveBeenCalled();

    a.unregister();
    b.unregister();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("clears the shared timeout while hidden and recreates one staggered chain when visible", () => {
    vi.useFakeTimers();
    let hidden = false;
    vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
    const registration = registerAvatarMotion("assistant-hidden", target());
    expect(vi.getTimerCount()).toBe(1);

    hidden = true;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(getAvatarMotionSchedulerDebug()).toMatchObject({ pendingTimers: 0, paused: true });
    expect(vi.getTimerCount()).toBe(0);

    hidden = false;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(getAvatarMotionSchedulerDebug()).toMatchObject({ pendingTimers: 1, paused: false });
    expect(vi.getTimerCount()).toBe(1);
    registration.unregister();
  });

  it("excludes offscreen avatars and keeps reduced motion to low-frequency blinking", async () => {
    vi.useFakeTimers();
    const offscreen = target();
    const reduced = target();
    const hiddenRegistration = registerAvatarMotion("assistant-offscreen", offscreen, { visible: false });
    const reducedRegistration = registerAvatarMotion("assistant-reduced", reduced, { reducedMotion: true });

    await vi.advanceTimersByTimeAsync(40_000);
    expect(offscreen.setBlinkPhase).not.toHaveBeenCalled();
    expect(offscreen.setGaze).not.toHaveBeenCalled();
    expect(reduced.setBlinkPhase).toHaveBeenCalled();
    expect(reduced.setGaze).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(1);

    hiddenRegistration.setVisible(true);
    await vi.advanceTimersByTimeAsync(20_000);
    expect(offscreen.setGaze).toHaveBeenCalled();
    hiddenRegistration.unregister();
    reducedRegistration.unregister();
  });
});
