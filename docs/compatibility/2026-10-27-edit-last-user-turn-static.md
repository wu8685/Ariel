# 2026-10-27：编辑最后一条用户消息的静态兼容性核对

## 结论与证据等级

- 当前本机 Desktop bundled CLI：`codex-cli 0.160.0`，由实际应用 bundle 中的 CLI `--version` 核对。
- 公开 Schema 与 Desktop 静态代码存在对应能力。2026-10-27 后续隔离写门禁已通过：仅向测试自行创建、授权并最终归档的 no-tools fixture 发送、停止和编辑；未改写真实工作会话。
- 官方文档与本机生成的非 experimental Schema 均包含 `thread/revert`。不能把 Schema 存在或静态方法名当成可安全接管 Desktop owner 的证据。

## 公开接口

- `thread/revert` 输入为 `threadId`、`beforeTurnId`；目标 turn 及其后续历史被移除，不撤销本地文件修改。
- response 包含 thread metadata 和 retained-history cursor；`thread.turns` 为空不代表会话没有保留历史。
- 当前官方文档说明 `thread/rollback` 已移除；本机生成 Schema 也未找到其输入结构。不要由旧 Desktop 兼容分支推断新版本仍可调用它。
- 官方依据：[Codex App Server — Revert thread history](https://learn.chatgpt.com/docs/app-server#revert-thread-history)。本机 Schema 通过 CLI 生成到系统临时目录，只核对字段，不读取用户会话历史。

## Desktop 静态入口

仅对应用安装包内静态 JavaScript 做了方法表和调用实现核对：

- 存在 `thread-follower-edit-last-user-turn`，静态方法表版本为 2；实际版本协商仍须经隔离 fixture 验证。
- handler 调用 owner 的 `editLastUserTurn`，最终只返回 `ok: true`，不直接返回新 turn ID。**该 receipt 不足以证明完成替换。**
- follower 发送的字段包含 `conversationId`、`turnId`、`message` 及若干模式／上下文字段。Ariel 不得因此开放任意透传，更不能借编辑升级权限。
- 实现检查目标为最近一次用户输入、后续不存在有输入的 turn、涉及 turn 均非 `inProgress`；随后选择 `thread/revert` 或旧版兼容路径，再通过 `edit_user_message` trigger 启动新 turn。
- 当前静态实现替换原输入中的首个 text item，并沿用其余 input；多文本段、图片、文件引用、权限和模型继承的真实行为仍需分别验证。

## 必须通过的运行态门禁

1. 在隔离 fixture 中发送、精确停止，再编辑并发送；核对 Desktop、持久历史及 follower 状态三者一致，旧 turn 不再作为当前历史输入，新文本只出现一次，前序 turn 不变。
2. 仅点击 Web 铅笔或取消编辑不得发送任何 native mutation。
3. 验证旧 turn、非最后 turn、仍运行、缺少 owner、跨 epoch 与并发 Desktop 续聊时的拒绝边界。
4. 模拟 native edit 请求前断线、回退后发送失败、回执丢失；不得凭 `ok` 认定成功，不得自动重放。
5. 确认可在有界 owner／持久读取范围内把新 turn 与此次编辑可靠关联；文本恰好相同或其他客户端竞争时，证据不足必须返回 unknown。
6. 版本化 Adapter 之外不出现私有 IPC 方法名；不通过独立 App Server 绕过 live owner，不直接写 SQLite／JSONL。

## 运行态结果（2026-10-27）

- `ARIEL_TEST_EDIT_WRITE=1 go test ./internal/desktopagent -run '^TestRealEditStoppedLatestUserMessage$' -v -count=1 -timeout=4m` 通过；后续加强模型／权限与 durable input 检查后再次通过（18.22s）。
- owner follower 验证：新 turn ID、`edit_user_message` 来源、新用户原文及非空 client ID，同时保留前序 turn；原生编辑生成**新的** client message ID，不能用旧 ID 相等来判定成功。
- 独立有界 App Server 只读查询验证：旧 turn 不在持久历史，新原文及完成结果存在，seed turn 保留。再次使用旧目标被拒绝。
- 编辑前后模型／强度／速度、approvalPolicy／sandboxPolicy 均未改变；请求仅携带 conversation、目标 turn、新文字和 `shouldSendPermissionOverrides=false`。
- UT 覆盖外部续聊导致目标过期、内容摘要错误、busy／interaction／附件拒绝、receipt 不构成成功证据、unknown 去重；Web fixture 覆盖多 watcher 替换与草稿、外部续聊及未知结果不重放。
- 仅启用本记录的精确 Desktop／bundled CLI 组合；其他版本、图片、文件引用、多段输入仍为 unsupported。原生 revert＋start 不承诺事务，回执丢失或无法证明完成时返回 unknown，不自动重发。
- Chromium viewport／touch 不等于物理 iPhone／Safari；物理设备体验留待用户复验。未用故障注入在原生进程中制造 revert 后 start 失败，相关异常语义由 Adapter／Service 的 fail-closed UT 验证，不声称完成原生故障实证。
