# 0073 会话增量同步与无白屏缓存恢复测试记录

- 日期：2026-10-11
- 规格：[0073 会话增量同步与无白屏缓存恢复](../specs/0073-incremental-thread-sync-and-stale-view-cache.md)
- 范围：协议 delta form、Desktop Agent per-subscription diff、Web 原子合并与页面内存缓存、手机 viewport 下切换和 resync；不写真实 Codex 会话。

## Red → Green

先写测试并实际观察失败，再分别实现：

1. 协议契约：`thread.subscribe` 的 `item-delta-v1`、full／patch 互斥以及重复顺序 ID；
2. Desktop Agent：变化 item 的完整 upsert、turn／item 删除、timestamp-only no-op、非法重复 ID 和 snapshot→activate race；
3. Web reducer：连续 seq 下原子合并 patch、未变化 turn／item 对象身份复用，以及畸形 patch fail closed；
4. 页面缓存：recent 4 turns 与 older history 共同计费，thread identity／重复 ID 校验；
5. App：A → B → A 立即恢复缓存内容；seq gap 保留正文和草稿，仅禁用 mutation，fresh snapshot 后恢复。

## 自动与浏览器回归

- `go test ./...`：通过；
- `go vet ./...`：通过；
- `go test -race ./internal/desktopagent ./internal/relay ./protocol`：通过；
- Web：23 个测试文件、190 项测试通过；
- production build：通过；
- Playwright：27/27 通过。

新增的真实 Chromium 390×844 touch fixture 验证：Web 请求 `item-delta-v1`；单 item streaming 更新不会替换未变化消息 DOM；composer focus 和 draft 保留；注入 seq gap 后正文继续显示、中央 loading 不出现、输入框可编辑但发送禁用，fresh snapshot 后恢复。物理 iPhone／Safari 的网络波动与页面观感留待用户在公网版本复验。
