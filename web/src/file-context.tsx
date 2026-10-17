import { useState, type MouseEvent } from "react";

export type FileChange = { itemId: string; path: string; kind: "add" | "update" | "delete"; diff?: string; diffTruncated?: boolean };
export type FileQuote = { threadId: string; turnId: string; itemId: string; path: string; sha256: string; modifiedAtMs: number; startLine: number; endLine: number; text: string };
export type OpenFile = { threadId: string; turnId: string; itemId: string; path: string; content: string; sha256: string; modifiedAtMs: number; lineCount: number; language: string; diff?: string; diffTruncated?: boolean; version: "current-workspace" };
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

export function TurnFileChanges({ changes, onOpen }: { changes: FileChange[]; onOpen: (change: FileChange) => void }) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? changes : changes.slice(0, 5);
  if (!changes.length) return null;
  return <section className="turn-files" aria-label="修改的文件">
    <div className="turn-files-title"><span>修改的文件</span><small>{changes.length}</small></div>
    <div className="turn-file-list">{visible.map(change => <button key={`${change.itemId}:${change.path}`} type="button" onClick={() => onOpen(change)} aria-label={`打开 ${change.path}`} disabled={change.kind === "delete"}>
      <span className={`file-kind ${change.kind}`} aria-hidden="true">{change.kind === "add" ? "+" : change.kind === "delete" ? "−" : "~"}</span><span>{change.path}</span>
    </button>)}</div>
    {changes.length > 5 && <button className="turn-files-more" type="button" onClick={() => setExpanded(value => !value)}>{expanded ? "收起" : `再看 ${changes.length - 5} 个`}</button>}
  </section>;
}

export function FileViewer({ file, onClose, onQuote }: { file: OpenFile; onClose: () => void; onQuote: (quote: FileQuote) => void }) {
  const [selected, setSelected] = useState<{ startLine:number;endLine:number;text:string } | null>(null);
  const lines = file.content.split("\n");
  function quote(value: { startLine:number;endLine:number;text:string }) { onQuote({ threadId:file.threadId,turnId:file.turnId,itemId:file.itemId,path:file.path,sha256:file.sha256,modifiedAtMs:file.modifiedAtMs,...value }); }
  function captureSelection(event: MouseEvent<HTMLElement>) {
    const selection = window.getSelection(); const text = selection?.toString() || ""; if (!text || !event.currentTarget.contains(selection?.anchorNode || null) || !event.currentTarget.contains(selection?.focusNode || null)) return;
    const offset = file.content.indexOf(text); if (offset < 0) return; setSelected(selectionLines(file.content,offset,offset+text.length));
  }
  return <div className="file-viewer-backdrop" role="presentation">
    <section className="file-viewer" role="dialog" aria-modal="true" aria-label={`文件 ${file.path}`}>
      <header><div><strong>{file.path}</strong><small>当前工作区版本 · {file.language}</small></div><button type="button" onClick={onClose} aria-label="关闭文件">×</button></header>
      <div className="file-code" onMouseUp={captureSelection} onTouchEnd={event => captureSelection(event as unknown as MouseEvent<HTMLElement>)}>{lines.map((line,index)=><div className="file-line" key={index}><button type="button" className="file-line-number" aria-label={`引用第 ${index+1} 行`} onClick={()=>quote({startLine:index+1,endLine:index+1,text:line})}>{index+1}</button><code>{line || " "}</code></div>)}</div>
      <footer><span>{selected ? `已选择 L${selected.startLine}–L${selected.endLine}` : "选择代码片段，或点击行号引用整行"}</span><button type="button" className="primary" disabled={!selected} onClick={()=>selected && quote(selected)}>引用到对话</button></footer>
    </section>
  </div>;
}

export function FileQuoteChips({ quotes, onRemove }: { quotes: FileQuote[]; onRemove: (index:number)=>void }) {
  if (!quotes.length) return null;
  return <div className="file-quote-chips" aria-label="待发送文件引用">{quotes.map((quote,index)=><span className="file-quote-chip" key={`${quote.path}:${quote.startLine}:${index}`}><span>{quote.path} · L{quote.startLine}{quote.endLine!==quote.startLine?`–${quote.endLine}`:""}</span><button type="button" aria-label={`移除引用：${quote.path}`} onClick={()=>onRemove(index)}>×</button></span>)}</div>;
}
