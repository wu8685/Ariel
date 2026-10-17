import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"f".repeat(64)}`;
const thread: Thread = { threadId:"file-context-thread",title:"文件引用验收",cwd:"/tmp/file-context",updatedAt:"2026-10-17T00:00:00Z",runtime:"idle",turns:[{turnId:"turn-a",status:"completed",items:[{itemId:"answer",role:"assistant",text:"我更新了文件。"}]}],pendingInteractions:[] };

test("mobile opens a changed file and adds an exact quote to the next message", async ({ page }) => {
  let sent: Record<string,unknown> | null = null;
  let fileReads = 0;
  const longSuffix = "x".repeat(180);
  await page.route("**/", async route => {
    const response = await route.fetch();
    const body = (await response.text()).replace("__ARIEL_CHILLING_FILE_CONTEXT__", "true");
    await route.fulfill({ response, body });
  });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message=JSON.parse(String(raw));
    if(message.type==="hello"){socket.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"fc",relayEpoch:"fc-epoch",sessionToken}));return;}
    if(message.type!=="request") return;
    let data:Record<string,unknown>={};
    if(message.method==="device.list") data={devices:[{deviceId:"mac",deviceName:"File Mac",agentOnline:true,codexReady:true,agentEpoch:"fc-agent",capabilities:{autoLoad:true,send:true,chillingFileContext:true}}]};
    if(message.method==="thread.list") data={threads:[thread],nextCursor:""};
    if(message.method==="thread.subscribe") data={subscriptionId:"sub",streamId:"stream"};
    if(message.method==="file.context.changes") data={turns:[{turnId:"turn-a",fileChanges:[{itemId:"change-a",path:"internal/hello.go",kind:"update",diff:"@@ -1,3 +1,3 @@\n-package old\n+package hello\n+// "+longSuffix,diffTruncated:true}]}]};
    if(message.method==="file.context.read") { fileReads++; data={path:"internal/hello.go",content:"package hello\n\nfunc Hi() string { return \"hello\" } // "+longSuffix+"\n",sha256:"a".repeat(64),modifiedAtMs:42,lineCount:4,language:"go",diff:"@@ -1,3 +1,3 @@\n-package old\n+package hello\n+// "+longSuffix,diffTruncated:true,version:"current-workspace"}; }
    if(message.method==="turn.start") sent=message.params;
    socket.send(JSON.stringify({type:"response",v:1,requestId:message.requestId,outcome:"accepted",data:message.method==="turn.start"?{turnId:"next-turn"}:data}));
    if(message.method==="thread.subscribe") socket.send(JSON.stringify({type:"event",v:1,event:"thread.snapshot",deviceId:"mac",threadId:thread.threadId,subscriptionId:"sub",streamId:"stream",seq:1,thread}));
  }));
  await page.goto("/");
  await page.getByRole("button",{name:"展开项目 file-context，/tmp/file-context"}).click();
  await page.getByText(thread.title).click();
  await page.getByRole("textbox",{name:"发送消息"}).fill("打开文件前的草稿");
  await expect(page.getByRole("button",{name:"查看 Diff：internal/hello.go"})).toBeVisible();
  await page.getByRole("button",{name:"查看 Diff：internal/hello.go"}).click();
  await expect(page.getByRole("dialog",{name:"文件 internal/hello.go"})).toBeVisible();
  await expect(page.getByText("Diff 已截断")).toBeVisible();
  await expect.poll(()=>fileReads).toBe(0);
  const diffViewport=page.getByTestId("file-diff");
  await expect.poll(async()=>diffViewport.evaluate(element=>element.scrollWidth<=element.clientWidth+1)).toBe(true);
  await expect(page).toHaveScreenshot("mobile-file-diff.png",{animations:"disabled",caret:"hide"});
  await page.getByRole("button",{name:"返回会话"}).click();
  await expect(page.getByRole("dialog",{name:"文件 internal/hello.go"})).toHaveCount(0);
  await expect(page.getByRole("textbox",{name:"发送消息"})).toHaveValue("打开文件前的草稿");

  await page.getByRole("button",{name:"查看原文：internal/hello.go"}).click();
  await expect(page.getByRole("dialog",{name:"文件 internal/hello.go"})).toBeVisible();
  await expect.poll(()=>fileReads).toBe(1);
  const sourceViewport=page.getByTestId("file-source");
  await expect.poll(async()=>sourceViewport.evaluate(element=>element.scrollWidth<=element.clientWidth+1)).toBe(true);
  await sourceViewport.evaluate(element=>{
    const code=element.querySelectorAll("code")[2];
    const node=code.firstChild!;
    const text=node.textContent || "";
    const start=text.indexOf("return");
    const range=document.createRange();
    range.setStart(node,start);
    range.setEnd(node,start+'return "hello"'.length);
    const selection=window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    element.dispatchEvent(new MouseEvent("mouseup",{bubbles:true}));
  });
  await expect(page.getByText("已选择 L3")).toBeVisible();
  await expect.poll(async()=>page.getByRole("button",{name:"返回会话"}).evaluate(element=>element.getBoundingClientRect().top)).toBeGreaterThanOrEqual(0);
  await expect(page).toHaveScreenshot("mobile-file-source-selection.png",{animations:"disabled",caret:"hide"});
  await page.getByRole("button",{name:"引用到会话"}).click();
  await expect(page.getByText("internal/hello.go · L3")).toBeVisible();
  await page.getByRole("textbox",{name:"发送消息"}).fill("解释这一行");
  await page.getByRole("button",{name:"发送"}).click();
  await expect.poll(()=>sent).not.toBeNull();
  expect(sent?.fileContexts).toEqual([expect.objectContaining({path:"internal/hello.go",startLine:3,endLine:3,text:'return "hello"'})]);
  await expect(page).toHaveScreenshot("mobile-file-context-quote.png",{animations:"disabled",caret:"hide"});
	await page.setViewportSize({width:1280,height:820});
	await page.getByRole("button",{name:"查看原文：internal/hello.go"}).click();
	const panel=page.getByRole("dialog",{name:"文件 internal/hello.go"});
	const box=await panel.boundingBox();
	expect(box?.width).toBeLessThan(900);
	expect(box?.x).toBeGreaterThan(400);
});

test("a rejected optional file summary never interrupts opening the conversation", async ({ page }) => {
  await page.route("**/", async route => {
    const response = await route.fetch();
    const body = (await response.text()).replace("__ARIEL_CHILLING_FILE_CONTEXT__", "true");
    await route.fulfill({ response, body });
  });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message=JSON.parse(String(raw));
    if(message.type==="hello"){socket.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"fc-error",relayEpoch:"fc-epoch",sessionToken}));return;}
    if(message.type!=="request") return;
    let data:Record<string,unknown>={};
    if(message.method==="device.list") data={devices:[{deviceId:"mac",deviceName:"File Mac",agentOnline:true,codexReady:true,agentEpoch:"fc-agent",capabilities:{autoLoad:true,send:true,chillingFileContext:true}}]};
    if(message.method==="thread.list") data={threads:[thread],nextCursor:""};
    if(message.method==="thread.subscribe") data={subscriptionId:"sub",streamId:"stream"};
    if(message.method==="file.context.changes") { socket.send(JSON.stringify({type:"response",v:1,requestId:message.requestId,outcome:"rejected",error:{code:"PATH_NOT_ALLOWED",message:"PATH_NOT_ALLOWED"}})); return; }
    socket.send(JSON.stringify({type:"response",v:1,requestId:message.requestId,outcome:"accepted",data}));
    if(message.method==="thread.subscribe") socket.send(JSON.stringify({type:"event",v:1,event:"thread.snapshot",deviceId:"mac",threadId:thread.threadId,subscriptionId:"sub",streamId:"stream",seq:1,thread}));
  }));
  await page.goto("/");
  await page.getByRole("button",{name:"展开项目 file-context，/tmp/file-context"}).click();
  await page.getByText(thread.title).click();
  await expect(page.getByText("我更新了文件。")).toBeVisible();
  await expect(page.getByRole("alert")).toHaveCount(0);
});
