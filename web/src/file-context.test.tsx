// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FileContentCache, FileViewer, TurnFileChanges, emptyFileReaderState, projectCanonicalFileContexts, selectionLines, webFileContextEnabled, type FileView, type OpenFile } from "./file-context";

afterEach(cleanup);

describe("Chilling file context UI", () => {
  it("requires the runtime flag", () => {
    document.head.innerHTML = '<meta name="ariel-chilling-file-context" content="false">';
    expect(webFileContextEnabled()).toBe(false);
    document.head.innerHTML = '<meta name="ariel-chilling-file-context" content="true">';
    expect(webFileContextEnabled()).toBe(true);
  });

  it("offers independent source and diff actions for changed files", () => {
    const open = vi.fn();
    render(<TurnFileChanges changes={[
      { itemId:"i1",path:"a.go",kind:"update",diff:"@@ -1 +1 @@\n-old\n+new" },
      { itemId:"i2",path:"gone.go",kind:"delete",diff:"@@ -1 +0,0 @@\n-old" },
    ]} onOpen={open}/>);
    const updated = screen.getByTestId("file-change:a.go");
    fireEvent.click(within(updated).getByRole("button", { name:"查看原文：a.go" }));
    fireEvent.click(within(updated).getByRole("button", { name:"查看 Diff：a.go" }));
    expect(open).toHaveBeenNthCalledWith(1, expect.objectContaining({ path:"a.go" }), "source");
    expect(open).toHaveBeenNthCalledWith(2, expect.objectContaining({ path:"a.go" }), "diff");
    const deleted = screen.getByTestId("file-change:gone.go");
    expect((within(deleted).getByRole("button", { name:"查看原文：gone.go" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(deleted).getByRole("button", { name:"查看 Diff：gone.go" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("turns a browser range into exact 1-based lines", () => {
    expect(selectionLines("one\ntwo\nthree", 4, 7)).toEqual({ startLine:2, endLine:2, text:"two" });
    expect(selectionLines("one\r\ntwo", 5, 8)).toEqual({ startLine:2, endLine:2, text:"two" });
  });

  it("keeps a partial duplicate selection in the viewer until explicit quote", () => {
    const quote = vi.fn();
    const close = vi.fn();
    const file: OpenFile = { threadId:"t",turnId:"turn",itemId:"item",path:"a.go",content:"repeat one\nrepeat two",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:2,language:"go",version:"current-workspace" };
    const view: FileView = { threadId:"t",turnId:"turn",change:{itemId:"item",path:"a.go",kind:"update",diff:"@@ -1 +1 @@\n-repeat one\n+repeat two"},mode:"source",file };
    render(<FileViewer view={view} onClose={close} onModeChange={()=>{}} onQuote={quote}/>);
    const code = screen.getAllByTestId("file-source-line")[1].querySelector("code")!;
    const range = document.createRange();
    range.setStart(code.firstChild!, 0);
    range.setEnd(code.firstChild!, 6);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    fireEvent.mouseUp(screen.getByTestId("file-source"));
    expect(screen.getByText("已选择 L2")).toBeTruthy();
    expect(close).not.toHaveBeenCalled();
    expect(quote).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name:/引用第/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name:"引用到会话" }));
    expect(quote).toHaveBeenCalledWith(expect.objectContaining({ path:"a.go",startLine:2,endLine:2,text:"repeat" }));
  });

  it("only shows the quote bar after selecting and clears it on an explicit in-file deselection", () => {
    const saved = emptyFileReaderState();
    const file: OpenFile = { threadId:"t",path:"a.go",content:"one two",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"go",version:"current-workspace" };
    render(<FileViewer readerState={saved} showDiff={false} view={{threadId:"t",turnId:"",change:{itemId:"",path:"a.go",kind:"update"},mode:"source",file}} onClose={()=>{}} onModeChange={()=>{}} onQuote={()=>{}}/>);
    expect(document.querySelector(".file-viewer footer")).toBeNull();
    expect(screen.queryByRole("button", { name:"引用到会话" })).toBeNull();
    const root = screen.getByTestId("file-source"), node = root.querySelector("code")!.firstChild!;
    const range = document.createRange(), selection = window.getSelection()!;
    range.setStart(node, 0); range.setEnd(node, 3);
    selection.removeAllRanges(); selection.addRange(range);
    fireEvent(document, new Event("selectionchange"));
    expect(screen.getByText("已选择 L1")).toBeTruthy();
    expect(saved.selection).toEqual({start:0,end:3});
    range.collapse(true); selection.removeAllRanges(); selection.addRange(range);
    fireEvent(document, new Event("selectionchange"));
    expect(document.querySelector(".file-viewer footer")).toBeNull();
    expect(saved.selection).toBeUndefined();
    range.selectNodeContents(node); selection.removeAllRanges(); selection.addRange(range);
    fireEvent.mouseUp(root);
    expect(screen.getByRole("button", { name:"引用到会话" })).toBeTruthy();
    selection.removeAllRanges(); fireEvent(document, new Event("selectionchange"));
    expect(document.querySelector(".file-viewer footer")).toBeNull();
    expect(saved.selection).toBeUndefined();
  });

  it("keeps the captured quote while its button receives keyboard focus even if native ranges clear", () => {
    const quote = vi.fn();
    const file: OpenFile = { threadId:"t",path:"a.go",content:"one",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"go",version:"current-workspace" };
    render(<FileViewer view={{threadId:"t",turnId:"",change:{itemId:"",path:"a.go",kind:"update"},mode:"source",file}} onClose={()=>{}} onModeChange={()=>{}} onQuote={quote}/>);
    const root = screen.getByTestId("file-source"), selection = window.getSelection()!;
    const range = document.createRange(); range.selectNodeContents(root.querySelector("code")!);
    selection.removeAllRanges(); selection.addRange(range); fireEvent.mouseUp(root);
    const button = screen.getByRole("button", { name:"引用到会话" });
    button.focus(); selection.removeAllRanges(); fireEvent(document, new Event("selectionchange"));
    fireEvent.click(button);
    expect(quote).toHaveBeenCalledWith(expect.objectContaining({text:"one",path:"a.go"}));
  });

  it("does not show quote hints until selection and retains disabled source checks afterwards", () => {
    const quote = vi.fn();
    const file: OpenFile = { threadId:"source",path:"a.go",content:"one",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"go",version:"current-workspace" };
    render(<FileViewer quoteDisabled view={{threadId:"source",turnId:"",change:{itemId:"",path:"a.go",kind:"update"},mode:"source",file}} onClose={()=>{}} onModeChange={()=>{}} onQuote={quote}/>);
    expect(screen.queryByText(/切回来源会话/)).toBeNull();
    const root = screen.getByTestId("file-source"), selection = window.getSelection()!;
    const range = document.createRange(); range.selectNodeContents(root.querySelector("code")!);
    selection.removeAllRanges(); selection.addRange(range); fireEvent.mouseUp(root);
    expect(screen.getByText(/切回来源会话/)).toBeTruthy();
    const button = screen.getByRole("button", { name:"引用到会话" }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    fireEvent.click(button); expect(quote).not.toHaveBeenCalled();
  });

  it("explains an unmappable selection but hides the bar again on file version change", () => {
    const file: OpenFile = { threadId:"t",path:"a.go",content:"one",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"go",version:"current-workspace" };
    const view: FileView = {threadId:"t",turnId:"",change:{itemId:"",path:"a.go",kind:"update"},mode:"source",file};
    const props = { onClose:vi.fn(), onModeChange:vi.fn(), onQuote:vi.fn() };
    const mounted = render(<FileViewer view={view} {...props}/>);
    const root = screen.getByTestId("file-source"), selection = window.getSelection()!;
    const range = document.createRange(); range.selectNodeContents(root.querySelector(".file-line-number")!);
    selection.removeAllRanges(); selection.addRange(range); fireEvent.mouseUp(root);
    expect(screen.getByText("无法识别该选区，请重新选择")).toBeTruthy();
    expect((screen.getByRole("button", { name:"引用到会话" }) as HTMLButtonElement).disabled).toBe(true);
    mounted.rerender(<FileViewer view={{...view,file:{...file,sha256:"b".repeat(64)}}} {...props}/>);
    expect(document.querySelector(".file-viewer footer")).toBeNull();
  });

  it("renders a read-only truncated diff and returns without quoting", () => {
    const close = vi.fn();
    const mode = vi.fn();
    const view: FileView = {threadId:"t",turnId:"turn",change:{itemId:"item",path:"a.go",kind:"update",diff:"@@ -1 +1 @@\n-old\n+new",diffTruncated:true},mode:"diff"};
    render(<FileViewer view={view} onClose={close} onModeChange={mode} onQuote={()=>{}}/>);
    expect(screen.getByText("Diff 已截断")).toBeTruthy();
    expect(screen.getByText("-old").classList.contains("diff-delete")).toBe(true);
    expect(screen.getByText("+new").classList.contains("diff-add")).toBe(true);
    expect(screen.queryByRole("button", {name:"引用到会话"})).toBeNull();
    fireEvent.click(screen.getByRole("button", {name:"返回会话"}));
    expect(close).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", {name:"查看原文"}));
    expect(mode).toHaveBeenCalledWith("source");
  });

  it("projects canonical context as metadata without swallowing malformed text", () => {
    const valid='question\n\n<ariel_file_context>\n{"path":"a.go","startLine":2,"endLine":3,"sha256":"'+"a".repeat(64)+'","text":"two"}\n</ariel_file_context>';
    expect(projectCanonicalFileContexts(valid)).toEqual({text:"question",quotes:[expect.objectContaining({path:"a.go",startLine:2,endLine:3})]});
    const malformed="question\n<ariel_file_context>bad</ariel_file_context>";
    expect(projectCanonicalFileContexts(malformed)).toEqual({text:malformed,quotes:[]});
  });

  it("bounds cached file content by LRU entry and byte limits", () => {
    const cache = new FileContentCache(2, 7);
    const file = (path:string,content:string):OpenFile => ({threadId:"t",turnId:"turn",itemId:path,path,content,sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"text",version:"current-workspace"});
    cache.set("a",file("a","aaa"));
    cache.set("b",file("b","bbb"));
    expect(cache.get("a")?.path).toBe("a");
    cache.set("c",file("c","cccc"));
    expect(cache.get("b")).toBeUndefined();
    expect(cache.get("a")?.path).toBe("a");
    expect(cache.get("c")?.path).toBe("c");
    cache.clear();
    expect(cache.get("a")).toBeUndefined();
  });

  it("does not discard a captured file selection when selecting main-conversation text",()=>{
    const file:OpenFile={threadId:"t",path:"a.md",content:"one two",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:1,language:"md",version:"current-workspace"};
    render(<><p data-testid="outside">主会话文字</p><FileViewer embedded view={{threadId:"t",turnId:"",change:{itemId:"",path:"a.md",kind:"update"},mode:"source",file}} onClose={()=>{}} onModeChange={()=>{}} onQuote={()=>{}}/></>);
    const selection=window.getSelection()!;selection.removeAllRanges();let range=document.createRange();range.selectNodeContents(screen.getByTestId("file-source").querySelector("code")!);selection.addRange(range);fireEvent.mouseUp(screen.getByTestId("file-source"));
    expect((screen.getByRole("button",{name:"引用到会话"}) as HTMLButtonElement).disabled).toBe(false);
    range=document.createRange();range.selectNodeContents(screen.getByTestId("outside"));selection.removeAllRanges();selection.addRange(range);fireEvent(document,new Event("selectionchange"));
    expect((screen.getByRole("button",{name:"引用到会话"}) as HTMLButtonElement).disabled).toBe(false);
  });
});
