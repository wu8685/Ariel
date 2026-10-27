import {createHash} from "node:crypto";
import {expect,test,type Page} from "@playwright/test";
import {chooseFileView} from "./file-view-menu";

const token=`s_${"b".repeat(64)}`;
const image=Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==","base64");
const text="# 文档标题\n\n重复片段\n\n重复**片段**与结尾\n\n![本地图片](../images/a.png)\n\n![外部图片](https://images.example.test/remote.png)\n\n| 长列 | 内容 |\n| --- | --- |\n| "+"long_cell_".repeat(35)+" | 表格可单独滚动 |\n\n"+Array.from({length:30},(_,i)=>`第 ${i+1} 段：用于检查滚动位置与延迟图片。`).join("\n\n")+"\n\n![末尾图片](../images/b.png)\n";
const thread={threadId:"reading",title:"阅读模式验收",cwd:"/work/reading",updatedAt:"2026-10-27T00:00:00Z",runtime:"idle",turns:[{turnId:"turn",status:"completed",items:[{itemId:"reply",role:"assistant",text:"[查看说明](docs/read.md)",images:[]}]}],pendingInteractions:[]};

async function fixture(page:Page,{legacy=false,lostCapability=false}={}){
 const requests:{method:string;params:Record<string,unknown>}[]=[];const external:string[]=[];
 await page.route("**/",async route=>{const response=await route.fetch();await route.fulfill({response,body:(await response.text()).replace("__ARIEL_CHILLING_FILE_CONTEXT__","true")})});
 await page.route("https://images.example.test/**",async route=>{external.push(route.request().url());await route.fulfill({contentType:"image/png",body:image})});
 await page.addInitScript(value=>sessionStorage.setItem("ariel.web-session.v1",value),token);
 await page.routeWebSocket("**/ws",socket=>socket.onMessage(raw=>{
  const m=JSON.parse(String(raw));if(m.type==="hello"){socket.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"reading",relayEpoch:"relay",sessionToken:token}));return}if(m.type!=="request")return;
  requests.push({method:m.method,params:m.params});let data:Record<string,unknown>={};
  if(m.method==="device.list")data={devices:[{deviceId:"mac",deviceName:"Reader Mac",agentEpoch:"epoch",agentOnline:true,codexReady:true,capabilities:{autoLoad:true,send:true,chillingFileContext:true,chillingMessageReferences:true,chillingWorkspaceBrowser:true}}],...(!legacy?{documentAssetsVersion:1}:{})};
  if(m.method==="thread.list")data={threads:[thread],nextCursor:""};
  if(m.method==="thread.cache.watch")data={watchId:"w",freshness:"fresh",agentOnline:true,agentEpoch:"epoch",cacheStreamId:"s",seq:1,capturedAt:"2026-10-27T00:00:00Z",thread};
  if(m.method==="file.context.changes")data={turns:[{turnId:"turn",fileChanges:[{itemId:"change",path:"docs/read.md",kind:"update",diff:"@@ -1 +1 @@\n-old\n+文档标题"}]}]};
  if(m.method==="workspace.files.list")data={path:m.params.path,revision:"r",entries:m.params.path==="docs"?[{name:"read.md",path:"docs/read.md",kind:"file",sizeBytes:text.length,modifiedAtMs:1,readable:true}]:[{name:"docs",path:"docs",kind:"directory",sizeBytes:1,modifiedAtMs:1,readable:true}]};
  if(["file.context.read","file.context.reference.read","workspace.file.read"].includes(m.method))data={path:"docs/read.md",content:text,sha256:"a".repeat(64),modifiedAtMs:1,lineCount:text.split("\n").length,language:"md",version:"current-workspace",...(!legacy?{documentAssets:{version:1,maxBytes:4*1024*1024,chunkBytes:256*1024}}:{})};
  if(m.method==="file.context.asset.read"){
   if(lostCapability){lostCapability=false;socket.send(JSON.stringify({type:"response",v:1,requestId:m.requestId,outcome:"rejected",error:{code:"PROTOCOL_UNSUPPORTED",message:"document assets not confirmed after Agent reconnect"}}));return}
   data={mime:"image/png",assetSha256:createHash("sha256").update(image).digest("hex"),width:1,height:1,totalBytes:image.length,offset:0,dataBase64:image.toString("base64")};
  }
  socket.send(JSON.stringify({type:"response",v:1,requestId:m.requestId,outcome:"accepted",data}));
 }));
 await page.goto("/");await page.getByRole("button",{name:"展开项目 reading，/work/reading"}).click();await page.getByText(thread.title,{exact:true}).click();
 await expect(page.getByRole("button",{name:"浏览工作区文件"})).toBeVisible();
 return {requests,external};
}

for(const entry of ["turn","reference","workspace"]){
 test(`Markdown ${entry} entry renders lazy images, keeps exact quotation and draft`,async({page})=>{
  const {requests,external}=await fixture(page);await page.getByRole("textbox",{name:"发送消息"}).fill("保留草稿");
  if(entry==="turn")await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();
  else if(entry==="reference")await page.getByRole("button",{name:"打开文档：查看说明"}).click();
  else{await page.getByRole("button",{name:"浏览工作区文件"}).click();await page.getByRole("button",{name:"打开目录 docs"}).click();await page.getByRole("button",{name:"打开文件 read.md"}).click()}
  await expect(page.getByTestId("file-source")).toBeVisible();expect(requests.filter(r=>r.method==="file.context.asset.read")).toHaveLength(0);
  const reads=requests.filter(r=>r.method.endsWith(".read")).length;
  await chooseFileView(page,"阅读模式");await expect(page.getByRole("heading",{name:"文档标题"})).toBeVisible();
  await expect.poll(()=>requests.filter(r=>r.method==="file.context.asset.read").length).toBe(1);
  const image=page.getByRole("img",{name:"本地图片",exact:true});await expect(image).toBeVisible();await expect.poll(()=>image.evaluate((img:HTMLImageElement)=>img.naturalWidth)).toBe(1);
  expect(external).toHaveLength(0);expect(requests.some(r=>r.params.imageReference==="../images/b.png")).toBe(false);
  await page.getByRole("button",{name:"允许加载外部图片"}).click();await expect.poll(()=>external.length).toBe(1);
  await page.getByTestId("file-reading").evaluate(root=>{const node=root.querySelector("strong span")!.firstChild!;const range=document.createRange();range.setStart(node,0);range.setEnd(node,2);const selection=getSelection()!;selection.removeAllRanges();selection.addRange(range);root.dispatchEvent(new MouseEvent("mouseup",{bubbles:true}))});
  await expect(page.getByRole("button",{name:"引用到会话"})).toBeEnabled();
  expect(requests.filter(r=>r.method.endsWith(".read")&&r.method!=="file.context.asset.read")).toHaveLength(reads);
  expect(await page.evaluate(()=>document.body.scrollWidth<=innerWidth)).toBe(true);
  await page.getByRole("button",{name:"引用到会话"}).click();await expect(page.getByRole("textbox",{name:"发送消息"})).toHaveValue("保留草稿");await expect(page.getByText("docs/read.md · L5")).toBeVisible();
 });
}

for(const width of [320,390,1280]){
 test(`document reading fits ${width}px and preserves independent source / reading / diff state`,async({page})=>{
  await page.setViewportSize({width,height:844});await fixture(page);
  await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式");
  const reading=page.getByTestId("file-reading");await reading.evaluate(el=>el.scrollTop=450);
  const position=await reading.evaluate(el=>el.scrollTop);
  await chooseFileView(page,"原版模式");await expect(page.getByTestId("file-source")).toBeVisible();
  await chooseFileView(page,"阅读模式");expect(await reading.evaluate(el=>el.scrollTop)).toBe(position);
  await chooseFileView(page,"查看 Diff");await expect(page.getByTestId("file-diff")).toBeVisible();
  await chooseFileView(page,"阅读模式");await expect(reading).toBeVisible();
  await reading.evaluate(el=>el.scrollTop=0);await expect(page.getByRole("img",{name:"本地图片",exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.body.scrollWidth<=innerWidth)).toBe(true);
  await expect(page).toHaveScreenshot(`document-reading-${width}.png`,{animations:"disabled",caret:"hide"});
 });
}

test("legacy endpoints still render Markdown text without sending unknown image requests",async({page})=>{
 const {requests}=await fixture(page,{legacy:true});await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式");await expect(page.getByRole("heading",{name:"文档标题"})).toBeVisible();await expect(page.getByText(/尚未启用文档图片读取/)).toBeVisible();expect(requests.some(r=>r.method==="file.context.asset.read")).toBe(false);
});

test("Agent reconnect re-confirms image capability without refreshing the conversation",async({page})=>{
 const {requests}=await fixture(page,{lostCapability:true});await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();await chooseFileView(page,"阅读模式");
 await expect(page.getByRole("img",{name:"本地图片",exact:true})).toBeVisible();
 expect(requests.filter(r=>r.method==="file.context.read")).toHaveLength(2);
 expect(requests.filter(r=>r.method==="file.context.asset.read")).toHaveLength(2);
 expect(requests.filter(r=>r.method==="thread.cache.watch")).toHaveLength(1);
});

test("reopening a cached document recreates its image session without rereading text",async({page})=>{
 const {requests}=await fixture(page);
 for(let attempt=0;attempt<2;attempt++){
  await page.getByRole("button",{name:"查看原文：docs/read.md"}).click();
  await chooseFileView(page,"阅读模式");
  const image=page.getByRole("img",{name:"本地图片",exact:true});
  await expect(image).toBeVisible();
  await expect.poll(()=>image.evaluate((element:HTMLImageElement)=>element.naturalWidth)).toBe(1);
  await page.getByRole("button",{name:"收起文件工作区",exact:true}).click();
 }
 expect(requests.filter(r=>r.method==="file.context.read")).toHaveLength(1);
 expect(requests.filter(r=>r.method==="file.context.asset.read")).toHaveLength(2);
 expect(requests.filter(r=>r.method==="thread.cache.watch")).toHaveLength(1);
});
