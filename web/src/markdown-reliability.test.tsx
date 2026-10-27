// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ConversationMarkdown } from "./markdown";
import { DocumentReading } from "./document-reading";
import { DocumentImageSession, type LoadedImage } from "./document-assets";
import { selectionFromDOM, type OpenFile } from "./file-context";

const observers: Array<{ callback: IntersectionObserverCallback; target?: Element }> = [];
beforeEach(() => {
  vi.stubGlobal("IntersectionObserver", class {
    entry: typeof observers[number];
    constructor(callback: IntersectionObserverCallback) { this.entry = { callback }; observers.push(this.entry); }
    observe(target: Element) { this.entry.target = target; }
    disconnect() { const index = observers.indexOf(this.entry); if (index >= 0) observers.splice(index, 1); }
  });
});
afterEach(() => { cleanup(); observers.length = 0; vi.restoreAllMocks(); vi.unstubAllGlobals(); });
function visible(near = true) {
  act(() => { for (const observer of [...observers]) observer.callback([{ isIntersecting: near, target: observer.target }] as IntersectionObserverEntry[], {} as IntersectionObserver); });
}
const file = { content: "", path: "docs/read.md", sha256: "a".repeat(64) } as OpenFile;
const allowed = new Set<string>();
const noop = () => {};
function reading(content: string, session = new DocumentImageSession(file)) {
  return <DocumentReading content={content} session={session} allowed={allowed} onAllow={noop} />;
}

it("renders all four formula delimiters without consuming TeX commands in either Markdown view", async () => {
  const text = String.raw`行内 $x^2 + \frac{a}{b}$ 与 \(y_1\)。

$$
\sum_{i=1}^{n} i
$$

\[\begin{pmatrix}a & b\\c & d\end{pmatrix}\]`;
  const rendered = render(<ConversationMarkdown text={text} />);
  await waitFor(() => expect(rendered.container.querySelectorAll(".katex")).toHaveLength(4));
  expect(rendered.container.querySelectorAll(".markdown-math-display")).toHaveLength(2);
  rendered.unmount();
  const document = render(reading(text));
  await waitFor(() => expect(document.container.querySelectorAll(".katex")).toHaveLength(4));
});

it("leaves ordinary prices, escaped delimiters, inline code and pseudocode untouched", async () => {
  const text = [String.raw`价格 $20 和 $30；\$x\$；\(x^2\)。`, "", "`$a$ \\(b\\)`", "", "```pseudocode", "IF $20 THEN", "\tFOR each item:", "        emit(item)", "", "```"].join("\n");
  const rendered = render(<ConversationMarkdown text={text} />);
  await waitFor(() => expect(rendered.container.querySelectorAll(".katex")).toHaveLength(1));
  expect(rendered.container.textContent).toContain("价格 $20 和 $30；$x$；");
  expect(rendered.container.querySelector("pre code")?.textContent).toBe("IF $20 THEN\n\tFOR each item:\n        emit(item)\n\n");
  expect(rendered.container.querySelector("p code")?.textContent).toBe("$a$ \\(b\\)");
});

it("keeps incomplete streaming math readable and renders when its delimiter arrives", async () => {
  const partial = String.raw`结果 \[\frac{a}{b}`;
  const rendered = render(<ConversationMarkdown text={partial} />);
  expect(rendered.container.textContent).toContain(partial);
  expect(rendered.container.querySelector(".katex")).toBeNull();
  rendered.rerender(<ConversationMarkdown text={String.raw`结果 \[\frac{a}{b}\] 完成`} />);
  await waitFor(() => expect(rendered.container.querySelector(".katex")).not.toBeNull());
  expect(rendered.container.textContent).toContain("完成");
});

it("keeps multiline formula source and separates macros, code, lists and neighboring prices", async () => {
  const text = [
    "- 行内 $x_1$", "", String.raw`> \[`, String.raw`> \begin{aligned}`,
    "> a &= b + c " + "\\".repeat(2), "> d &= e", String.raw`> \end{aligned}`, String.raw`> \]`, "",
    "金额 $20 与 $30，后面是公式 $a+b$。", "", String.raw`$\gdef\arielLeak{x} y$ $\arielLeak$`, "",
    "```text", String.raw`\[not a formula\]`, "```",
  ].join("\n");
  const rendered = render(<ConversationMarkdown text={text} />);
  await waitFor(() => expect(rendered.container.querySelectorAll(".katex")).toHaveLength(4));
  expect(rendered.container.textContent).toContain("金额 $20 与 $30，后面是公式");
  expect(rendered.container.querySelector("pre code")?.textContent).toBe(String.raw`\[not a formula\]` + "\n");
  expect(rendered.container.textContent).toContain("公式无法安全解析，已显示原文。");
});

it("rejects unsafe and oversized formulas locally without executing or hiding adjacent prose", async () => {
  const tooLong = "a".repeat(16 * 1024 + 1);
  const rendered = render(<ConversationMarkdown text={String.raw`保留正文 $\href{https://example.test}{x}$ $\includegraphics{https://example.test/a.png}$ $\unknowncommand{x}$` + `\n\n$$${tooLong}$$\n\n结尾`} />);
  await screen.findAllByText(/公式.*原文/);
  expect(rendered.container.querySelector("a, img, iframe, script")).toBeNull();
  expect(rendered.container.textContent).toContain("保留正文");
  expect(rendered.container.textContent).toContain("结尾");
  expect(rendered.container.textContent).toContain(tooLong);
});

it("bounds formula count and isolates formula source from file quotation mappings", async () => {
  const text = "前文 " + Array.from({ length: 257 }, () => "$x^2$").join(" ") + " 后文";
  const rendered = render(reading(text));
  await waitFor(() => expect(rendered.container.querySelectorAll(".katex")).toHaveLength(256));
  const math = rendered.container.querySelector(".katex")!;
  expect(math.closest("[data-source-start]")).toBeNull();
  expect(rendered.container.textContent).toContain("公式数量超过限制");
  const root = rendered.container;
  const spans = [...root.querySelectorAll<HTMLElement>("[data-source-start]")];
  const first = spans.find(span => span.textContent?.includes("前文"))!;
  const last = spans.find(span => span.textContent?.includes("后文"))!;
  const range = document.createRange(); range.setStart(first.firstChild!, 0); range.setEnd(last.firstChild!, last.textContent!.length);
  const selection = getSelection()!; selection.removeAllRanges(); selection.addRange(range);
  expect(selectionFromDOM(text, root, selection)).toBeNull();
});

it("offers explicit permission and retry for ordinary conversation images while refusing unproven local paths", async () => {
  render(<ConversationMarkdown text={"![图](https://images.example.test/a.png) ![本地](../a.png)"} />);
  expect(document.querySelector("img")).toBeNull();
  expect(screen.getByText(/images.example.test/)).toBeTruthy();
  expect(screen.getByText(/不支持此图片来源/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "允许加载外部图片" }));
  visible();
  const image = await screen.findByRole("img", { name: "图" });
  expect(image.getAttribute("referrerpolicy")).toBe("no-referrer");
  fireEvent.error(image);
  expect(await screen.findByText(/外部图片加载失败/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "重试图片" }));
  expect(await screen.findByRole("img", { name: "图" })).toBeTruthy();
});

it("revokes conversation image permission on message version changes", async () => {
  const rendered = render(<ConversationMarkdown text={"![图](https://images.example.test/a.png) 原文"} />);
  fireEvent.click(screen.getByRole("button", { name: "允许加载外部图片" })); visible();
  await screen.findByRole("img", { name: "图" });
  rendered.rerender(<ConversationMarkdown text={"![图](https://images.example.test/a.png) 更新正文"} />);
  expect(document.querySelector("img")).toBeNull();
  expect(screen.getByRole("button", { name: "允许加载外部图片" })).toBeTruthy();
  rendered.rerender(<ConversationMarkdown text={"![图](https://images.example.test/a.png) 原文"} />);
  expect(document.querySelector("img")).toBeNull();
  expect(screen.getByRole("button", { name: "允许加载外部图片" })).toBeTruthy();
});

it("retries a failed document image in place without rereading the parent document", async () => {
  const resource = { url: "blob:fixture", width: 2, height: 2, release: vi.fn() };
  const acquire = vi.spyOn(DocumentImageSession.prototype, "acquire").mockRejectedValueOnce(new Error("读取暂时失败")).mockResolvedValue(resource);
  render(reading("![本地](images/a.png)")); visible();
  await screen.findByText(/读取暂时失败/);
  fireEvent.click(screen.getByRole("button", { name: "重试图片" }));
  const image = await screen.findByRole("img", { name: "本地" }); fireEvent.load(image);
  expect(acquire).toHaveBeenCalledTimes(2);
});

it("distinguishes a Blob decode failure from successful document image loading", async () => {
  vi.spyOn(DocumentImageSession.prototype, "acquire").mockResolvedValue({ url: "blob:bad", width: 2, height: 2, release: vi.fn() });
  render(reading("![损坏](images/bad.png)")); visible();
  const image = await screen.findByRole("img", { name: "损坏" });
  expect(screen.getByText(/正在解码图片/)).toBeTruthy();
  fireEvent.error(image);
  expect(await screen.findByText(/图片无法解码/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "重试图片" })).toBeTruthy();
});

it("keeps image resources and pending reads across unrelated rerenders", async () => {
  let resolve!: (image: LoadedImage) => void;
  const release = vi.fn();
  const acquire = vi.spyOn(DocumentImageSession.prototype, "acquire").mockImplementation(() => new Promise(done => { resolve = done; }));
  const session = new DocumentImageSession(file), content = "![稳定](images/stable.png)";
  const rendered = render(<DocumentReading content={content} session={session} allowed={allowed} onAllow={noop} onOpenDocument={noop} />);
  visible(); await waitFor(() => expect(acquire).toHaveBeenCalledTimes(1));
  rendered.rerender(<DocumentReading content={content} session={session} allowed={allowed} onAllow={noop} onOpenDocument={() => {}} />);
  visible(); expect(acquire).toHaveBeenCalledTimes(1);
  resolve({ url: "blob:stable", width: 2, height: 2, release });
  const image = await screen.findByRole("img", { name: "稳定" }); fireEvent.load(image);
  visible(false); visible(true);
  expect(acquire).toHaveBeenCalledTimes(1);
  expect(release).not.toHaveBeenCalled();
  rendered.unmount(); expect(release).toHaveBeenCalledTimes(1);
});

it("reuses a previously decoded image when a mode restore places it outside the lazy viewport",async()=>{
  const source="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==";
  vi.spyOn(URL,"createObjectURL").mockReturnValue("blob:cached");vi.spyOn(URL,"revokeObjectURL").mockImplementation(()=>{});
  const session=new DocumentImageSession(file),cached=await session.acquire(source,new AbortController().signal);cached.release();
  render(reading(`![缓存](${source})`,session));
  // No IntersectionObserver callback: this is an existing memory lease, not
  // permission to fetch an unseen offscreen asset from the Desktop Agent.
  expect((await screen.findByRole("img",{name:"缓存"})).getAttribute("src")).toBe("blob:cached");
});
