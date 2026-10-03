---
title: "Codex 远程交互接入方式与实测证据"
created: 2026-10-03
tags: [codex, app-server, ipc, compatibility]
type: experiment
source: "[[2026-10-03-codex-remote-requirements]]"
project: "Codex 远程交互系统 待命名"
---

# Codex 远程交互接入方式与实测证据

## 结论与证据等级

2026-10-03 在当前 macOS 上，独立 App Server 成功读取现有 Desktop 会话历史；通过 Desktop 自身的本地 IPC 成功订阅实时状态，并对临时测试会话发送和停止任务。不能把这解释成“启动任何一个 App Server 就接管了正在运行的 Desktop”。

本文实验结论来自同一天早先完成的测试，整理 spec 时未重新运行。原始报告见 [实测报告原文](evidence/2026-10-03-codex-desktop-test-results.md)，探针见 [证据附件说明](evidence/README.md)。

公开协议与私有 IPC 要分别看待。[官方 App Server 文档](https://learn.chatgpt.com/docs/app-server) 说明标准 thread/list 用于列历史、thread/read 可读历史而不恢复执行，thread/resume 用于继续会话。该文档不构成本机私有 IPC 的稳定性承诺。

## 已测环境

| 项目 | 实测值 |
| --- | --- |
| Desktop 内置 Codex CLI | 0.159.0-alpha.12.1 |
| PATH 的 Codex CLI | 0.144.6，与 Desktop 不同 |
| Desktop 内置 binary | /Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex |
| Desktop App Server 入口 | 由 Desktop 持有的 stdio，非可直接连接的公开监听端点 |
| 默认公开 control socket | 本次环境不存在；daemon version 探测失败 |
| 本机 Desktop IPC socket | `<user-home>/.codex/ipc/ipc.sock`（路径脱敏） |
| 检查方式 | 已安装应用包的只读检查，无修改应用、配置或凭据 |

这些路径只记录实验环境。实现必须允许配置 binary、Codex home、socket；不能硬编码用户名、应用名、owner UUID、threadId 或 PID。另一台也是 macOS，仍须检查它的应用版本与协议。

## 已通过的测试

| 测试 | 结果与限制 |
| --- | --- |
| 列出及读取当前 Desktop 聊天 | 标准 thread/list 和 thread/read 成功 |
| 标准读取进程是否共享 Desktop runtime | 未共享；当前聊天运行中，独立进程返回 notLoaded，loaded/list 为空 |
| 当前聊天 owner 发现和订阅 | IPC 成功；收到 snapshot 和后续 patches，当前业务聊天只读未注入消息 |
| 对测试聊天发送消息 | IPC 成功，收到 IPC_REPLY_OK，Desktop 读回 completed |
| 停止测试 turn | 使用 expectedTurnId 成功，Desktop 读回 interrupted |
| IPC 断开重连 | 重新发现 owner 并收到包含已完成回复的快照 |
| 从新的 App Server 读取测试结果 | 三个 turn 可读，状态 completed、completed、interrupted |

测试聊天先在独立 App Server 创建并完成一条回复，然后该进程退出。在 Desktop 中打开后，Desktop 成为它的 owner。这说明本次测试不受“最初由谁创建”阻碍；并不证明所有历史格式与来源都兼容。

原临时测试会话已归档，可恢复；ID 已脱敏。当前业务会话未被测试停止，未写入测试消息。全部探针进程和连接已经退出。

## 尚未验证及其重要性

| 编号 | 验证项 | 为什么需要 |
| --- | --- | --- |
| V01 | 从未打开或当前无 owner 的旧会话自动加载 | 决定手机点击旧会话能否直接续聊；不通过可讨论手动 Desktop 打开的降级 |
| V02 | 命令、文件变更和权限审批的发现与响应 | 用户明确要求手机审批；不能仅凭接口名存在宣称完成 |
| V03 | 结构化问题、选项和自由输入的提交 | 用户明确要求手机回答；字段类型必须逐类验证 |
| V04 | 两端同时响应、请求过期、Agent 重启后请求恢复 | 防重复批准、过期问题误投与恢复后的漏请求 |
| V05 | Desktop 与 Web 同时发起 turn | 验证不排队语义，确认 owner 的冲突行为 |
| V06 | Desktop 完全退出、重启和升级 | owner、协议和状态路径可能变化 |
| V07 | 大历史、特殊 item、不同历史格式、MCP elicitation | 定义可支持范围和明确降级，不能把缺数据当作空会话 |

V01 至 V04 可以用本地探针先做，不需要 Relay 或手机页面。三端联调另验证手机展示、局域网、断线和实际交互。这是接口可行性测试与产品验收的区别。

## 建议的 Adapter 合约

以下是内部接口意图，不约束具体语言签名：

| 接口 | 职责 |
| --- | --- |
| DetectCapabilities | 返回 binary、Desktop 版本、socket 连通、已验证能力，不输出凭据 |
| ListThreads / ReadHistory | 只读持久化数据，给出数据完整性与支持范围 |
| DiscoverOwner | 获取 thread 的真实执行 owner，失败不擅自 resume |
| Subscribe / Unsubscribe | 获取快照与后续更新，管理每个 Web 订阅引用 |
| StartTurn / InterruptTurn | 仅对已识别 owner 操作，保持精确 thread/turn 关联 |
| ListPendingInteractions | 从 live requests 获取审批和问题，缺失要区分未知与空 |
| RespondInteraction | 验证请求仍有效，把用户的真实决定映射回 owner |
| TryLoadThread | 可选能力，V01 通过才启用；否则返回 needsDesktopOpen |

私有协议全部放在单独的版本化包内，Relay 和 Web 不知道原始方法名。另实现 Mock Adapter，模拟流式回复、审批、问题、错误、owner 失效和断线，用于开发和自动测试。

## 本机已验证的私有 IPC 线协议

只适用于上述版本，不是标准 JSON-RPC。每帧为 4 字节 little-endian 无符号长度加 UTF-8 JSON，允许拆包和粘包。原始探针使用了应用内观察到的 256 MiB 上限；正式实现应选择有界内存与更小的受控限制，超限明确失败，不能直接照抄该分配策略。

请求例子：

```json
{
  "type": "request",
  "requestId": "<UUID>",
  "sourceClientId": "<初始化分配的 client ID>",
  "version": 1,
  "method": "thread-owner-discovery",
  "params": {"hostId":"local","conversationId":"<thread ID>"},
  "timeoutMs": 10000
}
```

先发 initialize，version=0，sourceClientId 为 initializing-client，params 为 `{"clientType":"connectivity-probe"}`。成功响应的 result.clientId 是后续 sourceClientId。客户端应如实标识自己，不伪装 Desktop。

响应使用 `type=response`、requestId、resultType（success 或 error）、handledByClientId 和 result/error。owner discovery 成功的 handledByClientId 是后续请求的 targetClientId，不要持久化复用。无 owner 的本次响应为 no-client-found。

遇到 client-discovery-request，探针回复 client-discovery-response，保留 requestId，response.canHandle=false。正式 Agent 不应谎报自己拥有 Codex thread。

| 方法 | 本次版本 | 用法 |
| --- | --- | --- |
| initialize | 0 | 注册 IPC 客户端 |
| thread-owner-discovery | 1 | params 带 hostId=local、conversationId |
| thread-stream-following-changed | 1 | broadcast，following=true/false；targetClientIds 指向 owner |
| thread-follower-load-complete-history | 1 | 向 owner 请求，params 带 conversationId，返回 revision 并触发快照 |
| thread-stream-state-changed | 11 | 接收 snapshot 或 patches |
| thread-follower-start-turn | 2 | 向 owner 提交文字，继承原会话设置 |
| thread-follower-interrupt-turn | 4 | 带 mode=user-stop 和精确 expectedTurnId |

订阅广播为 `type=broadcast`，含 sourceClientId、version、method、params 和 targetClientIds 数组。params 中包含 hostId、conversationId、following。取消订阅不是停止 turn。

启动 turn 的原始 params：

```json
{
  "conversationId": "<thread ID>",
  "turnStart": {
    "request": {
      "threadId": "<thread ID>",
      "clientUserMessageId": "<UUID>",
      "input": [{"type":"text","text":"<用户输入>","text_elements":[]}]
    },
    "context": {"inheritThreadSettings":true}
  }
}
```

本次成功响应中 turn 位于 `result.result.turn`。解析失败应报告协议不兼容，不要猜测 turnId 后执行停止。expectedTurnId 的精确绑定是已测安全条件。

## 状态合并与审批映射

snapshot 的 change 含 type、revision、conversationState；patches 的 change 含 baseRevision、revision、patches。当前版本部分真实历史在 `turnHistory.history.entitiesByKey` 等结构中，顶层 `turns=[]` 并不表示没有历史。

正式 Adapter 需实现按数组路径的安全 patch 合并、基准 revision 校验、缺口重订阅、未知字段兼容与明确失败。原始探针只观察 patches，并通过再次获取快照检查结果，没有实现完整 patch reducer。因此它不能证明生产级流式归一化已经完成。

当前代码只读检查中发现了审批、权限响应、submit-user-input、MCP elicitation 等 follower 分支，但没有执行验证。不要在本规格中把这些候选分支填成确定的参数 Schema。V02/V03 需要记录真实 request、响应类型和快照变化，再补成版本化 fixtures；记录前应脱敏。

审批与问题恢复必须以 owner 当前 requests 为准。如果无法区分“已处理”与“数据未加载”，interactionStateKnown=false 并禁止提交旧卡片。响应不能只靠 Agent 内存去重：电脑也可直接处理，最终应由 owner 确认单次生效。

## 未加载会话的探针设计

1. 创建独立、无业务文件的临时 fixture，限定只读和不使用工具，完成一条短回复后结束创建进程。
2. 不在 Desktop 打开它，确认历史可读且 owner 不存在；这是测试前置条件。
3. 寻找当前版本明确支持的加载/打开入口，从独立探针尝试，而不是调用当前 Codex 聊天才有的工具并宣称独立 Agent 可用。
4. 加载后再次发现 owner，确认仍是原 threadId、原 cwd，没有创建新会话或并行 executor。
5. 仅对 fixture 发送短回复并停止另一个精确 turn，重新读取记录核对。
6. 未找到入口则记录不支持与手动打开路径；不要修改 Desktop 包、全局权限或绕过安全检查。

早先测试用了当前 App 提供的导航工具打开 fixture。这证明“在 Desktop 打开后可控”，但不是独立 Agent 能自动加载的证据。可能的 deep link 或其他入口只算待调查候选，不能写成已实现依赖。

## 另一台电脑的复验纪律

只读检测先行；业务聊天仅列出、读取和订阅。发送、停止、审批等验证只对明确授权的隔离 fixture 执行。审批测试用无害命令和隔离目录，检查允许、拒绝、电脑先处理、移动端先处理及请求过期。

不迁移当前机器的登录凭据、当前聊天 ID 或 IPC owner。原始 probe 的固定路径只能作为参考；新探针必须使用配置、显式目标 ID 与写操作开关。完成后关闭进程、恢复原界面，并归档测试会话，不删除业务数据。

## 相关规格

[[2026-10-03-codex-remote-architecture]]、[[2026-10-03-codex-remote-protocol]]、[[2026-10-03-codex-remote-delivery]]。
