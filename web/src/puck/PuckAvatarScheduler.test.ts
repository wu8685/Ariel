// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getAvatarMotionSchedulerDebug,
  motionDirections,
  poseForDirection,
  registerAvatarMotion,
  restPoseForKey,
  resetAvatarMotionSchedulerForTests,
  type AvatarMotionTarget,
} from "./PuckAvatarScheduler";

afterEach(() => {
  resetAvatarMotionSchedulerForTests();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function target(): AvatarMotionTarget {
  return { setBlinkPhase: vi.fn(), setPose: vi.fn() };
}

describe("Puck avatar global motion scheduler", () => {
  it("derives a stable angled rest face and eight directional head turns with perspective", () => {
    expect(restPoseForKey("indy")).toEqual(restPoseForKey("indy"));
    const rest = restPoseForKey("indy");
    expect(Math.abs(rest.faceRotate)).toBeGreaterThanOrEqual(20);
    expect(Math.abs(rest.faceRotate)).toBeLessThanOrEqual(30);
    expect(Math.min(rest.leftEyeScaleX, rest.rightEyeScaleX)).toBeLessThan(1);
    if (rest.faceX < 0) expect(rest.leftEyeScaleX).toBeLessThan(rest.rightEyeScaleX);
    if (rest.faceX > 0) expect(rest.rightEyeScaleX).toBeLessThan(rest.leftEyeScaleX);

    expect(motionDirections.filter(direction => direction.name !== "rest")).toHaveLength(8);
    for (const direction of motionDirections.filter(direction => direction.name !== "rest")) {
      const pose = poseForDirection(direction, rest);
      expect(Math.abs(pose.headRotate)).toBeGreaterThanOrEqual(10);
      expect(Math.abs(pose.headRotate)).toBeLessThanOrEqual(15);
      if (direction.x !== 0) expect(Math.abs(pose.headX)).toBeGreaterThanOrEqual(3);
      if (direction.y !== 0) expect(Math.abs(pose.headY)).toBeGreaterThanOrEqual(3);
      expect(Math.max(Math.abs(pose.headX), Math.abs(pose.headY))).toBeLessThanOrEqual(6);
      expect(Math.sign(pose.faceX)).toBe(direction.x === 0 ? 0 : Math.sign(direction.x));
      expect(Math.sign(pose.faceY)).toBe(direction.y === 0 ? 0 : Math.sign(direction.y));
      expect(pose.eyeRoundness).toBeGreaterThanOrEqual(1.5);
      expect(pose.eyeRoundness).toBeLessThanOrEqual(2);
      expect(pose.motionDuration).toBeGreaterThanOrEqual(600);
      expect(pose.motionDuration).toBeLessThanOrEqual(800);
      if (direction.x < 0) {
        expect(pose.leftEyeScaleX).toBeLessThan(pose.rightEyeScaleX);
        expect(pose.leftEyeScaleY).toBeLessThan(pose.rightEyeScaleY);
      }
      if (direction.x > 0) {
        expect(pose.rightEyeScaleX).toBeLessThan(pose.leftEyeScaleX);
        expect(pose.rightEyeScaleY).toBeLessThan(pose.leftEyeScaleY);
      }
      if (direction.x === 0) {
        expect(pose.leftEyeScaleX).toBe(pose.rightEyeScaleX);
        expect(pose.leftEyeScaleY).toBe(pose.rightEyeScaleY);
      }
    }
    expect(rest.motionDuration).toBeGreaterThanOrEqual(700);
    expect(rest.motionDuration).toBeLessThanOrEqual(900);
  });

  it("uses one smooth 220-280ms timeline for both eyelids", async () => {
    vi.useFakeTimers();
    const phases: Array<{ phase: string; at: number }> = [];
    const registration = registerAvatarMotion("assistant-blink", {
      setPose: vi.fn(),
      setBlinkPhase: phase => phases.push({ phase, at: Date.now() }),
    }, { reducedMotion: true });

    await vi.advanceTimersByTimeAsync(20_000);
    const first = ["closing", "closed", "opening", "open"].map(phase => phases.find(event => event.phase === phase)!);
    expect(first.every(Boolean)).toBe(true);
    expect(first[1].at - first[0].at).toBe(100);
    expect(first[2].at - first[1].at).toBe(40);
    expect(first[3].at - first[2].at).toBe(110);
    expect(first[3].at - first[0].at).toBe(250);
    registration.unregister();
  });

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
    expect(first.setPose).toHaveBeenCalled();
    expect(second.setBlinkPhase).toHaveBeenCalled();
    expect(second.setPose).toHaveBeenCalled();
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
    const offscreenInitialPoses = vi.mocked(offscreen.setPose).mock.calls.length;
    const reducedInitialPoses = vi.mocked(reduced.setPose).mock.calls.length;

    await vi.advanceTimersByTimeAsync(40_000);
    expect(offscreen.setBlinkPhase).not.toHaveBeenCalled();
    expect(offscreen.setPose).toHaveBeenCalledTimes(offscreenInitialPoses);
    expect(reduced.setBlinkPhase).toHaveBeenCalled();
    expect(reduced.setPose).toHaveBeenCalledTimes(reducedInitialPoses);
    expect(vi.getTimerCount()).toBe(1);

    hiddenRegistration.setVisible(true);
    await vi.advanceTimersByTimeAsync(20_000);
    expect(vi.mocked(offscreen.setPose).mock.calls.length).toBeGreaterThan(offscreenInitialPoses);
    hiddenRegistration.unregister();
    reducedRegistration.unregister();
  });
});
