---
title: "Codex Desktop IPC 探针源码"
created: 2026-10-03
tags: [codex, remote-control, evidence, source-code]
type: experiment
source: "[[2026-10-03-codex-remote-adapter]]"
project: "Codex 远程交互系统 待命名"
---

# Codex Desktop IPC 探针源码

以下为 2026-10-03 实测源码的脱敏归档，以 Markdown 代码块保存。原机器路径和真实会话标识已替换为占位符，不是可直接运行的生产客户端；使用限制见 [证据附件说明](README.md)。

## 脱敏源码

```javascript
import net from 'node:net';
import { randomUUID } from 'node:crypto';
import fs from 'node:fs';
const threadId = process.argv[2] || '<business-thread-id>';
const watchMs = Number(process.argv[3] || 15000);
const operationMode=process.argv[4] || 'read';
if(operationMode!=='read') {
  const fixture=JSON.parse(fs.readFileSync('<fixture-dir>/test-thread.json','utf8'));
  if(threadId!==fixture.threadId||threadId==='<business-thread-id>')throw Error('Operations are restricted to the disposable test thread');
}
const socket = net.createConnection('<user-home>/.codex/ipc/ipc.sock');
let clientId = 'initializing-client';
let buffer = Buffer.alloc(0);
let owner;
const pending = new Map();
const counts = {};
let snapshotSeen = false;
let latestState = null;
let patchEvents = 0;
function hasExactString(value,needle) {
  if(typeof value==='string')return value.trim()===needle;
  if(value&&typeof value==='object')return Object.values(value).some(v=>hasExactString(v,needle));
  return false;
}
function emit(obj) { console.log(JSON.stringify(obj)); }
function send(msg) {
  const body = Buffer.from(JSON.stringify(msg));
  const header = Buffer.alloc(4); header.writeUInt32LE(body.length);
  socket.write(Buffer.concat([header, body]));
}
function request(method, params, version, targetClientId, timeoutMs=10000) {
  const requestId = randomUUID();
  return new Promise((resolve,reject) => {
    const timer=setTimeout(() => {pending.delete(requestId);reject(new Error(method+' timeout'));},timeoutMs+1000);
    pending.set(requestId,{resolve,reject,timer});
    send({type:'request', requestId, sourceClientId:clientId, version, method, params, targetClientId, timeoutMs});
  });
}
function broadcast(method, params, version) {
  send({type:'broadcast',sourceClientId:clientId,method,params,version,targetClientIds:owner?[owner]:undefined});
}
function handle(m) {
  if(m.type==='response') {
    const p=pending.get(m.requestId);if(p){clearTimeout(p.timer);pending.delete(m.requestId);p.resolve(m);}return;
  }
  if(m.type==='client-discovery-request') {
    send({type:'client-discovery-response',requestId:m.requestId,response:{canHandle:false}});return;
  }
  if(m.type==='broadcast' && m.params?.conversationId===threadId) {
    counts[m.method]=(counts[m.method]||0)+1;
    if(m.method==='thread-stream-state-changed') {
      const change=m.params.change;
      if(change?.type==='snapshot') {
        snapshotSeen=true; latestState=change.conversationState;
        emit({event:'snapshot',revision:change.revision,sourceClientId:m.sourceClientId,turns:latestState?.turns?.length,runtime:latestState?.threadRuntimeStatus,resumeState:latestState?.resumeState,historyKeys:Object.keys(latestState?.turnHistory||{}),replyMarkerFound:hasExactString(latestState,'IPC_REPLY_OK')});
      } else if(change?.type==='patches') {
        patchEvents++;
        if(patchEvents<=10||patchEvents%20===0)emit({event:'patches',baseRevision:change.baseRevision,revision:change.revision,patchCount:change.patches?.length,paths:change.patches?.slice(0,5).map(p=>p.path)});
      }
    }
  }
}
socket.on('data',data=>{
  buffer=Buffer.concat([buffer,data]);
  while(buffer.length>=4){const length=buffer.readUInt32LE(0);if(length>268435456)throw Error('invalid frame');if(buffer.length<4+length)return;const body=buffer.subarray(4,4+length);buffer=buffer.subarray(4+length);handle(JSON.parse(body));}
});
await new Promise((resolve,reject)=>{socket.once('connect',resolve);socket.once('error',reject);});
try {
  const init=await request('initialize',{clientType:'connectivity-probe'},0);
  if(init.resultType!=='success')throw Error(JSON.stringify(init));
  clientId=init.result.clientId;emit({check:'ipc_initialize',ok:true,clientId});
  const discovery=await request('thread-owner-discovery',{hostId:'local',conversationId:threadId},1);
  emit({check:'owner_discovery',response:discovery});
  if(discovery.resultType!=='success')throw Error('no desktop owner discovered');
  owner=discovery.handledByClientId;
  broadcast('thread-stream-following-changed',{hostId:'local',conversationId:threadId,following:true},1);
  const history=await request('thread-follower-load-complete-history',{conversationId:threadId},1,owner,20000);
  emit({check:'load_complete_history',response:history});
  if(operationMode==='exercise') {
    const first=await request('thread-follower-start-turn',{conversationId:threadId,turnStart:{request:{threadId,clientUserMessageId:randomUUID(),input:[{type:'text',text:'这是用户授权的 Desktop IPC 连接测试。不要使用任何工具，不要读写文件。请只回复 IPC_REPLY_OK。',text_elements:[]}]},context:{inheritThreadSettings:true}}},2,owner,60000);
    emit({check:'ipc_start_reply_turn',response:first});
    if(first.resultType!=='success')throw Error('Desktop did not accept the reply turn');
    const deadline=Date.now()+90000;
    while(Date.now()<deadline&&!hasExactString(latestState,'IPC_REPLY_OK')) {
      await new Promise(r=>setTimeout(r,2000));
      const reload=await request('thread-follower-load-complete-history',{conversationId:threadId},1,owner,20000);
      if(reload.resultType!=='success')throw Error('History refresh failed');
    }
    if(!hasExactString(latestState,'IPC_REPLY_OK'))throw Error('Reply marker was not received before deadline');
    emit({check:'ipc_reply_observed',ok:true});
    const second=await request('thread-follower-start-turn',{conversationId:threadId,turnStart:{request:{threadId,clientUserMessageId:randomUUID(),input:[{type:'text',text:'这是用户授权的停止任务测试。不要使用任何工具，不要读写文件。请按顺序输出从 1 到 2000 的整数，每行一个，不要解释。',text_elements:[]}]},context:{inheritThreadSettings:true}}},2,owner,60000);
    emit({check:'ipc_start_interrupt_target',response:second});
    if(second.resultType!=='success')throw Error('Desktop did not accept the interrupt target');
    const result=second.result?.result;
    const turnId=result?.turn?.id??result?.turnId??result?.id;
    if(!turnId)throw Error('No exact turn ID returned; will not issue an unscoped interrupt');
    const stop=await request('thread-follower-interrupt-turn',{conversationId:threadId,mode:'user-stop',expectedTurnId:turnId},4,owner,20000);
    emit({check:'ipc_interrupt',response:stop,expectedTurnId:turnId});
    if(stop.resultType!=='success')throw Error('Interrupt failed');
    await new Promise(r=>setTimeout(r,1000));
    await request('thread-follower-load-complete-history',{conversationId:threadId},1,owner,20000);
  }
  await new Promise(resolve=>setTimeout(resolve,watchMs));
  emit({check:'watch_result',snapshotSeen,counts,title:latestState?.title,turnCount:latestState?.turns?.length});
} catch(e){emit({check:'fatal',error:e.message});process.exitCode=1;}
finally {
  if(owner)broadcast('thread-stream-following-changed',{hostId:'local',conversationId:threadId,following:false},1);
  socket.end();
}
```
