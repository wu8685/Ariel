import { Component,createContext,memo,useContext,useMemo,type ReactNode } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { type DocumentImageSession } from "./document-assets";
import { DocumentImage } from "./markdown-images";
import { remarkMath, mathSpan } from "./markdown-math";

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

class ReadingErrorBoundary extends Component<{children:ReactNode},{failed:boolean}>{
  state={failed:false};static getDerivedStateFromError(){return {failed:true}}
  render(){return this.state.failed?<p role="status">Markdown 无法解析，请切换到原版查看。</p>:this.props.children}
}

type ReadingContextValue={session:DocumentImageSession;allowed:ReadonlySet<string>;onAllow:(domain:string)=>void;onOpenDocument?:(path:string)=>void};
const ReadingContext=createContext<ReadingContextValue|null>(null);
const components:Components={
    span:mathSpan,
    img({src,alt}){const context=useContext(ReadingContext)!;return <DocumentImage source={src||""} alt={alt||""} {...context}/>},
    a({href,children}){
      const {onOpenDocument}=useContext(ReadingContext)!;
      if(!href||/[\u0000-\u001f\u007f\\]/u.test(href))return <span>{children}</span>;
      if(/^#[a-z\d_:-]+$/iu.test(href))return <a href={href}>{children}</a>;
      if(/^(?:https?:\/\/|mailto:)/iu.test(href)){try{const url=new URL(href);if(!url.username&&!url.password)return <a href={url.href} target="_blank" rel="noopener noreferrer">{children}</a>}catch{/* invalid URL stays text */}}
      if(!/^(?:[a-z][a-z\d+.-]*:|\/)/iu.test(href)&&onOpenDocument)return <button className="workspace-document-reference" type="button" onClick={()=>onOpenDocument(href)}>{children}</button>;
      return <span>{children}</span>;
    },
    table({children}){return <div className="markdown-table-scroll" role="region" aria-label="表格，可横向滚动" tabIndex={0}><table>{children}</table></div>}
};
const plugins=[remarkGfm,remarkMath];
export const DocumentReading=memo(function DocumentReading({content,...context}:{content:string}&ReadingContextValue){
  const positions=useMemo(()=>sourcePositions(content),[content]);
  if(content.length>256*1024)return <p role="status">文档超过阅读渲染预算，请切换到原版查看完整内容。</p>;
  return <ReadingErrorBoundary key={content}><ReadingContext.Provider value={context}><Markdown remarkPlugins={plugins} rehypePlugins={[positions]} skipHtml urlTransform={value=>value} components={components}>{content}</Markdown></ReadingContext.Provider></ReadingErrorBoundary>;
});
