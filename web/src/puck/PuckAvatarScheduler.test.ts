// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { avatarShapes, eyeCentersForShape } from "./PuckAvatar";
import {
  getAvatarMotionSchedulerDebug,
  horizontalEyeCenters,
  motionDirections,
  motionLoops,
  poseForDirection,
  registerAvatarMotion,
  restPoseForKey,
  resetAvatarMotionSchedulerForTests,
  type AvatarMotionTarget,
  type AvatarPose,
  type EyeCenter,
} from "./PuckAvatarScheduler";

afterEach(() => {
  resetAvatarMotionSchedulerForTests();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function target(): AvatarMotionTarget {
  return { setBlinkPhase: vi.fn(), setPose: vi.fn() };
}

function gazeCloserEyeIsRight(eyes: readonly [EyeCenter, EyeCenter], gazeX: number, gazeY: number, rotationDeg: number) {
  const radians = rotationDeg * Math.PI / 180;
  const cos = Math.cos(radians);
  const sin = Math.sin(radians);
  const dx = eyes[1].x - eyes[0].x;
  const dy = eyes[1].y - eyes[0].y;
  return (dx * cos - dy * sin) * gazeX + (dx * sin + dy * cos) * gazeY > 0;
}

function expectNearerEyeNarrower(pose: AvatarPose, eyes: readonly [EyeCenter, EyeCenter], gazeX: number, gazeY: number, nearScaleX: number, nearScaleY: number) {
  const rightNearer = gazeCloserEyeIsRight(eyes, gazeX, gazeY, pose.headRotate + pose.faceRotate);
  expect(pose.leftEyeScaleX).toBe(rightNearer ? 1 : nearScaleX);
  expect(pose.rightEyeScaleX).toBe(rightNearer ? nearScaleX : 1);
  expect(pose.leftEyeScaleY).toBe(rightNearer ? 1 : nearScaleY);
  expect(pose.rightEyeScaleY).toBe(rightNearer ? nearScaleY : 1);
  expect(Math.min(pose.leftEyeScaleX, pose.rightEyeScaleX)).toBeLessThan(Math.max(pose.leftEyeScaleX, pose.rightEyeScaleX));
}

describe("Puck avatar global motion scheduler", () => {
  it("derives a stable angled rest face and eight larger directional head turns with near-eye perspective", () => {
    expect(restPoseForKey("indy")).toEqual(restPoseForKey("indy"));
    const rest = restPoseForKey("indy");
    expect(Math.abs(rest.faceRotate)).toBeGreaterThanOrEqual(20);
    expect(Math.abs(rest.faceRotate)).toBeLessThanOrEqual(30);
    expect(Math.min(rest.leftEyeScaleX, rest.rightEyeScaleX)).toBeLessThan(1);
    expectNearerEyeNarrower(rest, horizontalEyeCenters, Math.sign(rest.faceX), Math.sign(rest.faceY), .72, .86);

    expect(motionDirections.filter(direction => direction.name !== "rest")).toHaveLength(8);
    for (const direction of motionDirections.filter(direction => direction.name !== "rest")) {
      const pose = poseForDirection(direction, rest);
      expect(Math.abs(pose.headRotate)).toBeGreaterThanOrEqual(18);
      expect(Math.abs(pose.headRotate)).toBeLessThanOrEqual(26);
      if (direction.x !== 0) expect(Math.abs(pose.headX)).toBeGreaterThanOrEqual(6);
      if (direction.y !== 0) expect(Math.abs(pose.headY)).toBeGreaterThanOrEqual(6);
      expect(Math.max(Math.abs(pose.headX), Math.abs(pose.headY))).toBeLessThanOrEqual(9);
      if (direction.x !== 0) expect(Math.abs(pose.faceX)).toBeGreaterThanOrEqual(10);
      if (direction.y !== 0) expect(Math.abs(pose.faceY)).toBeGreaterThanOrEqual(9);
      expect(Math.sign(pose.faceX)).toBe(direction.x === 0 ? 0 : Math.sign(direction.x));
      expect(Math.sign(pose.faceY)).toBe(direction.y === 0 ? 0 : Math.sign(direction.y));
      expect(pose.eyeRoundness).toBeGreaterThanOrEqual(1.5);
      expect(pose.eyeRoundness).toBeLessThanOrEqual(2);
      expect(pose.motionDuration).toBeGreaterThanOrEqual(600);
      expect(pose.motionDuration).toBeLessThanOrEqual(800);
      expectNearerEyeNarrower(pose, horizontalEyeCenters, direction.x, direction.y, .55, .78);
    }
    expect(rest.motionDuration).toBeGreaterThanOrEqual(700);
    expect(rest.motionDuration).toBeLessThanOrEqual(900);
  });

  it("narrows the eye closer to the gaze after head and face rotation for every shape", () => {
    const tilted = { ...restPoseForKey("indy"), faceRotate: -24 };
    const flower = eyeCentersForShape("flower");
    const upLeft = poseForDirection(motionDirections.find(direction => direction.name === "up-left")!, tilted, flower);
    expect(upLeft.headRotate + upLeft.faceRotate).toBe(-56);
    expect(upLeft.rightEyeScaleX).toBe(.55);
    expect(upLeft.leftEyeScaleX).toBe(1);
    expect(upLeft.rightEyeScaleY).toBe(.78);
    expect(upLeft.leftEyeScaleY).toBe(1);
    const up = poseForDirection(motionDirections.find(direction => direction.name === "up")!, tilted, flower);
    expect(up.rightEyeScaleX).toBe(.55);
    expect(up.leftEyeScaleX).toBe(1);
    const down = poseForDirection(motionDirections.find(direction => direction.name === "down")!, tilted, flower);
    expect(down.leftEyeScaleX).toBe(.55);
    expect(down.rightEyeScaleX).toBe(1);
    const left = poseForDirection(motionDirections.find(direction => direction.name === "left")!, tilted, flower);
    expect(left.leftEyeScaleX).toBe(.55);
    expect(left.rightEyeScaleX).toBe(1);

    for (const shape of [...avatarShapes.map(option => option.value), "pebble"]) {
      const eyes = eyeCentersForShape(shape);
      for (const faceRotate of [-24, 24]) {
        const rest = { ...tilted, faceRotate };
        for (const direction of motionDirections.filter(item => item.name !== "rest")) {
          expectNearerEyeNarrower(poseForDirection(direction, rest, eyes), eyes, direction.x, direction.y, .55, .78);
        }
      }
    }

    const resting = restPoseForKey("bot-12", flower);
    expect(resting.faceX).toBeLessThan(0);
    expect(resting.faceY).toBeLessThan(0);
    expect(resting.rightEyeScaleX).toBe(.72);
    expect(resting.leftEyeScaleX).toBe(1);
    expectNearerEyeNarrower(resting, flower, Math.sign(resting.faceX), Math.sign(resting.faceY), .72, .86);
  });

  it("defines horizontal and vertical orbit loops that sweep every side without returning to center", () => {
    expect(motionLoops.map(loop => loop.name)).toEqual(expect.arrayContaining(["horizontal-left", "horizontal-right", "vertical-up", "vertical-down"]));
    for (const loop of motionLoops) {
      expect(loop.directions.length).toBeGreaterThanOrEqual(6);
      expect(loop.directions.every(direction => direction.name !== "rest")).toBe(true);
      expect(new Set(loop.directions.map(direction => Math.sign(direction.x)))).toEqual(new Set([-1, 0, 1]));
      expect(new Set(loop.directions.map(direction => Math.sign(direction.y)))).toEqual(new Set([-1, 0, 1]));
      expect(loop.directions.some(direction => direction.x !== 0 && direction.y !== 0)).toBe(true);
    }
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
