---
title: "Codex App Server 探针源码"
created: 2026-10-03
tags: [codex, remote-control, evidence, source-code]
type: experiment
source: "[[2026-10-03-codex-remote-adapter]]"
project: "Codex 远程交互系统 待命名"
---

# Codex App Server 探针源码

以下为 2026-10-03 实测源码的脱敏归档，以 Markdown 代码块保存。原机器路径和真实会话标识已替换为占位符，不是可直接运行的生产客户端；使用限制见 [证据附件说明](README.md)。

## 脱敏源码

```javascript
import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import fs from 'node:fs';

const binary = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex';
const currentThread = process.argv[3] || '<business-thread-id>';
const pending = new Map();
let nextId = 1;
const child = spawn(binary, ['app-server', '--listen', 'stdio://'], {
  cwd: '<fixture-dir>',
  stdio: ['pipe', 'pipe', 'pipe'],
});
const events = [];
let stderrBytes = 0;
child.stderr.on('data', b => { stderrBytes += b.length; });
createInterface({ input: child.stdout }).on('line', line => {
  let msg;
  try { msg = JSON.parse(line); } catch { return; }
  if (msg.id != null && pending.has(msg.id) && !msg.method) {
    const p = pending.get(msg.id); pending.delete(msg.id); clearTimeout(p.timer);
    if (msg.error) p.reject(new Error(JSON.stringify(msg.error)));
    else p.resolve(msg.result);
  } else if (msg.method) {
    events.push(msg);
    if (msg.id != null) {
      child.stdin.write(JSON.stringify({id: msg.id, error: {code: -32601, message: 'Probe does not handle interactive requests'}}) + '\n');
    }
  }
});
child.on('exit', code => {
  for (const p of pending.values()) { clearTimeout(p.timer); p.reject(new Error('child exited ' + code)); }
  pending.clear();
});
function rpc(method, params = {}, timeout = 20000) {
  const id = nextId++;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { pending.delete(id); reject(new Error(method + ' timed out')); }, timeout);
    pending.set(id, {resolve, reject, timer});
    child.stdin.write(JSON.stringify({id, method, params}) + '\n');
  });
}
async function check(label, fn) {
  try { console.log(JSON.stringify({check: label, ok: true, result: await fn()})); }
  catch (e) { console.log(JSON.stringify({check: label, ok: false, error: e.message})); }
}
try {
  const initialized = await rpc('initialize', {clientInfo: {name: 'local_desktop_connectivity_probe', title: 'Local Desktop Connectivity Probe', version: '0.1.0'}});
  child.stdin.write(JSON.stringify({method: 'initialized', params: {}}) + '\n');
  console.log(JSON.stringify({check: 'initialize', ok: true, result: initialized, spawnedPid: child.pid}));
  if(process.argv[2] === 'seed') {
    const start = await rpc('thread/start', {cwd:'<fixture-dir>', ephemeral:false, sandbox:'read-only', approvalPolicy:'never'});
    const threadId=start.thread.id;
    console.log(JSON.stringify({check:'test_thread_created',threadId,model:start.model}));
    fs.writeFileSync('<fixture-dir>/test-thread.json',JSON.stringify({threadId,createdFor:'Desktop connectivity test',cwd:'<fixture-dir>'},null,2));
    const turn=await rpc('turn/start',{threadId,input:[{type:'text',text:'这是用户授权的连接测试。不要使用任何工具，不要读写文件，不要发起其他任务。只回复 CONNECTIVITY_SEED_OK。',text_elements:[]}]},45000);
    console.log(JSON.stringify({check:'seed_turn_started',turnId:turn.turn.id}));
    const deadline=Date.now()+90000;
    while(Date.now()<deadline&&!events.some(e=>e.method==='turn/completed'&&e.params?.turn?.id===turn.turn.id))await new Promise(r=>setTimeout(r,200));
    const complete=events.find(e=>e.method==='turn/completed'&&e.params?.turn?.id===turn.turn.id);
    if(!complete){await rpc('turn/interrupt',{threadId,turnId:turn.turn.id});throw Error('Seed timed out and was interrupted');}
    console.log(JSON.stringify({check:'seed_turn_completed',status:complete.params.turn.status,error:complete.params.turn.error}));
    await rpc('thread/name/set',{threadId,name:'[临时测试] Desktop 连接验证'});
    const history=await rpc('thread/read',{threadId,includeTurns:true});
    console.log(JSON.stringify({check:'seed_history',threadId,turns:history.thread.turns.length,agentText:history.thread.turns.flatMap(t=>t.items||[]).filter(i=>i.type==='agentMessage').map(i=>i.text)}));
  } else {
  await check('loaded_before', () => rpc('thread/loaded/list'));
  await check('current_thread_summary', async () => {
    const {thread} = await rpc('thread/read', {threadId: currentThread, includeTurns: false});
    return {id: thread.id, name: thread.name, status: thread.status, source: thread.source, cwd: thread.cwd, ephemeral: thread.ephemeral};
  });
  await check('current_thread_history', async () => {
    const {thread} = await rpc('thread/read', {threadId: currentThread, includeTurns: true});
    const summary={id: thread.id, status: thread.status, turns: thread.turns?.length, turnStatuses:thread.turns?.map(t=>({id:t.id,status:t.status})), itemTypes: [...new Set((thread.turns || []).flatMap(t => (t.items || []).map(i => i.type)))]};
    if(process.argv[3])summary.agentText=(thread.turns||[]).flatMap(t=>t.items||[]).filter(i=>i.type==='agentMessage').map(i=>i.text);
    return summary;
  });
  await check('list_local_threads', async () => {
    const r = await rpc('thread/list', {limit: 10, sortKey: 'updated_at', sourceKinds: ['cli','vscode','appServer'], cwd: '<business-workspace>'});
    return {count: r.data?.length, currentFound: r.data?.some(t => t.id === currentThread), ids: r.data?.map(t => t.id), nextPage: !!r.nextCursor};
  });
  await check('loaded_after_read', () => rpc('thread/loaded/list'));
  }
} catch(e) {
  console.log(JSON.stringify({check: 'fatal', ok: false, error: e.message}));
} finally {
  console.log(JSON.stringify({notificationTypes: [...new Set(events.map(x => x.method))], stderrBytes}));
  child.stdin.end();
  const stopTimer = setTimeout(() => child.kill('SIGTERM'), 3000);
  stopTimer.unref();
}
```
