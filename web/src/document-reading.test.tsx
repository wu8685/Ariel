// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach,expect,it,vi } from "vitest";
import { FileViewer,type FileView } from "./file-context";

afterEach(cleanup);
const content="# 阅读标题\n\n重复内容\n\n重复**内容**与结尾\n\n| A | B |\n| - | - |\n| 甲 | 乙 |\n\n<script>alert(1)</script>\n";
function view(path="read.MD"):FileView{return {threadId:"t",turnId:"turn",mode:"source",change:{itemId:"i",path,kind:"update",diff:"-old\n+new"},file:{threadId:"t",turnId:"turn",itemId:"i",path,content,sha256:"a".repeat(64),modifiedAtMs:1,lineCount:12,language:"md",version:"current-workspace"}}}

it("defaults to unchanged source, renders Markdown on demand and keeps reading mode after Diff",()=>{
 const original=view();const mode=vi.fn();const props={onClose:vi.fn(),onModeChange:mode,onQuote:vi.fn()};const rendered=render(<FileViewer view={original} {...props}/>);
 expect(screen.getByTestId("file-source").textContent).toContain("# 阅读标题");
 expect(screen.queryByRole("heading",{name:"阅读标题"})).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"阅读模式"}));
 expect(screen.getByRole("heading",{name:"阅读标题"})).toBeTruthy();
 expect(screen.getByRole("table")).toBeTruthy();expect(document.querySelector("script")).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"查看 Diff"}));expect(mode).toHaveBeenCalledWith("diff");
 rendered.rerender(<FileViewer view={{...original,mode:"diff"}} {...props}/>);
 expect(screen.queryByRole("button",{name:"引用到会话"})).toBeNull();
 rendered.rerender(<FileViewer view={original} {...props}/>);
 expect(screen.getByRole("heading",{name:"阅读标题"})).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:"原版模式"}));
 expect(screen.getByTestId("file-source").textContent).toContain("重复**内容**与结尾");
});

it("maps repeated formatted text by source position and quotes only after explicit action",()=>{
 const quote=vi.fn();render(<FileViewer view={view()} onClose={()=>{}} onModeChange={()=>{}} onQuote={quote}/>);
 fireEvent.click(screen.getByRole("button",{name:"阅读模式"}));
 const strong=screen.getByText("内容",{selector:"strong span"});const node=strong.firstChild!;
 const range=document.createRange();range.setStart(node,0);range.setEnd(node,2);
 const selection=window.getSelection()!;selection.removeAllRanges();selection.addRange(range);
 fireEvent.mouseUp(screen.getByTestId("file-reading"));
 expect(quote).not.toHaveBeenCalled();fireEvent.click(screen.getByRole("button",{name:"引用到会话"}));
 expect(quote).toHaveBeenCalledWith(expect.objectContaining({startLine:5,endLine:5,text:"内容"}));
 fireEvent.click(screen.getByRole("button",{name:"原版模式"}));
 expect(screen.queryByRole("button",{name:"引用到会话"})).toBeNull();
});

it("does not offer Markdown rendering for other file types",()=>{
 render(<FileViewer view={view("file.ts")} onClose={()=>{}} onModeChange={()=>{}} onQuote={()=>{}}/>);
 expect(screen.queryByRole("button",{name:"阅读模式"})).toBeNull();
});

it("scopes the reading surface to rendered Markdown, never source or Diff",()=>{
 const original=view();const props={onClose:vi.fn(),onModeChange:vi.fn(),onQuote:vi.fn()};
 const rendered=render(<FileViewer view={original} {...props}/>);
 expect(screen.getByRole("dialog").classList.contains("is-reading")).toBe(false);
 fireEvent.click(screen.getByRole("button",{name:"阅读模式"}));
 expect(screen.getByRole("dialog").classList.contains("is-reading")).toBe(true);
 rendered.rerender(<FileViewer view={{...original,mode:"diff"}} {...props}/>);
 expect(screen.getByRole("dialog").classList.contains("is-reading")).toBe(false);
 rendered.rerender(<FileViewer view={original} {...props}/>);
 expect(screen.getByRole("dialog").classList.contains("is-reading")).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"原版模式"}));
 expect(screen.getByRole("dialog").classList.contains("is-reading")).toBe(false);
});

it("quotes the exact original newline across a visually flowing soft break",()=>{
 const original=view();original.file!.content="第一部分\n第二部分";const quote=vi.fn();
 render(<FileViewer view={original} onClose={()=>{}} onModeChange={()=>{}} onQuote={quote}/>);
 fireEvent.click(screen.getByRole("button",{name:"阅读模式"}));
 const reading=screen.getByTestId("file-reading"),paragraph=reading.querySelector("p")!;
 // Text-node endpoints model a real drag across the soft break. An unmapped
 // paragraph-element endpoint intentionally uses the existing fail-closed UI.
 const node=paragraph.querySelector("[data-source-start]")!.firstChild!;
 const range=document.createRange();range.setStart(node,0);range.setEnd(node,node.textContent!.length);const selection=getSelection()!;selection.removeAllRanges();selection.addRange(range);fireEvent.mouseUp(reading);
 fireEvent.click(screen.getByRole("button",{name:"引用到会话"}));
 expect(quote).toHaveBeenCalledWith(expect.objectContaining({startLine:1,endLine:2,text:"第一部分\n第二部分"}));
});

it("keeps image resources usable when a cached file mounts under StrictMode",async()=>{
 const original=view();
 original.file!.content="![cached](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==)";
 const create=vi.spyOn(URL,"createObjectURL").mockReturnValue("blob:strict-fixture");
 const revoke=vi.spyOn(URL,"revokeObjectURL").mockImplementation(()=>{});
 const rendered=render(<StrictMode><FileViewer view={original} onClose={()=>{}} onModeChange={()=>{}} onQuote={()=>{}}/></StrictMode>);
 fireEvent.click(screen.getByRole("button",{name:"阅读模式"}));
 fireEvent.click(screen.getByRole("button",{name:"加载图片"}));
 expect((await screen.findByRole("img",{name:"cached"})).getAttribute("src")).toBe("blob:strict-fixture");
 rendered.unmount();expect(revoke).toHaveBeenCalledWith("blob:strict-fixture");
 create.mockRestore();revoke.mockRestore();
});
