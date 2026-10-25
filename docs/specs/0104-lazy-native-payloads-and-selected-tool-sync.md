# 0104：原生大字段 Lazy 化与当前会话 Tool 实时摘要

- 状态：Approved（用户于 2026-10-25 确认）
- 日期：2026-10-25
- 依据：[0023 会话截图](0023-conversation-screenshots.md)、[0100 Agent 持续跟随与 Relay 双 Turn Hot Cache](0100-agent-fed-relay-two-turn-hot-cache.md)、[0103 轻量历史首屏与 Owner 后台同步](0103-lightweight-history-first-owner-sync.md)。

## 1. 已验证事实

1. 图片在 Desktop Agent → Relay → Web 链路已经只传引用元数据，Web 通过 `thread.image` 按需加载；但 Codex App → Desktop Agent owner snapshot 仍可能内嵌原始图片数据。
2. 当前工具调用在 Web 中虽然默认折叠，规范化 snapshot／delta 仍携带最多 16 KiB 的 `activity.details`。UI 折叠不等于传输与缓存 lazy。
3. 当前 Desktop IPC frame 使用 4 字节长度头。读取器在声明长度超过 64 MiB 时会在读取正文前拒绝，因此普通 normalize 无法在拒绝前移除图片或 tool payload。
4. 当前版本尚未验证存在 metadata-only owner、字段选择或附件过滤 IPC；在完成兼容性探测前不得声称 Codex App 能从源头少发这些字节。

## 2. 目标与非目标

### 2.1 目标

1. owner snapshot／patch 中的内嵌图片和大型 tool payload 在 Desktop Adapter 边界被替换为有界引用，不进入 Relay 会话 cache。
2. Relay 最近 2 turn cache 只保存消息正文、图片引用、tool 摘要与必要状态；不保存 tool 参数、完整输出、完整 diff 或图片二进制。
3. 当前打开的会话实时同步 tool 的 `kind`、`label`、`status`、顺序和详情可用性；未打开会话不建立 tool detail 实时流。
4. 用户展开某个 tool item 时，Web 才读取该 item 的有界详情；收起、切换会话或 WebSocket 断开后停止详情读取／观察。
5. 历史会话与已完成 tool 通过 App Server item 子分页读取详情；进行中的 tool 若当前版本没有可靠详情源，显示明确的“详情生成中”，不得用旧内容冒充最新内容。

### 2.2 非目标

1. 兼容方案不能减少 Codex App 已写入本机 Unix Socket 的字节数；只有原生字段过滤能力通过探测后才能做到源头减量。
2. 不把 tool detail 或图片正文持久化到 Relay、数据库、Docker volume、访问日志或 crash dump。
3. 不为侧栏里的每个会话、每个 tool 建立独立 WebSocket、轮询或 Desktop follower。
4. tool detail 不作为 mutation receipt；发送、停止、审批与回答仍以当前 owner 状态为准。

## 3. 数据模型

### 3.1 常驻 tool 摘要

`Item.activity` 改为只包含：

- `kind`：`mcpToolCall`／`commandExecution`／`fileChange`／`sleep`／`imageView`；
- `label`：有界的人类可读名称；
- `status`：`inProgress`／`completed`／`failed`／`interrupted`／`unknown`；
- `detailState`：`omitted`／`available`／`loading`／`unavailable`；
- `detailRevision`：可选、不可逆的内容版本标识，用于拒绝旧响应覆盖新状态；
- `truncated`：仅表示原生详情本身无法完整提供，不再表示 snapshot 内含 16 KiB preview。

常驻 snapshot／delta 不再携带 `details`。

### 3.2 Lazy detail 请求

新增请求：

```json
{
  "method": "thread.activity.read",
  "params": {
    "threadId": "…",
    "turnId": "…",
    "itemId": "…",
    "cursor": "…",
    "limitBytes": 65536,
    "expectedRevision": "…"
  }
}
```

响应只返回该 item 的纯文本详情页、`nextCursor`、`revision` 与 `complete`。单页默认 64 KiB、硬上限 256 KiB；Relay 只转发，不缓存。

### 3.3 当前会话实时语义

1. 当前会话既有的 multiplexed WebSocket／`thread.cache.watch` 继续承载 thread 与 tool 摘要，不新增 per-tool WebSocket。
2. 只有用户展开的 tool item 可以建立临时 detail watch；同一 Web client 同时最多 1 个，切换展开项时取消旧 watch。
3. 默认只实时更新摘要。进行中详情若需要实时展示，使用同一 socket 上的有界 `thread.activity.watch/unwatch` 逻辑通道，并按 `revision + seq` 校验；不得把整个 tool output 反复重发。
4. 未展开 tool、后台会话与其他 Web client 不接收 detail delta。

## 4. Desktop IPC 兼容方案

1. 先做只读、只输出统计的探针，按字段类型统计 owner frame 中图片 data URI、tool arguments/output/diff、普通文本与其他字段的字节占比；不输出或落盘真实内容。
2. 同时探测当前 Codex 版本是否支持轻量 owner／字段过滤。若支持，优先使用原生能力，并保留流式 sanitizer 作为版本降级路径。
3. 若不支持，新增版本化 streaming sanitizer：
   - 对 frame 设置有限的 wire hard cap，建议初值 256 MiB；
   - 逐字节／逐 token 消费 JSON，不先分配声明长度大小的 buffer；
   - 只在已验证的 JSON path 移除图片 payload 与 tool detail，保留身份、状态、顺序及引用；
   - sanitized output 仍受现有 64 MiB 上限和全局并发预算约束；
   - 未识别 shape、非法 UTF-8、路径歧义或超出 wire cap 时 fail closed。
4. 不能使用会为超长 JSON string 分配完整字符串的通用 token API 假装“流式”；测试必须证明峰值内存不随被剔除 payload 线性增长。

## 5. Lazy detail 来源

1. 已完成／历史 item：优先通过 App Server `thread/items/list` 定位并读取，兼容裸 item 与 timeline entry 两种形状。
2. 当前进行中 item：优先读取已验证的 owner 增量；若 sanitized baseline 已丢弃详情且 App Server 尚不可见，返回 `DETAIL_PENDING`，等待后续 revision，不回退到完整 owner history。
3. 图片继续复用 `thread.image`；tool detail 不得携带图片 data URI，遇到图片只返回可继续按需加载的引用。
4. file change 的 diff 视为 tool detail，默认 lazy；文件原文／当前 diff viewer 仍走既有 file-context 权限边界。

## 6. 错误与安全边界

1. `DETAIL_PENDING`：进行中详情尚不可读；摘要仍实时更新。
2. `DETAIL_CHANGED`：`expectedRevision` 已过期；Web 清除旧页并按用户动作重新读取，不自动循环。
3. `DETAIL_TOO_LARGE`：单个不可分页字段超过 hard cap；保留摘要并提示在 Codex Desktop 查看。
4. `REFERENCE_NOT_FOUND`：thread／turn／item 身份不再存在或不匹配；不得按客户端路径读取。
5. tool detail 可能包含凭据和本机路径：不记日志、不进 Relay cache、不进 IndexedDB；Web 切换会话或刷新时从内存清除。

## 7. TDD 与验收

1. Red：snapshot／delta 中只有 tool 摘要，不出现 arguments、output、diff、data URI 或旧 `details` 字段。
2. Red：展开 tool 后只发送一次有身份约束的 detail read；收起或切换会话后迟发结果不能写入当前页面。
3. Red：当前会话 tool status 实时变化；后台会话只更新有界 cache 摘要，不产生 detail read/watch。
4. Red：detail 分页遵守 64 KiB 默认、256 KiB hard cap、cursor 与 revision；Relay 不缓存响应。
5. Red：streaming sanitizer 处理超过 64 MiB、主要由图片／tool payload 构成的 fixture，输出有界且峰值内存不随被剔除字段线性增长；未知路径 fail closed。
6. 真实兼容性验收只读目标大会话：记录原始 frame 声明大小、分类字节占比、sanitized 大小、首屏耗时与 owner 可用性；不得保存真实正文。
7. Web 补真实浏览器回归：展开、分页、进行中状态、切换会话取消与手机 viewport 均通过；物理 iPhone／Safari 仍由用户复验。
