import { useEffect, useRef, useState } from "react";

export type ScreenshotDraft = { name: string; bytes: number; dataUri: string };
const maxBytes = 4 << 20;
const maxCount = 3;

function readAsDataURI(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("无法读取截图。"));
    reader.onload = () => typeof reader.result === "string" ? resolve(reader.result) : reject(new Error("无法读取截图。"));
    reader.readAsDataURL(file);
  });
}

export async function readScreenshotFiles(existing: ScreenshotDraft[], files: Iterable<File>): Promise<ScreenshotDraft[]> {
  const additions = Array.from(files);
  if (existing.length + additions.length > maxCount) throw new Error("一次最多附加 3 张截图。 ");
  let total = existing.reduce((sum, image) => sum + image.bytes, 0);
  const result = [...existing];
  for (const file of additions) {
    if (file.type !== "image/png" && file.type !== "image/jpeg") throw new Error("仅支持 PNG 或 JPEG 截图。");
    total += file.size;
    if (!file.size || file.size > maxBytes || total > maxBytes) throw new Error("截图总大小不能超过 4 MiB。");
    const dataUri = await readAsDataURI(file);
    if (!dataUri.startsWith(`data:${file.type};base64,`)) throw new Error("截图格式无法确认。");
    result.push({ name: file.name, bytes: file.size, dataUri });
  }
  return result;
}

export function ConversationImage({ alt, load, autoLoad = true }: { alt: string; load: () => Promise<string>; autoLoad?: boolean }) {
  const [state, setState] = useState<"idle" | "loading" | "ready" | "failed">("idle");
  const [uri, setUri] = useState("");
  const [expanded, setExpanded] = useState(false);
  const control = useRef<HTMLButtonElement>(null);
  const pending = useRef(false);
  const label = alt || "截图";
  async function fetchImage() {
    if (pending.current || state === "ready") return;
    pending.current = true;
    setState("loading");
    try {
      const value = await load();
      if (!/^data:image\/(png|jpeg);base64,[a-z\d+/]+=*$/iu.test(value) || value.length > 5_600_000) throw new Error("图片数据无效");
      setUri(value); setState("ready");
    } catch { setState("failed"); }
    finally { pending.current = false; }
  }
  useEffect(() => {
    if (!autoLoad || !control.current || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) { observer.disconnect(); void fetchImage(); } }, { rootMargin: "180px" });
    observer.observe(control.current);
    return () => observer.disconnect();
  }, [autoLoad]);
  return <span className="conversation-image">
    {state === "ready" ? <button className="conversation-image-open" type="button" aria-label={`放大截图：${label}`} onClick={() => setExpanded(true)}><img src={uri} alt={label} loading="lazy" /></button>
      : <button ref={control} className="conversation-image-load" type="button" aria-label={`${state === "failed" ? "重试加载截图" : "加载截图"}：${label}`} onClick={() => void fetchImage()} disabled={state === "loading"}>{state === "loading" ? "正在加载截图…" : state === "failed" ? "截图未能加载，点此重试" : `查看截图${alt ? `：${alt}` : ""}`}</button>}
    {expanded && <div className="conversation-image-lightbox" role="dialog" aria-modal="true" aria-label={label} onClick={() => setExpanded(false)}><button type="button" aria-label="关闭截图" onClick={() => setExpanded(false)}>×</button><img src={uri} alt={label} onClick={event => event.stopPropagation()} /></div>}
  </span>;
}
