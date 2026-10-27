// @vitest-environment jsdom
import { webcrypto } from "node:crypto";
import { afterEach,expect,it,vi } from "vitest";
import { DocumentImageSession,classifyImageSource,rasterInfo } from "./document-assets";
import type { OpenFile } from "./file-context";

afterEach(()=>vi.unstubAllGlobals());
const png=Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg=="),c=>c.charCodeAt(0));
const file:OpenFile={source:"workspace_browser",threadId:"t",path:"docs/read.md",content:"![图](../a.png)",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"md",version:"current-workspace",documentAssets:{version:1,maxBytes:4*1024*1024,chunkBytes:256*1024}};

it("blocks unsafe image URLs and recognizes safe HTTPS / relative / bounded raster data",()=>{
 for(const src of ["javascript:alert(1)","file:///a.png","http://x.test/a.png","//x.test/a.png","https://127.0.0.1/a.png","https://[::1]/a.png","https://10.0.0.1/a.png","https://u:p@x.test/a.png","https://localhost/a.png","data:image/svg+xml,<svg/>"]){expect(classifyImageSource(src).kind,src).toBe("blocked")}
 expect(classifyImageSource("https://images.example.test/a.png").kind).toBe("external");
 expect(classifyImageSource("../a.png").kind).toBe("local");
 expect(rasterInfo(png,"image/png")).toEqual({width:1,height:1});
 expect(()=>rasterInfo(png,"image/jpeg")).toThrow();
});

it("validates chunk offset/digest and disposes Blob URLs without backend fallback",async()=>{
 vi.stubGlobal("crypto",webcrypto);const revoke=vi.fn();URL.createObjectURL=vi.fn(()=>"blob:fixture");URL.revokeObjectURL=revoke;
 const digest=Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",png))).map(n=>n.toString(16).padStart(2,"0")).join("");
 const request=vi.fn(async()=>({mime:"image/png",assetSha256:digest,width:1,height:1,totalBytes:png.length,offset:0,dataBase64:btoa(String.fromCharCode(...png))}));
 const session=new DocumentImageSession(file,{agentEpoch:"epoch",relayVersion:1,request});
 const controller=new AbortController();const result=await session.acquire("../a.png",controller.signal);
 expect(result.url).toBe("blob:fixture");expect(request).toHaveBeenCalledWith(expect.objectContaining({source:"workspace_browser",agentEpoch:"epoch",documentSha256:file.sha256,imageReference:"../a.png"}),expect.any(AbortSignal));
 result.release();session.dispose();expect(revoke).toHaveBeenCalledWith("blob:fixture");
 const unavailable=new DocumentImageSession(file,{agentEpoch:"epoch",relayVersion:0,request});
 await expect(unavailable.acquire("../a.png",controller.signal)).rejects.toThrow();expect(request).toHaveBeenCalledTimes(1);
});

it("invalidates a released image for decode retry without revoking resources still in use",async()=>{
 URL.createObjectURL=vi.fn().mockReturnValueOnce("blob:first").mockReturnValueOnce("blob:retry");
 URL.revokeObjectURL=vi.fn();
 const session=new DocumentImageSession(file),source=`data:image/png;base64,${btoa(String.fromCharCode(...png))}`;
 const first=await session.acquire(source,new AbortController().signal);
 expect(session).toHaveProperty("invalidate");
 const invalidate=(session as DocumentImageSession&{invalidate:(source:string)=>boolean}).invalidate.bind(session);
 expect(invalidate(source)).toBe(false);expect(URL.revokeObjectURL).not.toHaveBeenCalled();
 first.release();expect(invalidate(source)).toBe(true);expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:first");
 const second=await session.acquire(source,new AbortController().signal);expect(second.url).toBe("blob:retry");
 second.release();session.dispose();
});

it("still verifies local image digests when a LAN browser has no Web Crypto subtle API",async()=>{
 const digest=Array.from(new Uint8Array(await webcrypto.subtle.digest("SHA-256",png))).map(n=>n.toString(16).padStart(2,"0")).join("");
 vi.stubGlobal("crypto",{});URL.createObjectURL=vi.fn(()=>"blob:lan");URL.revokeObjectURL=vi.fn();
 const request=vi.fn(async()=>({mime:"image/png",assetSha256:digest,width:1,height:1,totalBytes:png.length,offset:0,dataBase64:btoa(String.fromCharCode(...png))}));
 const session=new DocumentImageSession(file,{agentEpoch:"epoch",relayVersion:1,request});
 const result=await session.acquire("../a.png",new AbortController().signal);expect(result.url).toBe("blob:lan");result.release();session.dispose();
 const corrupted=new DocumentImageSession(file,{agentEpoch:"epoch",relayVersion:1,request:async()=>({...await request(),assetSha256:"0".repeat(64)})});
 await expect(corrupted.acquire("../a.png",new AbortController().signal)).rejects.toThrow("图片内容校验失败");corrupted.dispose();
});
