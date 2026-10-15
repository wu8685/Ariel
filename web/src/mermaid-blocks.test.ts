import { describe, expect, it } from "vitest";
import { projectBareMermaidBlocks } from "./mermaid-blocks";

const flowchartWithProse = [
  "flowchart TD",
  "    A[HTTP 请求进入 DCG] --> B{SecurityContext<br/>已有身份?}",
  "",
  "    B -- 是 --> C[沿用已有身份]",
  "    B -- 否 --> D[AuthTokenFilter]",
  "",
  "    D --> E{Buservice<br/>存在域账号登录态?}",
  "    E -- 是 --> F[获取 workNo]",
  "    E -- 否 --> J[保持匿名]",
  "    J --> Q{URL 安全规则}",
  "    Q -- permitAll --> R[直接进入 Controller<br/>允许没有身份]",
  "核心结论：",
  "域账号入口依赖 Buservice 登录态。",
].join("\n");

describe("bare Mermaid projection", () => {
  it("wraps the bare flowchart without swallowing the following prose", () => {
    const projected = projectBareMermaidBlocks(flowchartWithProse);
    expect(projected).toContain("```mermaid\nflowchart TD");
    expect(projected).toContain("Q -- permitAll --> R[直接进入 Controller<br/>允许没有身份]\n```");
    expect(projected).toContain("\n核心结论：\n域账号入口依赖 Buservice 登录态。");
    expect(projected.indexOf("```\n核心结论：")).toBeGreaterThan(projected.indexOf("flowchart TD"));
  });

  it("recognizes an unindented sequence diagram and stops at ordinary Markdown", () => {
    const input = [
      "sequenceDiagram",
      "participant FE as web-fe",
      "participant D as DCG",
      "FE->>D: request(\"/api/v1/...\")",
      "Note over FE,D: 无自定义 Authorization header",
      "D-->>FE: HTTP 响应",
      "结论：前端走 Buservice 域账号登录态。",
    ].join("\n");
    const projected = projectBareMermaidBlocks(input);
    expect(projected).toContain("```mermaid\nsequenceDiagram");
    expect(projected).toContain("D-->>FE: HTTP 响应\n```");
    expect(projected).toContain("\n结论：前端走 Buservice 域账号登录态。");
  });

  it("leaves fenced Mermaid and unrelated prose unchanged", () => {
    const fenced = "```mermaid\nflowchart LR\nA --> B\n```\n\nAfter";
    expect(projectBareMermaidBlocks(fenced)).toBe(fenced);
    expect(projectBareMermaidBlocks("flowchart is a useful word in prose")).toBe("flowchart is a useful word in prose");
    expect(projectBareMermaidBlocks("```go\nflowchart TD\n```")).toBe("```go\nflowchart TD\n```");
  });
});
