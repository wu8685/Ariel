# 浏览器与回归测试：MVP 手机路径

- 规格：[M2](../specs/0004-m2-real-desktop-agent.md)、[M3](../specs/0005-m3-interactions.md)、[M4](../specs/0006-m4-recovery-capacity.md)、[0009](../specs/0009-mobile-drawer-and-logo.md)、[0010](../specs/0010-browser-relay-session.md)。
- 范围：本机真实 Codex Desktop、仅 `probe fixture create` 生成的隔离会话，以及 390×844 桌面浏览器视口。以下浏览器验收不能替代物理手机验收。

## 已进入自动回归

| 风险点 | 回归测试 |
|---|---|
| LAN HTTP 缺少 `crypto.randomUUID` 导致“Relay 已连接、设备为空” | `web/src/ids.test.ts` 模拟该 API 不存在，验证 `getRandomValues` 生成合法 UUID；`web/src/App.test.tsx` 从 `hello.ok`、`device.list` 到可见设备和会话选择。 |
| 手机侧栏遮挡对话 | `web/src/App.test.tsx` 实际点击遮罩、侧栏内部、会话行并按 Esc；`web/src/theme.test.ts` 检查移动端层叠与灰色 logo。 |
| 刷新重复输码／凭据边界 | `internal/relay/session_test.go` 覆盖签发、复用、24 小时过期、32 个上限、Relay 重启、角色隔离和 PIN 锁定；`web/src/session.test.ts`、`client.test.ts`、`App.test.tsx` 覆盖同标签页恢复、只存随机 token、失效清除和主动断开。 |
| Agent 子 App Server 退出后假在线 | `internal/desktopagent/health_test.go` 注入子进程退出，验证当前 Relay 会话取消并进入外层重连。 |
| 待审批与补充回答 UI | `web/src/InteractionCard.test.tsx` 点击测试原 owner 提供的命令、文件、权限决策；`interaction.test.ts` 覆盖选项和自由文本、缺项禁用。Relay/Agent 既有 Go 测试覆盖回执与状态核对。 |

`go test -race ./...`、Web 33 项测试、`npm run build` 均通过。测试只在仓库的测试和隔离 fixture 上写数据，不操作业务会话。

## 真实浏览器与 Desktop fixture

- 首次 PIN 登录后出现“昊天的 Mac”和 50 条最近会话；同一标签页刷新自动连接，独立新标签页要求 PIN，主动断开后刷新亦要求 PIN。浏览器 session 内容未被调试工具读取。
- 选择未加载的隔离历史后，原 owner 自动加载，侧栏收起，能看到 seed 与先前发送/精确停止的原始 turns；Agent 自己的 App Server 子进程被定点终止后，Agent 日志显示 `Codex App Server unavailable`，新子进程启动，浏览器在线状态和历史自动恢复。未终止 Codex Desktop 或其已有 App Server。
- 隔离命令审批：真实卡片显示 `/usr/bin/true`，浏览器点“仅本次允许”后工具状态 completed、原 turn 完成，回复退出码 0。
- 隔离文件审批：真实卡片显示只在 fixture 目录创建 `fixture-note.txt`，浏览器批准后文件字节为 `61 70 70 72 6f 76 65 64 0a`，与预期一致。
- 用户补充回答、消息发送与精确停止此前已在同一 390×844 浏览器视口的隔离 fixture 中验收，记录见 [M1–M4 验证](2026-10-04-m1-m4.md)；手机实际只确认 PIN 登录与设备出现，其他路径仍需物理手机复验。
- 权限请求：本次专用 fixture 仍未出现原生 `item/permissions/requestApproval` 卡片，turn 直接结束。当前环境的 `request_permissions_tool` 未启用，和既有兼容性结论一致；只可称协议、Agent 与组件测试覆盖，不可称真实端到端通过。未修改 Desktop feature 配置。

## 仍需物理手机确认

在同一可信 LAN 上以手机验证刷新免输码、侧栏遮罩、原始历史、发送/停止、命令或文件审批及切后台恢复。桌面浏览器测试是上线前的回归屏障，不把 viewport 模拟冒充手机操作系统与浏览器的兼容性结论。
