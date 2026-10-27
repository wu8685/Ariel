import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { CircleButton, CloseGlyph } from "./circular-button";
import { DocumentReading } from "./document-reading";
import { DocumentImageSession, type AssetContext } from "./document-assets";

export type FileChange = { itemId: string; path: string; kind: "add" | "update" | "delete"; diff?: string; diffTruncated?: boolean };
export type FileQuote = { source?: "turn_change" | "message_reference" | "workspace_browser"; threadId: string; turnId?: string; itemId?: string; path: string; sha256: string; modifiedAtMs: number; startLine: number; endLine: number; text: string; referenceKind?: "markdown_link" | "plain_path"; rawReference?: string };
export type OpenFile = { documentAssets?: {version:number;maxBytes:number;chunkBytes:number}; source?: "turn_change" | "message_reference" | "workspace_browser"; threadId: string; turnId?: string; itemId?: string; path: string; content: string; sha256: string; modifiedAtMs: number; lineCount: number; language: string; diff?: string; diffTruncated?: boolean; version: "current-workspace"; referenceKind?: "markdown_link" | "plain_path"; rawReference?: string };
export type FileViewMode = "source" | "diff";
export type FileView = { threadId:string;turnId:string;change:FileChange;mode:FileViewMode;file?:OpenFile;loading?:boolean;focusStartLine?:number;focusEndLine?:number };
export type FileReaderState = { reading:boolean; positions:{source:number;reading:number;diff:number}; identity?:string;sha256?:string; selection?:{start:number;end:number}; focusApplied?:boolean };
export const emptyFileReaderState=():FileReaderState=>({reading:false,positions:{source:0,reading:0,diff:0}});
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
  const current=document.querySelector<HTMLMetaElement>('meta[name="ariel-file-context"]')?.content;
  return (current && !current.startsWith("__") ? current : document.querySelector<HTMLMetaElement>('meta[name="ariel-chilling-file-context"]')?.content) === "true";
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
  const code = element?.closest<HTMLElement>("[data-source-start]");
  if (!code || !root.contains(code)) return null;
  const base = Number(code.dataset.sourceStart);
  const lineLength = Number(code.dataset.sourceLength);
  if (!Number.isInteger(base) || !Number.isInteger(lineLength)) return null;
  try {
    const prefix = document.createRange();
    prefix.selectNodeContents(code);
    prefix.setEnd(node, offset);
    const length=Math.min(prefix.toString().length,lineLength);
    const boundaries=code.dataset.sourceBoundaries?JSON.parse(code.dataset.sourceBoundaries) as number[]:null;
    return base + (boundaries?boundaries[length]:length);
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

export function FileViewer({ view, onClose, onModeChange, onQuote, backLabel="返回会话", source="turn_change", showDiff=true, assetContext, onOpenDocument, embedded=false, readerState, quoteDisabled=false, diffAvailable=!!view.change.diff }: { view: FileView; onClose: () => void; onModeChange:(mode:FileViewMode)=>void; onQuote: (quote: FileQuote) => void; backLabel?:string; source?:"turn_change"|"message_reference"|"workspace_browser"; showDiff?:boolean; assetContext?:AssetContext; onOpenDocument?:(path:string)=>void|Promise<void>;embedded?:boolean;readerState?:FileReaderState;quoteDisabled?:boolean;diffAvailable?:boolean }) {
  const internalState=useRef(emptyFileReaderState());
  const saved=readerState||internalState.current;
  const [selected,setSelected]=useState<{startLine:number;endLine:number;text:string}|null>(()=>saved.selection&&view.file&&saved.sha256===view.file.sha256?selectionLines(view.file.content,saved.selection.start,saved.selection.end):null);
  const [selectionError,setSelectionError]=useState(false);
  const [linkError,setLinkError]=useState("");
  const relativeHandler=useRef(onOpenDocument);relativeHandler.current=onOpenDocument;
  const openRelative=useCallback((path:string)=>{setLinkError("");const open=relativeHandler.current;if(open)void Promise.resolve(open(path)).catch(error=>setLinkError(error instanceof Error?error.message:"无法读取文档"));},[]);
  const [reading,setReading]=useState(saved.reading);
  const [versionChanged,setVersionChanged]=useState(false);
  const [allowed,setAllowed]=useState<ReadonlySet<string>>(new Set());
  const allowExternal=useCallback((domain:string)=>setAllowed(current=>new Set([...current,domain])),[]);
  const sourceRoot=useRef<HTMLDivElement>(null);
  const diffRoot=useRef<HTMLDivElement>(null);
  const quoteBar=useRef<HTMLElement>(null);
  const ownsDOMSelection=useRef(false);
  const positions=useRef(saved.positions);
  const file=view.file;
  const markdown=/\.(?:md|markdown)$/iu.test(view.change.path);
  const imageIdentity=useMemo(()=>({}),[file,source,assetContext?.agentEpoch,assetContext?.relayVersion,assetContext?.request]);
  const [imageState,setImageState]=useState<{identity:object;session:DocumentImageSession}|null>(null);
  const session=imageState?.identity===imageIdentity?imageState.session:null;
  useEffect(()=>{
    if(!file){setImageState(null);return}
    const current=new DocumentImageSession({...file,source},assetContext);
    setImageState({identity:imageIdentity,session:current});
    return()=>current.dispose();
  },[imageIdentity]);
  const lines=file?.content.split("\n")||[];
  const starts:number[]=[];let start=0;for(const line of lines){starts.push(start);start+=line.length+1;}
  useLayoutEffect(()=>{
    if(!file)return;
    const identity=JSON.stringify([view.threadId,view.turnId,view.change.itemId,view.change.path]);
    const changed=saved.sha256&&saved.sha256!==file.sha256;
    if(changed||saved.identity&&saved.identity!==identity){saved.reading=false;saved.selection=undefined;saved.focusApplied=false;saved.positions.source=0;saved.positions.reading=0;saved.positions.diff=0;setReading(false);setVersionChanged(!!changed);setAllowed(new Set());}
    saved.identity=identity;
    saved.sha256=file.sha256;
    ownsDOMSelection.current=false;
    setSelectionError(false);
    setSelected(saved.selection?selectionLines(file.content,saved.selection.start,saved.selection.end):null);
  },[file?.sha256,view.threadId,view.turnId,view.change.itemId,view.change.path]);
  useEffect(()=>{if(embedded)return;const key=(event:KeyboardEvent)=>{if(event.key==="Escape")onClose()};window.addEventListener("keydown",key);return()=>window.removeEventListener("keydown",key)},[onClose,embedded]);
  useLayoutEffect(()=>{if(sourceRoot.current)sourceRoot.current.scrollTop=positions.current[reading?"reading":"source"];if(diffRoot.current)diffRoot.current.scrollTop=positions.current.diff},[view.mode,reading,file?.sha256,session]);
  useLayoutEffect(()=>{const element=view.mode==="diff"?diffRoot.current:sourceRoot.current,key=view.mode==="diff"?"diff":reading?"reading":"source";return()=>{if(element&&element.scrollHeight>element.clientHeight)positions.current[key]=element.scrollTop}},[view.mode,reading]);

  function quote(value:{startLine:number;endLine:number;text:string}){
    if(file)onQuote({source,threadId:file.threadId,...(file.turnId?{turnId:file.turnId}:{}),...(file.itemId?{itemId:file.itemId}:{}),path:file.path,sha256:file.sha256,modifiedAtMs:file.modifiedAtMs,...value,...(file.referenceKind&&file.rawReference?{referenceKind:file.referenceKind,rawReference:file.rawReference}:{})});
  }
  function captureSelection(root:HTMLElement|null, clearEmpty=false){
    if(!file||!root)return;
    const selection=window.getSelection();
    if(!selection||selection.isCollapsed||!selection.rangeCount){
      // A content gesture/caret cancels the captured quote. Empty selection
      // events outside the content must not discard a restored Tab selection.
      const quoteFocused=quoteBar.current?.contains(document.activeElement);
      const caretInside=selection?.anchorNode&&root.contains(selection.anchorNode);
      if(clearEmpty||!quoteFocused&&(caretInside||ownsDOMSelection.current&&!selection?.rangeCount)){
        ownsDOMSelection.current=false;saved.selection=undefined;setSelected(null);setSelectionError(false);
      }
      return;
    }
    const currentRange=selection.getRangeAt(0);
    if(!root.contains(currentRange.startContainer)&&!root.contains(currentRange.endContainer)){ownsDOMSelection.current=false;return;}
    ownsDOMSelection.current=true;
    const value=selectionFromDOM(file.content,root,selection);
    if(!value){setSelected(null);saved.selection=undefined;setSelectionError(true);return}
    const range=selection.getRangeAt(0),start=sourceOffset(root,range.startContainer,range.startOffset),end=sourceOffset(root,range.endContainer,range.endOffset);
    saved.selection=start===null||end===null?undefined:{start:Math.min(start,end),end:Math.max(start,end)};
    setSelectionError(false);setSelected(value);
  }
  useEffect(()=>{
    if(view.mode!=="source"||!file)return;const update=()=>captureSelection(sourceRoot.current);
    document.addEventListener("selectionchange",update);return()=>document.removeEventListener("selectionchange",update);
  },[view.mode,reading,file?.sha256]);
  useEffect(()=>{
    if(view.mode!=="source"||!file||!view.focusStartLine||reading||saved.focusApplied)return;
    sourceRoot.current?.querySelector<HTMLElement>("[data-line-number=\""+view.focusStartLine+"\"]")?.scrollIntoView({block:"center"});
    saved.focusApplied=true;
  },[view.mode,reading,file?.sha256,view.focusStartLine]);
  function changeReading(value:boolean){ownsDOMSelection.current=false;saved.selection=undefined;saved.reading=value;setSelected(null);setSelectionError(false);setReading(value)}
  return <div className={"file-viewer-backdrop"+(embedded?" embedded":"")} role="presentation">
    <section className={"file-viewer"+(!showDiff&&!markdown?" source-only":"")} role="dialog" aria-modal={embedded?undefined:true} aria-label={"文件 "+view.change.path}>
      <header className="file-viewer-header"><button className="file-back" type="button" onClick={onClose} aria-label={backLabel}><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m15 18-6-6 6-6"/></svg><span>返回</span></button><div className="file-heading"><strong>{view.change.path}</strong><small>{view.mode==="source"?"当前工作区版本"+(file?.language?" · "+file.language:""):"本次修改 · "+view.change.kind}</small></div><CircleButton className="file-close" type="button" onClick={onClose} aria-label="关闭文件"><CloseGlyph/></CircleButton></header>
      {(showDiff||markdown)&&<nav className="file-view-tabs" aria-label="文件视图">
        {showDiff&&<><button type="button" className={view.mode==="source"?"active":""} aria-pressed={view.mode==="source"} onClick={()=>onModeChange("source")} disabled={view.change.kind==="delete"}>查看原文</button><button type="button" className={view.mode==="diff"?"active":""} aria-pressed={view.mode==="diff"} onClick={()=>onModeChange("diff")} disabled={!diffAvailable}>查看 Diff</button></>}
        {markdown&&view.mode==="source"&&<div className="file-reading-modes" role="group" aria-label="内容展示模式"><button type="button" className={!reading?"active":""} aria-label="原版模式" aria-pressed={!reading} onClick={()=>changeReading(false)}>原版</button><button type="button" className={reading?"active":""} aria-label="阅读模式" aria-pressed={reading} onClick={()=>changeReading(true)} disabled={!file}>阅读</button></div>}
      </nav>}
      {view.mode==="source"?view.loading||!file?<div className="file-view-state" role="status">正在读取文件…</div>:
        <div ref={sourceRoot} className={reading?"file-reading markdown-body":"file-code"} data-testid={reading?"file-reading":"file-source"} onScroll={event=>{positions.current[reading?"reading":"source"]=event.currentTarget.scrollTop}} onMouseUp={event=>captureSelection(event.currentTarget,true)} onTouchEnd={event=>{const root=event.currentTarget;window.setTimeout(()=>captureSelection(root,true),0)}}>
          {linkError&&<p role="alert">{linkError}</p>}{versionChanged&&<p role="status">文件已变化，旧选区已清除。</p>}
          {reading?session?<DocumentReading content={file.content} session={session} allowed={allowed} onAllow={allowExternal} onOpenDocument={onOpenDocument?openRelative:undefined}/>:<p role="status">正在准备阅读模式…</p>:
          lines.map((line,index)=>{const number=index+1;const focused=!!view.focusStartLine&&number>=view.focusStartLine&&number<=(view.focusEndLine||view.focusStartLine);return <div className={"file-line"+(focused?" reference-focus":"")} data-line-number={number} data-testid="file-source-line" key={index}><span className="file-line-number" aria-hidden="true">{number}</span><code data-source-start={starts[index]} data-source-length={line.length}>{line||"\u200b"}</code></div>})}
        </div>:
        <div className="file-diff-wrap">{view.change.diffTruncated&&<div className="file-diff-warning" role="status">Diff 已截断</div>}<div ref={diffRoot} className="file-diff" data-testid="file-diff" onScroll={event=>{positions.current.diff=event.currentTarget.scrollTop}}>{(view.change.diff||"").split("\n").map((line,index)=><code className={diffClass(line)} key={index}>{line||" "}</code>)}</div></div>}
      {view.mode==="source"&&file&&!view.loading&&(selected||selectionError)&&<footer ref={quoteBar} role="region" aria-label="文件引用"><span aria-live="polite">{quoteDisabled?"切回来源会话后可引用":selectionError?(reading?"无法映射原文，请切换到原版精确选择":"无法识别该选区，请重新选择"):selected?"已选择 L"+selected.startLine+(selected.endLine!==selected.startLine?"–L"+selected.endLine:"")+(reading?" · 引用对应原文片段":""):""}</span><button type="button" className="primary" disabled={quoteDisabled||!selected} onClick={()=>!quoteDisabled&&selected&&quote(selected)}>引用到会话</button></footer>}
    </section>
  </div>;
}

export function FileQuoteChips({ quotes, onRemove }: { quotes: FileQuote[]; onRemove: (index:number)=>void }) {
  if (!quotes.length) return null;
  return <div className="file-quote-chips" aria-label="待发送文件引用">{quotes.map((quote,index)=><span className="file-quote-chip" key={`${quote.path}:${quote.startLine}:${index}`}><span>{quote.path} · L{quote.startLine}{quote.endLine!==quote.startLine?`–${quote.endLine}`:""}</span><button type="button" aria-label={`移除引用：${quote.path}`} onClick={()=>onRemove(index)}>×</button></span>)}</div>;
}
