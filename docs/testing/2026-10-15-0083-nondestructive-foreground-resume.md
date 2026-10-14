# 0083 前后台与会话切换的非破坏性恢复验收

- 日期：2026-10-15
- 规格：[`0083-nondestructive-foreground-resume.md`](../specs/0083-nondestructive-foreground-resume.md)
- 结论：实现、自动测试、真实移动 Chromium 与公网 Docker 发布均通过；物理 iPhone／Safari 待用户复验。

## 行为与资源边界

浏览器回到前台时，如果原 WebSocket 仍为 authenticated、`ready` 且底层状态为 `OPEN`，`wake()` 现在是 no-op：不重建连接、不清空当前正文和草稿，也不把等待回执的 mutation 改成 `OUTCOME_UNKNOWN`。只有 socket 已不可用时才取消退避并立即重连。

浏览器到 Relay 仍只有一条 WebSocket。Web 在这条连接上最多保留当前和上一个会话两个 live subscription，并分别以 `deviceId + agentEpoch + threadId + subscriptionId + streamId + seq` 校验事件。A → B → A 直接复用 A 的连续 subscription；A → B → C 会先释放 A，因此会话总数不会扩大后台订阅数。背景会话的合法 delta 只更新页面内存缓存和 unread，不改动当前会话 DOM、滚动状态或 composer。

## TDD 证据

Red 阶段先增加 Client、App 和真实浏览器测试，旧实现出现以下预期失败：

```text
healthy wake created a second socket and resolved the pending mutation as unknown
A -> B -> A emitted a third thread.subscribe request
foreground return showed the syncing state and recreated the connection
```

Green 阶段覆盖：

- 健康 socket 前台恢复保持原连接、subscription、正文和草稿；
- socket 不可用时 `wake()` 仍立即重连；
- A → B → A 只创建两个 subscription，背景 A delta 连续合并；
- 背景 A 更新不改变当前 B 的消息 DOM 和 composer；
- A → B → C 释放最旧 A，切回 A 使用缓存 + fresh snapshot 回退；
- background seq gap、错误、Agent epoch 变化和真实断线继续受 identity fencing 与 resync 保护。

## 自动验证

```text
Vitest: 24 files, 202 tests passed
Playwright: 29 tests passed
Vite production build: passed
```

`web/e2e/mobile-history-cache.spec.ts` 在真实 Chromium 手机 viewport 中模拟了超过 5 秒的 hidden → visible，并断言连接数、订阅数和 composer 草稿不变；随后模拟 A 在 B 可见期间收到 delta，切回 A 后直接显示增量内容且无同步提示。

## 公网发布验证

公网 `www.kan.chat` 已部署 `ariel-relay:381e384`。验证结果：

```text
container: ariel-relay:381e384 status=running health=healthy
healthz: HTTP 200 / ok
web asset: index-Bbx1z3C-.js
Relay established connections: 3
```

旧容器保留为已停止的 `ariel-relay-prev-220cb24`，需要时可恢复。

## 保留复验

- 在物理 iPhone／Safari 输入未发送草稿，切换应用超过 5 秒再返回，确认正文、草稿、滚动位置和输入焦点不变。
- 在同一页面执行 A → B → A，并让 A 在后台产生新回复，确认切回后直接看到新内容且不出现“正在同步”。
