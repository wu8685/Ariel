import { useEffect, useRef, useState, type CSSProperties, type PointerEvent } from "react";

export type AvatarShape = "blob" | "cloud" | "star" | "triangle";
export type PuckState = "idle" | "thinking" | "working" | "waiting_user" | "awaiting_authorization" | "attention" | "error" | "done" | string;
export type AvatarDesign = { shape: AvatarShape; color: string };

const shapes: AvatarShape[] = ["blob", "cloud", "star", "triangle"];
const colors = ["#7c6ff2", "#ef8354", "#55b8a9", "#e05c92", "#e4b647", "#62a8e5", "#8fb85a"];

function hash(value: string): number {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0;
}

export function deriveAvatar(assistantId: string): AvatarDesign {
  const value = hash(assistantId || "puck");
  return { shape: shapes[value % shapes.length], color: colors[(value >>> 8) % colors.length] };
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

function shapePath(shape: AvatarShape) {
  if (shape === "star") return <path d="M50 4 61 30 90 26 71 49 87 76 58 68 38 91 35 62 7 51 34 38Z" />;
  if (shape === "cloud") return <path d="M17 77C2 67 8 47 24 43 20 25 42 15 54 28 68 12 91 26 85 45 103 55 91 80 72 77 61 91 39 89 31 77Z" />;
  if (shape === "triangle") return <path d="M48 7c4-3 9 0 12 5l34 66c4 8-2 16-11 16H17C7 94 2 85 7 77L40 15c2-4 5-7 8-8Z" />;
  return <path d="M19 20C35 3 67 4 84 18c15 14 12 38 5 57-7 18-28 24-49 18C19 87 5 73 7 50c1-12 4-22 12-30Z" />;
}

export function PuckAvatar({ assistantId, name, state = "idle", design, size = "medium", attention = false }: { assistantId: string; name: string; state?: PuckState; design?: AvatarDesign; size?: "small" | "medium" | "large"; attention?: boolean }) {
  const root = useRef<HTMLDivElement>(null);
  const hoverTimer = useRef<number | undefined>(undefined);
	const reactionTimer = useRef<number | undefined>(undefined);
  const [reacting, setReacting] = useState(false);
  const [blinking, setBlinking] = useState(false);
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
    if (hidden) return;
    let cancelled = false;
    let blinkTimer: number | undefined;
    let closeTimer: number | undefined;
    const schedule = () => { blinkTimer = window.setTimeout(() => {
      if (cancelled) return;
      setBlinking(true);
      closeTimer = window.setTimeout(() => { setBlinking(false); schedule(); }, 140);
    }, 2200 + Math.random() * 4800); };
    schedule();
    return () => { cancelled = true; if (blinkTimer) window.clearTimeout(blinkTimer); if (closeTimer) window.clearTimeout(closeTimer); };
  }, [hidden]);
  useEffect(() => () => {
		if (hoverTimer.current) window.clearTimeout(hoverTimer.current);
		if (reactionTimer.current) window.clearTimeout(reactionTimer.current);
	}, []);
	useEffect(() => {
		if (!reducedMotion) return;
		setReacting(false);
		if (reactionTimer.current) window.clearTimeout(reactionTimer.current);
		reactionTimer.current = undefined;
	}, [reducedMotion]);
  function follow(event: PointerEvent<HTMLDivElement>) {
    if (reducedMotion || event.pointerType === "touch" || !root.current) return;
    const box = root.current.getBoundingClientRect();
    const x = Math.max(-1, Math.min(1, (event.clientX - box.left) / box.width * 2 - 1));
    const y = Math.max(-1, Math.min(1, (event.clientY - box.top) / box.height * 2 - 1));
    root.current.style.setProperty("--puck-eye-x", `${x * 2.2}px`);
    root.current.style.setProperty("--puck-eye-y", `${y * 1.7}px`);
  }
  function react() {
		if (reducedMotion) return;
		if (reactionTimer.current) window.clearTimeout(reactionTimer.current);
    setReacting(true);
		reactionTimer.current = window.setTimeout(() => { setReacting(false); reactionTimer.current = undefined; }, 420);
  }
  return <div ref={root} className={`puck-avatar puck-avatar-${size}`} role="img" aria-label={`${name}，${stateLabel(state)}`} draggable={false}
    data-expression={expression} data-reacting={reacting ? "true" : "false"} data-blinking={blinking ? "true" : "false"} data-paused={hidden ? "true" : "false"} data-reduced-motion={reducedMotion ? "true" : "false"}
    style={{ "--puck-avatar-color": avatar.color } as CSSProperties} onPointerEnter={() => { if (!hoverTimer.current) hoverTimer.current = window.setTimeout(() => { hoverTimer.current = undefined; react(); }, 80 + Math.random() * 180); }} onPointerMove={follow} onPointerLeave={() => { if (hoverTimer.current) window.clearTimeout(hoverTimer.current); hoverTimer.current = undefined; root.current?.style.removeProperty("--puck-eye-x"); root.current?.style.removeProperty("--puck-eye-y"); }} onPointerDown={react} onDragStart={event => event.preventDefault()}>
		<svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
			<g className="puck-avatar-ears"><path className="puck-avatar-ear" d="M34 37c-7-4-13-12-12-20 9 3 15 9 18 18Z"/><path className="puck-avatar-ear" d="M66 37c7-4 13-12 12-20-9 3-15 9-18 18Z"/></g>
			<g className="puck-avatar-body">{shapePath(avatar.shape)}</g>
			<g className="puck-avatar-crown"><path className="puck-avatar-leaf" d="M45 25c-9-1-15-7-17-15 10 0 17 5 20 13Z"/><path className="puck-avatar-leaf" d="M54 23c3-9 10-14 19-14-2 9-8 15-17 17Z"/><circle className="puck-avatar-flower" cx="51" cy="20" r="3.5"/></g>
			<g className="puck-avatar-brows"><path d="M34 40c4-3 9-4 13-1"/><path d="M55 38c5-2 10-1 14 3"/></g>
			<g className="puck-avatar-eyes"><ellipse cx="40" cy="52" rx="5" ry="8"/><ellipse cx="61" cy="52" rx="5" ry="8"/></g>
		</svg>
		{reacting && <span className="puck-avatar-particles" aria-hidden="true">{Array.from({ length: 5 }, (_, index) => <i className="puck-avatar-particle" key={index}/>)}</span>}
    {attention && <span className="puck-avatar-badge" aria-hidden="true" />}
  </div>;
}
