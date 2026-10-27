export function resolveDocumentPath(parent:string,target:string):{path:string;startLine?:number}|null{
 if(!target||target.length>4096||target.startsWith("/")||/^[a-z][a-z\d+.-]*:/iu.test(target)||/[\u0000-\u001f\u007f\\?]/u.test(target))return null;
 const hash=target.match(/#L([1-9]\d*)(?:-L[1-9]\d*)?$/u);const raw=target.split("#")[0];let decoded:string;
 try{decoded=decodeURIComponent(raw)}catch{return null}
 if(decoded.startsWith("/")||/[\u0000-\u001f\u007f\\]/u.test(decoded))return null;
 const parts=parent.split("/").slice(0,-1);
 for(const part of decoded.split("/")){if(part===""||part===".")continue;if(part==="..") {if(!parts.length)return null;parts.pop()}else {if(part===".git")return null;parts.push(part)}}
 return parts.length?{path:parts.join("/"),...(hash?{startLine:Number(hash[1])}:{})}:null;
}
