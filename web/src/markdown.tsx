import { Component, isValidElement, useId, type ReactNode } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { MermaidDiagram } from "./MermaidDiagram";
import { projectBareMermaidBlocks } from "./mermaid-blocks";

const maxMarkdownCodeUnits = 256 * 1024;
const maxDocumentReferences = 24;
const documentURLPrefix = "ariel-document-reference:";
const plainDocumentPattern = /(^|[\s（(：:，,。；;])((?:\.?\.?\/|\/)?(?:[^\s()[\]{}<>"'，。；;：:]+\/)*[^\s/()[\]{}<>"'，。；;：:]+\.[\p{L}\p{N}]{1,16}(?::[1-9][0-9]*(?::[1-9][0-9]*)?|#L[1-9][0-9]*(?:-L[1-9][0-9]*)?)?)(?=$|[\s）)，,。；;])/gu;

export type WorkspaceDocumentReference = { kind: "markdown_link" | "plain_path"; rawReference: string; label: string; startLine?: number; endLine?: number };

type MarkdownNode = { type?: string; value?: string; url?: string; children?: MarkdownNode[] };

function rawHTMLDepthDelta(value: string): number {
  const voidElements = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"]);
  let delta = 0;
  for (const match of value.matchAll(/<\s*(\/?)\s*([a-z][\w:-]*)\b[^>]*>/giu)) {
    const closing = match[1] === "/";
    const name = match[2].toLowerCase();
    if (closing) delta--;
    else if (!voidElements.has(name) && !/\/\s*>$/u.test(match[0])) delta++;
  }
  return delta;
}

function referenceLocation(rawReference: string): Pick<WorkspaceDocumentReference, "startLine" | "endLine"> {
  const hash = rawReference.match(/#L([1-9][0-9]*)(?:-L([1-9][0-9]*))?$/u);
  if (hash) return { startLine: Number(hash[1]), endLine: Number(hash[2] || hash[1]) };
  const colon = rawReference.match(/:([1-9][0-9]*)(?::[1-9][0-9]*)?$/u);
  return colon ? { startLine: Number(colon[1]), endLine: Number(colon[1]) } : {};
}

function workspaceDocumentCandidate(rawReference: string): boolean {
  if (!rawReference || rawReference.length > 4096 || /[\u0000-\u001f\u007f]/u.test(rawReference) || /^(?:https?:\/\/|mailto:|file:|#)/iu.test(rawReference) || rawReference.includes("\\")) return false;
  const withoutLocation = rawReference.replace(/(?::[1-9][0-9]*(?::[1-9][0-9]*)?|#L[1-9][0-9]*(?:-L[1-9][0-9]*)?)$/u, "");
  if (!withoutLocation || withoutLocation.split("/").includes("..")) return false;
  return /(?:^|\/)[^/]+\.[\p{L}\p{N}]{1,16}$/u.test(withoutLocation);
}

function encodedDocumentURL(kind: WorkspaceDocumentReference["kind"], rawReference: string): string {
  return `${documentURLPrefix}${kind}:${encodeURIComponent(rawReference)}`;
}

function decodedDocumentURL(value?: string): Omit<WorkspaceDocumentReference, "label"> | null {
  if (!value?.startsWith(documentURLPrefix)) return null;
  const payload = value.slice(documentURLPrefix.length);
  const separator = payload.indexOf(":");
  const kind = payload.slice(0, separator);
  if ((kind !== "markdown_link" && kind !== "plain_path") || separator < 0) return null;
  try {
    const rawReference = decodeURIComponent(payload.slice(separator + 1));
    return workspaceDocumentCandidate(rawReference) ? { kind, rawReference, ...referenceLocation(rawReference) } : null;
  } catch { return null; }
}

function remarkWorkspaceDocumentCandidates() {
  return (tree: MarkdownNode) => {
    let referenceCount = 0;
    const walk = (node: MarkdownNode, blocked = false) => {
      if (node.type === "link" && typeof node.url === "string" && workspaceDocumentCandidate(node.url)) {
        if (referenceCount < maxDocumentReferences) {
          node.url = encodedDocumentURL("markdown_link", node.url);
          referenceCount++;
        }
        return;
      }
      const nextBlocked = blocked || ["link", "image", "inlineCode", "code", "html", "definition"].includes(node.type || "");
      if (!nextBlocked && Array.isArray(node.children)) {
        const next: MarkdownNode[] = [];
        let rawHTMLDepth = 0;
        for (const child of node.children) {
          if (child.type === "html" && typeof child.value === "string") {
            next.push(child);
            rawHTMLDepth = Math.max(0, rawHTMLDepth + rawHTMLDepthDelta(child.value));
            continue;
          }
          if (rawHTMLDepth > 0) { next.push(child); continue; }
          if (child.type !== "text" || typeof child.value !== "string") { walk(child, false); next.push(child); continue; }
          let cursor = 0;
          plainDocumentPattern.lastIndex = 0;
          for (const match of child.value.matchAll(plainDocumentPattern)) {
            const prefix = match[1] || "";
            const rawReference = match[2];
            const start = match.index! + prefix.length;
            if (referenceCount >= maxDocumentReferences || !workspaceDocumentCandidate(rawReference) || rawReference.includes("://") || rawReference.includes("@")) continue;
            if (start > cursor) next.push({ type: "text", value: child.value.slice(cursor, start) });
            next.push({ type: "link", url: encodedDocumentURL("plain_path", rawReference), children: [{ type: "text", value: rawReference }] });
            referenceCount++;
            cursor = start + rawReference.length;
          }
          if (cursor < child.value.length) next.push({ type: "text", value: child.value.slice(cursor) });
        }
        node.children = next;
      } else if (Array.isArray(node.children)) {
        for (const child of node.children) walk(child, true);
      }
    };
    walk(tree);
  };
}

const conversationMarkdownPlugins = [remarkGfm, remarkWorkspaceDocumentCandidates];
const standardMarkdownPlugins = [remarkGfm];

function safeHref(value?: string): string | null {
  if (!value || value !== value.trim() || /[\u0000-\u001f\u007f]/u.test(value)) return null;
  if (/^#[a-z\d_:-]+$/iu.test(value)) return value;
  if (!/^(https?:\/\/|mailto:)/iu.test(value)) return null;
  try {
    const parsed = new URL(value);
    if (parsed.username || parsed.password) return null;
    if (parsed.protocol === "mailto:") return parsed.pathname ? parsed.href : null;
    if ((parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.hostname) return parsed.href;
  } catch {
    // Malformed and ambiguous targets remain text, never a navigation.
  }
  return null;
}

function componentsFor(onOpenDocument?: (reference: WorkspaceDocumentReference) => void): Components { return {
  a({ href, children, node: _node, ...props }) {
    const document = decodedDocumentURL(href);
    if (document) {
      const label = String(children);
      return <button type="button" className="workspace-document-reference" aria-label={`打开文档：${label}`} onClick={() => onOpenDocument?.({ ...document, label })} disabled={!onOpenDocument}><span aria-hidden="true">▧</span>{children}</button>;
    }
    const safe = safeHref(href);
    return safe ? <a {...props} href={safe} target={safe.startsWith("#") ? undefined : "_blank"} rel={safe.startsWith("#") ? undefined : "noopener noreferrer"}>{children}</a> : <span className="markdown-unsafe-link">{children}</span>;
  },
  img({ alt }) {
    const label = alt ? `图片：${alt}` : "图片";
    return <span className="markdown-image-placeholder" role="img" aria-label={label}>{label}</span>;
  },
  table({ children }) {
    return <div className="markdown-table-scroll" tabIndex={0} role="region" aria-label="表格，可横向滚动"><table>{children}</table></div>;
  },
  pre({ children }) {
    if (isValidElement<{ className?: string; children?: ReactNode }>(children) && /(?:^|\s)language-mermaid(?:\s|$)/u.test(children.props.className || "")) {
      const source = String(children.props.children || "").replace(/\n$/u, "");
      return <MermaidDiagram source={source} />;
    }
    return <pre>{children}</pre>;
  },
}; }

function PlainFallback({ text, notice }: { text: string; notice: string }) {
  return <div className="markdown-fallback"><div className="markdown-fallback-note" role="status">{notice}</div><div className="markdown-fallback-text">{text}</div></div>;
}

class MarkdownErrorBoundary extends Component<{ text: string; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() { return { failed: true }; }

  componentDidUpdate(previous: Readonly<{ text: string; children: ReactNode }>) {
    if (previous.text !== this.props.text && this.state.failed) this.setState({ failed: false });
  }

  render() {
    return this.state.failed ? <PlainFallback text={this.props.text} notice="Markdown 无法解析，已改为纯文本显示。" /> : this.props.children;
  }
}

export function ConversationMarkdown({ text, renderImage, onOpenDocument }: { text: string; renderImage?: (source: string, alt: string) => ReactNode; onOpenDocument?: (reference: WorkspaceDocumentReference) => void }) {
  const messageID = useId().replace(/[^a-z\d_-]/giu, "");
  if (text.length > maxMarkdownCodeUnits) return <PlainFallback text={text} notice="内容过长，暂以纯文本显示。" />;
  const baseComponents = componentsFor(onOpenDocument);
  const imageComponents: Components = renderImage ? { ...baseComponents, img({ src, alt }) { return renderImage(src || "", alt || "") || <span className="markdown-image-placeholder" role="img" aria-label={alt ? `图片：${alt}` : "图片"}>{alt ? `图片：${alt}` : "图片"}</span>; } } : baseComponents;
  return <MarkdownErrorBoundary text={text}>
    <div className="markdown-body"><Markdown remarkPlugins={onOpenDocument ? conversationMarkdownPlugins : standardMarkdownPlugins} remarkRehypeOptions={{ clobberPrefix: `ariel-${messageID}-` }} skipHtml urlTransform={(url, key, node) => {
      if (node.tagName === "img" && key === "src") return url;
      if (url.startsWith(documentURLPrefix)) return url;
      return safeHref(url) || "";
    }} components={imageComponents}>{projectBareMermaidBlocks(text)}</Markdown></div>
  </MarkdownErrorBoundary>;
}
