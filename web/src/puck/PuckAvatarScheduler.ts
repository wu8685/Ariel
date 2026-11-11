export type BlinkPhase = "open" | "closing" | "closed" | "opening";
export type MotionDirection = { x: -1 | 0 | 1; y: -1 | 0 | 1; name: string };
export type MotionLoop = { name: "horizontal-left" | "horizontal-right" | "vertical-up" | "vertical-down"; directions: ReadonlyArray<MotionDirection> };
export type AvatarPose = {
  name: string;
  motionDuration: number;
  headX: number;
  headY: number;
  headRotate: number;
  faceX: number;
  faceY: number;
  faceRotate: number;
  leftEyeScaleX: number;
  rightEyeScaleX: number;
  leftEyeScaleY: number;
  rightEyeScaleY: number;
  eyeRoundness: number;
};

export type AvatarMotionTarget = {
  setBlinkPhase(phase: BlinkPhase): void;
  setPose(pose: AvatarPose): void;
};

export type EyeCenter = { x: number; y: number };
export const horizontalEyeCenters: readonly [EyeCenter, EyeCenter] = [{ x: 0, y: 0 }, { x: 1, y: 0 }];

// CSS rotate() is clockwise in screen space. The eye closer to the gaze is the one farther along that vector after the same head and face rotation.
export function perspectiveEyeScales(input: {
  gazeX: number;
  gazeY: number;
  rotationDeg: number;
  eyes: readonly [EyeCenter, EyeCenter];
  nearScaleX: number;
  nearScaleY: number;
}) {
  const neutral = { leftEyeScaleX: 1, rightEyeScaleX: 1, leftEyeScaleY: 1, rightEyeScaleY: 1 };
  if (Math.hypot(input.gazeX, input.gazeY) < 1e-6) return neutral;
  const radians = input.rotationDeg * Math.PI / 180;
  const cos = Math.cos(radians);
  const sin = Math.sin(radians);
  const dx = input.eyes[1].x - input.eyes[0].x;
  const dy = input.eyes[1].y - input.eyes[0].y;
  const score = (dx * cos - dy * sin) * input.gazeX + (dx * sin + dy * cos) * input.gazeY;
  if (Math.abs(score) < 1e-4) return neutral;
  const rightNearer = score > 0;
  return {
    leftEyeScaleX: rightNearer ? 1 : input.nearScaleX,
    rightEyeScaleX: rightNearer ? input.nearScaleX : 1,
    leftEyeScaleY: rightNearer ? 1 : input.nearScaleY,
    rightEyeScaleY: rightNearer ? input.nearScaleY : 1,
  };
}

type Entry = {
  id: number;
  target: AvatarMotionTarget;
  random: () => number;
  visible: boolean;
  reducedMotion: boolean;
  blinkPhase: BlinkPhase;
  blinkAt: number;
  poseAt: number;
  posePhase: "rest" | "look";
  motionQueue: MotionDirection[];
  eyes: readonly [EyeCenter, EyeCenter];
  restPose: AvatarPose;
};

export const motionDirections: ReadonlyArray<MotionDirection> = [
  { x: 0, y: 0, name: "rest" },
  { x: -1, y: 0, name: "left" }, { x: 1, y: 0, name: "right" },
  { x: 0, y: -1, name: "up" }, { x: 0, y: 1, name: "down" },
  { x: -1, y: -1, name: "up-left" }, { x: 1, y: -1, name: "up-right" },
  { x: -1, y: 1, name: "down-left" }, { x: 1, y: 1, name: "down-right" },
];

const direction = (name: string) => motionDirections.find(item => item.name === name)!;
export const motionLoops: ReadonlyArray<MotionLoop> = [
  { name: "horizontal-left", directions: ["left", "up-left", "up", "up-right", "right", "down-right", "down", "down-left"].map(direction) },
  { name: "horizontal-right", directions: ["right", "up-right", "up", "up-left", "left", "down-left", "down", "down-right"].map(direction) },
  { name: "vertical-up", directions: ["up", "up-right", "right", "down-right", "down", "down-left", "left", "up-left"].map(direction) },
  { name: "vertical-down", directions: ["down", "down-right", "right", "up-right", "up", "up-left", "left", "down-left"].map(direction) },
];

const entries = new Map<number, Entry>();
let nextID = 1;
let timer: ReturnType<typeof setTimeout> | undefined;
let listening = false;

function seed(value: string): number {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0 || 1;
}

function randomFor(value: string): () => number {
  let state = seed(value);
  return () => {
    state ^= state << 13; state ^= state >>> 17; state ^= state << 5;
    return (state >>> 0) / 4294967296;
  };
}

export function restPoseForKey(key: string, eyes: readonly [EyeCenter, EyeCenter] = horizontalEyeCenters): AvatarPose {
  const value = seed(key);
  const side: -1 | 1 = value & 1 ? 1 : -1;
  const vertical: -1 | 0 = value & 2 ? -1 : 0;
  const faceRotate = side * (20 + ((value >>> 4) % 11));
  return {
    name: "rest",
    motionDuration: 820,
    headX: 0, headY: 0, headRotate: 0,
    faceX: side * (3 + ((value >>> 8) % 3)), faceY: vertical * (2 + ((value >>> 11) % 3)), faceRotate,
    ...perspectiveEyeScales({ gazeX: side, gazeY: vertical, rotationDeg: faceRotate, eyes, nearScaleX: .72, nearScaleY: .86 }),
    eyeRoundness: 1,
  };
}

export function poseForDirection(direction: MotionDirection, rest: AvatarPose, eyes: readonly [EyeCenter, EyeCenter] = horizontalEyeCenters): AvatarPose {
  if (direction.name === "rest") return rest;
  const headRotate = direction.x !== 0 ? direction.x * 24 : direction.y * 20;
  const faceRotate = direction.x === 0 ? rest.faceRotate : direction.x * 28 + direction.y * 4;
  return {
    name: direction.name,
    motionDuration: 720,
    headX: direction.x * 7.5, headY: direction.y * 6.8, headRotate,
    faceX: direction.x * 11.5, faceY: direction.y * 10, faceRotate,
    ...perspectiveEyeScales({ gazeX: direction.x, gazeY: direction.y, rotationDeg: headRotate + faceRotate, eyes, nearScaleX: .55, nearScaleY: .78 }),
    eyeRoundness: 1.7,
  };
}

function between(entry: Entry, minimum: number, maximum: number): number {
  return minimum + Math.floor(entry.random() * (maximum - minimum + 1));
}

function active(entry: Entry): boolean {
  return entry.visible && !(typeof document !== "undefined" && document.hidden);
}

function initialize(entry: Entry, now = Date.now()) {
  entry.blinkPhase = "open";
  entry.posePhase = "rest";
  entry.motionQueue = [];
  entry.target.setPose(entry.restPose);
  entry.blinkAt = now + between(entry, entry.reducedMotion ? 8_000 : 2_000, entry.reducedMotion ? 16_000 : 7_000);
  entry.poseAt = entry.reducedMotion ? Number.POSITIVE_INFINITY : now + between(entry, 2_000, 8_000);
}

function clearWake() {
  if (timer !== undefined) clearTimeout(timer);
  timer = undefined;
}

function scheduleWake() {
  clearWake();
  if (typeof document !== "undefined" && document.hidden) return;
  let earliest = Number.POSITIVE_INFINITY;
  for (const entry of entries.values()) {
    if (!active(entry)) continue;
    earliest = Math.min(earliest, entry.blinkAt, entry.poseAt);
  }
  if (!Number.isFinite(earliest)) return;
  timer = setTimeout(runDueEvents, Math.max(0, earliest - Date.now()));
}

function advanceBlink(entry: Entry, now: number) {
  switch (entry.blinkPhase) {
    case "open":
      entry.blinkPhase = "closing";
      entry.target.setBlinkPhase("closing");
      entry.blinkAt = now + 100;
      return;
    case "closing":
      entry.blinkPhase = "closed";
      entry.target.setBlinkPhase("closed");
      entry.blinkAt = now + 40;
      return;
    case "closed":
      entry.blinkPhase = "opening";
      entry.target.setBlinkPhase("opening");
      entry.blinkAt = now + 110;
      return;
    case "opening": {
      entry.blinkPhase = "open";
      entry.target.setBlinkPhase("open");
      const doubleBlink = !entry.reducedMotion && entry.random() < 0.2;
      entry.blinkAt = now + (doubleBlink ? 120 : between(entry, entry.reducedMotion ? 8_000 : 3_000, entry.reducedMotion ? 16_000 : 11_000));
    }
  }
}

function advancePose(entry: Entry, now: number) {
  if (entry.motionQueue.length > 0) {
    const pose = poseForDirection(entry.motionQueue.shift()!, entry.restPose, entry.eyes);
    entry.target.setPose(pose);
    entry.posePhase = "look";
    entry.poseAt = now + pose.motionDuration;
    return;
  }
  if (entry.posePhase === "rest") {
    if (entry.random() < .38) {
      const loop = motionLoops[Math.floor(entry.random() * motionLoops.length)] || motionLoops[0];
      entry.motionQueue = [...loop.directions];
      advancePose(entry, now);
      return;
    }
    const direction = motionDirections[1 + Math.floor(entry.random() * (motionDirections.length - 1))] || motionDirections[1];
    const pose = poseForDirection(direction, entry.restPose, entry.eyes);
    entry.target.setPose(pose);
    entry.posePhase = "look";
    entry.poseAt = now + pose.motionDuration + between(entry, 500, 2_000);
    return;
  }
  if (entry.random() < .35) {
    const direction = motionDirections[1 + Math.floor(entry.random() * (motionDirections.length - 1))] || motionDirections[1];
    const pose = poseForDirection(direction, entry.restPose, entry.eyes);
    entry.target.setPose(pose);
    entry.poseAt = now + pose.motionDuration + between(entry, 500, 2_000);
    return;
  }
  entry.target.setPose(entry.restPose);
  entry.posePhase = "rest";
  entry.poseAt = now + entry.restPose.motionDuration + between(entry, 3_000, 9_000);
}

function runDueEvents() {
  timer = undefined;
  const now = Date.now();
  for (const entry of entries.values()) {
    if (!active(entry)) continue;
    if (entry.blinkAt <= now) advanceBlink(entry, now);
    if (!entry.reducedMotion && entry.poseAt <= now) advancePose(entry, now);
  }
  scheduleWake();
}

function visibilityChanged() {
  clearWake();
  if (document.hidden) return;
  const now = Date.now();
  for (const entry of entries.values()) if (entry.visible) initialize(entry, now);
  scheduleWake();
}

function ensureVisibilityListener() {
  if (listening || typeof document === "undefined") return;
  document.addEventListener("visibilitychange", visibilityChanged);
  listening = true;
}

function removeVisibilityListenerIfIdle() {
  if (!listening || entries.size !== 0 || typeof document === "undefined") return;
  document.removeEventListener("visibilitychange", visibilityChanged);
  listening = false;
}

export function registerAvatarMotion(key: string, target: AvatarMotionTarget, options: { visible?: boolean; reducedMotion?: boolean; eyes?: readonly [EyeCenter, EyeCenter] } = {}) {
  const id = nextID++;
  const eyes = options.eyes ?? horizontalEyeCenters;
  const entry: Entry = {
    id, target, random: randomFor(`${key}:${id}`),
    visible: options.visible ?? true, reducedMotion: options.reducedMotion ?? false,
    blinkPhase: "open", blinkAt: Number.POSITIVE_INFINITY, poseAt: Number.POSITIVE_INFINITY, posePhase: "rest", motionQueue: [],
    eyes, restPose: restPoseForKey(key, eyes),
  };
  initialize(entry);
  entries.set(entry.id, entry);
  ensureVisibilityListener();
  scheduleWake();
  return {
    setVisible(visible: boolean) {
      if (entry.visible === visible) return;
      entry.visible = visible;
      if (visible) initialize(entry);
      scheduleWake();
    },
    setReducedMotion(reducedMotion: boolean) {
      if (entry.reducedMotion === reducedMotion) return;
      entry.reducedMotion = reducedMotion;
      initialize(entry);
      scheduleWake();
    },
    unregister() {
      entries.delete(entry.id);
      scheduleWake();
      removeVisibilityListenerIfIdle();
    },
  };
}

export function getAvatarMotionSchedulerDebug() {
  return { entries: entries.size, pendingTimers: timer === undefined ? 0 : 1, paused: typeof document !== "undefined" && document.hidden };
}

export function resetAvatarMotionSchedulerForTests() {
  clearWake();
  entries.clear();
  nextID = 1;
  if (listening && typeof document !== "undefined") document.removeEventListener("visibilitychange", visibilityChanged);
  listening = false;
}
