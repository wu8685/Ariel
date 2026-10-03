---
title: "Codex ASAR 只读检查脚本源码"
created: 2026-10-03
tags: [codex, remote-control, evidence, source-code]
type: experiment
source: "[[2026-10-03-codex-remote-adapter]]"
project: "Codex 远程交互系统 待命名"
---

# Codex ASAR 只读检查脚本源码

以下为 2026-10-03 实测使用的原始源码，以 Markdown 代码块归档，供 Obsidian 阅读与查证。代码含原机器的固定路径和测试标识，不是可直接运行的生产客户端；使用限制见 [证据附件说明](README.md)。

## 原始源码

```javascript
import fs from 'node:fs';
const filename = '/Applications/ChatGPT.app/Contents/Resources/app.asar';
const fd = fs.openSync(filename, 'r');
const lead = Buffer.alloc(16); fs.readSync(fd, lead, 0, 16, 0);
const headerSize = lead.readUInt32LE(4);
const jsonSize = lead.readUInt32LE(12);
const header = Buffer.alloc(jsonSize); fs.readSync(fd, header, 0, jsonSize, 16);
const tree = JSON.parse(header.toString());
const entries = [];
function walk(node, prefix = '') {
  for (const [name, value] of Object.entries(node.files || {})) {
    const path = prefix + name;
    if (value.files) walk(value, path + '/');
    else entries.push({path, ...value});
  }
}
walk(tree);
const [mode, query, needle] = process.argv.slice(2);
if (mode === 'list') {
  const re = new RegExp(query || '(^|/)(main|ipc)|app-server|codex-app');
  for (const e of entries.filter(e => re.test(e.path))) console.log(e.path, e.size);
} else if (mode === 'slice') {
  const e = entries.find(e => e.path === query);
  if (!e || e.unpacked || e.offset == null) throw new Error('No packed file');
  const b = Buffer.alloc(e.size); fs.readSync(fd, b, 0, e.size, 8 + headerSize + Number(e.offset));
  console.log(b.toString().slice(Number(needle), Number(process.argv[5])));
} else if (mode === 'search') {
  for (const e of entries.filter(e => new RegExp(query).test(e.path) && !e.unpacked && e.offset != null)) {
    const b = Buffer.alloc(e.size); fs.readSync(fd, b, 0, e.size, 8 + headerSize + Number(e.offset));
    const s = b.toString(); let pos = 0; let hits = 0;
    while ((pos = s.indexOf(needle, pos)) >= 0 && hits++ < 15) {
      console.log(JSON.stringify({path: e.path, at: pos, snippet: s.slice(Math.max(0,pos - 350),pos + needle.length + 650)}));
      pos += needle.length;
    }
  }
}
fs.closeSync(fd);
```
