// @vitest-environment jsdom
import{act,cleanup,fireEvent,render,screen}from "@testing-library/react";
import{afterEach,expect,it,vi}from "vitest";
import{StrictMode}from "react";
import{WorkspaceBrowser,WorkspaceDirectoryCache}from "./workspace-browser";
import{FileContentCache}from "./file-context";

afterEach(cleanup);
it("does not reopen or cache a late file response after returning to the tree",async()=>{
 type Reply={outcome:string;data:Record<string,unknown>};
 let finish!:(value:Reply)=>void;
 const fileCache=new FileContentCache();
 const request=vi.fn(async(method:string)=>method==="workspace.files.list"?{outcome:"accepted",data:{path:"",revision:"r",entries:[{name:"a.md",path:"a.md",kind:"file",sizeBytes:5,modifiedAtMs:1,readable:true}]}}:await new Promise<Reply>(resolve=>finish=resolve));
 render(<WorkspaceBrowser deviceId="mac" agentEpoch="e" threadId="t" cwd="/work" request={request} cache={new WorkspaceDirectoryCache()} fileCache={fileCache} onClose={()=>{}} onQuote={()=>{}}/>);
 fireEvent.click(await screen.findByRole("button",{name:"打开文件 a.md"}));
 expect(screen.getByRole("status").textContent).toContain("正在读取文件");
 fireEvent.click(screen.getByRole("button",{name:"返回文件列表"}));
 await act(async()=>finish({outcome:"accepted",data:{path:"a.md",content:"hello",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"md",version:"current-workspace"}}));
 expect(screen.queryByRole("dialog",{name:"文件 a.md"})).toBeNull();
 expect(fileCache.get("mac\0e\0t\0file\0a.md")).toBeUndefined();
});

it("coalesces the initial directory read across StrictMode effect replay",async()=>{
 const request=vi.fn(async()=>({outcome:"accepted",data:{path:"",revision:"r",entries:[]}}));
 render(<StrictMode><WorkspaceBrowser deviceId="mac" agentEpoch="e" threadId="t" cwd="/work" request={request} cache={new WorkspaceDirectoryCache()} fileCache={new FileContentCache()} onClose={()=>{}} onQuote={()=>{}}/></StrictMode>);
 await screen.findByText("此目录为空");
 expect(request).toHaveBeenCalledTimes(1);
});
