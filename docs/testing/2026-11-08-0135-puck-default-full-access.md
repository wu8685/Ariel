# 0135 Puck 默认 Full Access 实现与验证

日期：2026-11-08

## 根因

授权卡来自两套独立门禁：

1. Relay 内 `internal/puck` 的 B1/B2/B5/B6/B10 policy。它把每个命中的 mutation 冻结成单次 authorization，默认 30 分钟，并生成卡片。
2. Desktop Agent 对 Puck 创建与续投显式覆盖 Codex 为 `approvalPolicy=on-request`、`workspace-write`。即使 Relay 的一次授权被消费，执行中的 Codex 仍可能继续提出 native approval。

`approve_once` 只把一条 authorization 从 pending 改为 approved，再消费该 frozen action；它没有改变 Bot 或 thread 的权限模式。因此 Brain 的下一次 mutation、修改后的 action 或 Codex 的下一次 native approval 都会再次生成交互。

审计还发现单次批准执行使用的内部 origin trigger 是 `user_decision`，但 v1 schema 原先漏了该 enum。它不会造成下一张卡，却可能让已经批准的 frozen action 在 outbound validation 阶段失败；本轮一并补齐该兼容契约。

“主机／目录未说明”对应 Brain 主动调用抽象 `request_authorization`：该工具原参数只有 summary/action，没有 device/cwd。`start_task` 与以 task 定位的 `send_turn` 在 policy 前会解析真实 device/cwd；它们的 ask-mode 卡片保留真实目标。Web 现在只在 payload 确实包含目标时显示主机／目录，不再把不存在的字段伪装成“未说明”。默认 full 下抽象 `request_authorization` 直接 auto-approved，不再生成卡片。

## 实现

- Bot policy 持久保存 `permissionMode=full|ask`；创建默认 full，旧记录缺字段也归一为 full。
- `puck.create/update/list` 与 Web 助理信息页公开该模式，可切回 ask。
- full 跳过 Puck 的可选 authorization 规则，但 `PUCK_SELF_TARGET` 仍在任何模式下先行拒绝。
- Relay 内部 Puck origin 携带由持久 Bot 配置注入的 permission mode；Brain arguments 不能覆盖。
- Desktop Agent 对新 thread 和每个后续 turn 显式下发：
  - full：`approvalPolicy=never`，thread sandbox 为 `danger-full-access`，turn sandbox policy 为 `dangerFullAccess`；
  - ask：原 `on-request + workspaceWrite`，且限定 cwd。
- Agent 收到旧 Relay 未带 mode 的 origin 时回退 ask；Relay 遇到不支持显式 execution policy 的旧 Agent 时仍 fail closed。
- Passkey/HTTPS、owner session、设备认证、method allowlist、write quota、recursion/self-target 和 mutation owner 均未放宽。

## TDD 证据

Red 阶段先加入以下断言并运行，分别因接口、字段或行为不存在而失败：

- app-server 与 Desktop IPC 的 full/ask wire policy；
- policy engine 在 full 下跳过可选 authorization、仍拒绝 self-target；
- `request_authorization` auto-approve；
- Bot 默认 full、持久切换 ask、高风险任务不生成卡片；
- protocol 的 permission mode 与内部 origin；
- Web 的运行权限 selector 与持久更新。

实现后全部转绿。

## 回归结果

- `go test ./...`：通过。
- `go test -race ./internal/puck/... ./internal/relay ./internal/desktopagent`：通过。
- `go test -race ./internal/desktopagent -run '^TestPuck' -count=1`：通过。
- `go vet ./...`：通过。
- `go build ./...`：通过。
- `web/npm test`：47 files，412 tests，通过。
- `web/npm run test:ui`：183 项既有真实 Chromium 回归通过。
- 新增权限 selector Chromium 用例：1 项通过。
- `web/npm run build`：TypeScript 与 Vite production build 通过；仅保留既有大 chunk warning。

未执行公网部署、Desktop Agent 重启或真实业务 Codex 会话 mutation。
