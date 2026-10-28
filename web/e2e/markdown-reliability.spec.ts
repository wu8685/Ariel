import { createHash } from "node:crypto";
import { expect, test, type Page, type Route } from "@playwright/test";
import { chooseFileView } from "./file-view-menu";

const token = `s_${"a".repeat(64)}`;
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==", "base64");
const svg = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="96" height="48" viewBox="0 0 72 36"><defs><path id="glyph" d="M0 0L10 0L5 10Z"/><clipPath id="clip"><rect width="72" height="36"/></clipPath></defs><g clip-path="url(#clip)"><use href="#glyph" fill="#3584e4" transform="translate(5 5)"/></g></svg>');
const content = [
  "# 公式与算法", "", String.raw`行内 $x^2 + \frac{a}{b}$，以及 \(y_1\)。价格 $20 和 $30 不转换。`, "",
  "$$", String.raw`\sum_{i=1}^{n} i = \frac{n(n+1)}{2}`, "$$", "",
  String.raw`\[\begin{pmatrix}a & b\\c & d\end{pmatrix}\]`, "",
  "```pseudocode", "IF ready THEN", "\tFOR each item:", "        emit(item)", "", "        " + "long_identifier_".repeat(30), "```", "",
  "可引用原文", "", "![本地图片](images/a.png)", "", "![外部图片](https://images.example.test/a.png)", "",
  ...Array.from({ length: 40 }, (_, i) => `第 ${i} 段，检查图片滚动和文件阅读现场。`), "", "![末尾图片](images/later.png)",
].join("\n");

async function fixture(page: Page, options: { image?: "good" | "transient" | "broken" | "held"; svg?: "supported" | "oldRelay" | "oldAgent" | "malicious"; imageError?: string; conversation?: string; document?:string; noSubtle?:boolean } = {}) {
  const requests: Array<{ method: string; params: Record<string, unknown> }> = [], external: string[] = [], fonts: string[] = [];
  const body=options.document??content;
  const thread = { threadId: "reading", title: "Markdown 修复验收", cwd: "/work/reading", updatedAt: "2026-10-28T00:00:00Z", runtime: "idle", turns: [{ turnId: "turn", status: "completed", items: [{ itemId: "reply", role: "assistant", text: options.conversation || "[查看文档](docs/read.md)", images: [] }] }], pendingInteractions: [] };
  let imageAttempt = 0, externalAttempt = 0, finishImage = () => {}, push = (_text: string) => {}, seq = 1;
  page.on("request", request => { if (/\.(woff2?|ttf)(?:\?|$)/u.test(request.url())) fonts.push(request.url()); });
  await page.route("**/", async route => { const response = await route.fetch(); await route.fulfill({ response, body: (await response.text()).replace("__ARIEL_CHILLING_FILE_CONTEXT__", "true") }); });
  await page.route("https://images.example.test/**", async route => { external.push(route.request().url()); if (++externalAttempt === 1) await route.fulfill({ status: 503, body: "temporary failure" }); else await route.fulfill({ contentType: "image/png", body: png }); });
  await page.addInitScript(value => sessionStorage.setItem("ariel.web-session.v1", value), token);
  if(options.noSubtle)await page.addInitScript(()=>Object.defineProperty(crypto,"subtle",{value:undefined}));
  await page.routeWebSocket("**/ws", socket => {
    push = text => { thread.turns[0].items[0].text = text; socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: "reading", watchId: "w", agentOnline: true, agentEpoch: "epoch", freshness: "fresh", cacheStreamId: "s", seq: ++seq, capturedAt: "2026-10-28T00:00:01Z", thread })); };
    socket.onMessage(raw => {
      const m = JSON.parse(String(raw));
      if (m.type === "hello") { socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "reading", relayEpoch: "relay", sessionToken: token })); return; }
      if (m.type !== "request") return;
      requests.push({ method: m.method, params: m.params }); let data: Record<string, unknown> = {};
      const respond = () => socket.send(JSON.stringify({ type: "response", v: 1, requestId: m.requestId, outcome: "accepted", data }));
      if (m.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Reader Mac", agentEpoch: "epoch", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, send: true, chillingFileContext: true, chillingMessageReferences: true } }], documentAssetsVersion: 1, ...(options.svg && options.svg !== "oldRelay" ? {documentSvgVersion:1} : {}) };
      if (m.method === "thread.list") data = { threads: [thread], nextCursor: "" };
      if (m.method === "thread.cache.watch") data = { watchId: "w", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "s", seq: 1, capturedAt: "2026-10-28T00:00:00Z", thread };
      if (m.method === "file.context.changes") data = { turns: [{ turnId: "turn", fileChanges: [{ itemId: "change", path: "docs/read.md", kind: "update", diff: "@@ -1 +1 @@\n-old\n+new" }] }] };
      if (["file.context.read", "file.context.reference.read"].includes(m.method)) data = { path: "docs/read.md", content:body, sha256: "a".repeat(64), modifiedAtMs: 1, lineCount: body.split("\n").length, language: "md", version: "current-workspace", documentAssets: { version: 1, maxBytes: 4 * 1024 * 1024, chunkBytes: 256 * 1024, ...(options.svg && options.svg !== "oldAgent" ? {svgVersion:1} : {}) } };
      if (m.method === "file.context.asset.read") {
        imageAttempt++;
        if (options.imageError && imageAttempt === 1) { socket.send(JSON.stringify({type: "response", v: 1, requestId: m.requestId, outcome: "rejected", error: {code: options.imageError, message: "private backend detail must not leak"}})); return; }
        if (options.image === "transient" && imageAttempt === 1) { socket.send(JSON.stringify({ type: "response", v: 1, requestId: m.requestId, outcome: "rejected", error: { code: "TIMEOUT", message: "image temporary timeout" } })); return; }
        const isSVG = options.svg && String(m.params.imageReference).endsWith('.svg');
        const bytes = isSVG ? options.svg === "malicious" ? Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="96" height="48"><script>window.__svgExecuted=true</script><image href="https://images.example.test/secret.png"/></svg>') : svg : options.image === "broken" && imageAttempt === 1 ? png.subarray(0, 24) : png;
        data = { mime: isSVG ? "image/svg+xml" : "image/png", assetSha256: createHash("sha256").update(bytes).digest("hex"), width: isSVG ? 96 : 1, height: isSVG ? 48 : 1, totalBytes: bytes.length, offset: 0, dataBase64: bytes.toString("base64") };
        if (options.image === "held" && imageAttempt === 1) { finishImage = respond; return; }
      }
      respond();
    });
  });
  await page.goto("/"); await page.getByRole("button", { name: "展开项目 reading，/work/reading" }).click(); await page.getByText(thread.title, { exact: true }).click();
  await expect(page.getByRole("button", { name: "查看原文：docs/read.md" })).toBeVisible();
  return { requests, external, fonts, finishImage: () => finishImage(), push: (text: string) => push(text) };
}

for (const width of [320,390,1280]) test(`negotiated local static SVG decodes as an isolated image at ${width}px`, async ({page})=>{
 await page.setViewportSize({width,height:844});const {requests,external}=await fixture(page,{svg:"supported",document:"# 静态 SVG\n\n![静态矢量图](images/a.svg)\n\n可引用原文"+"\n\n正文".repeat(80)+"\n\n![未可见图片](images/later.svg)"});
 await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();expect(requests.some(r=>r.method==="file.context.asset.read")).toBe(false);
 await chooseFileView(page,"阅读模式");const reading=page.getByTestId("file-reading"),img=reading.getByRole("img",{name:"静态矢量图",exact:true});
 await expect.poll(()=>img.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(96);
 expect(requests.filter(r=>r.method==="file.context.asset.read").every(r=>r.params.svgVersion===1)).toBe(true);
 expect(requests.filter(r=>r.method==="file.context.asset.read")).toHaveLength(1);
 expect(await reading.locator("svg,object,iframe,embed").count()).toBe(0);expect(external).toHaveLength(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await expect(page).toHaveScreenshot(`static-svg-${width}.png`,{animations:"disabled",caret:"hide"});
 await chooseFileView(page,"原版模式");await expect(page.getByTestId("file-source")).toContainText("images/a.svg");
 await chooseFileView(page,"阅读模式");await expect.poll(()=>img.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(96);
 expect(requests.filter(r=>r.method==="file.context.asset.read")).toHaveLength(1);
});

for(const svg of ["oldRelay","oldAgent","malicious"] as const)test(`SVG ${svg} fails locally without unknown opt-in, scripts, or external requests`,async({page})=>{
 const {requests,external}=await fixture(page,{svg,document:"# 静态 SVG\n\n![静态矢量图](images/a.svg)"});
 await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式");
 await expect(page.getByTestId("file-reading").getByRole("button",{name:"重试图片"})).toBeVisible();
 if(svg!=="malicious")expect(requests.filter(r=>r.method==="file.context.asset.read").every(r=>r.params.svgVersion===undefined)).toBe(true);
 expect(external).toHaveLength(0);expect(await page.evaluate(()=>(window as unknown as {__svgExecuted?:boolean}).__svgExecuted)).not.toBe(true);
 await expect(page.getByTestId("file-reading").locator("img,svg,iframe,object,embed")).toHaveCount(0);
});

for (const width of [320, 390, 1280]) test(`math and pseudocode fit ${width}px with local fonts and exact source quotation`, async ({ page }) => {
  await page.setViewportSize({ width, height: 844 }); const { requests, fonts } = await fixture(page);
  await page.getByRole("button", { name: "查看原文：docs/read.md" }).click(); await chooseFileView(page, "阅读模式");
  const reading = page.getByTestId("file-reading"); await expect(reading.locator(".katex")).toHaveCount(4);
  const code = reading.locator("pre code"); await expect(code).toHaveCSS("tab-size", "4");
  expect(await code.textContent()).toContain("\tFOR each item:\n        emit(item)\n\n");
  expect(await code.evaluate(el => getComputedStyle(el).fontFamily)).toMatch(/mono/iu);
  expect(await page.evaluate(() => document.body.scrollWidth <= innerWidth)).toBe(true);
  for (const block of await reading.locator("pre, .markdown-math-display").all()) {
    const box = await block.boundingBox(); expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.x + box!.width).toBeLessThanOrEqual(width);
  }
  const pre = reading.locator("pre"); expect(await pre.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true);
  await page.evaluate(() => document.fonts.ready); expect(fonts.length).toBeGreaterThan(0); expect(fonts.every(url => new URL(url).origin === "http://127.0.0.1:4173")).toBe(true);
  await reading.evaluate(el => el.scrollTop = 0);
  await expect(page).toHaveScreenshot(`markdown-math-code-${width}.png`, { animations: "disabled", caret: "hide" });
  const quote = reading.getByText("可引用原文", { exact: true }); await quote.scrollIntoViewIfNeeded();
  await quote.evaluate(el => { const node = el.firstChild!, range = document.createRange(); range.selectNodeContents(node); const selection = getSelection()!; selection.removeAllRanges(); selection.addRange(range); el.dispatchEvent(new MouseEvent("mouseup", { bubbles: true })); });
  await expect(page.getByRole("button", { name: "引用到会话" })).toBeEnabled();
  expect(requests.filter(r => r.method === "file.context.read")).toHaveLength(1); expect(requests.filter(r => r.method === "thread.cache.watch")).toHaveLength(1);
});

for (const width of [320, 390, 1280]) for (const [code, message] of [
  ["UNSUPPORTED_FILE_TYPE", "图片格式不受支持或文件已损坏；当前支持 PNG、JPEG、GIF、WebP。"],
  ["FILE_TOO_LARGE", "图片超过大小、尺寸或复杂度限制（最大 4 MiB、单边 8192 px、1200 万像素）。"],
  ["FILE_NOT_FOUND", "图片文件已不存在，请检查文档中的图片引用。"],
  ["PATH_NOT_ALLOWED", "这项图片引用不允许读取。"],
] as const) test(`document image error ${code} has image-specific feedback at ${width}px instead of UTF-8 text restrictions`, async ({page}) => {
  await page.setViewportSize({width, height: 844});
  const {requests} = await fixture(page, {imageError: code, document: "# 格式提示验收\n\n![矢量插图](images/a.svg)\n\n可引用原文"});
  await page.getByRole("textbox", {name: "发送消息"}).fill("保留主会话草稿");
  await page.getByRole("button", {name: "查看原文：docs/read.md"}).click(); await chooseFileView(page, "阅读模式");
  const reading = page.getByTestId("file-reading");
  await expect(reading.getByText(`矢量插图 · ${message}`, {exact: true})).toBeVisible();
  await expect(reading).not.toContainText("UTF-8"); await expect(reading).not.toContainText("private backend detail");
  expect(requests.filter(r => r.method === "file.context.asset.read")).toHaveLength(1);
  expect(requests.find(r => r.method === "file.context.asset.read")?.params.imageReference).toBe("images/a.svg");
  expect(requests.filter(r => r.method === "thread.cache.watch")).toHaveLength(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  if (code === "UNSUPPORTED_FILE_TYPE") await expect(page).toHaveScreenshot(`document-image-format-error-${width}.png`, {animations: "disabled", caret: "hide"});
  await reading.getByRole("button", {name: "重试图片"}).click();
  const img = reading.getByRole("img", {name: "矢量插图", exact: true});
  await expect.poll(() => img.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBe(1);
  expect(requests.filter(r => r.method === "file.context.read")).toHaveLength(1);
  await page.getByRole("button", {name: "收起文件工作区", exact: true}).click();
  await expect(page.getByRole("textbox", {name: "发送消息"})).toHaveValue("保留主会话草稿");
});

for (const image of ["transient", "broken"] as const) test(`document image ${image} failure retries and actually decodes without reopening`, async ({ page }) => {
  const { requests } = await fixture(page, { image });
  await page.getByRole("button", { name: "查看原文：docs/read.md" }).click(); await chooseFileView(page, "阅读模式");
  const reading = page.getByTestId("file-reading"); await reading.locator(".document-image").first().scrollIntoViewIfNeeded();
  await expect(reading.getByRole("button", { name: "重试图片" })).toBeVisible();
  if (image === "broken") await expect(reading.getByText(/图片无法解码/)).toBeVisible();
  await reading.getByRole("button", { name: "重试图片" }).click();
  const img = reading.getByRole("img", { name: "本地图片", exact: true }); await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(1);
  expect(requests.filter(r => r.method === "file.context.asset.read")).toHaveLength(2); expect(requests.filter(r => r.method === "file.context.read")).toHaveLength(1);
});

test("a pending document image survives scrolling and unrelated permission changes", async ({ page }) => {
  const { requests, finishImage } = await fixture(page, { image: "held" });
  await page.getByRole("button", { name: "查看原文：docs/read.md" }).click(); await chooseFileView(page, "阅读模式");
  const reading = page.getByTestId("file-reading"); await reading.locator(".document-image").first().scrollIntoViewIfNeeded();
  await expect.poll(() => requests.filter(r => r.method === "file.context.asset.read").length).toBe(1);
  await reading.evaluate(el => el.scrollTop = 0); await reading.getByRole("button", { name: "允许加载外部图片" }).click();
  finishImage(); const img = reading.getByRole("img", { name: "本地图片", exact: true });
  await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(1);
  await img.scrollIntoViewIfNeeded(); expect(requests.filter(r => r.method === "file.context.asset.read")).toHaveLength(1);
  expect(requests.some(r => r.params.imageReference === "images/later.png")).toBe(false);
});

test("conversation image consent has no implicit fetch, retries failure, and resets on text changes", async ({ page }) => {
  const text = "![会话图片](https://images.example.test/a.png)";
  const { external, push } = await fixture(page, { conversation: text });
  const allow = page.getByRole("button", { name: "允许加载外部图片" }); await expect(allow).toBeVisible(); expect(external).toHaveLength(0);
  await allow.click(); await expect(page.getByText(/外部图片加载失败/)).toBeVisible(); await page.getByRole("button", { name: "重试图片" }).click();
  const img = page.getByRole("img", { name: "会话图片", exact: true }); await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(1);
  expect(external).toHaveLength(2); push(text + "\n\n正文更新");
  await expect(allow).toBeVisible(); await expect(img).toHaveCount(0); expect(external).toHaveLength(2);
  push(text);await expect(allow).toBeVisible();await expect(img).toHaveCount(0);expect(external).toHaveLength(2);
});

test("wide inline and display formulas scroll from their true left edge in both views",async({page})=>{
  const formula=String.raw`\underbrace{`+"x".repeat(90)+String.raw`}_{n}`;
  const text=`行内 $${formula}$\n\n$$${formula}$$\n\n`+["```pseudocode","\tFOR each item:","        emit(item)","```"].join("\n");
  await fixture(page,{conversation:text,document:text});
  for(const file of [false,true]){
    if(file){await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式")}
    const root=file?page.getByTestId("file-reading"):page.locator(".transcript .markdown-body");
    await expect(root.locator(".katex")).toHaveCount(2);await expect(root.locator("pre code")).toHaveCSS("tab-size","4");
    for(const math of await root.locator(".markdown-math").all()){
      const geometry=await math.evaluate(el=>{const box=el.getBoundingClientRect(),base=el.querySelector(".katex-html")!.getBoundingClientRect();return {left:box.left,right:box.right,baseLeft:base.left,width:el.clientWidth,scroll:el.scrollWidth}});
      expect(geometry.right).toBeLessThanOrEqual(390);expect(geometry.left).toBeGreaterThanOrEqual(0);
      expect(geometry.baseLeft).toBeGreaterThanOrEqual(geometry.left-1);expect(geometry.scroll).toBeGreaterThan(geometry.width);
      expect(await math.evaluate(el=>{el.scrollLeft=100;return el.scrollLeft})).toBeGreaterThan(0);
    }
    expect(await page.evaluate(()=>document.body.scrollWidth<=innerWidth)).toBe(true);
  }
});

test("document images verify and decode without a secure-context Web Crypto API",async({page})=>{
  await fixture(page,{noSubtle:true});await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式");
  await page.getByTestId("file-reading").locator(".document-image").first().scrollIntoViewIfNeeded();
  const img=page.getByRole("img",{name:"本地图片",exact:true});await expect.poll(()=>img.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(1);
  expect(await page.evaluate(()=>crypto.subtle)).toBeUndefined();
});

for(const userScroll of [false,true])test(`refresh waits for delayed formula layout and ${userScroll?"keeps a new user scroll":"restores the saved bottom"}`,async({page})=>{
  const document=Array.from({length:40},(_,i)=>`第 ${i} 个公式\n\n$$\\frac{a+b}{c+d}=\\sum_{k=1}^{n} k$$\n\n`).join("");
  await fixture(page,{document});await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式");
  const reading=page.getByTestId("file-reading");await expect(reading.locator(".katex")).toHaveCount(40);await page.evaluate(()=>window.document.fonts.ready);
  await reading.evaluate(el=>el.scrollTop=el.scrollHeight);
  const position=await reading.evaluate(el=>el.scrollTop);
  const held:Route[]=[];let released=false;
  await page.route("**/*katex*.js*",route=>{if(released)return route.continue();held.push(route)});
  await page.reload();await expect(reading.locator(".markdown-math-pending")).toHaveCount(40);
  if(userScroll){await reading.hover();await page.mouse.wheel(0,-20000);await expect.poll(()=>reading.evaluate(el=>el.scrollTop)).toBe(0)}
  expect(held.length).toBeGreaterThan(0);released=true;for(const route of held)await route.continue();
  await expect(reading.locator(".katex")).toHaveCount(40);await page.evaluate(()=>window.document.fonts.ready);
  await expect.poll(()=>reading.evaluate((el,old)=>el.scrollTop===Math.min(old,el.scrollHeight-el.clientHeight),userScroll?0:position)).toBe(true);
});
