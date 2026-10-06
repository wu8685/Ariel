# 0025：最低版本兼容门禁与运行期失败

- 状态：Implemented（用户于 2026-10-06 明确批准；自动测试、当前 Desktop 只读探针与 LAN 启动已验证）。
- 背景：Ariel 当前只允许 Desktop `26.930.31730` 与内置 Codex CLI `0.160.0` 这一组精确版本启动。用户在 2026-10-06 要求改为最低版本门禁：版本不低于基线即可启动；若更高版本实际改变了私有 IPC，则在具体功能调用时显式报错。

## 用户可见行为

1. 最低支持版本分别为 Desktop `26.930.31730`、内置 Codex CLI `0.160.0`。两者都达到或高于最低版本时，Relay／Desktop Agent 可以启动；不再要求精确命中一组白名单。
2. 版本低于最低值、缺失或无法解析时，启动失败，并同时报告检测到的版本和最低版本。版本比较按数字／语义版本进行，不按字符串字典序比较；允许可选的 `v` 前缀，预发布版本低于对应正式版本。
3. 高于基线的版本会被标记为“允许运行、尚未逐版本验证”，但不阻止启动。日志不得把它描述成已验证兼容。
4. 若新版本在实际使用中出现 IPC method、参数、返回结构、事件顺序或状态语义不兼容，当前操作显式失败并返回可定位的兼容性错误；其他尚未触发错误的功能仍可继续使用。
5. 用户显式指定 `ARIEL_CODEX_BINARY` 时，所选二进制仍必须与 Desktop bundle 内置 Codex CLI 完全一致。PATH 中的其他 Codex CLI 仍只用于诊断，不参与兼容门禁。

## 兼容性边界与失败语义

- 最低版本门禁仅代表“允许尝试运行”，不承诺所有更高版本都保持私有 IPC 兼容。
- 现有 method、schema、owner、session、turn、item 和 request identity 校验继续生效；不得为了兼容未知新版本而猜测字段、静默吞错或伪造成功。
- 只读操作不兼容时，只失败该次读取并明确报告原生 method 与原因。
- 发送消息、审批、补充回答、停止 turn 等可能改变原始会话的操作，如果原生结果无法确认，必须保持 `unknown`／失败语义；不得自动重放、自动重试或切换 executor，以免重复修改原始会话。
- Agent 断线与重连可沿用现有机制，但重连不得导致不确定操作再次执行。
- 本规格不新增按版本分叉的 Adapter，不改变 Relay／Web 协议、认证模型或局域网安全边界。

## 探针与证据

1. `detect`／`history` 继续报告实际观察到的能力，不因版本高于基线就宣称完整兼容。
2. 启动检查、owner probe 与 fixture probe 统一使用最低版本判断，避免同一机器在不同入口得到相反结论。
3. 兼容性输出区分三类证据：低于最低版本、最低版本已验证、高于最低版本但未逐版本验证。
4. 当前机器的 Desktop `26.930.51102` 与内置 Codex CLI `0.160.0` 应通过启动门禁；现有只读探针结果只能作为 detect／history 证据，不能扩张为写操作兼容结论。

## TDD 与验收

1. Red：为统一版本判断补充低于、等于、高于、混合高低、可选 `v` 前缀、预发布、空值和畸形版本测试，并覆盖容易被字符串比较误判的数字段。
2. Red：启动、Desktop Agent、owner probe 与 fixture probe 测试当前版本 `26.930.51102`／`0.160.0` 可通过，任一版本低于最低值则拒绝。
3. Red：为私有 IPC 不兼容补充错误传播测试；可能改变会话的调用在结果不确定时不得自动重试或重放。
4. Green：用一个共享的最低版本判断替代各入口的精确匹配；保留显式二进制与 bundle 版本精确一致的检查。
5. 回归：Go race tests、`go vet` 与 build 全部通过；再用当前本机版本执行只读探针并启动 Ariel，确认 Relay、Agent 和 LAN HTTP 状态。不得在业务会话上用写操作制造兼容性失败。

## 验收标准

- 当前机器的 Desktop `26.930.51102`／Codex CLI `0.160.0` 可以启动 Ariel，并显示“高于最低版本、未逐版本验证”的提示。
- Desktop `26.930.31730`／Codex CLI `0.160.0` 仍可通过；任一版本低于门槛、缺失或畸形均在启动前拒绝。
- 模拟未知 method、schema 变化或不确定写结果时，Web／日志能看到明确错误，且同一写操作只发送一次。
- 文档清楚区分“最低允许版本”和“最后验证基线”，不再将兼容性描述为精确版本白名单。

## 与既有规格的关系

本规格获批后，取代 0001 中“Desktop 与 bundle CLI 必须精确命中受支持组合”的启动门禁；0001 的探针、证据和私有 IPC fail-closed 原则继续有效。

## 实施记录（2026-10-06）

- 版本门禁已统一为最低版本比较；启动器、Desktop Agent、owner probe 与 fixture probe 不再精确匹配单一版本组合。显式 Codex binary 仍须与 Desktop bundle 一致。
- `detect` 输出最低版本和 `verified`／`unverified`／`unsupported` 证据状态。当前 Desktop `26.930.51102`／Codex `0.160.0` 返回 `unverified`，并成功完成 IPC initialize 与只读 history 探针。
- 原生 method、schema 或 IPC protocol 不兼容会映射为 `PROTOCOL_UNSUPPORTED`；已有不确定写结果单次调用测试继续通过，不增加重试或重放。
- Go race tests、`go vet`、Web 97 项测试与 production build 全部通过；当前 LAN 地址的 Relay 页面和 health endpoint 返回 HTTP 200，Desktop Agent 已与 Relay 建立连接。详见[专项测试记录](../testing/2026-10-06-0025-minimum-version-compatibility.md)。
