import { Component, useId, type ReactNode } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

const maxMarkdownCodeUnits = 256 * 1024;
const markdownPlugins = [remarkGfm];

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

const components: Components = {
  a({ href, children, node: _node, ...props }) {
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
};

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

export function ConversationMarkdown({ text, renderImage }: { text: string; renderImage?: (source: string, alt: string) => ReactNode }) {
  const messageID = useId().replace(/[^a-z\d_-]/giu, "");
  if (text.length > maxMarkdownCodeUnits) return <PlainFallback text={text} notice="内容过长，暂以纯文本显示。" />;
  const imageComponents: Components = renderImage ? { ...components, img({ src, alt }) { return renderImage(src || "", alt || "") || <span className="markdown-image-placeholder" role="img" aria-label={alt ? `图片：${alt}` : "图片"}>{alt ? `图片：${alt}` : "图片"}</span>; } } : components;
  return <MarkdownErrorBoundary text={text}>
    <div className="markdown-body"><Markdown remarkPlugins={markdownPlugins} remarkRehypeOptions={{ clobberPrefix: `ariel-${messageID}-` }} skipHtml urlTransform={(url, key, node) => node.tagName === "img" && key === "src" ? url : safeHref(url) || ""} components={imageComponents}>{text}</Markdown></div>
  </MarkdownErrorBoundary>;
}
