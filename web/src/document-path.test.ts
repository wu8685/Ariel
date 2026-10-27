import{expect,it}from "vitest";
import{resolveDocumentPath}from "./document-path";
it("resolves links relative to the opened document without allowing workspace escape",()=>{
 expect(resolveDocumentPath("docs/a/read.md","../说明.md#L3")).toEqual({path:"docs/说明.md",startLine:3});
 expect(resolveDocumentPath("docs/read.md","./other.md")).toEqual({path:"docs/other.md"});
 for(const value of ["../../secret.md","/%65tc/passwd","file:///x","https://x.test/a.md","%2e%2e/%2e%2e/secret.md",".git/config","//x.test/a.md"]){expect(resolveDocumentPath("docs/read.md",value),value).toBeNull()}
});
