import type { OpenFile } from "./file-context";
import {staticSVGInfo} from "./static-svg";

export type AssetContext = { agentEpoch:string; relayVersion:number; relaySvgVersion?:number; request:(params:Record<string,unknown>,signal:AbortSignal)=>Promise<Record<string,unknown>> };
export type ImageSource = {kind:"local"|"data";url:string}|{kind:"external";url:string;domain:string}|{kind:"blocked";reason:string};
const maxBytes=4*1024*1024, maxMemory=16*1024*1024, chunkBytes=256*1024;
const supportedMIME=new Set(["image/png","image/jpeg","image/gif","image/webp"]);
const abortError=()=>new DOMException("读取已取消","AbortError");

// Asset failures share wire codes with text reads, not their UI constraints.
// Never forward backend details (which may contain local paths) into the viewer.
export function documentAssetErrorText(code?: string, svgEnabled = false): string {
  switch (code) {
    case "UNSUPPORTED_FILE_TYPE": return svgEnabled
      ? "图片格式不受支持、文件已损坏或 SVG 不满足静态安全要求；当前支持 PNG、JPEG、GIF、WebP 和受限静态 SVG。"
      : "图片格式不受支持或文件已损坏；当前支持 PNG、JPEG、GIF、WebP。";
    case "FILE_TOO_LARGE": return "图片超过大小、尺寸或复杂度限制（最大 4 MiB、单边 8192 px、1200 万像素）。";
    case "FILE_NOT_FOUND": return "图片文件已不存在，请检查文档中的图片引用。";
    case "PATH_NOT_ALLOWED": return "这项图片引用不允许读取。";
    case "FILE_CHANGED": return "文档或图片已经变化，请重新打开文档。";
    case "FEATURE_DISABLED": return "当前设备尚未启用文档图片读取。";
    case "TIMEOUT": return "图片读取超时或分块已过期，请重试图片。";
    case "OVERLOADED": return "等待加载的图片过多，请稍后重试图片。";
    case "PROTOCOL_UNSUPPORTED": return "当前设备的文档图片能力暂不可用，请重新打开文档。";
    default: return "图片暂时无法读取，请重试。";
  }
}

export function classifyImageSource(source:string):ImageSource{
  if(!source||source.length>Math.ceil(maxBytes*4/3)+128||/[\u0000-\u001f\u007f\\]/u.test(source))return {kind:"blocked",reason:"图片地址不受支持"};
  if(source.startsWith("data:"))return /^data:image\/(?:png|jpeg|gif|webp);base64,[a-z\d+/=]+$/iu.test(source)?{kind:"data",url:source}:{kind:"blocked",reason:"不支持的内嵌图片"};
  if(source.startsWith("//")||source.startsWith("/"))return {kind:"blocked",reason:"不允许绝对图片路径"};
  if(/^[a-z][a-z\d+.-]*:/iu.test(source)){
    try{
      const url=new URL(source),host=url.hostname.replace(/^\[|\]$/gu,"").toLowerCase();
      const parts=host.split(".").map(Number);
      const ipv4=parts.length===4&&parts.every(n=>Number.isInteger(n)&&n>=0&&n<=255);
      const privateIPv4=ipv4&&(parts[0]===0||parts[0]===10||parts[0]===127||parts[0]>=224||parts[0]===169&&parts[1]===254||parts[0]===172&&parts[1]>=16&&parts[1]<=31||parts[0]===192&&parts[1]===168||parts[0]===100&&parts[1]>=64&&parts[1]<=127||parts[0]===198&&(parts[1]===18||parts[1]===19));
      const privateHost=host==="localhost"||/\.(?:localhost|local|internal|lan)$/u.test(host)||!host.includes(".")&&!host.includes(":")||host==="::"||host==="::1"||/^(?:f[cd]|fe[89ab]|::ffff:)/u.test(host);
      if(url.protocol!=="https:"||url.username||url.password||privateIPv4||privateHost)return {kind:"blocked",reason:"不允许此图片地址"};
      return {kind:"external",url:url.href,domain:url.hostname};
    }catch{return {kind:"blocked",reason:"图片地址无效"}}
  }
  return source.length<=4096?{kind:"local",url:source}:{kind:"blocked",reason:"图片地址过长"};
}

export function rasterInfo(bytes:Uint8Array,mime:string):{width:number;height:number}{
  const view=new DataView(bytes.buffer,bytes.byteOffset,bytes.byteLength);
  const ascii=(from:number,to:number)=>String.fromCharCode(...bytes.subarray(from,to));
  let width=0,height=0;
  if(mime==="image/png"&&bytes.length>=24&&bytes[0]===137&&ascii(1,8)==="PNG\r\n\x1a\n"&&ascii(12,16)==="IHDR"){
    width=view.getUint32(16);height=view.getUint32(20);
  }else if(mime==="image/gif"&&bytes.length>=10&&/GIF8[79]a/u.test(ascii(0,6))){width=view.getUint16(6,true);height=view.getUint16(8,true)}
  else if(mime==="image/jpeg"&&bytes.length>=4&&bytes[0]===255&&bytes[1]===216){
    let offset=2;
    while(offset+4<=bytes.length){
      if(bytes[offset]!==255)break;const marker=bytes[offset+1];if(marker===255){offset++;continue}
      const length=view.getUint16(offset+2);if(length<2||offset+2+length>bytes.length)break;
      if([192,193,194,195,197,198,199,201,202,203,205,206,207].includes(marker)&&length>=7){height=view.getUint16(offset+5);width=view.getUint16(offset+7);break}
      offset+=length+2;
    }
  }else if(mime==="image/webp"&&bytes.length>=30&&ascii(0,4)==="RIFF"&&ascii(8,12)==="WEBP"){
    if(ascii(12,16)==="VP8X"){width=1+bytes[24]+(bytes[25]<<8)+(bytes[26]<<16);height=1+bytes[27]+(bytes[28]<<8)+(bytes[29]<<16)}
    else if(ascii(12,16)==="VP8L"&&bytes[20]===47){const bits=view.getUint32(21,true);width=1+(bits&16383);height=1+((bits>>>14)&16383)}
    else if(ascii(12,16)==="VP8 "&&ascii(23,26)==="\x9d\x01\x2a"){width=view.getUint16(26,true)&16383;height=view.getUint16(28,true)&16383}
  }
  if(!width||!height||width>8192||height>8192||width*height>12_000_000)throw new Error("图片格式无效或尺寸超过限制");
  return {width,height};
}

function decodeBase64(value:string):Uint8Array{
  const raw=atob(value);return Uint8Array.from(raw,char=>char.charCodeAt(0));
}
function stopped(signal:AbortSignal){if(signal.aborted)throw abortError()}
async function abortable<T>(promise:Promise<T>,signal:AbortSignal):Promise<T>{
  stopped(signal);
  return new Promise<T>((resolve,reject)=>{
    const abort=()=>reject(abortError());signal.addEventListener("abort",abort,{once:true});
    promise.then(value=>{signal.removeEventListener("abort",abort);if(signal.aborted)reject(abortError());else resolve(value)},error=>{signal.removeEventListener("abort",abort);reject(error)});
  });
}

type Entry={url:string;width:number;height:number;bytes:number;refs:number};
export type LoadedImage={url:string;width:number;height:number;release:()=>void};

export class DocumentImageSession{
  private cache=new Map<string,Entry>();private bytes=0;private active=0;private pending:Array<()=>void>=[];private controllers=new Set<AbortController>();private disposed=false;
  constructor(private file:OpenFile,private context?:AssetContext){}
  private pump(){while(this.active<2&&this.pending.length)this.pending.shift()!()}
  private reference(entry:Entry):LoadedImage{entry.refs++;let released=false;return {...entry,release:()=>{if(!released){released=true;entry.refs--}}}}
  isCached(source:string):boolean{return !this.disposed&&this.cache.has(source)}
  // Only evict after the failed image has left the DOM and released its lease.
  // A second view of the same image must never lose its live Blob URL.
  invalidate(source:string):boolean{
    const entry=this.cache.get(source);if(!entry||entry.refs!==0)return false;
    this.cache.delete(source);this.bytes-=entry.bytes;URL.revokeObjectURL(entry.url);return true;
  }
  async acquire(source:string,signal:AbortSignal):Promise<LoadedImage>{
    if(this.disposed)throw abortError();stopped(signal);
    const found=this.cache.get(source);if(found){this.cache.delete(source);this.cache.set(source,found);return this.reference(found)}
    if(this.pending.length>=32)throw new Error("等待加载的图片过多");
    const controller=new AbortController();this.controllers.add(controller);
    const abort=()=>controller.abort();signal.addEventListener("abort",abort,{once:true});
    let granted=false;
    try{
      await new Promise<void>((resolve,reject)=>{
        const cancel=()=>{const index=this.pending.indexOf(run);if(index>=0)this.pending.splice(index,1);reject(abortError())};
        const run=()=>{controller.signal.removeEventListener("abort",cancel);if(controller.signal.aborted){reject(abortError());return}this.active++;granted=true;resolve()};
        controller.signal.addEventListener("abort",cancel,{once:true});this.pending.push(run);this.pump();
      });
      const image=await this.read(source,controller.signal);stopped(controller.signal);if(this.disposed)throw abortError();
      const duplicate=this.cache.get(source);if(duplicate)return this.reference(duplicate);
      for(const [key,entry]of this.cache){if(this.bytes+image.body.length<=maxMemory&&this.cache.size<32)break;if(entry.refs===0){this.cache.delete(key);this.bytes-=entry.bytes;URL.revokeObjectURL(entry.url)}}
      if(this.bytes+image.body.length>maxMemory||this.cache.size>=32)throw new Error("图片缓存已达到限制");
      const entry:Entry={url:URL.createObjectURL(new Blob([image.body as BlobPart],{type:image.mime})),width:image.width,height:image.height,bytes:image.body.length,refs:0};
      this.cache.set(source,entry);this.bytes+=entry.bytes;return this.reference(entry);
    }finally{signal.removeEventListener("abort",abort);this.controllers.delete(controller);if(granted){this.active--;this.pump()}}
  }
  private async read(source:string,signal:AbortSignal):Promise<{body:Uint8Array;mime:string;width:number;height:number}>{
    const kind=classifyImageSource(source);
    if(kind.kind==="data"){
      const [header,value]=source.split(",");if(value.length>Math.ceil(maxBytes*4/3)+4)throw new Error("图片超过 4 MiB");
      const body=decodeBase64(value);if(body.length>maxBytes)throw new Error("图片超过 4 MiB");const mime=header.slice(5,-7);return {body,mime,...rasterInfo(body,mime)};
    }
    if(kind.kind!=="local")throw new Error("不支持此本地图片");
    if(!this.context||this.context.relayVersion!==1||!this.context.agentEpoch||this.file.documentAssets?.version!==1)throw new Error("当前设备尚未启用文档图片读取");
    const svgAllowed=this.context.relaySvgVersion===1&&this.file.documentAssets.svgVersion===1;
    const chunks:Uint8Array[]=[];let offset=0,total=0,digest="",mime="",width=0,height=0,cursor="";
    const {threadId,turnId,itemId,path,sha256,referenceKind,rawReference}=this.file;
    for(let page=0;page<16;page++){
      stopped(signal);
      const params={source:this.file.source||"turn_change",threadId,...(turnId?{turnId}:{}),...(itemId?{itemId}:{}),path,...(referenceKind&&rawReference?{referenceKind,rawReference}:{}),agentEpoch:this.context.agentEpoch,documentSha256:sha256,imageReference:source,...(svgAllowed?{svgVersion:1}:{}),...(cursor?{cursor}:{})};
      const data=await abortable(this.context.request(params,signal),signal);
      if(new TextEncoder().encode(JSON.stringify(data)).byteLength>384*1024)throw new Error("图片分块超过限制");
      const nextTotal=data.totalBytes,nextOffset=data.offset,nextDigest=data.assetSha256,nextMIME=data.mime,nextWidth=data.width,nextHeight=data.height;
      if(!Number.isInteger(nextTotal)||Number(nextTotal)<1||Number(nextTotal)>maxBytes||nextOffset!==offset||typeof nextDigest!=="string"||!/^[a-f\d]{64}$/u.test(nextDigest)||typeof nextMIME!=="string"||!(supportedMIME.has(nextMIME)||svgAllowed&&nextMIME==="image/svg+xml")||!Number.isInteger(nextWidth)||!Number.isInteger(nextHeight)||Number(nextWidth)<1||Number(nextHeight)<1||Number(nextWidth)>8192||Number(nextHeight)>8192||Number(nextWidth)*Number(nextHeight)>12_000_000||typeof data.dataBase64!=="string"||data.dataBase64.length>Math.ceil(chunkBytes*4/3)+4)throw new Error("图片分块格式无效");
      if(offset&&(nextTotal!==total||nextDigest!==digest||nextMIME!==mime||nextWidth!==width||nextHeight!==height))throw new Error("图片读取期间发生变化");
      total=Number(nextTotal);digest=nextDigest;mime=nextMIME;width=Number(nextWidth);height=Number(nextHeight);
      const part=decodeBase64(data.dataBase64);if(!part.length||part.length>chunkBytes||offset+part.length>total)throw new Error("图片分块范围无效");
      chunks.push(part);offset+=part.length;cursor=typeof data.nextCursor==="string"?data.nextCursor:"";
      if(!cursor){
        if(offset!==total)throw new Error("图片分块不完整");const body=new Uint8Array(total);let position=0;for(const chunk of chunks){body.set(chunk,position);position+=chunk.length}
        let hash:Uint8Array;
        if(globalThis.crypto?.subtle)hash=new Uint8Array(await crypto.subtle.digest("SHA-256",body));
        else{const {sha256}=await import("@noble/hashes/sha2.js");stopped(signal);hash=sha256(body)}
        const actual=Array.from(hash).map(n=>n.toString(16).padStart(2,"0")).join("");stopped(signal);
        if(actual!==digest)throw new Error("图片内容校验失败");
        const dimensions=mime==="image/svg+xml"?staticSVGInfo(body):rasterInfo(body,mime);if(dimensions.width!==width||dimensions.height!==height)throw new Error("图片内容校验失败");
        return {body,mime,...dimensions};
      }
      if(!/^[a-z\d_-]{16,128}$/iu.test(cursor)||offset>=total)throw new Error("图片续读标识无效");
    }
    throw new Error("图片分块过多");
  }
  dispose(){this.disposed=true;for(const controller of this.controllers)controller.abort();for(const entry of this.cache.values())URL.revokeObjectURL(entry.url);this.cache.clear();this.bytes=0}
}
