import { Component,memo,useEffect,useMemo,useRef,useState,type ReactNode } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { classifyImageSource,type DocumentImageSession,type LoadedImage } from "./document-assets";

type HastNode={type:string;tagName?:string;value?:string;properties?:Record<string,unknown>;children?:HastNode[];position?:{start:{offset?:number};end:{offset?:number}}};

function boundaries(raw:string,value:string):number[]|null{
  if(raw.length>8192)return null;const map=[0];let r=0,i=0;
  while(r<raw.length&&i<value.length){
    let decoded=raw[r],length=1;
    if(raw[r]==="\\"&&/[!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~]/u.test(raw[r+1]||"")){decoded=raw[r+1];length=2}
    else if(raw[r]==="\r"&&raw[r+1]==="\n"){decoded="\n";length=2}
    else if(raw[r]==="&"){
      const match=raw.slice(r).match(/^&(?:#(\d+)|#x([a-f\d]+)|(amp|lt|gt|quot|apos|nbsp));/iu);
      if(match){const code=match[1]?Number(match[1]):match[2]?parseInt(match[2],16):null;decoded=code!==null&&code>0&&code<=0x10ffff?String.fromCodePoint(code):({amp:"&",lt:"<",gt:">",quot:'"',apos:"'",nbsp:"\u00a0"}[match[3]?.toLowerCase() as "amp"]||"");length=match[0].length}
    }
    if(!decoded||value.slice(i,i+decoded.length)!==decoded)return null;
    for(let n=1;n<=decoded.length;n++)map.push(n===decoded.length?r+length:r);
    r+=length;i+=decoded.length;
  }
  return r===raw.length&&i===value.length?map:null;
}

function sourcePositions(source:string){
  return ()=> (tree:unknown)=>{
    const walk=(node:HastNode)=>{
      if(!node.children)return;
      node.children=node.children.map(child=>{
        if(child.type!=="text"||!child.position||typeof child.value!=="string"){walk(child);return child}
        const start=child.position.start.offset,end=child.position.end.offset;
        if(start===undefined||end===undefined)return child;
        const raw=source.slice(start,end),map=raw===child.value?null:boundaries(raw,child.value);
        const properties:Record<string,unknown>=raw===child.value||map?{"data-source-start":start,"data-source-length":child.value.length,...(map?{"data-source-boundaries":JSON.stringify(map)}:{})}:{"data-source-unmapped":"true"};
        return {type:"element",tagName:"span",properties,children:[child]};
      });
    };walk(tree as HastNode);
  };
}

function DocumentImage({source,alt,session,allowed,onAllow}:{source:string;alt:string;session:DocumentImageSession;allowed:ReadonlySet<string>;onAllow:(domain:string)=>void}){
  const holder=useRef<HTMLSpanElement>(null),policy=useMemo(()=>classifyImageSource(source),[source]);
  const [near,setNear]=useState(false),[loaded,setLoaded]=useState<LoadedImage|null>(null),[error,setError]=useState("");
  useEffect(()=>{
    const element=holder.current;if(!element||typeof IntersectionObserver==="undefined")return;
    const observer=new IntersectionObserver(entries=>setNear(entries.some(entry=>entry.isIntersecting)),{rootMargin:"240px"});observer.observe(element);return()=>observer.disconnect();
  },[]);
  useEffect(()=>{
    if(!near||policy.kind!=="local"&&policy.kind!=="data")return;
    const controller=new AbortController();let resource:LoadedImage|undefined;
    setError("");void session.acquire(source,controller.signal).then(value=>{if(controller.signal.aborted){value.release();return}resource=value;setLoaded(value)},reason=>{if(!controller.signal.aborted)setError(reason instanceof Error?reason.message:"无法读取图片")});
    return()=>{controller.abort();resource?.release();setLoaded(null)};
  },[near,session,source,policy.kind]);
  const name=alt||"文档图片";
  return <span className="document-image" ref={holder}>
    {policy.kind==="blocked"?<span role="status">{name}：{policy.reason}</span>:policy.kind==="external"?
      allowed.has(policy.domain)&&near?<img src={policy.url} alt={name} loading="lazy" referrerPolicy="no-referrer" onError={()=>setError("外部图片加载失败")}/>:<span className="document-image-placeholder"><span>{name} · {policy.domain}</span><button type="button" onClick={()=>{onAllow(policy.domain);setNear(true)}}>允许加载外部图片</button></span>:
      loaded?<img src={loaded.url} alt={name} width={loaded.width} height={loaded.height}/>:<span className="document-image-placeholder" role="status">{name} · {error|| (near?"正在加载图片…":"图片按需加载")} {!near&&<button type="button" onClick={()=>setNear(true)}>加载图片</button>}</span>}
    {error&&policy.kind==="external"&&<span role="status">{error}</span>}
  </span>;
}

class ReadingErrorBoundary extends Component<{children:ReactNode},{failed:boolean}>{
  state={failed:false};static getDerivedStateFromError(){return {failed:true}}
  render(){return this.state.failed?<p role="status">Markdown 无法解析，请切换到原版查看。</p>:this.props.children}
}

export const DocumentReading=memo(function DocumentReading({content,session,allowed,onAllow,onOpenDocument}:{content:string;session:DocumentImageSession;allowed:ReadonlySet<string>;onAllow:(domain:string)=>void;onOpenDocument?:(path:string)=>void}){
  const positions=useMemo(()=>sourcePositions(content),[content]);
  if(content.length>256*1024)return <p role="status">文档超过阅读渲染预算，请切换到原版查看完整内容。</p>;
  return <ReadingErrorBoundary key={content}><Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[positions]} skipHtml urlTransform={value=>value} components={{
    img({src,alt}){return <DocumentImage source={src||""} alt={alt||""} session={session} allowed={allowed} onAllow={onAllow}/>},
    a({href,children}){
      if(!href||/[\u0000-\u001f\u007f\\]/u.test(href))return <span>{children}</span>;
      if(/^#[a-z\d_:-]+$/iu.test(href))return <a href={href}>{children}</a>;
      if(/^(?:https?:\/\/|mailto:)/iu.test(href)){try{const url=new URL(href);if(!url.username&&!url.password)return <a href={url.href} target="_blank" rel="noopener noreferrer">{children}</a>}catch{/* invalid URL stays text */}}
      if(!/^(?:[a-z][a-z\d+.-]*:|\/)/iu.test(href)&&onOpenDocument)return <button className="workspace-document-reference" type="button" onClick={()=>onOpenDocument(href)}>{children}</button>;
      return <span>{children}</span>;
    },
    table({children}){return <div className="markdown-table-scroll" role="region" aria-label="表格，可横向滚动" tabIndex={0}><table>{children}</table></div>}
  }}>{content}</Markdown></ReadingErrorBoundary>;
});
