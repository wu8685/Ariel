import { useEffect, useId, useMemo, useRef, type CSSProperties, type PointerEvent, type ReactNode } from "react";
import { registerAvatarMotion, restPoseForKey, type AvatarPose, type BlinkPhase } from "./PuckAvatarScheduler";

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
      name: "pointer", motionDuration: 720, headX: x * 4.5, headY: y * 4, headRotate: Math.abs(x) > .12 ? x * 14 : y * 10,
      faceX: x * 7, faceY: y * 6, faceRotate: Math.abs(x) > .12 ? x * 22 + y * 3 : currentPose.current.faceRotate,
      leftEyeScaleX: x > .12 ? .62 : 1, rightEyeScaleX: x < -.12 ? .62 : 1,
      leftEyeScaleY: x > .12 ? .82 : 1, rightEyeScaleY: x < -.12 ? .82 : 1, eyeRoundness: 1.7,
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
    "--puck-avatar-color": avatar.color,
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
    <span className="puck-avatar-motion"><svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
      <g className="puck-avatar-head">
      <g className="puck-avatar-ears"><path className="puck-avatar-ear" d="M38 43C35 31 36 19 40 10c4 12 6 23 5 33Z"/><path className="puck-avatar-ear" d="M62 43c3-12 2-24-2-33-4 12-6 23-5 33Z"/></g>
      <g className="puck-avatar-body">{shapePath(avatar.shape)}</g>
      <g className="puck-avatar-face">
        <g className="puck-avatar-eye puck-avatar-eye-left"><ellipse className="puck-avatar-eye-shape" cx="43" cy="55" rx="4" ry="8"/></g>
        <g className="puck-avatar-eye puck-avatar-eye-right"><ellipse className="puck-avatar-eye-shape" cx="57" cy="55" rx="4" ry="8"/></g>
      </g>
      </g>
    </svg></span>
    <span className="puck-avatar-sparkles" aria-hidden="true">{Array.from({ length: 5 }, (_, index) => <i className="puck-avatar-sparkle" key={index}/>)}</span>
    {attention && <span className="puck-avatar-badge" aria-hidden="true" />}
  </div>;
}
