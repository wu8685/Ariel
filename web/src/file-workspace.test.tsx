// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  FileWorkspace,
  FileWorkspaceModel,
  type FileTarget,
} from "./file-workspace";

afterEach(cleanup);
const target = (path: string): FileTarget => ({
  deviceId: "mac",
  agentEpoch: "e",
  threadId: "t",
  path,
  source: "workspace_browser",
});
const file = (path: string, content = "# " + path) => ({
  path,
  content,
  sha256: "a".repeat(64),
  modifiedAtMs: 1,
  lineCount: content.split("\n").length,
  language: "md",
  version: "current-workspace",
});
const request = vi.fn(
  async (
    _method: string,
    _device: string,
    params: Record<string, unknown>,
  ) => ({ outcome: "accepted", data: file(String(params.path)) }),
);
function chooseView(name: string) {
  fireEvent.click(screen.getByRole("button", { name: "文件菜单" }));
  fireEvent.click(screen.getByRole("button", { name }));
}
function mount(model: FileWorkspaceModel, send: Parameters<FileWorkspaceModel["load"]>[1] = request) {
  return render(
    <>
      <textarea aria-label="主会话" />
      <FileWorkspace
        model={model}
        request={send}
        currentThreadId="t"
        onQuote={() => {}}
      />
    </>,
  );
}

it("keeps file views only in the existing dropdown and closes the menu without collapsing the file", async () => {
  const model = new FileWorkspaceModel();
  model.open(target("a.md"));
  mount(model);
  await screen.findByText("# a.md");
  expect(screen.queryByRole("button", { name: "阅读模式" })).toBeNull();
  expect(document.querySelector(".file-workspace-body nav")).toBeNull();
  chooseView("阅读模式");
  await screen.findByRole("heading", { name: "a.md" });
  expect(screen.queryByRole("button", { name: "阅读模式" })).toBeNull();
  const trigger = screen.getByRole("button", { name: "文件菜单" });
  expect(document.activeElement).toBe(trigger);
  fireEvent.click(trigger);
  expect(screen.getByRole("button", { name: "阅读模式" }).getAttribute("aria-pressed")).toBe("true");
  fireEvent.keyDown(screen.getByRole("button", { name: "阅读模式" }), { key: "Escape" });
  expect(screen.queryByRole("button", { name: "阅读模式" })).toBeNull();
  expect(screen.getByTestId("file-reading")).toBeTruthy();
  expect(document.activeElement).toBe(trigger);
  fireEvent.click(trigger);
  fireEvent.pointerDown(screen.getByRole("heading", { name: "a.md" }));
  expect(screen.queryByRole("button", { name: "阅读模式" })).toBeNull();
  chooseView("原版模式");
  expect(screen.getByTestId("file-source")).toBeTruthy();
});

it("does not offer reading for non-Markdown or a Diff without change provenance", async () => {
  const model = new FileWorkspaceModel();
  model.open(target("a.go"));
  mount(model);
  await screen.findByText("# a.go");
  fireEvent.click(screen.getByRole("button", { name: "文件菜单" }));
  expect(screen.getByRole("button", { name: "原版模式" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "阅读模式" })).toBeNull();
  expect(screen.queryByRole("button", { name: "查看 Diff" })).toBeNull();
});

it("disables reading until the current document finishes loading", async () => {
  const model = new FileWorkspaceModel();
  model.open(target("a.md"));
  let finish!: (value: {outcome:string;data:ReturnType<typeof file>}) => void;
  mount(model, () => new Promise<{outcome:string;data:ReturnType<typeof file>}>(resolve => { finish = resolve; }));
  fireEvent.click(screen.getByRole("button", { name: "文件菜单" }));
  expect((screen.getByRole("button", { name: "阅读模式" }) as HTMLButtonElement).disabled).toBe(true);
  await act(async () => finish({ outcome: "accepted", data: file("a.md") }));
  expect((screen.getByRole("button", { name: "阅读模式" }) as HTMLButtonElement).disabled).toBe(false);
});

it("keeps 150 logical file tabs without retaining every body or subscribing", async () => {
  const model = new FileWorkspaceModel();
  for (let i = 0; i < 150; i++) model.open(target(`file-${i}.md`));
  mount(model);
  await screen.findByText("# file-149.md");
  expect(model.tabs).toHaveLength(150);
  expect(document.querySelectorAll(".file-viewer")).toHaveLength(1);
  fireEvent.click(screen.getByRole("button", { name: "文件菜单" }));
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索文件标签" }), {
    target: { value: "file-0.md" },
  });
  fireEvent.click(screen.getByRole("option", { name: "file-0.md" }));
  await screen.findByText("# file-0.md");
  expect(model.tabs).toHaveLength(150);
  expect(model.open(target("file-0.md"))).toBe(model.activeId);
  expect(model.tabs).toHaveLength(150);
  expect(
    request.mock.calls.every(([method]) => method === "workspace.file.read"),
  ).toBe(true);
});

it("does not invalidate a fresh restore/read with a historical change query that began before it", async()=>{
  const model=new FileWorkspaceModel(), fence=model.captureResources();
  const id=model.open(target("a.md"));await model.load(id,request);
  model.invalidate("mac","t","a.md",fence);
  expect(model.view(id)?.file?.content).toBe("# a.md");
  const old=model.captureResources();model.invalidate("mac","t","a.md",old);
  expect(model.view(id)?.file).toBeUndefined();await model.load(id,request);
  model.invalidate("mac","t","a.md",old);expect(model.view(id)?.file?.content).toBe("# a.md");
});

it("does not invalidate current-workspace content with its own already completed source change",async()=>{
  const model=new FileWorkspaceModel();
  const id=model.open({...target("a.md"),source:"turn_change",turnId:"turn",itemId:"change"});await model.load(id,request);
  model.invalidate("mac","t","a.md",model.captureResources(),{turnId:"turn",itemId:"change"});
  expect(model.view(id)?.file?.content).toBe("# a.md");
});

it("restores per-tab reading mode, scroll and selected offsets after switch or collapse", async () => {
  const model = new FileWorkspaceModel();
  const a = model.open(target("a.md"));
  mount(model);
  await screen.findByText("# a.md");
  const source = screen.getByTestId("file-source");
  source.scrollTop = 75;
  fireEvent.scroll(source);
  const node = source.querySelector("code")!.firstChild!;
  const range = document.createRange();
  range.setStart(node, 2);
  range.setEnd(node, 3);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  fireEvent.mouseUp(source);
  expect(
    (screen.getByRole("button", { name: "引用到会话" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
  act(() => {
    model.open(target("b.md"));
  });
  await screen.findByText("# b.md");
  act(() => model.activate(a));
  await screen.findByText("# a.md");
  expect(screen.getByTestId("file-source").scrollTop).toBe(75);
  expect(
    (screen.getByRole("button", { name: "引用到会话" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "文件菜单" }));
  fireEvent.click(screen.getByRole("button", { name: "阅读模式" }));
  await screen.findByRole("heading", { name: "a.md" });
  fireEvent.change(screen.getByRole("textbox", { name: "主会话" }), {
    target: { value: "草稿保留" },
  });
  fireEvent.click(screen.getByRole("button", { name: "收起文件工作区" }));
  expect(screen.queryByTestId("file-reading")).toBeNull();
  fireEvent.keyDown(screen.getByRole("textbox", { name: "主会话" }), {
    key: "Escape",
  });
  fireEvent.click(screen.getByRole("button", { name: /展开文件工作区/ }));
  await screen.findByRole("heading", { name: "a.md" });
  expect(
    (screen.getByRole("textbox", { name: "主会话" }) as HTMLTextAreaElement)
      .value,
  ).toBe("草稿保留");
});

it("bounds body bytes, keeps metadata-only tabs and lazily reloads evicted sources", async () => {
  const model = new FileWorkspaceModel(1, 100);
  const send = vi.fn(
    async (_m: string, _d: string, p: Record<string, unknown>) => ({
      outcome: "accepted",
      data: file(String(p.path)),
    }),
  );
  const a = model.open(target("a.md"));
  mount(model, send);
  await screen.findByText("# a.md");
  act(() => {
    model.open(target("b.md"));
  });
  await screen.findByText("# b.md");
  expect(model.resourceCount).toBe(1);
  expect(JSON.stringify(model.tabs)).not.toContain('"content"');
  expect(model.bytes).toBeLessThanOrEqual(100);
  act(() => model.activate(a));
  await screen.findByText("# a.md");
  expect(send).toHaveBeenCalledTimes(3);
});

it("isolates late reads, closed tab responses, errors, retries and identity clearing", async () => {
  const model = new FileWorkspaceModel();
  const pending = new Map<string, (value: unknown) => void>();
  const send = vi.fn(
    (_m: string, _d: string, p: Record<string, unknown>) =>
      new Promise<unknown>((resolve) => pending.set(String(p.path), resolve)),
  ) as typeof request;
  const a = model.open(target("a.md"));
  mount(model, send);
  await waitFor(() => expect(pending.has("a.md")).toBe(true));
  act(() => {
    model.open(target("b.md"));
  });
  await waitFor(() => expect(pending.has("b.md")).toBe(true));
  await act(async () =>
    pending.get("a.md")!({ outcome: "accepted", data: file("a.md") }),
  );
  expect(model.activeId).not.toBe(a);
  await act(async () =>
    pending.get("b.md")!({
      outcome: "rejected",
      error: { code: "PATH_NOT_ALLOWED" },
    }),
  );
  expect(screen.getByRole("alert").textContent).toContain("读取");
  fireEvent.click(screen.getByRole("button", { name: "重试读取文件" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(3));
  act(() => model.close(model.activeId));
  await screen.findByText("# a.md");
  await act(async () =>
    pending.get("b.md")!({ outcome: "accepted", data: file("b.md") }),
  );
  expect(model.tabs).toHaveLength(1);
  act(() => model.clear());
  expect(model.tabs).toHaveLength(0);
  expect(model.resourceCount).toBe(0);
});

it("retains original proof and refuses quoting into another conversation", async () => {
  const model = new FileWorkspaceModel();
  model.open({
    ...target("a.md"),
    source: "message_reference",
    turnId: "turn",
    itemId: "answer",
    referenceKind: "markdown_link",
    rawReference: "a.md:2",
  });
  const send = vi.fn(async () => ({
    outcome: "accepted",
    data: file("a.md", "one\ntwo"),
  }));
  const quote = vi.fn();
  const rendered = render(
    <FileWorkspace
      model={model}
      request={send}
      currentThreadId="other"
      onQuote={quote}
    />,
  );
  await screen.findByText("two");
  expect(screen.queryByRole("button", { name: "引用到会话" })).toBeNull();
  const blockedRange = document.createRange();
  blockedRange.selectNodeContents(screen.getByTestId("file-source").querySelectorAll("code")[1]);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(blockedRange);
  fireEvent.mouseUp(screen.getByTestId("file-source"));
  expect(screen.getByText(/切回来源会话/)).toBeTruthy();
  expect((screen.getByRole("button", { name: "引用到会话" }) as HTMLButtonElement).disabled).toBe(true);
  rendered.rerender(
    <FileWorkspace
      model={model}
      request={send}
      currentThreadId="t"
      onQuote={quote}
    />,
  );
  const node = screen
    .getByTestId("file-source")
    .querySelectorAll("code")[1].firstChild!;
  const range = document.createRange();
  range.selectNodeContents(node);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  fireEvent.mouseUp(screen.getByTestId("file-source"));
  fireEvent.click(screen.getByRole("button", { name: "引用到会话" }));
  expect(quote).toHaveBeenCalledWith(
    expect.objectContaining({
      threadId: "t",
      referenceKind: "markdown_link",
      rawReference: "a.md:2",
      text: "two",
    }),
  );
});

it("can reload a Diff after its cached body was evicted", async () => {
  const model = new FileWorkspaceModel(1, 1000);
  const send = vi.fn(
    async (method: string, _device: string, p: Record<string, unknown>) =>
      method === "file.context.changes"
        ? {
            outcome: "accepted",
            data: {
              turns: [
                {
                  turnId: "turn",
                  fileChanges: [
                    { itemId: "change", path: "a.md", diff: "-old\n+new" },
                  ],
                },
              ],
            },
          }
        : { outcome: "accepted", data: file(String(p.path)) },
  );
  const a = model.open(
    {
      ...target("a.md"),
      source: "turn_change",
      turnId: "turn",
      itemId: "change",
    },
    "source",
    { diff: "-old\n+new" },
  );
  render(
    <FileWorkspace
      model={model}
      request={send}
      currentThreadId="t"
      onQuote={() => {}}
    />,
  );
  await screen.findByText("# a.md");
  act(() => {
    model.open(target("b.md"));
  });
  await screen.findByText("# b.md");
  act(() => model.activate(a));
  await screen.findByText("# a.md");
  fireEvent.click(screen.getByRole("button", { name: "文件菜单" }));
  expect(
    (screen.getByRole("button", { name: "查看 Diff" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "查看 Diff" }));
  await screen.findByText("+new");
});

it("invalidates selected offsets when the source SHA changes and ignores a late cleared read", async () => {
  const model = new FileWorkspaceModel();
  let version = "a";
  const send = vi.fn(
    async (_m: string, _d: string, p: Record<string, unknown>) => ({
      outcome: "accepted",
      data: { ...file(String(p.path)), sha256: version.repeat(64) },
    }),
  );
  const a = model.open(target("a.md"));
  mount(model, send);
  await screen.findByText("# a.md");
  model.tabs[0].reader.selection = { start: 0, end: 1 };
  model.tabs[0].reader.positions.source = 100;
  model.tabs[0].reader.reading = true;
  version = "b";
  act(() => model.invalidate("mac", "t", "a.md"));
  await screen.findByText("文件已变化，旧选区已清除。");
  expect(model.tabs[0].reader.selection).toBeUndefined();
  expect(model.tabs[0].reader.positions.source).toBe(0);
  act(() => model.close(a));
  expect(model.tabs).toHaveLength(0);
  let finish!: (value: Awaited<ReturnType<typeof request>>) => void;
  const pending = vi.fn(
    () =>
      new Promise<Awaited<ReturnType<typeof request>>>(
        (resolve) => (finish = resolve),
      ),
  );
  const b = model.open(target("b.md"));
  const loading = model.load(b, pending);
  model.clear();
  finish({ outcome: "accepted", data: file("b.md") });
  await loading;
  expect(model.resourceCount).toBe(0);
  expect(model.tabs).toHaveLength(0);
});

it("a rewritten conversation removes only its own tabs, preserving another conversation", () => {
  const model = new FileWorkspaceModel();
  model.open(target("a.md"));
  const other = model.open({ ...target("b.md"), threadId: "other" });
  model.removeThread("mac", "t");
  expect(model.tabs).toHaveLength(1);
  expect(model.activeId).toBe(other);
});

it("keeps keyboard focus on the adjacent tab when closing a tab", async () => {
  const model = new FileWorkspaceModel();
  model.open(target("a.md"));
  mount(model);
  await screen.findByText("# a.md");
  act(() => {
    model.open(target("b.md"));
  });
  await screen.findByText("# b.md");
  const tab = screen.getByRole("tab", { name: "b.md" });
  tab.focus();
  fireEvent.keyDown(tab, { key: "Delete" });
  expect(document.activeElement).toBe(
    screen.getByRole("tab", { name: "a.md" }),
  );
});

it("a late background response cannot evict the visible file", async () => {
  const model = new FileWorkspaceModel(1, 1000);
  let finish!: (value: Awaited<ReturnType<typeof request>>) => void;
  const pending = vi.fn(
    () =>
      new Promise<Awaited<ReturnType<typeof request>>>(
        (resolve) => (finish = resolve),
      ),
  );
  const a = model.open(target("a.md"));
  const loading = model.load(a, pending);
  const b = model.open(target("b.md"));
  await model.load(b, request);
  finish({ outcome: "accepted", data: file("a.md") });
  await loading;
  expect(model.view(b)?.file?.path).toBe("b.md");
  expect(model.resourceCount).toBe(1);
});
