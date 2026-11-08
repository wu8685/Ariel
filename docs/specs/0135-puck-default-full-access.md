# 0135 Puck 自建 Bot 默认 Full Access

- **Status**: Implemented（2026-11-08；未部署）
- **Supersedes**: 0130 §4.4 中执行 thread 强制 `on-request + workspace-write`、B1/B2/B5/B6/B10 必须生成人工授权卡的规定

## 1. 目标

唯一 owner 在 Passkey 会话中创建的 Puck Bot，以及该 Bot 派发或续投的 Codex 执行 thread，默认以 `full` 权限模式运行，不再因 Puck mutation policy 或 Codex native approval 反复显示 `request_authorization` 卡片。

每个 Bot 持久保存 `permissionMode`：

- `full`（默认）：Codex 使用 `approvalPolicy=never` 与 `danger-full-access`；Puck mutation 自动批准。
- `ask`：保留 0130 的 `approvalPolicy=on-request`、`workspace-write` 与 Puck 单次授权卡行为。

创建请求省略 `permissionMode` 时写入 `full`。升级前的 Bot 若持久配置没有该字段，也按 `full` 解释。用户可通过 `puck.update` 和 Web 助理设置在两种模式间切换。

## 2. 输入与输出

### 2.1 Web API

- `puck.create.permissionMode?: "full" | "ask"`
- `puck.update.permissionMode?: "full" | "ask"`
- `puck.list`、`puck.create`、`puck.update` 返回的 assistant 数据必须包含规范化后的 `permissionMode`。
- 其他值返回 `INVALID_ARGUMENT`，不得修改原配置。

### 2.2 Relay → Desktop Agent

Puck 内部 origin 增加 `permissionMode: "full" | "ask"`。该字段只能由已认证 Web 请求所进入的 Puck 模块根据持久 Bot 配置生成，Brain tool arguments 不能覆盖它。

Desktop Agent 必须按 origin 中的模式显式设置每次 Puck 创建/续投：

| mode | `thread/start` | `turn/start` |
|---|---|---|
| `full` | `approvalPolicy=never`; `sandbox=danger-full-access` | `approvalPolicy=never`; `sandboxPolicy.type=dangerFullAccess` |
| `ask` | `approvalPolicy=on-request`; `approvalsReviewer=user`; `sandbox=workspace-write` | `approvalPolicy=on-request`; `approvalsReviewer=user`; workspace-bound `sandboxPolicy` |

不得读取或修改 `~/.codex/config.toml`。显式 per-thread/per-turn 参数是唯一生效来源。

## 3. Puck mutation 行为

### 3.1 `full`

- `start_task`、`send_turn` 对 B1/B2/B5/B6/B10 不生成授权记录或卡片，直接执行。
- Brain 主动调用 `request_authorization` 时返回已自动批准的成功结果，不生成授权记录或卡片。
- `PUCK_SELF_TARGET` 仍是无条件拒绝，不能由 `full` 绕过。
- 目标设备离线、缺少原子 bootstrap/send/execution-policy 能力、协议不兼容、配额超限、结果不确定等仍按原错误语义失败，不能降级成无策略执行。

### 3.2 `ask`

保留 0130 的策略分类、30 分钟单次授权、冻结动作 hash、`approve_once`/`reject` 与消费语义。授权卡必须包含解析后的真实 `deviceId` 与 `cwd`；task locator 必须先解析为 task 的 device/thread/cwd，再生成卡片。

## 4. 安全不变量

1. Puck 仍只在 Passkey + HTTPS、已认证 owner Web session 下开放。
2. Bot identity 由 Relay/Module 注入；Brain 不能伪造或选择其他 assistant。
3. 可用设备仍限于当前 owner Relay 下已认证并在线的 Desktop Agent。
4. Brain thread 自指、跨 Brain recursion、method allowlist、write quota、bounded memory、single mutation owner 与现有 user-decision gate 不变。
5. `full` 不自动响应普通 Codex `user_input`；它只消除命令/文件 permission approval。业务性提问仍需用户输入。

## 5. 兼容性与上线

- 新 Relay + 旧 Agent：对 Puck mutation fail closed，返回 execution policy 不可强制；不得静默使用 Agent 全局设置。
- 新 Agent + 旧 Relay：旧 origin 没有 `permissionMode` 时按 `ask`，避免旧 Relay 意外扩大权限。
- 必须先发布支持双模式的 Desktop Agent，再发布默认 `full` 的 Relay/Web。
- 回滚 Relay 会恢复旧 Puck 授权卡；已创建 thread 的后续 turn 仍由实际发送方显式策略决定，不依赖持久全局配置。

## 6. 验收标准

1. 新建和旧 Bot 均显示 `full`；切到 `ask` 后重启 Relay 仍保持。
2. `full` Bot 的高风险 `start_task`、owned/unowned `send_turn`、`request_authorization` 均无 authorization row/card。
3. `full` 创建与续投的 wire 参数分别为 `never + danger-full-access`、`never + dangerFullAccess`。
4. `ask` 的原有授权与 workspace-bound wire 参数保持不变，卡片显示真实主机/目录。
5. 未认证 Web、伪造 assistant、离线或非 owner 设备、Brain 自指仍被拒绝。
6. Go 全量测试、Puck/Relay/Desktop Agent race、`go vet ./...`、Go build、Web 测试与生产 build 通过。
