# 轻量 Owner 状态兼容性探测（2026-10-25）

## 范围与安全边界

- Desktop：`26.930.31730`；bundled Codex：`0.160.0`；IPC profile：`verified`。
- 对一个本地大会话执行只读结构探测，不发送消息、不停止 turn、不审批、不修改会话或工作区。
- 输出仅保留字节数、字段名、计数和状态，不记录 thread ID、cwd、标题、正文、tool output、diff、图片或原始 IPC frame。

## 探测 A：仅 Follow，不加载完整历史

新增 `probe owner-passive`：

1. 精确执行 `thread-owner-discovery`；
2. 向该 owner 发送 `thread-stream-following-changed(following=true)`；
3. 在有界观察窗口内接收 owner event；
4. 结束时发送 `following=false`；
5. 全程不调用 `thread-follower-load-complete-history`。

TDD 通过：测试确认 passive probe 的 wire sequence 只有 discovery、follow 和 unfollow，任何额外 complete-history request 都会失败。

真实结果：仅 follow 后，owner 仍主动发送一份 `snapshot`：

- IPC event：39,046,355 bytes；
- reducer materialized `conversationState`：39,773,193 bytes；
- revision：3912；
- snapshot 包含 `threadRuntimeStatus`、`requests`、`latestThreadSettings`、`turnHistory`、`turns`、`paginatedHistory`、`turnsPagination` 等顶层字段。

结论：删除显式 `thread-follower-load-complete-history` 不能消除大帧。当前 Desktop owner 在 follower 加入时会主动发送完整 baseline；后续 patch 必须依赖该 baseline，不能从中途 patch 安全重建轻量状态。

## 探测 B：公共 App Server 轻量读取

新增 `probe history-light`，依次执行：

1. `thread/read(includeTurns:false)`；
2. `thread/turns/list(limit:2, sortDirection:desc, itemsView:full)`。

真实结果：

- metadata 编码后 221 bytes，turn 数为 0，thread identity 匹配；
- 最近 2 个完整 turn 编码后 2,151,417 bytes，共 87 个 item；
- 两个 turn 状态分别为 completed、interrupted；
- 存在 older cursor，可继续按需分页。

结论：当前版本已验证能够用公共 App Server 有界取得页面首屏需要的 thread metadata 和最近 2 个 turn，且不读取完整历史。

## Bundle 静态核对

当前 bundle 中可识别的 follower 读取 method 只有 `thread-follower-load-complete-history`；没有发现 `thread-follower-load-metadata` 或其他轻量 snapshot method。`thread-stream-following-status-requested` 被 bundle 分类为 follower coordination，不是 conversation state 读取接口。静态字符串只能用于缩小探测范围，不能单独证明 capability supported。

## 兼容性结论

1. **首屏阅读路径可轻量化**：使用 App Server metadata + 最近 2 turn，Desktop Agent 规范化后再发送 Relay。
2. **当前 owner follow 无法轻量化**：只要建立 Desktop IPC follower，就会立即产生约 39 MiB 完整 baseline；不能通过删除 complete-history request 规避。
3. **不得猜测私有 method**：当前版本没有验证过的 metadata-only owner API。
4. 可落地方案是把阅读与 owner authority 解耦：先展示 App Server 的有界内容；owner follower 仅在后台或写操作需要时建立。是否后台预热或按需建立，由后续已批准 spec 明确。

