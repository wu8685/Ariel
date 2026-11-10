import { useEffect, useId, useMemo, useRef, type CSSProperties, type PointerEvent, type ReactNode } from "react";
import { registerAvatarMotion, restPoseForKey, type AvatarPose, type BlinkPhase } from "./PuckAvatarScheduler";

export type AvatarShape = "blob" | "pebble" | "bean" | "egg" | "squircle" | "tablet" | "capsule" | "cylinder" | "hex" | "gem" | "crystal" | "wedge" | "shield" | "dome" | "arch" | "cloud" | "teardrop" | "leaf" | "square" | "sparkle" | "clover" | "flower" | "heart" | "house" | "star";
export type AvatarColor = "black" | "brown" | "red" | "orange" | "yellow" | "green" | "cyan" | "blue" | "violet" | "magenta" | "gray";
export type PuckState = "idle" | "thinking" | "working" | "waiting_user" | "awaiting_authorization" | "attention" | "error" | "done" | string;
export type AvatarDesign = { shape: AvatarShape; color: AvatarColor; colorHex: string; image?: string };

export const avatarShapes: ReadonlyArray<{ value: AvatarShape; label: string }> = [
  { value: "blob", label: "团块" }, { value: "pebble", label: "卵石" }, { value: "bean", label: "豆形" },
  { value: "egg", label: "蛋形" }, { value: "squircle", label: "超椭圆" }, { value: "tablet", label: "平板" },
  { value: "capsule", label: "胶囊" }, { value: "cylinder", label: "圆柱" }, { value: "hex", label: "六边形" },
  { value: "gem", label: "宝石" }, { value: "crystal", label: "晶体" }, { value: "wedge", label: "楔形" },
  { value: "shield", label: "盾形" }, { value: "dome", label: "圆顶" }, { value: "arch", label: "拱形" },
  { value: "cloud", label: "云" }, { value: "teardrop", label: "水滴" }, { value: "leaf", label: "叶" },
  { value: "square", label: "方形" }, { value: "sparkle", label: "闪光" }, { value: "clover", label: "四叶" },
  { value: "flower", label: "花" }, { value: "heart", label: "心" }, { value: "house", label: "房子" },
  { value: "star", label: "星" },
];

// Hex values are copied from an unofficial extraction of the Grok Bot 0.78.1
// client table `nn` (bundle index.eager-common-DbK3Oi_r.js, sha1 0bdc0ca0ebd4).
// They are not a live measurement of the product.
export const avatarColors: ReadonlyArray<{ value: AvatarColor; label: string; hex: string }> = [
  { value: "black", label: "黑", hex: "#000000" }, { value: "brown", label: "棕", hex: "#936439" },
  { value: "red", label: "红", hex: "#ff263c" }, { value: "orange", label: "橙", hex: "#ff6700" },
  { value: "yellow", label: "黄", hex: "#ff9800" }, { value: "green", label: "绿", hex: "#00c972" },
  { value: "cyan", label: "青", hex: "#00bca6" }, { value: "blue", label: "蓝", hex: "#1084fe" },
  { value: "violet", label: "紫", hex: "#9159fe" }, { value: "magenta", label: "品红", hex: "#ff309b" },
  { value: "gray", label: "灰", hex: "#777777" },
];

const legacyShapes: Record<string, AvatarShape> = {
  circle: "blob", tilted_square: "square", four_star: "sparkle", drop: "teardrop",
  triangle: "wedge", tilted_rect: "tablet", burst: "star", hexagon: "hex", pumpkin: "dome",
};

function hash(value: string): number {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0;
}

export function canonicalShape(shape: string): AvatarShape {
  if (avatarShapes.some(option => option.value === shape)) return shape as AvatarShape;
  return legacyShapes[shape] || "blob";
}

function channel(hex: string, index: number): number {
  return Number.parseInt(hex.slice(index, index + 2), 16);
}

export function nearestAvatarColor(color: string): AvatarColor {
  const named = avatarColors.find(option => option.value === color);
  if (named) return named.value;
  const hex = color.trim().toLowerCase();
  if (!/^#[0-9a-f]{6}$/.test(hex)) return "black";
  let best: AvatarColor = "black";
  let score = Number.POSITIVE_INFINITY;
  for (const option of avatarColors) {
    const distance = [1, 3, 5].reduce((sum, index) => sum + (channel(hex, index) - channel(option.hex, index)) ** 2, 0);
    if (distance < score) { score = distance; best = option.value; }
  }
  return best;
}

export function avatarColorHex(color: string): string {
  const named = avatarColors.find(option => option.value === color);
  if (named) return named.hex;
  return /^#[0-9a-fA-F]{6}$/.test(color) ? color.toLowerCase() : "#000000";
}

export function avatarImage(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const image = value.trim();
  if (image.length > 262144) return undefined;
  if (/^data:image\/(?:png|jpeg|webp);base64,[a-z0-9+/=\s]+$/i.test(image)) return image;
  if (/^https:\/\/[^\s]+$/i.test(image)) return image;
  return undefined;
}

export function deriveAvatar(assistantId: string): AvatarDesign {
  const value = hash(assistantId || "puck");
  const color = avatarColors[(value >>> 8) % avatarColors.length];
  return { shape: avatarShapes[value % avatarShapes.length].value, color: color.value, colorHex: color.hex };
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

function applyPose(element: HTMLElement, pose: AvatarPose) {
  element.dataset.pose = pose.name;
  element.style.setProperty("--puck-motion-duration", `${pose.motionDuration}ms`);
  element.style.setProperty("--puck-head-x", `${pose.headX}%`);
  element.style.setProperty("--puck-head-y", `${pose.headY}%`);
  element.style.setProperty("--puck-head-rotate", `${pose.headRotate}deg`);
  element.style.setProperty("--puck-face-x", `${pose.faceX}px`);
  element.style.setProperty("--puck-face-y", `${pose.faceY}px`);
  element.style.setProperty("--puck-face-rotate", `${pose.faceRotate}deg`);
  element.style.setProperty("--puck-left-eye-xscale", String(pose.leftEyeScaleX));
  element.style.setProperty("--puck-right-eye-xscale", String(pose.rightEyeScaleX));
  element.style.setProperty("--puck-left-eye-yscale", String(pose.leftEyeScaleY));
  element.style.setProperty("--puck-right-eye-yscale", String(pose.rightEyeScaleY));
  element.style.setProperty("--puck-eye-roundness", String(pose.eyeRoundness));
}

// Front-view silhouettes only. These paths are not the Grok Bot mesh projection.
export const avatarShapePaths: Record<AvatarShape, string> = {
  blob: "M50 16c18 0 32 14 32 32s-14 34-32 34S18 66 18 48 32 16 50 16Z",
  pebble: "M50 20c16-6 30 4 32 20 2 18-8 34-24 38-18 4-34-8-36-26C20 32 32 26 50 20Z",
  bean: "M34 22c16-8 34 0 36 16 2 18-8 24-6 36-8 12-28 14-38 2-12-14 2-22 0-36 0-8 4-14 8-18Z",
  egg: "M50 14c14 2 26 16 26 34 0 20-12 36-26 36S24 68 24 48C24 30 36 16 50 14Z",
  squircle: "M28 18h44c10 0 16 8 16 18v28c0 12-8 18-18 18H30c-12 0-18-8-18-18V36c0-10 6-18 16-18Z",
  tablet: "M18 28h64c8 0 12 6 12 14v16c0 10-6 14-14 14H20c-8 0-14-6-14-14V42c0-8 4-14 12-14Z",
  capsule: "M22 30h56c12 0 18 10 18 20s-6 20-18 20H22C10 70 4 60 4 50s6-20 18-20Z",
  cylinder: "M24 28h52c8 0 12 4 12 10v28c0 8-6 12-14 12H26c-8 0-14-4-14-12V38c0-6 4-10 12-10Zm0 8c8 4 36 4 52 0",
  hex: "M32 16h36l18 32-18 32H32L14 48 32 16Z",
  gem: "M50 12 82 40 50 88 18 40 50 12Zm0 0 32 28H18L50 12Z",
  crystal: "M50 10 68 34 78 28 70 52 84 70 50 90 16 70 30 52 22 28 32 34 50 10Z",
  wedge: "M18 78 50 16l32 62H18Z",
  shield: "M50 12 82 26v26c0 20-14 32-32 38-18-6-32-18-32-38V26L50 12Z",
  dome: "M18 52h64v22c0 8-6 12-14 12H32c-8 0-14-4-14-12V52Zm0 0c0-18 14-32 32-32s32 14 32 32",
  arch: "M22 84V48c0-16 12-30 28-30s28 14 28 30v36H22Z",
  cloud: "M28 68c-10 0-16-8-14-16 2-8 10-12 16-10 2-10 14-16 24-10 8-8 22-4 24 6 10 0 16 8 12 16-2 8-12 14-20 12-4 8-16 12-26 8-6 2-12-2-16-6Z",
  teardrop: "M50 12c16 18 28 32 28 50 0 16-12 28-28 28S22 78 22 62C22 44 34 30 50 12Z",
  leaf: "M50 14c22 8 34 28 30 48-6 18-24 28-40 24-8-20-6-46 10-72Z",
  square: "M22 22h56v56H22V22Z",
  sparkle: "M50 8c4 16 10 22 26 26-16 4-22 10-26 26-4-16-10-22-26-26 16-4 22-10 26-26Z",
  clover: "M50 18c8 0 14 8 14 16 8-2 18 4 16 14-2 8-12 12-18 8 2 10-4 20-14 18-8-2-12-12-8-20-8 4-18 0-18-10 0-8 8-14 16-12-2-8 4-14 12-14Z",
  flower: "M50 18c6-8 18-6 20 4 10-4 18 6 14 16 10 2 12 16 4 22 6 8-2 18-12 16 0 10-10 16-18 10-6 8-18 6-20-2-10 2-18-8-12-16-8-4-8-16 2-20-4-10 4-18 14-16 2-8 12-10 18-4Z",
  heart: "M50 82C28 66 16 52 16 38 16 26 26 18 36 22c6 2 10 8 14 14 4-6 8-12 14-14 10-4 20 4 20 16 0 14-12 28-34 44Z",
  house: "M18 46 50 18l32 28v36H18V46Zm18 18h10v18H36V64Z",
  star: "M50 10 61 38h30L67 56l8 28-25-16-25 16 8-28L19 38h30L50 10Z",
};

function shapePath(shape: AvatarShape): ReactNode {
  return <path d={avatarShapePaths[shape]} />;
}

export function PuckAvatar({ assistantId, name, state = "idle", design, size = "medium", attention = false, animated = true }: { assistantId: string; name: string; state?: PuckState; design?: AvatarDesign; size?: "small" | "medium" | "large"; attention?: boolean; animated?: boolean }) {
  const instanceId = useId().replaceAll(":", "");
  const root = useRef<HTMLDivElement>(null);
  const reducedMotion = useRef(false);
  const restPose = useMemo(() => restPoseForKey(`${assistantId}:${instanceId}`), [assistantId, instanceId]);
  const currentPose = useRef(restPose);
  const avatar = useMemo(() => design || deriveAvatar(assistantId), [assistantId, design]);
  const expression = expressionForState(state);

  useEffect(() => {
    if (!animated || !root.current) return;
    const element = root.current;
    const countMotion = () => element.dataset.motionCount = String(Number(element.dataset.motionCount || 0) + 1);
    const target = {
      setBlinkPhase(phase: BlinkPhase) { element.dataset.blinkPhase = phase; countMotion(); },
      setPose(pose: AvatarPose) {
        currentPose.current = pose;
        applyPose(element, pose);
        countMotion();
      },
    };
    const media = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-reduced-motion: reduce)") : undefined;
    reducedMotion.current = media?.matches ?? false;
    element.dataset.reducedMotion = String(reducedMotion.current);
    const registration = registerAvatarMotion(`${assistantId}:${instanceId}`, target, { reducedMotion: reducedMotion.current });
    const mediaChanged = () => {
      reducedMotion.current = media?.matches ?? false;
      element.dataset.reducedMotion = String(reducedMotion.current);
      registration.setReducedMotion(reducedMotion.current);
    };
    media?.addEventListener("change", mediaChanged);
    const observer = typeof IntersectionObserver === "function" ? new IntersectionObserver(records => {
      const record = records[0];
      if (record) registration.setVisible(record.isIntersecting);
    }) : undefined;
    observer?.observe(element);
    return () => {
      observer?.disconnect();
      media?.removeEventListener("change", mediaChanged);
      registration.unregister();
    };
  }, [animated, assistantId, instanceId]);

  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const moving = new Set<EventTarget>();
    const transitionStarted = (event: Event) => {
      if ((event as TransitionEvent).propertyName !== "transform") return;
      moving.add(event.target!);
      element.dataset.moving = "true";
    };
    const transitionFinished = (event: Event) => {
      if ((event as TransitionEvent).propertyName !== "transform") return;
      moving.delete(event.target!);
      if (moving.size === 0) element.dataset.moving = "false";
    };
    element.addEventListener("transitionrun", transitionStarted);
    element.addEventListener("transitionend", transitionFinished);
    element.addEventListener("transitioncancel", transitionFinished);
    return () => {
      element.removeEventListener("transitionrun", transitionStarted);
      element.removeEventListener("transitionend", transitionFinished);
      element.removeEventListener("transitioncancel", transitionFinished);
    };
  }, []);

  function follow(event: PointerEvent<HTMLDivElement>) {
    if (reducedMotion.current || event.pointerType === "touch" || !root.current) return;
    const box = root.current.getBoundingClientRect();
    const x = Math.max(-1, Math.min(1, (event.clientX - box.left) / box.width * 2 - 1));
    const y = Math.max(-1, Math.min(1, (event.clientY - box.top) / box.height * 2 - 1));
    applyPose(root.current, {
      name: "pointer", motionDuration: 720, headX: x * 7.5, headY: y * 6.8, headRotate: Math.abs(x) > .12 ? x * 24 : y * 20,
      faceX: x * 11.5, faceY: y * 10, faceRotate: Math.abs(x) > .12 ? x * 28 + y * 4 : currentPose.current.faceRotate,
      leftEyeScaleX: x < -.12 ? .55 : 1, rightEyeScaleX: x > .12 ? .55 : 1,
      leftEyeScaleY: x < -.12 ? .78 : 1, rightEyeScaleY: x > .12 ? .78 : 1, eyeRoundness: 1.7,
    });
  }

  function react() {
    const element = root.current;
    if (!element || reducedMotion.current) return;
    element.dataset.reacting = "true";
    const animations = Array.from(element.querySelectorAll<HTMLElement>(".puck-avatar-sparkle")).flatMap((sparkle, index) => typeof sparkle.animate === "function" ? [sparkle.animate([
      { opacity: 0, transform: "translate(-50%, -50%) scale(.2)" },
      { opacity: 1, offset: .28, transform: `translate(calc(-50% + ${index % 2 ? 16 : -16}px), calc(-50% - ${12 + index * 2}px)) scale(1)` },
      { opacity: 0, transform: `translate(calc(-50% + ${index % 2 ? 22 : -22}px), calc(-50% - ${18 + index * 3}px)) scale(.4)` },
    ], { duration: 420, easing: "ease-out" })] : []);
    const winkTarget = element.querySelector<SVGElement>(".puck-avatar-eye-left .puck-avatar-eye-shape");
    const wink = winkTarget && typeof winkTarget.animate === "function" ? winkTarget.animate([
      { transform: "scaleY(1)" }, { transform: "scaleY(.05)", offset: .45 }, { transform: "scaleY(1)" },
    ], { duration: 300, easing: "ease-out" }) : undefined;
    if (wink) animations.push(wink);
    if (animations.length === 0) { element.dataset.reacting = "false"; return; }
    void Promise.allSettled(animations.map(animation => animation.finished)).then(() => { if (root.current === element) element.dataset.reacting = "false"; });
  }

  const style = {
    "--puck-avatar-color": avatar.colorHex,
    "--puck-motion-duration": `${restPose.motionDuration}ms`,
    "--puck-head-x": `${restPose.headX}%`, "--puck-head-y": `${restPose.headY}%`, "--puck-head-rotate": `${restPose.headRotate}deg`,
    "--puck-face-x": `${restPose.faceX}px`, "--puck-face-y": `${restPose.faceY}px`, "--puck-face-rotate": `${restPose.faceRotate}deg`,
    "--puck-left-eye-xscale": restPose.leftEyeScaleX, "--puck-right-eye-xscale": restPose.rightEyeScaleX,
    "--puck-left-eye-yscale": restPose.leftEyeScaleY, "--puck-right-eye-yscale": restPose.rightEyeScaleY,
    "--puck-eye-roundness": restPose.eyeRoundness,
  } as CSSProperties;

  return <div ref={root} className={`puck-avatar puck-avatar-${size}`} role="img" aria-label={`${name}，${stateLabel(state)}`} draggable={false}
    data-expression={expression} data-shape={avatar.shape} data-reacting="false" data-moving="false" data-blink-phase="open" data-pose="rest" data-motion-count="0"
    style={style} onPointerMove={follow} onPointerLeave={() => { if (root.current) applyPose(root.current, currentPose.current); }}
    onPointerDown={react} onDragStart={event => event.preventDefault()}>
    {avatar.image ? <img className="puck-avatar-photo" src={avatar.image} alt="" /> : <span className="puck-avatar-motion"><svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
      <g className="puck-avatar-head">
      <g className="puck-avatar-body">{shapePath(avatar.shape)}</g>
      <g className="puck-avatar-face">
        <g className="puck-avatar-eye puck-avatar-eye-left"><rect className="puck-avatar-eye-shape" x="39" y="47" width="8" height="16" rx="3"/></g>
        <g className="puck-avatar-eye puck-avatar-eye-right"><rect className="puck-avatar-eye-shape" x="53" y="47" width="8" height="16" rx="3"/></g>
      </g>
      </g>
    </svg></span>}
    <span className="puck-avatar-sparkles" aria-hidden="true">{Array.from({ length: 5 }, (_, index) => <i className="puck-avatar-sparkle" key={index}/>)}</span>
    {attention && <span className="puck-avatar-badge" aria-hidden="true" />}
  </div>;
}
