import { useEffect, useId, useMemo, useRef, useState, type CSSProperties, type PointerEvent, type ReactNode } from "react";

export type AvatarShape = "circle" | "cloud" | "tilted_square" | "four_star" | "clover" | "heart" | "flower" | "drop" | "capsule" | "triangle" | "tilted_rect" | "burst" | "hexagon" | "pumpkin";
export type PuckState = "idle" | "thinking" | "working" | "waiting_user" | "awaiting_authorization" | "attention" | "error" | "done" | string;
export type AvatarDesign = { shape: AvatarShape; color: string };

export const avatarShapes: ReadonlyArray<{ value: AvatarShape; label: string }> = [
  { value: "circle", label: "圆" }, { value: "cloud", label: "云团" }, { value: "tilted_square", label: "倾斜圆角方块" },
  { value: "four_star", label: "四角星" }, { value: "clover", label: "四叶花" }, { value: "heart", label: "心形团" },
  { value: "flower", label: "波浪花" }, { value: "drop", label: "梨形水滴" }, { value: "capsule", label: "胶囊" },
  { value: "triangle", label: "圆角三角" }, { value: "tilted_rect", label: "倾斜圆角矩形" }, { value: "burst", label: "尖刺星" },
  { value: "hexagon", label: "圆角六边形" }, { value: "pumpkin", label: "南瓜" },
];

export const avatarColors: ReadonlyArray<{ value: string; label: string }> = [
  { value: "#ffffff", label: "白" }, { value: "#b87532", label: "棕" }, { value: "#ff3045", label: "红" },
  { value: "#ff6b0a", label: "橙" }, { value: "#ff9f0a", label: "琥珀" }, { value: "#16c96a", label: "绿" },
  { value: "#1db9aa", label: "青绿" }, { value: "#2588f7", label: "蓝" }, { value: "#8957f5", label: "紫" },
  { value: "#ff2f92", label: "粉" }, { value: "#858585", label: "灰" },
];

function hash(value: string): number {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0;
}

export function deriveAvatar(assistantId: string): AvatarDesign {
  const value = hash(assistantId || "puck");
  return { shape: avatarShapes[value % avatarShapes.length].value, color: avatarColors[(value >>> 8) % avatarColors.length].value };
}

export function expressionForState(state: PuckState): "calm" | "searching" | "waiting" | "worried" | "happy" {
  if (state === "thinking" || state === "working") return "searching";
  if (state === "waiting_user" || state === "awaiting_authorization") return "waiting";
  if (state === "attention" || state === "error") return "worried";
  if (state === "done") return "happy";
  return "calm";
}

export function stateLabel(state: PuckState): string {
  if (state === "thinking" || state === "working") return "正在工作";
  if (state === "waiting_user") return "等待你的回复";
  if (state === "awaiting_authorization") return "等待你的授权";
  if (state === "attention" || state === "error") return "需要关注";
  if (state === "done") return "已完成";
  return "空闲";
}

function shapePath(shape: AvatarShape): ReactNode {
  switch (shape) {
    case "circle": return <circle cx="50" cy="55" r="34" />;
    case "cloud": return <path d="M18 75C5 67 10 48 25 44c-3-16 17-25 29-14 12-13 33-2 30 15 16 7 11 29-6 32-8 13-29 13-38 2-8 5-16 3-22-4Z" />;
    case "tilted_square": return <rect x="20" y="22" width="60" height="62" rx="15" transform="rotate(-13 50 53)" />;
    case "four_star": return <path d="M50 10c6 21 11 28 31 34-20 7-26 14-31 36-6-22-12-29-32-36 20-6 26-13 32-34Z" />;
    case "clover": return <path d="M50 49C37 49 24 41 25 30c1-12 16-15 25-5 9-10 24-7 25 5 1 11-12 19-25 19Zm0 0c13 0 25 8 24 20-1 11-15 14-24 4-9 10-23 7-24-4-1-12 11-20 24-20Z" />;
    case "heart": return <path d="M50 84C42 74 17 62 16 41 15 21 39 17 50 32c11-15 35-11 34 9-1 21-26 33-34 43Z" />;
    case "flower": return <path d="M50 18c9-8 20 0 18 12 13-5 22 7 14 18 13 4 13 19 1 24 7 11-4 22-16 16-3 13-19 14-25 2-11 8-23-1-18-14-13-3-15-19-3-25-7-11 3-23 16-20 1-12 14-18 23-10Z" />;
    case "drop": return <path d="M51 11c8 17 29 32 29 52 0 18-14 29-31 28-18-1-29-13-28-30 1-19 21-34 30-50Z" />;
    case "capsule": return <rect x="14" y="31" width="72" height="48" rx="24" transform="rotate(-7 50 55)" />;
    case "triangle": return <path d="M47 16c3-3 8-3 11 1l31 54c5 9-1 16-11 16H21c-11 0-17-8-11-17l30-51c2-2 4-3 7-3Z" />;
    case "tilted_rect": return <rect x="12" y="29" width="76" height="50" rx="15" transform="rotate(11 50 54)" />;
    case "burst": return <path d="m50 9 9 20 22-9-7 22 20 10-21 9 7 22-22-8-10 20-8-21-23 8 8-22L5 50l21-9-7-22 22 9Z" />;
    case "hexagon": return <path d="M36 14h28c5 0 8 2 11 7l14 25c3 5 3 9 0 14L75 83c-3 5-6 7-12 7H36c-5 0-9-2-12-7L10 59c-3-5-3-9 0-14l14-24c3-5 7-7 12-7Z" />;
    case "pumpkin": return <><path d="M50 28c8-7 20-5 25 2 10-1 17 10 16 24-1 21-14 34-41 34S10 75 9 54c-1-14 6-25 16-24 5-7 17-9 25-2Z" /><path className="puck-avatar-stem" d="M47 27c0-8 4-13 11-16 1 7-1 13-6 18Z" /></>;
  }
}

function motionDelays(assistantId: string, instanceId: string) {
  const phase = hash(`${assistantId}:${instanceId}`);
  return { phase, blink: 2000 + phase % 4001, tilt: 6000 + (phase >>> 5) % 9001, look: 3500 + (phase >>> 11) % 5501 };
}

export function PuckAvatar({ assistantId, name, state = "idle", design, size = "medium", attention = false, animated = true }: { assistantId: string; name: string; state?: PuckState; design?: AvatarDesign; size?: "small" | "medium" | "large"; attention?: boolean; animated?: boolean }) {
  const instanceId = useId();
  const delays = useMemo(() => motionDelays(assistantId, instanceId), [assistantId, instanceId]);
  const root = useRef<HTMLDivElement>(null);
  const hoverTimer = useRef<number | undefined>(undefined);
  const reactionTimer = useRef<number | undefined>(undefined);
  const [reacting, setReacting] = useState(false);
  const [blinking, setBlinking] = useState(false);
  const [blinkCount, setBlinkCount] = useState(0);
  const [tilting, setTilting] = useState(false);
  const [tiltDirection, setTiltDirection] = useState(1);
  const [looking, setLooking] = useState<-1 | 0 | 1>(0);
  const [hidden, setHidden] = useState(() => typeof document !== "undefined" && document.hidden);
  const [reducedMotion, setReducedMotion] = useState(() => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
  const avatar = design || deriveAvatar(assistantId);
  const expression = expressionForState(state);

  useEffect(() => {
    const update = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);

  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReducedMotion(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  useEffect(() => {
    if (hidden || !animated) return;
    const timers = new Set<number>();
    let cancelled = false;
    const later = (callback: () => void, delay: number) => { const timer = window.setTimeout(() => { timers.delete(timer); if (!cancelled) callback(); }, delay); timers.add(timer); };
    const schedule = (delay = 2000 + Math.random() * 4000) => later(() => {
      setBlinking(true); setBlinkCount(count => count + 1);
      later(() => {
        setBlinking(false);
        if (Math.random() < 0.22) later(() => { setBlinking(true); setBlinkCount(count => count + 1); later(() => { setBlinking(false); schedule(); }, 110); }, 110);
        else schedule();
      }, 110);
    }, delay);
    schedule(delays.blink);
    return () => { cancelled = true; timers.forEach(timer => window.clearTimeout(timer)); };
  }, [animated, delays.blink, hidden]);

  useEffect(() => {
    if (hidden || reducedMotion || !animated) { setTilting(false); return; }
    const timers = new Set<number>(); let cancelled = false;
    const later = (callback: () => void, delay: number) => { const timer = window.setTimeout(() => { timers.delete(timer); if (!cancelled) callback(); }, delay); timers.add(timer); };
    const schedule = (delay = 6000 + Math.random() * 9000) => later(() => { setTiltDirection(Math.random() < 0.5 ? -1 : 1); setTilting(true); later(() => { setTilting(false); schedule(); }, 720); }, delay);
    schedule(delays.tilt);
    return () => { cancelled = true; timers.forEach(timer => window.clearTimeout(timer)); };
  }, [animated, delays.tilt, hidden, reducedMotion]);

  useEffect(() => {
    if (hidden || reducedMotion || !animated) { setLooking(0); return; }
    const timers = new Set<number>(); let cancelled = false;
    const later = (callback: () => void, delay: number) => { const timer = window.setTimeout(() => { timers.delete(timer); if (!cancelled) callback(); }, delay); timers.add(timer); };
    const schedule = (delay = 3500 + Math.random() * 5500) => later(() => { setLooking(Math.random() < 0.5 ? -1 : 1); later(() => { setLooking(0); schedule(); }, 620); }, delay);
    schedule(delays.look);
    return () => { cancelled = true; timers.forEach(timer => window.clearTimeout(timer)); };
  }, [animated, delays.look, hidden, reducedMotion]);

  useEffect(() => () => { if (hoverTimer.current) window.clearTimeout(hoverTimer.current); if (reactionTimer.current) window.clearTimeout(reactionTimer.current); }, []);
  useEffect(() => { if (!reducedMotion) return; setReacting(false); if (reactionTimer.current) window.clearTimeout(reactionTimer.current); reactionTimer.current = undefined; }, [reducedMotion]);

  function follow(event: PointerEvent<HTMLDivElement>) {
    if (reducedMotion || event.pointerType === "touch" || !root.current) return;
    const box = root.current.getBoundingClientRect();
    const x = Math.max(-1, Math.min(1, (event.clientX - box.left) / box.width * 2 - 1));
    const y = Math.max(-1, Math.min(1, (event.clientY - box.top) / box.height * 2 - 1));
    root.current.style.setProperty("--puck-eye-x", `${x * 2.2}px`); root.current.style.setProperty("--puck-eye-y", `${y * 1.7}px`);
  }

  function react() {
    if (reducedMotion) return;
    if (reactionTimer.current) window.clearTimeout(reactionTimer.current);
    setReacting(true);
    reactionTimer.current = window.setTimeout(() => { setReacting(false); reactionTimer.current = undefined; }, 420);
  }

  const style = { "--puck-avatar-color": avatar.color, "--puck-auto-eye-x": `${looking * 1.9}px`, "--puck-tilt-angle": `${tiltDirection * 4}deg`, "--puck-tilt-x": `${tiltDirection * 1.3}px` } as CSSProperties;

  return <div ref={root} className={`puck-avatar puck-avatar-${size}`} role="img" aria-label={`${name}，${stateLabel(state)}`} draggable={false}
    data-expression={expression} data-shape={avatar.shape} data-reacting={reacting ? "true" : "false"} data-blinking={blinking ? "true" : "false"} data-blink-count={blinkCount}
    data-tilting={tilting ? "true" : "false"} data-looking={looking} data-paused={hidden ? "true" : "false"} data-reduced-motion={reducedMotion ? "true" : "false"}
    data-motion-phase={delays.phase} data-blink-delay={delays.blink} data-tilt-delay={delays.tilt} data-look-delay={delays.look}
    style={style} onPointerEnter={() => { if (!hoverTimer.current) hoverTimer.current = window.setTimeout(() => { hoverTimer.current = undefined; react(); }, 80 + Math.random() * 180); }}
    onPointerMove={follow} onPointerLeave={() => { if (hoverTimer.current) window.clearTimeout(hoverTimer.current); hoverTimer.current = undefined; root.current?.style.removeProperty("--puck-eye-x"); root.current?.style.removeProperty("--puck-eye-y"); }}
    onPointerDown={react} onDragStart={event => event.preventDefault()}>
    <span className="puck-avatar-motion"><svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
      <g className="puck-avatar-ears"><path className="puck-avatar-ear" d="M39 37C36 29 36 20 40 13c3 8 5 16 5 24Z"/><path className="puck-avatar-ear" d="M61 37c3-8 3-17-1-24-3 8-5 16-5 24Z"/></g>
      <g className="puck-avatar-body">{shapePath(avatar.shape)}</g>
      <g className="puck-avatar-eyes"><ellipse className="puck-avatar-eye" cx="40" cy="55" rx="4.6" ry="8"/><ellipse className="puck-avatar-eye" cx="61" cy="55" rx="4.6" ry="8"/></g>
    </svg></span>
    {reacting && <span className="puck-avatar-sparkles" aria-hidden="true">{Array.from({ length: 5 }, (_, index) => <i className="puck-avatar-sparkle" key={index}/>)}</span>}
    {attention && <span className="puck-avatar-badge" aria-hidden="true" />}
  </div>;
}
