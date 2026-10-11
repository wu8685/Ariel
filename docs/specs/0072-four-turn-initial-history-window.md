# 0072：新会话首屏最近四回合

- 状态：Implemented（Go／Web／race／真实移动 Chromium／production build 已验）
- 来源：新选择会话时，默认同步 10 个 turn 等待时间仍然偏长。
- 关联：[0013 大会话最近十回合与 64 MiB 有界同步](0013-paged-history-and-large-owner.md)、[0062 Web 端有界会话历史缓存](0062-bounded-web-history-cache.md)。
- 范围：Desktop Agent 的首屏最近窗口、Web 对分段首屏的自动补齐目标；不改变用户主动加载旧历史的页大小、64 MiB 本机上限、7 MiB 页面预算或 8 MiB Relay 单帧上限。

## 输入、输出与行为

1. 新选择／重新同步会话时，Desktop Agent 最多向 Web 投影最新 4 个完整 native turn；不足 4 个则全部返回，顺序仍为时间正序。
2. live owner 快照与 App Server stored history 使用同一个 4-turn 首屏窗口，不能出现一个为 4、另一个仍为 10 的分叉。
3. 首屏因 7 MiB 页面预算而分段时，`recentComplete: false` 的自动补齐目标改为 4 个 turn；已到历史末尾时可以少于 4 个并标记完成。
4. `historyComplete`、`recentComplete`、巨型 turn item 子分页、当前 runtime／pending interaction／权限与原 owner 安全门禁保持现状。
5. 用户显式点击“加载更早消息”时，`thread.history` 单页仍最多请求 10 个 turn。首屏窗口与旧页 page size 必须拆成独立常量，避免把首屏减小误变成更多旧页网络往返。
6. Web 的同 epoch 有界历史缓存继续复用已经下载的旧页；本变更不建立新持久化缓存，也不清理用户已缓存的内容。

## 边界与错误

- 4 指 turn，不是 message 或 item；不得截断一个正常大小的 turn 来凑数量。
- 单个 turn 超出页面预算时继续使用 0013 的 item 子分页；不能因为首屏窗口变小而放宽容量限制。
- `thread.history` 协议仍允许显式 `limit` 1–10，以保持旧历史手动分页兼容；只有自动首屏目标固定为 4。
- Relay 不保存或拼接正文，Codex 仍是会话 SSOT。

## TDD 与验收

1. Red：Desktop Agent normalize 测试要求 12-turn live owner 只保留 `turn-09`…`turn-12`。
2. Red：stored history 首屏测试要求首次 App Server turns 请求为 `limit: 4`，结果严格为最新 4 个 turn。
3. Red：Web 测试要求 `recentComplete: false` 时只自动补齐到 4；手动旧页请求仍为 `limit: 10`。
4. Green 后运行 Go／Web 全量测试、`go vet`、race、production build，并在隔离 WebSocket fixture 的真实移动 Chromium 中验证首屏自动补齐请求与手动旧页请求的 limit 分离。
5. 物理 iPhone／Safari 的实际加载耗时由用户在公网部署后复验。

## 实施结果

- Desktop Agent 将 live owner 与 stored history 的首屏窗口统一为最新 4 个 turn，并把显式旧页上限独立保留为 10。
- Web 对分段首屏只请求实际缺少的 turn 数；1 个已可见 turn 的 fixture 首次请求 `limit: 3`，手动旧页仍请求 `limit: 10`。
- Go 全量测试、`go vet`、race、Web 23 个文件／187 项测试、production build 和 Playwright 26 个真实浏览器用例通过。详见[专项测试记录](../testing/2026-10-11-0072-four-turn-initial-history-window.md)。
