import { useEffect, useId, useRef, useState, type CSSProperties } from "react";

const maxDiagramCodeUnits = 32 * 1024;
const maxDiagramLines = 500;
const renderTimeoutMs = 5_000;

type MermaidModule = typeof import("mermaid");
type RenderState = { phase: "loading" } | { phase: "ready"; svg: string; naturalWidth: number } | { phase: "failed"; notice: string };

let mermaidModule: Promise<MermaidModule> | null = null;
let initialized = false;

async function loadMermaid(): Promise<MermaidModule> {
  mermaidModule ||= import("mermaid");
  const module = await mermaidModule;
  if (!initialized) {
    module.default.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      htmlLabels: false,
      suppressErrorRendering: true,
      maxTextSize: maxDiagramCodeUnits,
      maxEdges: 500,
      theme: "dark",
      themeVariables: {
        background: "#151515",
        primaryColor: "#242424",
        primaryTextColor: "#f2f2f2",
        primaryBorderColor: "#686868",
        lineColor: "#a9a9a9",
        secondaryColor: "#1d1d1d",
        tertiaryColor: "#101010",
        fontFamily: 'Inter, "PingFang SC", sans-serif',
      },
      flowchart: { htmlLabels: false },
    });
    initialized = true;
  }
  return module;
}

function safeSvg(svg: string): { svg: string; naturalWidth: number } | null {
  const parsed = new DOMParser().parseFromString(svg, "image/svg+xml");
  const root = parsed.documentElement;
  if (root.localName !== "svg" || parsed.querySelector("parsererror, script, foreignObject, iframe, object, embed, audio, video")) return null;
  for (const element of parsed.querySelectorAll("*")) {
    for (const attribute of [...element.attributes]) {
      const name = attribute.name.toLowerCase();
      const value = attribute.value.trim();
      if (name.startsWith("on")) return null;
      if ((name === "href" || name.endsWith(":href")) && value && !value.startsWith("#")) return null;
      if ((name === "src" || name === "srcset") && value) return null;
      if ((name === "style" || element.localName === "style") && /@import|url\s*\(\s*['"]?(?:https?:|data:|\/\/)/iu.test(value || element.textContent || "")) return null;
    }
  }
  const viewBox = (root.getAttribute("viewBox") || "").trim().split(/\s+/u).map(Number);
  const measuredWidth = viewBox.length === 4 && viewBox.every(Number.isFinite) && viewBox[2] > 0 ? viewBox[2] : 640;
  return { svg: new XMLSerializer().serializeToString(root), naturalWidth: Math.max(320, Math.min(1_200, Math.round(measuredWidth))) };
}

function MermaidFallback({ source, notice }: { source: string; notice: string }) {
  return <div className="mermaid-fallback">
    <div className="mermaid-fallback-note" role="status">{notice}</div>
    <pre><code>{source}</code></pre>
  </div>;
}

export function MermaidDiagram({ source }: { source: string }) {
  const reactID = useId().replace(/[^a-z\d_-]/giu, "");
  const diagramRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<RenderState>({ phase: "loading" });

  useEffect(() => {
    let current = true;
    if (source.length > maxDiagramCodeUnits || source.split("\n").length > maxDiagramLines) {
      setState({ phase: "failed", notice: "图表内容过大，已显示源码。" });
      return () => { current = false; };
    }
    setState({ phase: "loading" });
    const timeout = new Promise<never>((_, reject) => window.setTimeout(() => reject(new Error("Mermaid render timeout")), renderTimeoutMs));
    void Promise.race([
      loadMermaid().then(module => module.default.render(`ariel-mermaid-${reactID}`, source)),
      timeout,
    ]).then(result => {
      if (!current) return;
      const sanitized = safeSvg(result.svg);
      setState(sanitized ? { phase: "ready", ...sanitized } : { phase: "failed", notice: "图表未通过安全检查，已显示源码。" });
    }).catch(() => {
      if (current) setState({ phase: "failed", notice: "图表暂时无法渲染，已显示源码。" });
    });
    return () => { current = false; };
  }, [reactID, source]);

  useEffect(() => {
    if (state.phase !== "ready" || !diagramRef.current) return;
    const viewport = diagramRef.current;
    const firstNode = viewport.querySelector<SVGGraphicsElement>(".node, .actor, .statediagram-state");
    if (firstNode) {
      const viewportRect = viewport.getBoundingClientRect();
      const nodeRect = firstNode.getBoundingClientRect();
      viewport.scrollLeft = Math.max(0, viewport.scrollLeft + nodeRect.left - viewportRect.left - (viewport.clientWidth - nodeRect.width) / 2);
      return;
    }
    viewport.scrollLeft = Math.max(0, (viewport.scrollWidth - viewport.clientWidth) / 2);
  }, [state]);

  if (state.phase === "failed") return <MermaidFallback source={source} notice={state.notice} />;
  if (state.phase === "loading") return <div className="mermaid-loading" role="status" aria-label="正在渲染 Mermaid 图表"><span aria-hidden="true" />正在渲染图表…</div>;
  return <div ref={diagramRef} className="mermaid-diagram" role="img" aria-label="Mermaid 图表" tabIndex={0} style={{ "--mermaid-natural-width": `${state.naturalWidth}px` } as CSSProperties} dangerouslySetInnerHTML={{ __html: state.svg }} />;
}
