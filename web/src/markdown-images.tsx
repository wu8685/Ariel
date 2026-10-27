import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { classifyImageSource, type DocumentImageSession, type LoadedImage } from "./document-assets";

function useRequested(initial = false) {
  const holder = useRef<HTMLSpanElement>(null), [requested, request] = useState(initial);
  useEffect(() => {
    if (requested || !holder.current || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { request(true); observer.disconnect(); }
    }, { rootMargin: "240px" });
    observer.observe(holder.current); return () => observer.disconnect();
  }, [requested]);
  return { holder, requested, request: () => request(true) };
}

export function ExternalMarkdownImage({ source, alt, allowed, onAllow }: { source: string; alt: string; allowed: boolean; onAllow: () => void }) {
  const policy = useMemo(() => classifyImageSource(source), [source]);
  const { holder, requested, request } = useRequested();
  const [attempt, setAttempt] = useState(0), [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  if (policy.kind !== "external") return null;
  const start = () => { setState("loading"); request(); onAllow(); };
  return <span className="document-image" ref={holder} data-source-unmapped="true" data-image-state={!allowed ? "permission" : state}>
    {!allowed ? <span className="document-image-placeholder"><span><span>{alt ? `图片：${alt}` : "图片"}</span> · {policy.domain}</span><button type="button" onClick={start}>允许加载外部图片</button></span> : state === "failed" ?
      <span className="document-image-placeholder" role="status"><span>{alt || "图片"} · {policy.domain} · 外部图片加载失败</span><button type="button" onClick={() => { setState("loading"); setAttempt(value => value + 1); request(); }}>重试图片</button></span> :
      <>{requested ? <img key={attempt} src={policy.url} alt={alt || "文档图片"} loading="lazy" referrerPolicy="no-referrer" onLoad={() => setState("ready")} onError={() => setState("failed")} /> : <span className="document-image-placeholder">图片按需加载<button type="button" onClick={request}>加载图片</button></span>}
        {requested && state === "loading" && <span className="document-image-status" role="status">正在加载图片…</span>}</>}
  </span>;
}

type LocalState = { source: string; session: DocumentImageSession; phase: "loading" | "decoding" | "ready" | "failed"; image?: LoadedImage; error?: string };
function LocalDocumentImage({ source, alt, session }: { source: string; alt: string; session: DocumentImageSession }) {
  const { holder, requested, request } = useRequested(session.isCached(source));
  const [attempt, setAttempt] = useState(0), [state, setState] = useState<LocalState>({ source, session, phase: "loading" });
  useEffect(() => {
    if (!requested) return;
    if (attempt > 0) session.invalidate(source);
    const controller = new AbortController(); let resource: LoadedImage | undefined;
    setState({ source, session, phase: "loading" });
    void session.acquire(source, controller.signal).then(value => {
      if (controller.signal.aborted) { value.release(); return; }
      resource = value; setState({ source, session, phase: "decoding", image: value });
    }, reason => { if (!controller.signal.aborted) setState({ source, session, phase: "failed", error: reason instanceof Error ? reason.message : "无法读取图片" }); });
    return () => { controller.abort(); resource?.release(); };
  }, [source, session, requested, attempt]);
  const current = state.source === source && state.session === session ? state : undefined;
  const phase = !requested ? "idle" : current?.phase || "loading";
  const retry = () => { setAttempt(value => value + 1); request(); };
  return <span className="document-image" ref={holder} data-source-unmapped="true" data-image-state={phase}>
    {current?.image && (phase === "decoding" || phase === "ready") ? <>
      <img key={attempt} src={current.image.url} width={current.image.width} height={current.image.height} alt={alt || "文档图片"}
        onLoad={() => setState(value => value.image === current.image ? { ...value, phase: "ready" } : value)}
        onError={() => setState(value => value.image === current.image ? { ...value, phase: "failed", error: "图片无法解码，文件可能已损坏" } : value)} />
      {phase === "decoding" && <span className="document-image-status" role="status">正在解码图片…</span>}
    </> : <span className="document-image-placeholder" role="status"><span>{alt || "文档图片"} · {phase === "failed" ? current?.error : requested ? "正在加载图片…" : "图片按需加载"}</span>
      {phase === "failed" ? <button type="button" onClick={retry}>重试图片</button> : !requested && <button type="button" onClick={request}>加载图片</button>}
    </span>}
  </span>;
}

export function DocumentImage({ source, alt, session, allowed, onAllow }: { source: string; alt: string; session: DocumentImageSession; allowed: ReadonlySet<string>; onAllow: (domain: string) => void }) {
  const policy = classifyImageSource(source);
  if (policy.kind === "external") return <ExternalMarkdownImage key={source} source={source} alt={alt} allowed={allowed.has(policy.domain)} onAllow={() => onAllow(policy.domain)} />;
  if (policy.kind === "blocked") return <span className="document-image document-image-placeholder" role="status" data-source-unmapped="true">{alt || "文档图片"}：{policy.reason}</span>;
  return <LocalDocumentImage key={source} source={source} alt={alt} session={session} />;
}

export const ConversationImageContext = createContext<{ version: string; renderImage?: (source: string, alt: string) => ReactNode }>({ version: "" });
function ConversationExternalImage({ source, alt }: { source: string; alt: string }) {
  const [allowed, setAllowed] = useState(false);
  return <ExternalMarkdownImage source={source} alt={alt} allowed={allowed} onAllow={() => setAllowed(true)} />;
}
export function ConversationMarkdownImage({ source, alt }: { source: string; alt: string }) {
  const { version, renderImage } = useContext(ConversationImageContext);
  const native = renderImage?.(source, alt);
  if (native) return native;
  const policy = classifyImageSource(source);
  if (policy.kind === "external") return <ConversationExternalImage key={version} source={source} alt={alt} />;
  return <span className="markdown-image-placeholder" data-source-unmapped="true"><span>{alt ? `图片：${alt}` : "图片"}</span> · 不支持此图片来源{policy.kind === "blocked" ? `：${policy.reason}` : ""}</span>;
}
