// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConversationMarkdown, type WorkspaceDocumentReference } from "./markdown";

vi.mock("mermaid", () => ({ default: {
  initialize: vi.fn(),
  render: vi.fn(async (id: string) => ({ svg: `<svg xmlns="http://www.w3.org/2000/svg" id="${id}" viewBox="0 0 120 40"><text>diagram</text></svg>` })),
} }));

afterEach(cleanup);

describe("conversation Markdown projection", () => {
  it("renders CommonMark and GFM as semantic, readable chat content", () => {
    const text = [
      "## Heading", "", "**bold** and *emphasis* and ~~removed~~ with `inline`.", "",
      "> quoted", "", "- first", "- [x] done", "", "1. ordered", "", "---", "",
      "| Name | Value |", "| --- | --- |", "| key | value |", "",
      "```go", "fmt.Println(\"hello\")", "```", "",
      "A footnote[^1].", "", "[^1]: Note text.",
    ].join("\n");
    render(<ConversationMarkdown text={text} />);
    expect(screen.getByRole("heading", { name: "Heading", level: 2 })).toBeTruthy();
    expect(screen.getByText("bold").tagName).toBe("STRONG");
    expect(screen.getByText("emphasis").tagName).toBe("EM");
    expect(screen.getByText("removed").tagName).toBe("DEL");
    expect(screen.getByText("inline").tagName).toBe("CODE");
    expect(screen.getByText("quoted").closest("blockquote")).toBeTruthy();
    expect(screen.getByText("first").closest("li")).toBeTruthy();
    expect(screen.getByRole("checkbox").hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("table").querySelector("td")?.textContent).toBe("key");
    expect(screen.getByText(/fmt\.Println/).closest("pre")).toBeTruthy();
    expect(screen.getByText(/Note text\./)).toBeTruthy();
  });

  it("makes only explicit safe links navigable without automatic requests", () => {
    render(<ConversationMarkdown text={'[web](https://example.test/path) [mail](mailto:me@example.test) [local](#note) [script](javascript:alert(1)) [data](data:text/html,evil) [relative](../private) [protocol-relative](//example.test/x) [credentials](https://user:pass@example.test/)'} />);
    const web = screen.getByRole("link", { name: "web" });
    expect(web.getAttribute("href")).toBe("https://example.test/path");
    expect(web.getAttribute("target")).toBe("_blank");
    expect(web.getAttribute("rel")).toContain("noopener");
    expect(web.getAttribute("rel")).toContain("noreferrer");
    expect(screen.getByRole("link", { name: "mail" }).getAttribute("href")).toBe("mailto:me@example.test");
    expect(screen.getByRole("link", { name: "local" }).getAttribute("href")).toBe("#note");
    for (const name of ["script", "data", "relative", "protocol-relative", "credentials"]) {
      expect(screen.getByText(name).closest("a")).toBeNull();
    }
  });

  it("projects Markdown and bare workspace documents as controlled buttons", () => {
	const opened: WorkspaceDocumentReference[] = [];
	render(<ConversationMarkdown text={'[周报](docs/ops.md:7) 文件：ops-2026-10-25.md'} onOpenDocument={reference => opened.push(reference)} />);
	const markdown = screen.getByRole("button", { name: "打开文档：周报" });
	const plain = screen.getByRole("button", { name: "打开文档：ops-2026-10-25.md" });
	expect(markdown.closest("a")).toBeNull();
	fireEvent.click(markdown);
	fireEvent.click(plain);
	expect(opened).toEqual([
	  expect.objectContaining({ kind: "markdown_link", rawReference: "docs/ops.md:7", startLine: 7, endLine: 7 }),
	  expect.objectContaining({ kind: "plain_path", rawReference: "ops-2026-10-25.md" }),
	]);
  });

  it("does not promote code, URLs or email text as bare workspace documents", () => {
	render(<ConversationMarkdown text={'`secret.md`\n\n```text\nblocked.md\n```\nhttps://example.test/remote.md me@example.test <span>hidden.md</span>'} onOpenDocument={() => undefined} />);
	expect(screen.queryByRole("button", { name: /打开文档/ })).toBeNull();
  });

  it("limits one message to 24 controlled document references", () => {
    const references = Array.from({ length: 25 }, (_, index) => `[文档 ${index + 1}](docs/file-${index + 1}.md)`).join(" ");
    render(<ConversationMarkdown text={references} onOpenDocument={() => undefined} />);
    expect(screen.getAllByRole("button", { name: /打开文档/ })).toHaveLength(24);
    expect(screen.getByText("文档 25").closest("button")).toBeNull();
  });

  it("does not render raw HTML or auto-fetch Markdown images", () => {
    render(<ConversationMarkdown text={'Visible <script>alert(1)</script> <iframe src="https://example.test/"></iframe>\n\n![diagram](https://example.test/track.png) ![](data:image/png;base64,abc)'} />);
    expect(document.querySelector("script, iframe, img, video, audio, object, embed")).toBeNull();
    expect(screen.getByText("图片：diagram")).toBeTruthy();
    expect(screen.getByText("图片", { exact: true })).toBeTruthy();
  });

  it("renders only a matched local screenshot reference through the supplied loader", () => {
    render(<ConversationMarkdown text={'![local](shot%20one.png) ![remote](https://example.test/track.png)'} renderImage={(source, alt) => source === "shot%20one.png" ? <button aria-label={`加载截图：${alt}`}>截图</button> : null} />);
    expect(screen.getByRole("button", { name: "加载截图：local" })).toBeTruthy();
    expect(screen.getByText("图片：remote")).toBeTruthy();
    expect(document.querySelector("img")).toBeNull();
  });

  it("retains the entire raw body with an explicit notice when one item is too large to parse", () => {
    const text = "a".repeat(256 * 1024 + 1);
    render(<ConversationMarkdown text={text} />);
    expect(screen.getByText(/内容过长，暂以纯文本显示/)).toBeTruthy();
    expect(document.querySelector(".markdown-fallback-text")?.textContent).toBe(text);
  });

  it("keeps unfinished Markdown readable during streaming", () => {
    render(<ConversationMarkdown text={'**unfinished\n\n```go\nfmt.Println(1)'} />);
    expect(document.querySelector(".markdown-body")?.textContent).toContain("unfinished");
    expect(document.querySelector(".markdown-body")?.textContent).toContain("fmt.Println(1)");
  });

  it("retains single chat line breaks inside a paragraph", () => {
    render(<ConversationMarkdown text={"first line\nsecond line"} />);
    expect(document.querySelector(".markdown-body p")?.textContent).toBe("first line\nsecond line");
  });

  it("keeps footnote targets distinct across different messages", () => {
    const text = "A note[^1].\n\n[^1]: The note.";
    render(<><ConversationMarkdown text={text} /><ConversationMarkdown text={text} /></>);
    const references = [...document.querySelectorAll<HTMLAnchorElement>('a[data-footnote-ref]')];
    expect(references).toHaveLength(2);
    expect(references[0].getAttribute("href")).not.toBe(references[1].getAttribute("href"));
    for (const reference of references) {
      const target = reference.getAttribute("href")?.slice(1);
      expect(target && document.getElementById(target)).toBeTruthy();
    }
  });

  it("renders fenced Mermaid as an accessible inline diagram", async () => {
    render(<ConversationMarkdown text={"```mermaid\nflowchart LR\nA[开始] --> B[完成]\n```"} />);
    const diagram = await screen.findByRole("img", { name: "Mermaid 图表" });
    expect(diagram.querySelector("svg")).toBeTruthy();
    expect(diagram.textContent).not.toContain("flowchart LR");
  });

  it("renders a bare Mermaid block while retaining prose after it", async () => {
    const text = "flowchart TD\n    A[请求] --> B{有身份?}\n    B -- 是 --> C[进入 Controller]\n核心结论：\n后续正文仍然可读。";
    render(<ConversationMarkdown text={text} />);
    expect(await screen.findByRole("img", { name: "Mermaid 图表" })).toBeTruthy();
    expect(document.querySelector(".markdown-body")?.textContent).toContain("核心结论：\n后续正文仍然可读。");
  });
});
