# 0084 Passkey 滑动会话与本地持久恢复验收

- 日期：2026-10-15
- 规格：[`0084-passkey-sliding-session-and-durable-resume.md`](../specs/0084-passkey-sliding-session-and-durable-resume.md)
- 实现提交：`8aecc78`
- 结论：实现、自动测试、真实移动 Chromium 与公网 Docker 发布均通过；物理 iPhone／Safari 待用户复验。

## 根因与最终行为

Passkey Cookie 原先固定 24 小时过期，已建立的 WebSocket 会掩盖过期，直到 Safari 切后台后 socket 丢失并重新握手才显示登录失效。Web 同时会在任意非 `ready` 状态清空当前 view，因此登录或重连又触发整页同步。

实现后，有效 Passkey 会话在首次加载、前台恢复和最长每 12 小时一次的状态检查中续期 24 小时；无效或已过期 Cookie 不会被续期。真实 socket 断开仍会在后台重新订阅以保证正确性，但页面保留最后确认的正文、历史、草稿和滚动位置，fresh owner snapshot 前禁止 mutation。Safari 页面进程被回收时，从同源 IndexedDB 恢复最近选择和本地显示缓存，再后台合并 fresh snapshot。

持久缓存按 `deviceId + agentEpoch + threadId` 隔离，只保存渲染所需的已确认数据；不保存凭据、permission owner、pending interaction 回答、mutation receipt 或待发送图片。缓存上限为 20 个会话、单条 2 MiB、合计 25 MiB和 7 天 TTL。

## TDD 证据

Red 阶段先观察到以下预期失败：

```text
authenticated /api/auth/status did not return a renewed Set-Cookie
WebSocket close 1006 cleared the current conversation
durable history cache module was absent
```

Green 阶段覆盖：

- 只为仍有效且可解密的 Passkey owner session 滑动续期；
- socket 1006 close、retry、`hello.ok` 和 fresh snapshot 全程保留正文与草稿，owner 恢复前 mutation 禁用；
- reload 后先恢复 IndexedDB 正文，再后台订阅，不重复拉取已缓存的旧历史页；
- epoch／device／thread 身份隔离、TTL、LRU、条目／总容量限制、损坏记录、quota 错误与显式清理；
- fresh snapshot 恢复 owner，旧 subscription 的迟到事件仍由 identity fencing 丢弃。

## 自动验证

```text
go test ./... -count=1: passed
go test -race ./... -count=1: passed
Vitest: 25 files, 207 tests passed
Playwright: 29 tests passed
Vite production build: passed
```

Passkey 真实 Chromium virtual authenticator 回归额外验证：续期响应仍使用 `Secure + HttpOnly + SameSite=Strict` Cookie。移动 Chromium 回归实际关闭 socket 并 reload 页面，验证正文稳定、草稿保留、无登录面板、无整页 loading，以及 fresh owner 前禁止发送。

## 公网发布验证

公网 `www.kan.chat` 已部署 `ariel-relay:8aecc78`：

```text
container: ariel-relay:8aecc78 status=running health=healthy
healthz: ok
web asset: index-BqndPrYj.js
unauthenticated /api/auth/status: HTTP 200, authenticated=false, no Set-Cookie
Relay established connections: 3
```

旧容器保留为已停止的 `ariel-relay-prev-381e384`，配置备份为 `/opt/ariel/config.env.pre-8aecc78`，需要时可以恢复。公网无用户 Passkey Cookie，因此线上只验证无效会话不会被错误续期；有效会话的 Cookie 轮换由真实 virtual authenticator E2E 覆盖。

## 保留复验

- 物理 iPhone／Safari 登录一次后切换应用再返回，确认不再重复要求 Passkey。
- 断网或切换应用导致 socket 重建时，确认正文、草稿和滚动位置不变，恢复期间不能发送，fresh snapshot 到达后恢复操作。
- Safari 回收页面进程并 reload 时，确认先显示本地缓存正文，再在后台补充最新增量，且不出现整页同步。
