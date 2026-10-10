# 0062 Web 端有界会话历史缓存验收记录

- 日期：2026-10-10
- 范围：页面内存 history cache、fresh snapshot 合并、cursor 失效回退、会话切换和移动端真实浏览器回归。
- 结论：实现与自动化验收通过；缓存只加速同一页面、同一 Agent epoch 内的重复访问，冷加载和刷新页面后的首次下载保持原流程。

## TDD 证据

先添加独立 cache 模块与 App 行为测试并运行：

```text
cd web && npm test -- src/history-cache.test.ts src/App.test.tsx
```

Red 阶段分别因 `history-cache` 模块尚不存在，以及 A → B → A 后已下载旧历史消失而失败。实现页面内 cache 与 snapshot 后恢复逻辑后，同一组测试转绿。

覆盖点：

- `deviceId + agentEpoch + threadId` 隔离、5 分钟 TTL、8 entry／32 MiB LRU；
- UTF-8 字节计数、同 key 更新、超大 entry 整体拒绝且不截断；
- 非 completed turn、重复 turnId／itemId 和无关 item override fail closed；
- fresh snapshot 与 cache 重叠时 live turn 胜出，不重复显示 turn／item；
- A 加载旧页 → 切 B → 回 A 后不重复请求相同历史页；
- restored cursor 被拒绝后仅重建一次，并以重新确认的历史替换缓存内容。

## 真实浏览器验收

`web/e2e/mobile-history-cache.spec.ts` 使用隔离 WebSocket fixture，在 Chromium 手机 viewport 中完成 A → B → A：

- A 已下载的旧页恢复，`thread.history` 请求计数保持为 1；
- B 的 fresh snapshot 显示 running，回到 A 后 fresh snapshot 显示 idle，证明 cache 没有恢复旧 runtime；
- owner subscription 每次重新建立，cache 未绕过订阅成功。

完整 Playwright 结果为 17 项全部通过。该结果属于真实 Chromium viewport／touch 验收，不代表物理 iPhone／Safari 已复验。

## 全量回归

通过：

```text
GO111MODULE=on go test ./...
cd web && npm test
cd web && npm run build
cd web && npm run test:ui
git diff --check
```

结果：

- Go 全包测试通过；
- Web 22 个测试文件、168 项测试全部通过；
- TypeScript 与 Vite production build 通过；
- Playwright 17 项真实 Chromium 用例全部通过；
- Vite 仍报告既有的单 chunk 超过 500 kB 警告，本功能没有把会话正文写入新的持久化 bundle 或服务器端存储。
