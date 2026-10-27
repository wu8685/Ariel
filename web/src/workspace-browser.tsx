import { useEffect, useMemo, useRef, useState } from "react";
import { CircleButton, BackGlyph, RefreshGlyph } from "./circular-button";
import { FileContentCache, FileViewer, type FileQuote, type FileView, type OpenFile } from "./file-context";
import type { AssetContext } from "./document-assets";
import { resolveDocumentPath } from "./document-path";

export type WorkspaceEntry={name:string;path:string;kind:"file"|"directory"|"symlink";sizeBytes:number;modifiedAtMs:number;readable:boolean};
export type WorkspacePage={path:string;entries:WorkspaceEntry[];nextCursor?:string;revision:string};
type Reply={outcome:string;data?:Record<string,unknown>;error?:{code?:string;message?:string}};
type Request=(method:"workspace.files.list"|"workspace.file.read",deviceId:string,params:Record<string,unknown>)=>Promise<Reply>;

export class WorkspaceDirectoryCache{
  private entries=new Map<string,{base:string;page:WorkspacePage;bytes:number}>();private latest=new Map<string,string>();private total=0;
  constructor(private maxEntries=32,private maxBytes=2*1024*1024){}
  get size(){return this.entries.size} get bytes(){return this.total}
  get(base:string){const key=this.latest.get(base);if(!key)return;const found=this.entries.get(key);if(!found){this.latest.delete(base);return}this.entries.delete(key);this.entries.set(key,found);return found.page}
  set(base:string,page:WorkspacePage){const bytes=new TextEncoder().encode(JSON.stringify(page)).byteLength;if(bytes>this.maxBytes)return;this.delete(base);const key=`${base}\0${page.revision}`;this.entries.set(key,{base,page,bytes});this.latest.set(base,key);this.total+=bytes;while(this.entries.size>this.maxEntries||this.total>this.maxBytes){const first=this.entries.entries().next().value as [string,{base:string;page:WorkspacePage;bytes:number}]|undefined;if(!first)break;this.entries.delete(first[0]);if(this.latest.get(first[1].base)===first[0])this.latest.delete(first[1].base);this.total-=first[1].bytes}}
  delete(base:string){const key=this.latest.get(base);if(!key)return;const found=this.entries.get(key);this.latest.delete(base);if(!found)return;this.entries.delete(key);this.total-=found.bytes}
  clear(){this.entries.clear();this.latest.clear();this.total=0}
}

function validPage(data:Record<string,unknown>|undefined):WorkspacePage|null{
  if(!data||typeof data.path!=="string"||typeof data.revision!=="string"||!Array.isArray(data.entries))return null;
  const entries=data.entries.filter((entry):entry is WorkspaceEntry=>!!entry&&typeof entry==="object"&&typeof (entry as WorkspaceEntry).name==="string"&&typeof (entry as WorkspaceEntry).path==="string"&&["file","directory","symlink"].includes((entry as WorkspaceEntry).kind)&&typeof (entry as WorkspaceEntry).readable==="boolean");
  if(entries.length!==data.entries.length)return null;
  return {path:data.path,revision:data.revision,entries,...(typeof data.nextCursor==="string"?{nextCursor:data.nextCursor}:{})};
}
function validFile(data:Record<string,unknown>|undefined,threadId:string):OpenFile|null{
  if(!data||typeof data.path!=="string"||typeof data.content!=="string"||typeof data.sha256!=="string"||typeof data.modifiedAtMs!=="number"||typeof data.lineCount!=="number"||typeof data.language!=="string"||data.version!=="current-workspace")return null;
  return {source:"workspace_browser",threadId,path:data.path,content:data.content,sha256:data.sha256,modifiedAtMs:data.modifiedAtMs,lineCount:data.lineCount,language:data.language,version:"current-workspace",documentAssets:data.documentAssets as OpenFile["documentAssets"]};
}

export function WorkspaceBrowser({deviceId,agentEpoch,threadId,cwd,request,cache,fileCache,onClose,onQuote,assetContext,onOpenFile}:{deviceId:string;agentEpoch:string;threadId:string;cwd:string;request:Request;cache:WorkspaceDirectoryCache;fileCache:FileContentCache;assetContext?:AssetContext;onClose:()=>void;onQuote:(quote:FileQuote)=>void;onOpenFile?:(path:string)=>void}){
  const [path,setPath]=useState("");const [page,setPage]=useState<WorkspacePage|null>(null);const [loading,setLoading]=useState(false);const [error,setError]=useState("");const [file,setFile]=useState<FileView|null>(null);
  const inFlight=useRef(new Map<string,Promise<Reply>>());
  const alive=useRef(true),directoryGeneration=useRef(0),fileGeneration=useRef(0);
  useEffect(()=>{alive.current=true;return()=>{alive.current=false;directoryGeneration.current++;fileGeneration.current++}},[]);
  const project=useMemo(()=>cwd.split("/").filter(Boolean).at(-1)||"工作区",[cwd]);
  const cacheKey=(value:string)=>`${deviceId}\0${agentEpoch}\0${threadId}\0${value}`;
  async function load(target:string,cursor="",append=false){
    const requestKey=`${cacheKey(target)}\0${cursor}`;
    const generation=++directoryGeneration.current;
    const current=()=>alive.current&&generation===directoryGeneration.current;
    if(!append){const cached=cache.get(cacheKey(target));if(cached)setPage(cached)}
    setLoading(true);setError("");let reply:Reply;
    let pending=inFlight.current.get(requestKey);
    if(!pending){pending=Promise.resolve().then(()=>request("workspace.files.list",deviceId,{threadId,path:target,limit:100,...(cursor?{cursor}:{})}));inFlight.current.set(requestKey,pending)}
    try{reply=await pending}catch{if(current())setError("无法读取此目录。");return}finally{if(inFlight.current.get(requestKey)===pending)inFlight.current.delete(requestKey);if(current())setLoading(false)}
    if(!current())return;
    if(reply.outcome!=="accepted"){setError(reply.error?.code==="DIRECTORY_CHANGED"?"目录已变化，请刷新。":"无法读取此目录。");return}
    const next=validPage(reply.data);if(!next||next.path!==target){setError("目录响应格式无法识别。");return}
    const merged=append&&page&&page.path===target&&page.revision===next.revision?{...next,entries:[...page.entries,...next.entries]}:next;setPage(merged);cache.set(cacheKey(target),merged);
  }
  useEffect(()=>{setPage(cache.get(cacheKey(path))||null);void load(path)},[deviceId,agentEpoch,threadId,path]);
  async function open(entry:WorkspaceEntry,preserve=false){
    if(!entry.readable)return;if(entry.kind==="directory"){fileGeneration.current++;setPath(entry.path);return}if(entry.kind!=="file")return;
    if(onOpenFile){onOpenFile(entry.path);return}
    const generation=++fileGeneration.current;
    const current=()=>alive.current&&generation===fileGeneration.current;
    const key=cacheKey(`file\0${entry.path}`);const cached=fileCache.get(key);const base:FileView={threadId,turnId:"",change:{itemId:"",path:entry.path,kind:"update"},mode:"source"};
    if(cached){setFile({...base,file:cached});return}
    if(!preserve)setFile({...base,loading:true});let reply:Reply;
    try{reply=await request("workspace.file.read",deviceId,{threadId,path:entry.path})}catch{if(!current())return;if(preserve)throw new Error("无法读取此文档");setFile(null);setError("无法读取此文件。");return}
    if(!current())return;
    const opened=validFile(reply.data,threadId);if(reply.outcome!=="accepted"||!opened||opened.path!==entry.path){if(preserve)throw new Error("无法读取此文档");setFile(null);setError("无法读取此文件。");return}fileCache.set(key,opened);setFile({...base,file:opened})
  }
  const closeFile=()=>{fileGeneration.current++;setFile(null)};
  const closeBrowser=()=>{fileGeneration.current++;directoryGeneration.current++;alive.current=false;onClose()};
  if(file)return <div className="workspace-browser workspace-file-open"><FileViewer view={file} assetContext={assetContext} onOpenDocument={async target=>{const resolved=resolveDocumentPath(file.change.path,target);if(!resolved)throw new Error("文档路径不允许读取");await open({name:resolved.path.split("/").at(-1)||resolved.path,path:resolved.path,kind:"file",sizeBytes:0,modifiedAtMs:0,readable:true},true)}} source="workspace_browser" showDiff={false} backLabel="返回文件列表" onClose={closeFile} onModeChange={()=>{}} onQuote={quote=>{onQuote(quote);closeBrowser()}}/></div>;
  const crumbs=path?path.split("/"):[];
  return <section className="workspace-browser" role="dialog" aria-modal="true" aria-label="工作区文件">
    <header><CircleButton type="button" className="workspace-close" aria-label="返回会话" onClick={closeBrowser}><BackGlyph/></CircleButton><div><strong>{project}</strong><small>工作区文件</small></div><CircleButton type="button" className="workspace-refresh" aria-label="刷新目录" onClick={()=>void load(path)}><RefreshGlyph/></CircleButton></header>
    <nav className="workspace-breadcrumb" aria-label="文件路径"><button type="button" onClick={()=>setPath("")}>{project}</button>{crumbs.map((crumb,index)=><span key={`${crumb}:${index}`}>/ <button type="button" onClick={()=>setPath(crumbs.slice(0,index+1).join("/"))}>{crumb}</button></span>)}</nav>
    <div className="workspace-tree">{page?.entries.map(entry=><button type="button" key={entry.path} className={`workspace-entry ${entry.kind}`} aria-label={`${entry.kind==="directory"?"打开目录":entry.kind==="file"?"打开文件":"不可打开"} ${entry.name}`} disabled={!entry.readable} onClick={()=>void open(entry)}><span aria-hidden="true">{entry.kind==="directory"?"▸":entry.kind==="file"?"·":"↗"}</span><span>{entry.name}</span>{entry.kind==="file"&&<small>{entry.sizeBytes} B</small>}</button>)}{loading&&<p role="status">正在读取目录…</p>}{error&&<p role="alert">{error}</p>}{!loading&&!error&&page?.entries.length===0&&<p>此目录为空</p>}</div>
    {page?.nextCursor&&<button className="workspace-more" type="button" aria-label="加载更多" disabled={loading} onClick={()=>void load(path,page.nextCursor,true)}>加载更多</button>}
  </section>;
}
