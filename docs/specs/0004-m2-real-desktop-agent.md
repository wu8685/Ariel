# 0004：M2 真实 Desktop Agent 与历史会话自动加载

- 状态：实施中。用户于 2026-10-04 授权连续推进到 MVP；本规格沿已批准的基线和 M0 证据收窄实现，不视作新增产品取舍。
- 依据：[M0 兼容性报告](../compatibility/2026-10-03-m0.md)、[M1 协议](0003-m1-relay-mock-web.md)。

## 输入、输出与不可变约束

- Agent 输入：Relay 转发的 `thread.list/read/subscribe/unsubscribe`、`turn.start/interrupt`、`interaction.respond`；配置的 Desktop 内置 Codex binary、IPC socket、设备 ID 与 token。
- 输出：规范化历史、snapshot/update、真实 turn ID、准确的拒绝/未知结果。Relay 不读取 Codex home，也不储存正文。
- `thread.read` 和列表来自只读 App Server；发送、停止与交互必须交给 Desktop 真正 owner，不由独立 App Server 新建 executor。
- `thread.subscribe` 对未加载的原会话自动执行受控 deep link，然后重新发现 owner；同一 threadId 和 cwd 必须与只读历史一致。没有 owner 不能标为可发送。
- deep link 仅允许 UUID threadId，使用系统 `open` 的固定参数，不拼 shell；自动加载限时 15 秒，不自动启动 Desktop。失败明确告知，不能悄悄创建新 thread。
- 每个 thread 的 start 入口串行化；interrupt/respond 可在慢 start 期间并发进入，但各自重新核对 owner 快照和 exact turn/request。start 前重新获取快照，必须 idle、无 pending、cwd 相同。busy 返回 TURN_BUSY，不传给原生 owner，不排队。使用 clientMessageId 作为原生 message ID；同 ID 重放在本地有界去重。外部 Desktop 同时发起的竞态需要原生 message 身份验证，不能仅凭 `turn.start` 回执宣称成功。
- 精确停止再次核对当前 turn ID；native 回执必须指向 expectedTurnId。
- 订阅事件从 owner 当前快照与 patch reducer 派生，缺口停止写入并重新连接、重取快照。多个 Web 独立订阅；Agent 重连后废弃旧 stream/subscription。
- 正文归一化只处理已验证的 text item；未知非文本 item 要以可见占位/错误说明，不能吞成空白。历史太大时显式报容量错误。
- 已知的内部 `reasoning` 占位不作为历史消息重复展示；已知 command/fileChange 展示命令或路径与状态摘要，补充回答展示“已回答”摘要，不把工具活动伪装成 Codex 普通回复。未知 item 仍保留可见占位。
- 当前 Desktop 私有协议仅支持已测版本；版本或结构漂移时 fail closed，不把推测当兼容。
- 订阅真实 owner 后，手机会话头部显示**当前 Desktop owner** 的 sandbox 与审批模式；`thread.list/read` 无历史权限字段时显示“权限未知”，不能把当前值写成“沿用原权限”。识别 `dangerFullAccess` 时醒目说明它允许广泛本机操作；本项仅增加只读可见性，是否限制 Full Access 下的远程发送等待用户选择。

## 自动加载与焦点

手机选旧会话即开始自动加载。系统 `open codex://threads/<id>` 在 M0 隔离 fixture 上已证明可加载同一原会话，但可能改变 Desktop 前台焦点；首版先保证无需电脑操作与原执行者一致，焦点影响作为可见兼容性限制记录和回归，不以手工打开替代。

## TDD / 验收

1. 先写历史与 live 状态规范化的正反例测试（canonical、legacy、缺失 item/ID、超大状态），再实现。
2. 先写 auto-load 的假 `open` 与 owner 重试测试，确保只允许 UUID、原 cwd、原 threadId 和超时 fail closed，再实现。
3. 先写双 Web start 竞争、busy、重复 ID、stale stop、owner 变更测试，再实现。
4. 对隔离 fixture 做 Desktop 真机链路：未加载历史由 Web 点选自动载入，真实发送、逐步更新、精确停止、独立持久化核对；不向业务会话发送测试文本。
5. 隔离 fixture 复验 `clientUserMessageId` 与原 owner 的 userMessage item 身份关联；如果原生回执无法明确关联本次消息，生产端应返回 `unknown` 而非 accepted。
6. 只在显式测试开关和已验证的无工具 fixture 上，让 Agent Service 与独立 IPC follower 同时尝试向同一原 owner 发消息；两条消息使用不同 clientMessageId。不能把第二客户端的 `ok` 或返回 turnId 直接算作持久化。记录每侧 accepted/busy/unknown、原 owner 中各消息的身份及终态；仅对该 fixture 中实际运行的 exact turn 做清理。该实验不代表人工 Desktop UI 点击被验证。
7. 在隔离 fixture 上核对创建时的 sandbox/approval 设置、自动加载后的 Desktop owner 当前设置，以及公共 `thread/read` 是否能提供足以判断权限变化的字段。若当前权限与创建时不一致，不得声称“原权限保持不变”；新增远程权限操作前先确定用户可见的处理语义。
8. 先写 `latestThreadSettings` 已知、未知与 Full Access 的归一化及 Web 权限提示测试，再把当前权限作为只读 thread 字段放到 Web 会话头部。不得把 `approvalPolicy=on-request` 单独解释成有 sandbox 限制。
9. 原 owner 在启动 turn 时若短暂发出严格限定的 canonical 占位条目（仅空 `turnId`、`inProgress`、空 items，runtime active 且无 pending request），已订阅 Web 暂不发布该不可寻址快照，等待最多 8 秒后续原生更新；期间所有新变更仍 fail closed，绝不重发原消息。若占位消失，继续原订阅；超时或出现其他结构异常，终止订阅并报 `NATIVE_STATE_UNCERTAIN`。follower 对本次提交的核验同样允许占位短暂存在，但不能仅凭回执认定成功。首次订阅仍不能以占位构造假快照。

M2 不是 MVP 完成；审批、补充回答和故障恢复进入后续规格/测试。
