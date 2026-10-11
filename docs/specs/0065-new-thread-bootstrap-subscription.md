# 0065：新建会话的 bootstrap 订阅

- 状态：Implemented（Go／Web／race／真实 Chromium／全量回归已验；物理 iPhone／Safari 待部署后复验）
- 来源：手机端创建新会话后已经显示“未命名会话”和正确项目目录，但首个订阅返回 `RESYNC_REQUIRED`，页面长期停留于“正在同步会话”。
- 范围：Desktop Agent 对已确认创建回执的短期 bootstrap、Web 新建会话后的 bounded subscribe retry；不改变 `thread.create` 的 mutation 语义，不建立第二套会话数据库，不自动重发创建请求。

## 已确认事实与待验证项

1. 截图证明 `thread.create` 已返回可识别的 thread 对象，否则 Web 不会关闭创建框并进入该 thread；失败发生在随后 `thread.subscribe` 或首个 owner 状态加载阶段。
2. 当前 `Service.CreateThread` 已验证 `threadId` 和 canonical `cwd`，但返回后不保留该元数据。紧接着 `Service.Subscribe` 的 `ensureLive` 会再次调用持久化 `history.Read(threadId)`；若 App Server 的创建回执先于状态索引可见，订阅会失败。
3. Web 对创建后的首次 subscribe 没有 bounded retry。`RESYNC_REQUIRED` 直接成为持久 notice，即使新 thread 数百毫秒后已经可读取，也不会自行恢复。
4. 我不确定目标设备当次失败是在 `history.Read`、owner discovery、deep-link open 还是 follower verify；当前 Agent 没有逐请求诊断日志。规格采用不依赖该细分原因的有界修复，并增加不含正文／凭据的阶段诊断。

## 行为与不变量

1. `thread.create` 成功并完成 thread ID／cwd 校验后，Desktop Agent 将 `{threadId, cwd, title}` 写入现有有界 `threadController`，作为该进程内的 bootstrap hint：
   - 只存路由元数据，不存 turns、正文、pending interaction 或权限；
   - 受现有最多 64 个 controller 的容量与淘汰规则约束；
   - Agent 重启后允许丢失，不落盘，不成为 Codex 之外的 SSOT。
2. `ensureLive` 遇到带 bootstrap hint 的 controller 时，直接用已验证的 cwd 连接／自动加载原 owner，不要求持久化 `history.Read` 已经立即可见；普通历史会话仍以 `history.Read` 为准。
3. 创建回执不能绕过 owner 身份与 cwd 校验。`OpenFollower` 仍须完成 discovery／deep-link load／follower verify；验证失败不得伪造 snapshot。
4. Web 在 `thread.create` accepted 后只发送一次初始 subscribe。若其确定失败为 `RESYNC_REQUIRED` 或 `TIMEOUT`，等待 500 ms 后最多重试一次同一 thread 的 subscribe：
   - 不重发 `thread.create`；
   - 不重发任何消息、审批或队列 mutation；
   - 若用户切换设备／会话、Relay 断开或 generation 改变，取消重试；
   - 第二次失败后显示原错误，停止自动恢复。
5. 初次失败到重试完成期间保留新 thread header、项目目录和 loading 状态，不显示短暂的 seq-gap notice；成功 snapshot 到达后正常启用输入。
6. 普通历史会话的 seq gap 仍按 0063 fail closed；本规格不得把真正不连续的已建立 stream 当成创建 bootstrap 竞态。
7. Desktop Agent 增加结构化阶段日志时，只允许记录 request method、错误分类、耗时和脱敏 thread ID 前缀；不得记录 token、cwd、标题、正文、IPC payload 或私有方法参数。

## 错误与边界

- `thread.create` 回执 ID／cwd 不一致：继续 `unknown/OUTCOME_UNKNOWN`，不得建立 bootstrap hint。
- controller 容量已满且无 idle victim：创建结果仍可返回；跳过 hint，Web 的一次 bounded retry作为降级。
- owner 在 28 秒 subscribe budget 内仍不可加载：返回 `TIMEOUT`／明确错误，Web 完成一次重试后停止。
- Agent 或 Relay 在 create 回执后断开：不自动重发 create；重连后通过 Codex 持久化索引重新发现 thread。
- 用户在重试前离开新 thread：旧 subscribe 回执按 selection generation 丢弃，并清理已接受的 orphan subscription。

## TDD 与验收

1. Red：Service fake creator 返回新 ID，但 fake history 对该 ID 返回 `NOT_FOUND`；`CreateThread` 后立即 `Subscribe`，断言能使用 bootstrap hint 建立 live，且不调用该 ID 的 history read。
2. Red：非法／不匹配创建回执不得写入 controller hint；原 `OUTCOME_UNKNOWN` 测试保持。
3. Red：Web 测试让创建后的首次 subscribe 返回 `RESYNC_REQUIRED`，推进 500 ms，断言只重试 subscribe、不重发 create；第二次成功 snapshot 恢复输入。
4. Red：在 500 ms 内切换设备／会话，断言不执行创建恢复重试。
5. 真实 Chromium 隔离 WebSocket fixture 覆盖 create accepted → first subscribe rejected → bounded retry → fresh snapshot；断言没有 seq-gap banner、创建请求仅一次、最终可输入。
6. 完成 Go 全包、`go vet`、相关 race detector、Web 全量测试、production build 与真实 Chromium 回归。物理 iPhone／Safari 和真实目标 Desktop 在部署后复验。

## 实现结果

- Desktop Agent 将通过校验的创建回执路由元数据放入现有 64 项 controller cache；立即订阅不再依赖新 thread 已进入持久化历史索引。
- Web 只在创建后的首次 subscribe 返回 `RESYNC_REQUIRED`／`TIMEOUT` 时等待 500 ms 并重试一次；切换 selection 会取消重试，`thread.create` 始终只发送一次。
- 2026-10-11 验证：Go 全包与 `go vet` 通过，Desktop Agent／Relay race 通过，176 个 Web 单测、production build 和 22 个真实 Chromium 用例通过。
