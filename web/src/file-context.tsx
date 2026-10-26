import { useEffect, useRef, useState } from "react";
import { CircleButton, CloseGlyph } from "./circular-button";

export type FileChange = { itemId: string; path: string; kind: "add" | "update" | "delete"; diff?: string; diffTruncated?: boolean };
export type FileQuote = { source?: "turn_change" | "message_reference" | "workspace_browser"; threadId: string; turnId?: string; itemId?: string; path: string; sha256: string; modifiedAtMs: number; startLine: number; endLine: number; text: string; referenceKind?: "markdown_link" | "plain_path"; rawReference?: string };
export type OpenFile = { source?: "turn_change" | "message_reference" | "workspace_browser"; threadId: string; turnId?: string; itemId?: string; path: string; content: string; sha256: string; modifiedAtMs: number; lineCount: number; language: string; diff?: string; diffTruncated?: boolean; version: "current-workspace"; referenceKind?: "markdown_link" | "plain_path"; rawReference?: string };
export type FileViewMode = "source" | "diff";
export type FileView = { threadId:string;turnId:string;change:FileChange;mode:FileViewMode;file?:OpenFile;loading?:boolean;focusStartLine?:number;focusEndLine?:number };
export type HistoricalFileQuote = Pick<FileQuote,"path"|"sha256"|"startLine"|"endLine"|"text">;

export class FileContentCache {
  private readonly entries = new Map<string, { file: OpenFile; bytes: number }>();
  private totalBytes = 0;

  constructor(private readonly maxEntries = 10, private readonly maxBytes = 8 * 1024 * 1024) {}

  get(key: string): OpenFile | undefined {
    const entry = this.entries.get(key);
    if (!entry) return undefined;
    this.entries.delete(key);
    this.entries.set(key, entry);
    return entry.file;
  }

  set(key: string, file: OpenFile): void {
    const bytes = new TextEncoder().encode(file.content).byteLength;
    if (bytes > this.maxBytes) return;
    const previous = this.entries.get(key);
    if (previous) {
      this.totalBytes -= previous.bytes;
      this.entries.delete(key);
    }
    this.entries.set(key, { file, bytes });
    this.totalBytes += bytes;
    while (this.entries.size > this.maxEntries || this.totalBytes > this.maxBytes) {
      const oldest = this.entries.entries().next().value as [string, { file: OpenFile; bytes: number }] | undefined;
      if (!oldest) break;
      this.entries.delete(oldest[0]);
      this.totalBytes -= oldest[1].bytes;
    }
  }

  delete(key: string): void {
    const entry = this.entries.get(key);
    if (!entry) return;
    this.entries.delete(key);
    this.totalBytes -= entry.bytes;
  }

  clear(): void {
    this.entries.clear();
    this.totalBytes = 0;
  }
}

export function projectCanonicalFileContexts(value:string):{text:string;quotes:HistoricalFileQuote[]}{
  const quotes:HistoricalFileQuote[]=[];
  const text=value.replace(/(?:\n\n)?<ariel_file_context>\n([^\n]+)\n<\/ariel_file_context>/gu,(whole,body:string)=>{
    try{
      const parsed=JSON.parse(body) as Partial<HistoricalFileQuote>;
      if(typeof parsed.path!=="string"||!parsed.path||parsed.path.startsWith("/")||parsed.path.split("/").includes("..")||typeof parsed.sha256!=="string"||!/^[0-9a-f]{64}$/u.test(parsed.sha256)||!Number.isInteger(parsed.startLine)||!Number.isInteger(parsed.endLine)||(parsed.startLine||0)<1||(parsed.endLine||0)<(parsed.startLine||0)||typeof parsed.text!=="string"||!parsed.text)return whole;
      quotes.push(parsed as HistoricalFileQuote);return "";
    }catch{return whole;}
  }).trimEnd();
  return {text,quotes};
}

export function HistoricalFileQuotes({quotes}:{quotes:HistoricalFileQuote[]}){
  if(!quotes.length)return null;
  return <div className="historical-file-quotes" aria-label="已发送文件引用">{quotes.map((quote,index)=><span key={`${quote.path}:${quote.startLine}:${index}`}>{quote.path} · L{quote.startLine}{quote.endLine!==quote.startLine?`–${quote.endLine}`:""}</span>)}</div>;
}

export function webFileContextEnabled(): boolean {
  return document.querySelector<HTMLMetaElement>('meta[name="ariel-chilling-file-context"]')?.content === "true";
}

export function selectionLines(content: string, startOffset: number, endOffset: number): { startLine: number; endLine: number; text: string } | null {
  if (!Number.isInteger(startOffset) || !Number.isInteger(endOffset) || startOffset < 0 || endOffset <= startOffset || endOffset > content.length) return null;
  const text = content.slice(startOffset, endOffset);
  if (!text) return null;
  const startLine = content.slice(0, startOffset).split("\n").length;
  const newlineCount = (text.match(/\n/g) || []).length;
  const endLine = startLine + newlineCount - (text.endsWith("\n") && newlineCount > 0 ? 1 : 0);
  return { startLine, endLine, text };
}

function sourceOffset(root: HTMLElement, node: Node, offset: number): number | null {
  const element = node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement;
  const code = element?.closest<HTMLElement>("code[data-source-start]");
  if (!code || !root.contains(code)) return null;
  const base = Number(code.dataset.sourceStart);
  const lineLength = Number(code.dataset.sourceLength);
  if (!Number.isInteger(base) || !Number.isInteger(lineLength)) return null;
  try {
    const prefix = document.createRange();
    prefix.selectNodeContents(code);
    prefix.setEnd(node, offset);
    return base + Math.min(prefix.toString().length, lineLength);
  } catch {
    return null;
  }
}

export function selectionFromDOM(content:string, root:HTMLElement, selection:Selection | null):ReturnType<typeof selectionLines>{
  if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  const start = sourceOffset(root, range.startContainer, range.startOffset);
  const end = sourceOffset(root, range.endContainer, range.endOffset);
  if (start === null || end === null) return null;
  return selectionLines(content,Math.min(start,end),Math.max(start,end));
}

export function TurnFileChanges({ changes, onOpen }: { changes: FileChange[]; onOpen: (change: FileChange, mode:FileViewMode) => void }) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? changes : changes.slice(0, 5);
  if (!changes.length) return null;
  return <section className="turn-files" aria-label="修改的文件">
    <div className="turn-files-title"><span>修改的文件</span><small>{changes.length}</small></div>
    <div className="turn-file-list">{visible.map(change => <div className="turn-file-row" data-testid={`file-change:${change.path}`} key={`${change.itemId}:${change.path}`}>
      <div className="turn-file-path"><span className={`file-kind ${change.kind}`} aria-hidden="true">{change.kind === "add" ? "+" : change.kind === "delete" ? "−" : "~"}</span><span title={change.path}>{change.path}</span></div>
      <div className="turn-file-actions"><button type="button" onClick={() => onOpen(change,"source")} aria-label={`查看原文：${change.path}`} disabled={change.kind === "delete"}>原文</button><button type="button" onClick={() => onOpen(change,"diff")} aria-label={`查看 Diff：${change.path}`} disabled={!change.diff}>Diff</button></div>
    </div>)}</div>
    {changes.length > 5 && <button className="turn-files-more" type="button" onClick={() => setExpanded(value => !value)}>{expanded ? "收起" : `再看 ${changes.length - 5} 个`}</button>}
  </section>;
}

function diffClass(line:string):string{
  if (line.startsWith("@@")) return "diff-hunk";
  if (line.startsWith("+++ ") || line.startsWith("--- ") || line.startsWith("diff ") || line.startsWith("index ")) return "diff-header";
  if (line.startsWith("+")) return "diff-add";
  if (line.startsWith("-")) return "diff-delete";
  return "diff-context";
}

export function FileViewer({ view, onClose, onModeChange, onQuote, backLabel="返回会话", source="turn_change", showDiff=true }: { view: FileView; onClose: () => void; onModeChange:(mode:FileViewMode)=>void; onQuote: (quote: FileQuote) => void; backLabel?:string; source?:"turn_change"|"message_reference"|"workspace_browser"; showDiff?:boolean }) {
  const [selected, setSelected] = useState<{ startLine:number;endLine:number;text:string } | null>(null);
  const [selectionError,setSelectionError]=useState(false);
  const sourceRoot=useRef<HTMLDivElement>(null);
  const file = view.file;
  const lines = file?.content.split("\n") || [];
  const starts:number[]=[]; let nextStart=0; for(const line of lines){starts.push(nextStart);nextStart+=line.length+1;}
  useEffect(()=>{setSelected(null);setSelectionError(false);},[view.change.itemId,view.change.path,view.mode,file?.sha256]);
  useEffect(()=>{const key=(event:KeyboardEvent)=>{if(event.key==="Escape")onClose();};window.addEventListener("keydown",key);return()=>window.removeEventListener("keydown",key);},[onClose]);
  function quote(value: { startLine:number;endLine:number;text:string }) { if(file) onQuote({ source,threadId:file.threadId,...(file.turnId?{turnId:file.turnId}:{}),...(file.itemId?{itemId:file.itemId}:{}),path:file.path,sha256:file.sha256,modifiedAtMs:file.modifiedAtMs,...value,...(file.referenceKind&&file.rawReference?{referenceKind:file.referenceKind,rawReference:file.rawReference}:{}) }); }
  function captureSelection(root:HTMLElement|null) {
    if(!file||!root)return;
    const selection=window.getSelection();
    if(!selection||selection.isCollapsed)return;
    const value=selectionFromDOM(file.content,root,selection);
    if(!value){setSelectionError(true);return;}
    setSelectionError(false);setSelected(value);
  }
  useEffect(()=>{
    if(view.mode!=="source"||!file)return;
    const update=()=>captureSelection(sourceRoot.current);
    document.addEventListener("selectionchange",update);
    return()=>document.removeEventListener("selectionchange",update);
  },[view.mode,file?.sha256]);
  useEffect(()=>{
    if(view.mode!=="source"||!file||!view.focusStartLine)return;
    sourceRoot.current?.querySelector<HTMLElement>(`[data-line-number="${view.focusStartLine}"]`)?.scrollIntoView({block:"center"});
  },[view.mode,file?.sha256,view.focusStartLine]);
  return <div className="file-viewer-backdrop" role="presentation">
    <section className={`file-viewer${showDiff?"":" source-only"}`} role="dialog" aria-modal="true" aria-label={`文件 ${view.change.path}`}>
      <header className="file-viewer-header"><button className="file-back" type="button" onClick={onClose} aria-label={backLabel}><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m15 18-6-6 6-6"/></svg><span>返回</span></button><div className="file-heading"><strong>{view.change.path}</strong><small>{view.mode==="source"?`当前工作区版本${file?.language?` · ${file.language}`:""}`:`本次修改 · ${view.change.kind}`}</small></div><CircleButton className="file-close" type="button" onClick={onClose} aria-label="关闭文件"><CloseGlyph/></CircleButton></header>
      {showDiff&&<nav className="file-view-tabs" aria-label="文件视图"><button type="button" className={view.mode==="source"?"active":""} aria-pressed={view.mode==="source"} onClick={()=>onModeChange("source")} disabled={view.change.kind==="delete"}>查看原文</button><button type="button" className={view.mode==="diff"?"active":""} aria-pressed={view.mode==="diff"} onClick={()=>onModeChange("diff")} disabled={!view.change.diff}>查看 Diff</button></nav>}
      {view.mode==="source" ? view.loading||!file ? <div className="file-view-state" role="status">正在读取文件…</div> : <div ref={sourceRoot} className="file-code" data-testid="file-source" onMouseUp={event=>captureSelection(event.currentTarget)} onTouchEnd={event=>{const root=event.currentTarget;window.setTimeout(()=>captureSelection(root),0);}}>{lines.map((line,index)=>{const number=index+1;const focused=!!view.focusStartLine&&number>=view.focusStartLine&&number<=(view.focusEndLine||view.focusStartLine);return <div className={`file-line${focused?" reference-focus":""}`} data-line-number={number} data-testid="file-source-line" key={index}><span className="file-line-number" aria-hidden="true">{number}</span><code data-source-start={starts[index]} data-source-length={line.length}>{line || "\u200b"}</code></div>;})}</div> : <div className="file-diff-wrap">{view.change.diffTruncated&&<div className="file-diff-warning" role="status">Diff 已截断</div>}<div className="file-diff" data-testid="file-diff">{(view.change.diff||"").split("\n").map((line,index)=><code className={diffClass(line)} key={index}>{line||" "}</code>)}</div></div>}
      {view.mode==="source"&&<footer><span aria-live="polite">{selectionError?"无法识别该选区，请重新选择":selected ? `已选择 L${selected.startLine}${selected.endLine!==selected.startLine?`–L${selected.endLine}`:""}` : "选择要引用的代码片段"}</span><button type="button" className="primary" disabled={!selected||!file} onClick={()=>selected && quote(selected)}>引用到会话</button></footer>}
    </section>
  </div>;
}

export function FileQuoteChips({ quotes, onRemove }: { quotes: FileQuote[]; onRemove: (index:number)=>void }) {
  if (!quotes.length) return null;
  return <div className="file-quote-chips" aria-label="待发送文件引用">{quotes.map((quote,index)=><span className="file-quote-chip" key={`${quote.path}:${quote.startLine}:${index}`}><span>{quote.path} · L{quote.startLine}{quote.endLine!==quote.startLine?`–${quote.endLine}`:""}</span><button type="button" aria-label={`移除引用：${quote.path}`} onClick={()=>onRemove(index)}>×</button></span>)}</div>;
}
