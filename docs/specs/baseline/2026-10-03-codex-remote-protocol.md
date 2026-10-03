---
title: "Codex 远程交互系统通信协议草案"
created: 2026-10-03
tags: [codex, remote-control, websocket, protocol]
type: experiment
source: "[[2026-10-03-codex-remote-requirements]]"
project: "Codex 远程交互系统 待命名"
---

# Codex 远程交互系统通信协议草案

## 草案边界

本协议是本系统自定义的 Web 与 Relay 与 Agent 协议，不是 Codex 官方协议，也不是已完成实现。以 JSON over WebSocket 为首版提案；Codex 私有 IPC 只存在于 Agent Adapter 内。

协议的核心约束来自已确认需求：无离线投递、无业务数据库、Codex 接受才算发送成功、运行中不自动排队、手机可审批和回答。字段名和工程参数可在实现前调整，但不能悄悄改变这些语义。

## 连接与鉴别

所有客户端主动连 Relay `/ws`。第一帧是 `hello`，成功前不得发业务请求；成功后 Relay 赋予本次连接 `connectionId`。

```json
{"v":1,"type":"hello","role":"web","token":"<配置 token>","clientInstanceId":"<页面 UUID>"}
```

Agent 的 hello 额外包含 `deviceId`、`deviceName`、`agentEpoch`、`adapterVersion` 和 `capabilities`。`agentEpoch` 每次 Agent 启动变化；`connectionId` 每次重新连接变化。Relay 不接受业务帧伪造来源连接身份。

Relay 返回 `hello.ok`，包含 `connectionId`、`relayEpoch` 和协议版本。鉴别失败或不支持的版本关闭连接。同一 deviceId 同时出现两个活跃 Agent 连接时拒绝新连接，旧连接通过关闭或心跳失效后才允许重新注册，避免无声替换路由。

Agent 在 hello 和后续 `device.status` 帧中报告 codexReady 与 capabilities；Relay 只接受来自该设备当前注册连接的报告。上线、失联或能力变化时，Relay 向已鉴别 Web 发出 `device.updated` 事件。心跳和状态报告不携带聊天正文。

建议初值：hello 超时 5 秒，应用层 ping/pong 间隔 15 秒、45 秒失联；重连退避 1、2、4、8 秒，上限 15 秒并加抖动。它们是可配置工程参数，不是产品 SLA。

## 请求和响应

请求 ID 使用 UUID；Relay 内部按 `(connectionId, requestId)` 关联，并生成唯一 relayRequestId 转给 Agent。Agent 响应沿原路返回，不广播给无关连接。跨连接不存在自动续传请求的能力。

```json
{
  "v": 1,
  "type": "request",
  "requestId": "<UUID>",
  "deviceId": "mac-main",
  "method": "turn.start",
  "params": {
    "threadId": "<Codex thread ID>",
    "clientMessageId": "<UUID>",
    "text": "继续分析刚才的问题"
  }
}
```

```json
{
  "v": 1,
  "type": "response",
  "requestId": "<原 requestId>",
  "ok": true,
  "result": {"accepted": true, "threadId": "<thread ID>", "turnId": "<Codex turn ID>"}
}
```

```json
{
  "v": 1,
  "type": "response",
  "requestId": "<原 requestId>",
  "ok": false,
  "error": {"code": "TURN_BUSY", "message": "当前会话仍在执行", "outcome": "not_submitted"}
}
```

`outcome` 对变更请求必填：`not_submitted` 表示确定未向 Codex 提交，`rejected` 表示 Codex 明确拒绝，`unknown` 表示可能已受理但没有可靠结果。超时发生在转交之后，必须用 unknown，不能误报为失败可重试。

建议单次请求期限 60 秒。超时只结束这次等待，不代表取消 Codex 操作。Agent 在向 Codex 发出变更前须检查请求是否已过期；发出之后超时则进入 unknown。Relay 不自动重试、不持久化，也不把“收到请求”当作 accepted。

## 首版方法

| 方法 | 参数 | 成功结果与约束 |
| --- | --- | --- |
| `device.list` | 无 | Relay 内存中的设备列表、agentOnline、codexReady、capabilities；Agent 尚未上线时可为空 |
| `thread.list` | cursor、limit、可选 cwd/query | items、nextCursor、filterScope；默认非归档，不能把局部过滤冒充全量搜索 |
| `thread.read` | threadId | 持久化历史视图、historyComplete、controlAvailability；读取不等于接管 |
| `thread.subscribe` | threadId | subscriptionId；随后必须先收到 thread.snapshot，再接收更新 |
| `thread.unsubscribe` | subscriptionId | 取消此 Web 的订阅，不停止执行 |
| `turn.start` | threadId、clientMessageId、text | Codex 受理后返回 turnId；busy 时拒绝，不排队 |
| `turn.interrupt` | threadId、expectedTurnId | 精确目标的 accepted 或 alreadyTerminal；不可停止“碰巧现在运行”的另一个 turn |
| `interaction.respond` | threadId、interactionId、interactionRevision、response | owner 接受或明确拒绝；不得凭 Web UI 状态判定请求仍有效 |

除 device.list 外由 Agent 处理。字段验证、限制和来源鉴别在 Relay 与 Agent 两侧执行；Web 不能传任意 Codex 方法名、主机路径指令或权限覆盖参数。

thread.list 每项最少包含 threadId、title、preview、cwd、updatedAt。无实时来源时 `runtimeState` 必须是 unknown，不能把标准读取进程的 notLoaded 转成 idle。owner 检查可在点选后进行，不要求对全部历史会话逐个建立实时订阅。

`controlAvailability` 值为 ready、needsDesktopOpen、unsupported、unavailable，并附 reason。capabilities 最少覆盖 list/read/subscribe/start/interrupt/approval/userInput/autoLoad；值为 supported、unsupported 或 unverified，不以方法名存在代替实测支持。

列表和历史读取采用按请求拉取，不另加持久化索引。详情页以 live 快照取代旧历史视图；不同来源不能仅按到达时间覆盖，持久化读取不能覆盖已经收到的较新 live 状态。列表在进入、手动刷新、重连以及当前会话终态后刷新。

## 快照和增量

Agent 将 Desktop 快照规范化，Web 不解析 Codex 私有 patch 路径。建议使用完整的 item 替换更新，不把 token delta 当成唯一可恢复记录。

```json
{
  "v": 1,
  "type": "event",
  "deviceId": "mac-main",
  "threadId": "<thread ID>",
  "subscriptionId": "<订阅 UUID>",
  "streamId": "<本次快照流 UUID>",
  "seq": 1,
  "event": "thread.snapshot",
  "data": {
    "runtimeState": "idle",
    "activeTurnId": null,
    "controlAvailability": "ready",
    "turns": [],
    "items": [],
    "interactions": [],
    "historyComplete": true,
    "interactionStateKnown": true
  }
}
```

上述空数组只展示结构，不表示实际会话为空。一个 item 包含 `id`、`turnId`、`kind`、`text`、可选 `status` 和 `details`；kind 为 userMessage、assistantMessage、toolActivity、fileChange 或 unsupported。快照中的 items 按 Codex 的展示顺序排列；无法可靠识别的 item 必须可见地标识，不静默抛弃。

runtimeState 取 idle、running、waitingApproval、waitingInput、unknown 或 unavailable；waiting 状态仍占用当前 turn，不开放新消息发送。turns 摘要数组中每项包含 id、status 和可选 error；终态为 completed、failed 或 interrupted，运行中为 inProgress，无法确定为 unknown。不能仅凭 activeTurnId 变空推断成功。

无 owner 时 thread.read 可返回持久化只读视图，但 thread.subscribe 返回 THREAD_NOT_OWNED，不制造假 live 流。Web 可继续展示只读历史，禁止发送、停止或回答旧请求；稍后用户刷新即可重新发现 owner。

增量事件 `thread.update` 含 `baseSeq`、完整替换的 `upsertItems`、`removedItemIds`，并在顺序变化时包含完整 `itemOrder`；可同时替换 runtimeState、activeTurnId、interactions 和 interactionStateKnown。所有替换均按 ID，不追加同一段完整正文造成重复。终态必须明确呈现 completed、failed、interrupted，不能统一显示完成。

一致性规则：

1. 每次新订阅或重同步生成新的 streamId，快照为 seq=1。Agent 必须先返回订阅成功响应，再有序发出该订阅事件；Relay 保持顺序。Web 收到 subscriptionId 之后才应用对应事件。
2. Agent 在一个有序执行通道上处理 IPC patch 与快照转换，快照之后的事件不留空窗。
3. seq 必须连续，baseSeq 必须匹配；发生缺口、乱序或未知 item 时停用写入，重新订阅取快照。
4. 更换 streamId 后丢弃旧流迟到事件；不能把两个 owner 或两次 Agent 启动的快照混在一起。
5. Relay 重启后重建订阅，Agent 重新发送快照，不依赖 Relay 持久化 replay。

owner 丢失、版本不兼容等订阅级故障用 `subscription.error` 事件报告，包含 subscriptionId、streamId、code 和 message；停止继续发旧流。需重同步时使用 RESYNC_REQUIRED。只有成功的新快照才能重新开放操作，不因 WebSocket 恢复就立即恢复按钮。

Agent 可以将纯文本刷新合并至约 100 毫秒一次；审批、运行状态与终态不应被长时间延迟。最初联调可以先用限频全量快照实现，但必须通过大历史负载测试后再决定是否作为首版最终方案。

历史读取不能静默截断。首版可对可承载的会话整体读取；遇到超限会话，返回 HISTORY_TOO_LARGE 和明确说明，不把最后几条伪装成完整历史。后续分页需单独定义游标一致性；本草案不假定所有 Codex 历史格式均支持分页。

## 审批和补充回答

待处理对象由 Agent 从 Desktop 当前 requests 状态生成，保留原请求身份映射在内存中。公开给 Web 的 `interactionId` 应绑定 `agentEpoch`、owner 身份和原请求 ID，避免进程重启后误用旧卡片。

```json
{
  "interactionId": "<不透明 ID>",
  "interactionRevision": 1,
  "turnId": "<turn ID>",
  "kind": "approval",
  "status": "pending",
  "title": "允许执行此命令吗",
  "context": {"command": "<来自 Codex>", "cwd": "<来自 Codex>"},
  "allowedDecisions": ["allow_once", "deny"]
}
```

审批 context 按类型包含命令、目标文件、diff、申请的权限范围或风险说明；不能凭空补造。展示内容与 interactionRevision 绑定，内容变更使旧响应无效。

审批响应形如 `{"decision":"allow_once"}` 或 `{"decision":"deny"}`，只允许卡片给出的值。文件和权限请求由 Adapter 映射到该版本真实支持的最小授权范围；没有单次许可语义时不得自造，需要缩减可选决定或报告不支持。

补充回答 kind 为 userInput，含 `questions`，每题有 id、header、prompt、options、是否允许自由输入、是否允许多选等真实约束。结构化回答形如 `{"answers":{"<question ID>":["<option ID 或自由文本>"]}}`；这只是自定义协议表示，Adapter 必须转换成已验证的 Codex 原生结构。没有选项的题目提供自由输入。不得无依据增加“跳过”能力。

处理步骤：

1. Web 展示卡片，用户明确选择后点提交；按下后显示处理中并禁用重复点击。
2. Agent 再检查 owner、请求 ID、revision 与 pending 状态，按请求串行提交给 Codex。
3. owner 的接受响应才表示已受理；随后状态快照移除或标记该请求 resolved。
4. 电脑已处理的请求返回 INTERACTION_RESOLVED；内容变化返回 STALE_INTERACTION，重新展示。
5. 发出后断线或超时用 unknown，不自动重发。重连重新查询 owner 当前 requests，不从历史聊天推断待审批列表。

手机提交审批时不能解除整个 turn 的 busy 约束：它是同一 turn 内的响应，不是新用户消息。补充回答同理。

## 投递与重复点击

Web 使用 clientMessageId 区分一次提交，Agent 在当前进程内维护有界去重表。建议容量 1000 条、保留 10 分钟；不能淘汰仍未结束的提交关联，容量不足则拒绝新请求。

同一 clientMessageId 和同一内容可返回已有受理结果；同 ID 不同内容返回 ID_REUSE。TTL 过期、Agent 重启、Relay 重启后的 unknown 不能依靠它得到 exactly-once。即使 clientMessageId 被透传给 Codex，也不得未经测试宣称原生幂等。

页面发送状态为 draft、submitting、accepted、rejected 或 unknown。accepted 与 turn completed 是两回事。unknown 时先刷新历史；仅看见相同文字不足以自动关联执行，更不能据此自动补发。

## 错误与容量

| code | 含义 |
| --- | --- |
| UNAUTHORIZED / PROTOCOL_UNSUPPORTED | 鉴别或协议版本不符 |
| DEVICE_OFFLINE / CODEX_UNAVAILABLE | 设备未连接，或 Desktop 不可用 |
| THREAD_NOT_FOUND / THREAD_NOT_OWNED | 会话不存在，或 Desktop 尚未接入 |
| HISTORY_UNSUPPORTED / HISTORY_TOO_LARGE | 历史格式或大小不能安全处理 |
| TURN_BUSY / STALE_TURN | 当前忙，或停止目标已不是当前 turn |
| INTERACTION_RESOLVED / STALE_INTERACTION | 审批或问题已处理，或内容已改变 |
| INTERACTION_UNSUPPORTED | 不支持该交互，要求回电脑处理并说明类型 |
| OUTCOME_UNKNOWN / RESYNC_REQUIRED | 变更结果不明，或视图需要重建 |
| INVALID_ARGUMENT / OVERLOADED | 输入不合法，或有界资源已满 |

建议参数：用户输入上限 64 KiB，单帧上限 8 MiB，单连接待发缓冲 16 MiB，单 Web 未完成请求 32 个。具体上限应以测试调优；大历史和大 diff 超限必须有可见错误，不得截去关键信息后仍允许审批。后台日志不能成为隐形聊天数据库。

## 相关规格

[[2026-10-03-codex-remote-architecture]]、[[2026-10-03-codex-remote-adapter]]、[[2026-10-03-codex-remote-delivery]]。
