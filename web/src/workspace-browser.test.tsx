// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkspaceDirectoryCache, WorkspaceBrowser } from "./workspace-browser";
import { FileContentCache, type FileQuote } from "./file-context";

afterEach(cleanup);

describe("workspace browser", () => {
  it("loads one directory at a time and continues the current page", async () => {
    const request = vi.fn(async (method:string,_device:string,params:Record<string,unknown>) => {
      if(method==="workspace.files.list" && params.path==="" && !params.cursor) return {outcome:"accepted",data:{path:"",revision:"r1",nextCursor:"c1",entries:[{name:"docs",path:"docs",kind:"directory",sizeBytes:0,modifiedAtMs:1,readable:true}]}};
      if(method==="workspace.files.list" && params.cursor==="c1") return {outcome:"accepted",data:{path:"",revision:"r1",entries:[{name:"README.md",path:"README.md",kind:"file",sizeBytes:5,modifiedAtMs:1,readable:true}]}};
      if(method==="workspace.files.list" && params.path==="docs") return {outcome:"accepted",data:{path:"docs",revision:"r2",entries:[]}};
      throw new Error(`unexpected ${method}`);
    });
    render(<WorkspaceBrowser deviceId="mac" agentEpoch="epoch" threadId="thread" cwd="/work/project" request={request as never} cache={new WorkspaceDirectoryCache()} fileCache={new FileContentCache()} onClose={()=>{}} onQuote={()=>{}}/>);
    expect(await screen.findByRole("button",{name:"打开目录 docs"})).toBeTruthy();
    fireEvent.click(screen.getByRole("button",{name:"加载更多"}));
    expect(await screen.findByRole("button",{name:"打开文件 README.md"})).toBeTruthy();
    fireEvent.click(screen.getByRole("button",{name:"打开目录 docs"}));
    await waitFor(()=>expect(request).toHaveBeenCalledWith("workspace.files.list","mac",expect.objectContaining({threadId:"thread",path:"docs",limit:100})));
  });

  it("opens text and quotes a native selection without fake turn identity", async () => {
    const request = vi.fn(async (method:string) => method==="workspace.files.list"
      ? {outcome:"accepted",data:{path:"",revision:"r1",entries:[{name:"README.md",path:"README.md",kind:"file",sizeBytes:5,modifiedAtMs:1,readable:true}]}}
      : {outcome:"accepted",data:{path:"README.md",content:"hello",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"md",version:"current-workspace"}});
    const quotes:FileQuote[]=[];
    render(<WorkspaceBrowser deviceId="mac" agentEpoch="epoch" threadId="thread" cwd="/work/project" request={request as never} cache={new WorkspaceDirectoryCache()} fileCache={new FileContentCache()} onClose={()=>{}} onQuote={quote=>quotes.push(quote)}/>);
    fireEvent.click(await screen.findByRole("button",{name:"打开文件 README.md"}));
    await screen.findByTestId("file-source");
    expect(screen.queryByRole("button",{name:"查看 Diff"})).toBeNull();
    const code=screen.getByText("hello");
    const range=document.createRange();range.selectNodeContents(code);const selection=window.getSelection();selection?.removeAllRanges();selection?.addRange(range);document.dispatchEvent(new Event("selectionchange"));
    fireEvent.click(await screen.findByRole("button",{name:"引用到会话"}));
    expect(quotes).toEqual([expect.objectContaining({source:"workspace_browser",threadId:"thread",path:"README.md",text:"hello"})]);
    expect(quotes[0]).not.toHaveProperty("turnId"); expect(quotes[0]).not.toHaveProperty("itemId");
  });

  it("bounds directory cache by entries and bytes", () => {
    const cache=new WorkspaceDirectoryCache(2,160);
    cache.set("a",{path:"",revision:"1",entries:[{name:"a",path:"a",kind:"file",sizeBytes:1,modifiedAtMs:1,readable:true}]});
    cache.set("b",{path:"b",revision:"2",entries:[{name:"b",path:"b/b",kind:"file",sizeBytes:1,modifiedAtMs:1,readable:true}]});
    cache.set("c",{path:"c",revision:"3",entries:[{name:"c",path:"c/c",kind:"file",sizeBytes:1,modifiedAtMs:1,readable:true}]});
    expect(cache.get("a")).toBeUndefined(); expect(cache.size).toBeLessThanOrEqual(2); expect(cache.bytes).toBeLessThanOrEqual(160);
  });

  it("keeps only the newest revision for a directory identity and can invalidate it", () => {
    const cache=new WorkspaceDirectoryCache();
    cache.set("root",{path:"",revision:"r1",entries:[]});
    cache.set("root",{path:"",revision:"r2",entries:[{name:"new",path:"new",kind:"file",sizeBytes:1,modifiedAtMs:2,readable:true}]});
    expect(cache.size).toBe(1);expect(cache.get("root")?.revision).toBe("r2");
    cache.delete("root");expect(cache.get("root")).toBeUndefined();expect(cache.bytes).toBe(0);
  });
});
