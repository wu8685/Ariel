export type BlinkPhase = "open" | "closing" | "closed" | "opening";
export type GazeDirection = { x: -1 | 0 | 1; y: -1 | 0 | 1; name: string };

export type AvatarMotionTarget = {
  setBlinkPhase(phase: BlinkPhase): void;
  setGaze(direction: GazeDirection): void;
};

type Entry = {
  id: number;
  target: AvatarMotionTarget;
  random: () => number;
  visible: boolean;
  reducedMotion: boolean;
  blinkPhase: BlinkPhase;
  blinkAt: number;
  gazeAt: number;
};

const directions: GazeDirection[] = [
  { x: 0, y: 0, name: "center" },
  { x: -1, y: 0, name: "left" }, { x: 1, y: 0, name: "right" },
  { x: 0, y: -1, name: "up" }, { x: 0, y: 1, name: "down" },
  { x: -1, y: -1, name: "up-left" }, { x: 1, y: -1, name: "up-right" },
  { x: -1, y: 1, name: "down-left" }, { x: 1, y: 1, name: "down-right" },
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

function between(entry: Entry, minimum: number, maximum: number): number {
  return minimum + Math.floor(entry.random() * (maximum - minimum + 1));
}

function active(entry: Entry): boolean {
  return entry.visible && !(typeof document !== "undefined" && document.hidden);
}

function initialize(entry: Entry, now = Date.now()) {
  entry.blinkPhase = "open";
  entry.blinkAt = now + between(entry, entry.reducedMotion ? 8_000 : 2_000, entry.reducedMotion ? 16_000 : 7_000);
  entry.gazeAt = entry.reducedMotion ? Number.POSITIVE_INFINITY : now + between(entry, 700, 4_500);
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
    earliest = Math.min(earliest, entry.blinkAt, entry.gazeAt);
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
      entry.blinkAt = now + 50;
      return;
    case "closed":
      entry.blinkPhase = "opening";
      entry.target.setBlinkPhase("opening");
      entry.blinkAt = now + 100;
      return;
    case "opening": {
      entry.blinkPhase = "open";
      entry.target.setBlinkPhase("open");
      const doubleBlink = !entry.reducedMotion && entry.random() < 0.2;
      entry.blinkAt = now + (doubleBlink ? 120 : between(entry, entry.reducedMotion ? 8_000 : 3_000, entry.reducedMotion ? 16_000 : 11_000));
    }
  }
}

function advanceGaze(entry: Entry, now: number) {
  const direction = directions[Math.floor(entry.random() * directions.length)] || directions[0];
  entry.target.setGaze(direction);
  entry.gazeAt = now + between(entry, direction.name === "center" ? 3_000 : 1_000, direction.name === "center" ? 11_000 : 3_000);
}

function runDueEvents() {
  timer = undefined;
  const now = Date.now();
  for (const entry of entries.values()) {
    if (!active(entry)) continue;
    if (entry.blinkAt <= now) advanceBlink(entry, now);
    if (!entry.reducedMotion && entry.gazeAt <= now) advanceGaze(entry, now);
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

export function registerAvatarMotion(key: string, target: AvatarMotionTarget, options: { visible?: boolean; reducedMotion?: boolean } = {}) {
  const entry: Entry = {
    id: nextID++, target, random: randomFor(`${key}:${nextID}`),
    visible: options.visible ?? true, reducedMotion: options.reducedMotion ?? false,
    blinkPhase: "open", blinkAt: Number.POSITIVE_INFINITY, gazeAt: Number.POSITIVE_INFINITY,
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
