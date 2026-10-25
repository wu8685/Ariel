import {expect,test} from "@playwright/test";
import type {Thread} from "../src/generated/protocol";

const token=`s_${"e".repeat(64)}`;
const thread:Thread={threadId:"workspace-browser",title:"浏览项目文件",cwd:"/work/ariel",updatedAt:"2026-10-25T00:00:00Z",runtime:"idle",turns:[],pendingInteractions:[]};

test("workspace tree is lazy, responsive, and quotes selection into the current conversation",async({page})=>{
  let sent:Record<string,unknown>|null=null;const methods:string[]=[];
  await page.route("**/",async route=>{const response=await route.fetch();const body=(await response.text()).replace("__ARIEL_CHILLING_FILE_CONTEXT__","true");await route.fulfill({response,body})});
  await page.addInitScript(value=>sessionStorage.setItem("ariel.web-session.v1",value),token);
  await page.routeWebSocket("**/ws",socket=>socket.onMessage(raw=>{
    const message=JSON.parse(String(raw));if(message.type==="hello"){socket.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"wb",relayEpoch:"wb-e",sessionToken:token}));return}if(message.type!=="request")return;methods.push(message.method);let data:Record<string,unknown>={};
    if(message.method==="device.list")data={devices:[{deviceId:"mac",deviceName:"Workspace Mac",agentOnline:true,codexReady:true,agentEpoch:"agent",capabilities:{autoLoad:true,send:true,chillingFileContext:true,chillingWorkspaceBrowser:true}}]};
    if(message.method==="thread.list")data={threads:[thread],nextCursor:""};
    if(message.method==="thread.cache.watch")data={watchId:"watch",freshness:"fresh",agentOnline:true,agentEpoch:"agent",cacheStreamId:"cache",seq:1,capturedAt:"2026-10-25T00:00:00Z",thread};
    if(message.method==="workspace.files.list")data=message.params.path==="docs"?{path:"docs",revision:"docs-r",entries:[{name:"spec.md",path:"docs/spec.md",kind:"file",sizeBytes:15,modifiedAtMs:2,readable:true}]}:{path:"",revision:"root-r",entries:[{name:"docs",path:"docs",kind:"directory",sizeBytes:64,modifiedAtMs:1,readable:true},{name:".git",path:".git",kind:"directory",sizeBytes:64,modifiedAtMs:1,readable:false}]};
    if(message.method==="workspace.file.read")data={path:"docs/spec.md",content:"first line\nsecond line",sha256:"d".repeat(64),modifiedAtMs:2,lineCount:2,language:"md",version:"current-workspace"};
    if(message.method==="turn.start"){sent=message.params;data={turnId:"new-turn"}}
    socket.send(JSON.stringify({type:"response",v:1,requestId:message.requestId,outcome:"accepted",data}));
  }));
  await page.goto("/");await page.getByRole("button",{name:"展开项目 ariel，/work/ariel"}).click();await page.getByText(thread.title).click();await page.getByRole("button",{name:"浏览工作区文件"}).click();
  const browser=page.getByRole("dialog",{name:"工作区文件"});await expect(browser).toBeVisible();let box=await browser.boundingBox();expect(box?.width).toBe(390);expect(methods.filter(x=>x==="workspace.files.list")).toHaveLength(1);await expect(page.getByRole("button",{name:"打开目录 .git"})).toBeDisabled();
  await page.getByRole("button",{name:"打开目录 docs"}).click();await page.getByRole("button",{name:"打开文件 spec.md"}).click();const source=page.getByTestId("file-source");await expect(source).toBeVisible();await expect(page.getByRole("button",{name:"查看 Diff"})).toHaveCount(0);await source.evaluate(element=>{const code=element.querySelectorAll("code")[1];const range=document.createRange();range.selectNodeContents(code);const selection=window.getSelection()!;selection.removeAllRanges();selection.addRange(range);element.dispatchEvent(new MouseEvent("mouseup",{bubbles:true}))});await page.getByRole("button",{name:"引用到会话"}).click();await expect(page.getByText("docs/spec.md · L2")).toBeVisible();await page.getByRole("textbox",{name:"发送消息"}).fill("解释这个片段");await page.getByRole("button",{name:"发送"}).click();await expect.poll(()=>sent).not.toBeNull();expect(sent?.fileContexts).toEqual([expect.objectContaining({source:"workspace_browser",threadId:thread.threadId,path:"docs/spec.md",text:"second line"})]);expect((sent?.fileContexts as Record<string,unknown>[])[0]).not.toHaveProperty("turnId");
  await page.setViewportSize({width:1280,height:820});await page.getByRole("button",{name:"浏览工作区文件"}).click();await expect(browser).toBeVisible();box=await browser.boundingBox();expect(box?.width).toBeLessThanOrEqual(361);expect(box?.x).toBeGreaterThan(900);
});
