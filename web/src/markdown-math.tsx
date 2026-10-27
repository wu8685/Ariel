import { useEffect, useState, type ReactNode } from "react";
import type { Components } from "react-markdown";
import type { Literal, Root } from "mdast";
import type { Extension as FromMarkdownExtension } from "mdast-util-from-markdown";
import type { Code, Extension, State, Tokenizer } from "micromark-util-types";
import type { Processor } from "unified";

interface MathNode extends Literal { type: "arielMath" }
declare module "mdast" { interface PhrasingContentMap { arielMath: MathNode } interface RootContentMap { arielMath: MathNode } }
declare module "micromark-util-types" { interface TokenTypeMap { arielMath: "arielMath"; arielMathData: "arielMathData" } }
declare module "unified" { interface Data { micromarkExtensions?: Extension[]; fromMarkdownExtensions?: Array<FromMarkdownExtension | FromMarkdownExtension[]> } }

const whitespace = (code: Code) => code !== null && (code < 0 || code === 32);

// Tokenize before CommonMark consumes escapes or interprets TeX's _, *, links
// and matrix ampersands. Code fences / inline code remain owned by CommonMark.
const tokenize: Tokenizer = function (effects, ok, nok) {
  let slash = false, display = false, closing = 0, previous: Code = null, count = 0, dataOpen = false;
  function consume(code: Code) { if (!dataOpen) { effects.enter("arielMathData"); dataOpen = true; } effects.consume(code); }
  function closeData() { if (dataOpen) { effects.exit("arielMathData"); dataOpen = false; } }
  return start;
  function start(code: Code): State | undefined {
    slash = code === 92;
    effects.enter("arielMath"); consume(code);
    return opener;
  }
  function opener(code: Code): State | undefined {
    if (slash) {
      if (code !== 40 && code !== 91) return nok(code);
      display = code === 91; closing = display ? 93 : 41;
      consume(code); return body;
    }
    if (code === 36) { display = true; consume(code); return body; }
    if (code === null || whitespace(code)) return nok(code);
    return body(code);
  }
  function body(code: Code): State | undefined {
    if (code === null || (!display && code <= -3)) {
      if (slash) { closeData(); effects.exit("arielMath"); return ok(code); }
      return nok(code);
    }
    if (code <= -3) { closeData(); effects.enter("lineEnding"); effects.consume(code); effects.exit("lineEnding"); previous = code; count++; return body; }
    if (slash && code === 92) { consume(code); return slashEnd; }
    if (!slash && code === 92) { consume(code); return dollarEscape; }
    if (!slash && code === 36) {
      if (!count || !display && whitespace(previous)) return nok(code);
      consume(code); return display ? secondDollar : end;
    }
    consume(code); previous = code; count++; return body;
  }
  function dollarEscape(code: Code): State | undefined {
    if (code === null) return nok(code);
    if (code <= -3) return body(code);
    consume(code); previous = code; count += 2; return body;
  }
  function slashEnd(code: Code): State | undefined {
    if (code === closing && count) { consume(code); return end; }
    if (code === null) { closeData(); effects.exit("arielMath"); return ok(code); }
    consume(code); previous = code; count += 2; return body;
  }
  function secondDollar(code: Code): State | undefined {
    if (code === 36) { consume(code); return end; }
    previous = 36; count++; return body(code);
  }
  function end(code: Code): State | undefined {
    // In '$20 and $30', the second dollar is not a closing delimiter.
    if (!slash && !display && code !== null && code >= 48 && code <= 57) return nok(code);
    closeData(); effects.exit("arielMath"); return ok(code);
  }
};
const syntax: Extension = { text: { 36: { name: "arielMath", tokenize }, 92: { name: "arielMath", tokenize } } };
const fromMarkdown: FromMarkdownExtension = {
  canContainEols: ["arielMath"],
  enter: { arielMath(token) { this.enter({ type: "arielMath", value: "" }, token); this.buffer(); } },
  exit: { arielMath(token) {
    this.resume();
    const node = this.stack[this.stack.length - 1] as MathNode;
    node.value = this.sliceSerialize(token);
    node.data = { hName: "span", hProperties: { "data-math-source": node.value, "data-source-unmapped": "true" }, hChildren: [] };
    this.exit(token);
  } },
};

export function remarkMath(this: Processor) {
  const data = this.data();
  (data.micromarkExtensions ||= []).push(syntax);
  (data.fromMarkdownExtensions ||= []).push(fromMarkdown);
  return (tree: Root) => {
    let count = 0;
    const walk = (node: unknown) => {
      const current = node as { type?: string; data?: MathNode["data"]; children?: unknown[] };
      if (current.type === "arielMath" && ++count > 256) current.data!.hProperties!["data-math-error"] = "公式数量超过限制，已显示原文。";
      current.children?.forEach(walk);
    };
    walk(tree);
  };
}

let renderer: Promise<typeof import("katex")> | undefined;
function loadRenderer() {
  renderer ||= Promise.all([import("katex"), import("katex/dist/katex.min.css")]).then(([module]) => module).catch(error => { renderer = undefined; throw error; });
  return renderer;
}

function MathFormula({ source, error }: { source: string; error?: string }) {
  const display = source.startsWith("$$") || source.startsWith("\\[");
  const delimiter = source.startsWith("$") ? display ? 2 : 1 : 2;
  const incomplete = source.startsWith("\\(") ? !source.endsWith("\\)") : source.startsWith("\\[") && !source.endsWith("\\]");
  const [state, setState] = useState<{ source: string; html?: string; error?: string }>({ source });
  const invalid = error || (source.length - 2 * delimiter > 16 * 1024 ? "公式超过大小限制，已显示原文。" : /\\(?:href|url|includegraphics|html[A-Za-z]*)\b/u.test(source) ? "公式包含不支持的命令，已显示原文。" : "");
  useEffect(() => {
    if (invalid || incomplete) return;
    let current = true;
    void loadRenderer().then(module => {
      const html = module.default.renderToString(source.slice(delimiter, -delimiter), {
        displayMode: display, throwOnError: true, trust: false, strict: "error", maxExpand: 1000, maxSize: 20, macros: {},
      });
      if (current) setState({ source, html });
    }).catch(() => { if (current) setState({ source, error: "公式无法安全解析，已显示原文。" }); });
    return () => { current = false; };
  }, [source, delimiter, display, invalid, incomplete]);
  const active = state.source === source ? state : { source };
  const notice = invalid || active.error;
  return <span className={display ? "markdown-math markdown-math-display" : "markdown-math"} data-source-unmapped="true" data-math-pending={!notice && !incomplete && !active.html ? "true" : undefined}>
    {notice ? <><span className="markdown-math-note" role="status">{notice}</span><code>{source}</code></> : active.html ?
      <span dangerouslySetInnerHTML={{ __html: active.html }} /> : <span className="markdown-math-pending">{source}</span>}
  </span>;
}

export const mathSpan: Components["span"] = function MathSpan({ node, children, ...props }) {
  const source = node?.properties?.["data-math-source"];
  if (typeof source === "string") return <MathFormula source={source} error={node?.properties?.["data-math-error"] as string | undefined} />;
  return <span {...props}>{children as ReactNode}</span>;
};
