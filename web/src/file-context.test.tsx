// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FileContentCache, FileViewer, TurnFileChanges, projectCanonicalFileContexts, selectionLines, webFileContextEnabled, type OpenFile } from "./file-context";

describe("Chilling file context UI", () => {
  it("requires the runtime flag", () => {
    document.head.innerHTML = '<meta name="ariel-chilling-file-context" content="false">';
    expect(webFileContextEnabled()).toBe(false);
    document.head.innerHTML = '<meta name="ariel-chilling-file-context" content="true">';
    expect(webFileContextEnabled()).toBe(true);
  });

  it("renders bounded change cards and opens one on demand", () => {
    const open = vi.fn();
    render(<TurnFileChanges changes={[{ itemId:"i1",path:"a.go",kind:"update"},{itemId:"i2",path:"b.go",kind:"add"}]} onOpen={open}/>);
    fireEvent.click(screen.getByRole("button", { name: /a.go/ }));
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ path:"a.go" }));
  });

  it("turns a browser range into exact 1-based lines", () => {
    expect(selectionLines("one\ntwo\nthree", 4, 7)).toEqual({ startLine:2, endLine:2, text:"two" });
  });

  it("adds a validated file quote from the viewer", () => {
    const quote = vi.fn();
    const file: OpenFile = { threadId:"t",turnId:"turn",itemId:"item",path:"a.go",content:"one\ntwo",sha256:"a".repeat(64),modifiedAtMs:1,lineCount:2,language:"go",version:"current-workspace" };
    render(<FileViewer file={file} onClose={()=>{}} onQuote={quote}/>);
    fireEvent.click(screen.getByRole("button", { name:"引用第 1 行" }));
    expect(quote).toHaveBeenCalledWith(expect.objectContaining({ path:"a.go",startLine:1,endLine:1,text:"one" }));
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
});
