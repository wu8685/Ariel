import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const token = `s_${"b".repeat(64)}`;
const thread: Thread = {
  threadId: "tabs",
  title: "文件标签验收",
  cwd: "/work/tabs",
  runtime: "idle",
  updatedAt: "2026-10-27T00:00:00Z",
  turns: [
    {
      turnId: "turn",
      status: "completed",
      items: [
        {
          itemId: "answer",
          role: "assistant",
          text: "文档：[第二个文件](b.md)",
        },
      ],
    },
  ],
  pendingInteractions: [],
};
async function setup(page: Page, many = false) {
  const methods: string[] = [];
  const reads: string[] = [];
  const current = many
    ? {
        ...thread,
        turns: [
          {
            turnId: "turn",
            status: "completed",
            items: Array.from({ length: 150 }, (_, i) => ({
              itemId: `answer-${i}`,
              role: "assistant",
              text: `[文件 ${i}](file-${i}.md)`,
            })),
          },
        ],
      }
    : thread;
  await page.route("**/", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(
        "__ARIEL_CHILLING_FILE_CONTEXT__",
        "true",
      ),
    });
  });
  await page.addInitScript(
    (value) => sessionStorage.setItem("ariel.web-session.v1", value),
    token,
  );
  await page.routeWebSocket("**/ws", (socket) =>
    socket.onMessage((raw) => {
      const m = JSON.parse(String(raw));
      if (m.type === "hello") {
        socket.send(
          JSON.stringify({
            type: "hello.ok",
            v: 1,
            connectionId: "tabs",
            relayEpoch: "tabs",
            sessionToken: token,
          }),
        );
        return;
      }
      if (m.type !== "request") return;
      methods.push(m.method);
      let data: Record<string, unknown> = {};
      if (m.method === "device.list")
        data = {
          devices: [
            {
              deviceId: "mac",
              deviceName: "Tab Mac",
              agentOnline: true,
              codexReady: true,
              agentEpoch: "e",
              capabilities: {
                autoLoad: true,
                send: true,
                chillingFileContext: true,
                chillingMessageReferences: true,
                chillingWorkspaceBrowser: true,
              },
            },
          ],
        };
      if (m.method === "thread.list")
        data = { threads: [current], nextCursor: "" };
      if (m.method === "thread.cache.watch")
        data = {
          watchId: "w",
          cacheStreamId: "s",
          seq: 1,
          agentEpoch: "e",
          agentOnline: true,
          freshness: "fresh",
          capturedAt: "2026-10-27T00:00:00Z",
          thread: current,
        };
      if (m.method === "file.context.changes")
        data = {
          turns: [
            {
              turnId: "turn",
              fileChanges: [
                {
                  itemId: "change",
                  path: "a.md",
                  kind: "update",
                  diff: "@@ -1 +1 @@\n-old\n+new",
                },
              ],
            },
          ],
        };
      if (
        [
          "file.context.read",
          "file.context.reference.read",
          "workspace.file.read",
        ].includes(m.method)
      ) {
        const path =
          m.method === "file.context.reference.read"
            ? String(m.params.rawReference)
            : m.params.path;
        reads.push(path);
        data = {
          path,
          content:
            `# ${path}\n\n` +
            Array.from(
              { length: 80 },
              (_, i) => `第 ${i + 1} 行正文：` + "可换行的阅读文本".repeat(8),
            ).join("\n"),
          sha256: "a".repeat(64),
          modifiedAtMs: 1,
          lineCount: 82,
          language: "md",
          version: "current-workspace",
        };
      }
      if (m.method === "workspace.files.list")
        data = {
          path: "",
          revision: "r",
          entries: [
            {
              name: "c.md",
              path: "c.md",
              kind: "file",
              readable: true,
              sizeBytes: 5,
              modifiedAtMs: 1,
            },
          ],
        };
      socket.send(
        JSON.stringify({
          type: "response",
          v: 1,
          requestId: m.requestId,
          outcome: "accepted",
          data,
        }),
      );
    }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 tabs，/work/tabs" }).click();
  await page.getByText(thread.title).click();
  return { methods, reads };
}

for (const width of [320, 390, 1280])
  test(`file tabs coexist with the composer and restore reading at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const { methods, reads } = await setup(page);
    const composer = page.getByRole("textbox", { name: "发送消息" });
    await composer.fill("保留主会话草稿");
    await page.getByRole("button", { name: "查看原文：a.md" }).click();
    await expect(page.getByRole("tablist", { name: "文件标签" })).toBeVisible();
    await expect(page.getByTestId("file-source")).toContainText("# a.md");
    const panel = page.getByRole("region", { name: "文件工作区" });
    const p = await panel.boundingBox();
    const c = await composer.boundingBox();
    expect(p!.x + p!.width).toBeLessThanOrEqual(width + 1);
    if (width > 800) expect(p!.x).toBeGreaterThanOrEqual(c!.x + c!.width);
    else expect(p!.y + p!.height).toBeLessThanOrEqual(c!.y);
    await composer.fill("文件仍打开时可以编辑");
    await page.getByRole("button", { name: "收起文件工作区" }).click();
    await page.getByRole("button", { name: "打开文档：第二个文件" }).click();
    await expect(
      page.getByRole("tab", { name: "a.md", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("tab", { name: "b.md", exact: true }),
    ).toBeVisible();
    await page.getByRole("tab", { name: "a.md", exact: true }).click();
    await page.getByRole("button", { name: "阅读模式" }).click();
    const reading = page.getByTestId("file-reading");
    await expect(reading.getByRole("heading", { name: "a.md" })).toBeVisible();
    await reading.evaluate((el) => {
      el.scrollTop = 170;
    });
    await page.getByRole("tab", { name: "b.md", exact: true }).click();
    await expect(page.getByTestId("file-source")).toContainText("# b.md");
    await page.getByRole("tab", { name: "a.md", exact: true }).click();
    await expect.poll(() => reading.evaluate((el) => el.scrollTop)).toBe(170);
    await page.getByRole("button", { name: "收起文件工作区" }).click();
    await expect(reading).toHaveCount(0);
    await page.getByRole("button", { name: /展开文件工作区/ }).click();
    await expect.poll(() => reading.evaluate((el) => el.scrollTop)).toBe(170);
    expect(reads).toEqual(["a.md", "b.md"]);
    await expect(composer).toHaveValue("文件仍打开时可以编辑");
    expect(
      methods.filter((m) =>
        ["thread.subscribe", "thread.bootstrap", "turn.start"].includes(m),
      ),
    ).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await expect(page).toHaveScreenshot(`file-tabs-${width}.png`, {
      animations: "disabled",
      caret: "hide",
    });
    await page.getByRole("button", { name: "收起文件工作区" }).click();
    await page.getByRole("button", { name: "浏览工作区文件" }).click();
    await page.getByRole("button", { name: "打开文件 c.md" }).click();
    await expect(
      page.getByRole("tab", { name: "c.md", exact: true }),
    ).toBeVisible();
    await expect(page.getByTestId("file-source")).toContainText("# c.md");
    await page.getByRole("button", { name: "关闭标签 c.md" }).click();
    await expect(
      page.getByRole("tab", { name: "c.md", exact: true }),
    ).toHaveCount(0);
  });

test("150 file tabs keep one body and one socket, searchable without losing metadata", async ({
  page,
}) => {
  const { methods } = await setup(page, true);
  // All source link handlers are exercised in one browser event batch; inactive
  // tabs should remain lightweight rather than fetching 150 hidden bodies.
  await page
    .getByRole("button", { name: /^打开文档：文件 \d+$/ })
    .evaluateAll((buttons) => {
      for (const button of buttons) (button as HTMLButtonElement).click();
    });
  await expect(page.getByRole("tab")).toHaveCount(150);
  await expect(page.getByTestId("file-source")).toHaveCount(1);
  await expect(page.getByTestId("file-source")).toContainText("# file-149.md");
  expect(
    methods.filter((m) => m === "file.context.reference.read"),
  ).toHaveLength(1);
  expect(methods.filter((m) => m === "thread.cache.watch")).toHaveLength(1);
  await page.getByRole("button", { name: "全部文件标签" }).click();
  await expect(page.getByRole("option")).toHaveCount(40);
  await page.getByRole("searchbox", { name: "搜索文件标签" }).fill("file-0.md");
  await page.getByRole("option", { name: "file-0.md", exact: true }).click();
  await expect(page.getByTestId("file-source")).toContainText("# file-0.md");
  await expect(page.getByRole("tab")).toHaveCount(150);
  await page.getByRole("tab", { name: "file-0.md", exact: true }).press("End");
  await expect(
    page.getByRole("tab", { name: "file-149.md", exact: true }),
  ).toBeFocused();
});

test("file selection survives switching and shrinking the viewport keeps the composer usable", async ({
  page,
}) => {
  const { reads } = await setup(page);
  await page.getByRole("button", { name: "查看原文：a.md" }).click();
  await page.getByTestId("file-source").evaluate((root) => {
    const node = root.querySelectorAll("code")[2].firstChild!;
    const range = document.createRange();
    range.setStart(node, 0);
    range.setEnd(node, 4);
    getSelection()!.removeAllRanges();
    getSelection()!.addRange(range);
    root.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
  });
  await expect(page.getByRole("button", { name: "引用到会话" })).toBeEnabled();
  await page.getByRole("button", { name: "收起文件工作区" }).click();
  await page.getByRole("button", { name: "打开文档：第二个文件" }).click();
  await page.getByRole("tab", { name: "a.md", exact: true }).click();
  await expect(page.getByRole("button", { name: "引用到会话" })).toBeEnabled();
  await page.setViewportSize({ width: 390, height: 420 });
  const composer = page.getByRole("textbox", { name: "发送消息" });
  await composer.fill("短屏草稿");
  const box = await composer.boundingBox();
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.y + box!.height).toBeLessThanOrEqual(420);
  await page.getByRole("button", { name: "引用到会话" }).click();
  await expect(page.getByText("a.md · L3")).toBeVisible();
  await expect(composer).toHaveValue("短屏草稿");
  expect(reads).toEqual(["a.md", "b.md"]);
});
