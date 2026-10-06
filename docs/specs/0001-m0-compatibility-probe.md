# 0001：M0 兼容性探针

- 状态：Approved（第 10 节第 5 项保留 TODO）
- 创建日期：2026-10-03
- 批准记录：2026-10-03 用户确认开始推进，第 10 节第 1–4 项通过，第 5 项保留 TODO。
- 适用平台：macOS 26.5，ChatGPT Desktop 26.930.31730
- 当前 Desktop 内置 Codex CLI：`0.160.0`
- 当前 PATH Codex CLI：`0.145.0-alpha.27`

## 1. 目标

在编写 Relay、Desktop Agent 或 Web 之前，用可重复、默认只读的探针确认当前 Mac 上 Codex 的真实能力，并生成脱敏 fixtures 与支持矩阵。M0 只回答“当前版本能安全做到什么”，不实现最终产品链路。

## 2. 范围

### 2.1 必须验证

| 能力 | 成功判定 |
| --- | --- |
| 环境检测 | 报告 macOS、Desktop、内置 CLI、PATH CLI、IPC socket；不输出凭据 |
| 历史列表与读取 | 通过与 Desktop 匹配的 App Server 列出、分页并读取已有 fixture，会话 ID 不变 |
| owner 发现与订阅 | 对 Desktop 已打开 fixture 找到 owner，收到完整快照和连续更新 |
| 未加载会话 | TODO（必须补齐）：手机点选后自动加载原会话并取得 Desktop owner，无需电脑端人工操作；M0 暂缓实现，只报告 owner 实际状态，不创建平行 executor |
| 发送消息 | 仅在显式写开关下，对隔离 fixture 发送短消息并取得真实 turn ID |
| 精确停止 | 只按 `expectedTurnId` 停止指定 turn，旧 turn ID 不影响新 turn |
| 审批 | 验证无害命令、文件变更与权限请求的发现、允许、拒绝、过期和竞争处理 |
| 补充回答 | 验证选项、自由输入、多问题及 resolved/expired 生命周期 |
| 重连与重启 | IPC 重连后重建当前快照；旧 owner、revision 与 request 不能复用 |
| 协议异常 | 拆包、粘包、超长帧、revision 缺口和未知请求均明确失败 |

### 2.2 不在 M0 中实现

- Relay、自有 WebSocket 协议和 Web UI。
- 公网访问、TLS、账号系统、Push 通知。
- 对业务会话执行发送、停止、审批或回答。
- 修改 ChatGPT.app、Codex 配置、登录凭据或 Desktop 启动参数。
- 为通过测试而自动批准请求或扩大 sandbox 权限。

## 3. 技术边界

### 3.1 Binary 选择

探针支持 `--codex-binary`。用户未指定时使用 Desktop bundle 内置 binary；候选规则为：

1. 用户显式指定路径时，校验它与 Desktop 内置版本是否匹配；
2. 未指定时使用 ChatGPT Desktop bundle 内置 binary；
3. PATH binary 仅作为诊断对象，不在版本不一致时静默替代 Desktop binary。

实际选择和版本必须写入结果。找不到匹配 binary 时报告环境错误，历史能力保持未验证；不能将环境检查失败推断为接口不支持，也不能猜测兼容。

### 3.2 两种数据来源

- `AppServerProbe`：只负责公开 App Server 的 capability、thread list/read、fixture 创建与持久化结果核对。
- `DesktopIPCProbe`：只负责当前 Desktop owner 的发现、订阅、发送、停止、审批与回答。

两者的结果分开记录。独立 App Server 的 `notLoaded` 不能转换为 Desktop 的 idle；从 App Server 读取到历史也不代表已经取得 Desktop owner。

### 3.3 私有协议隔离

Desktop IPC 的 framing、method/version、原始 Schema 和 patch reducer 放在独立包中。M0 允许记录版本专用 fixture，但公共 probe result 不泄漏私有方法名之外的敏感内容。将来 Relay 与 Web 只能消费规范化能力与状态。

## 4. 命令接口

计划提供单一 probe 程序，具体命令名在实现阶段确定；行为接口如下：

```text
probe detect [--codex-binary PATH] [--ipc-socket PATH]
probe fixture create --workspace PATH --write-enabled
probe history verify --fixture FILE
probe owner verify --fixture FILE
probe interaction verify --fixture FILE --case CASE --write-enabled
probe report --results DIR
```

当前已实现命令与 flags 见 [探针操作说明](../compatibility/probe-usage.md)。`fixture create` 仅在显式写开关下创建隔离目录、保存 manifest，再执行一个无工具短回复；fixture 创建是 manifest 要求的引导步骤，后续写操作都必须验证已保存的身份。manifest 另含 `guardId`，对应隔离目录内 `.ariel-fixture` 标记，防止误选普通目录。

### 4.1 默认行为

- 未传 `--write-enabled` 时，任何会创建 turn、停止 turn、响应 interaction 或改文件的操作都必须拒绝。
- 所有写操作要求 fixture manifest；目标 thread ID 与 manifest 不一致时拒绝。
- fixture workspace 必须位于进程创建的临时目录或用户显式指定的隔离目录。
- 任一步骤失败后不得自动换一个真实会话继续。

## 5. 输入与输出

### 5.1 Fixture manifest

最少包含：

```json
{
  "schemaVersion": 1,
  "threadId": "<fixture thread id>",
  "workspace": "<isolated absolute path>",
  "createdAt": "<RFC3339>",
  "createdByProbe": true
}
```

manifest 不包含 token、账号信息、完整聊天正文或 owner ID。

### 5.2 Probe result

每项能力输出：

```json
{
  "capability": "approval.command",
  "status": "supported|unsupported|unverified|failed",
  "environment": {
    "desktopVersion": "26.930.31730",
    "bundledCodexVersion": "0.160.0"
  },
  "evidenceFixture": "fixtures/approval-command.redacted.json",
  "reason": null
}
```

支持矩阵必须区分：

- `unsupported`：已得到明确不支持证据；
- `unverified`：尚未完成或无法安全执行验证；
- `failed`：探针、协议或环境异常，不能据此推断能力不支持。

## 6. 数据脱敏

- 保存前移除 token、账号、真实用户名、业务目录、真实 thread/turn/request/owner ID 与聊天正文。
- ID 保留同一 fixture 内的稳定替代关系，例如 `thread_fixture_1`。
- 命令审批只使用无害命令；文件审批只操作 fixture workspace。
- 原始未脱敏帧仅可存在于进程内存，不写仓库或日志。

## 7. 错误处理

| 场景 | 必须行为 |
| --- | --- |
| Desktop 未运行 | `codexReady=false`，不自动启动 Desktop |
| socket 不存在或无权限 | 明确环境错误，不扫描任意用户目录寻找替代 socket |
| App Server 与 Desktop 版本不一致 | 报告 mismatch，不静默使用 PATH binary |
| 找不到 owner | 当前阶段报告未取得 owner，禁止控制；自动加载是待实现能力，手动打开不算最终方案，不调用独立 executor 接管 |
| 响应超时且请求可能已发送 | outcome=`unknown`，不自动重试 |
| revision 缺口 | 当前 stream 失效，重新获取快照；旧流不得继续产出成功结果 |
| 未知 interaction | 记录类型并返回 unsupported，不自动批准或构造响应 |
| fixture 身份不匹配 | 拒绝写操作 |

## 8. TDD 顺序

实现获批后严格按以下顺序推进：

1. Red：framing 拆包、粘包、长度限制和畸形 JSON 测试。
2. Green：最小 framing decoder/encoder。
3. Red：request correlation、timeout outcome 和断线清理测试。
4. Green：最小 IPC client，不接业务方法。
5. Red：revision reducer、缺口、未知 patch 与重订阅测试。
6. Green：最小 snapshot/patch reducer。
7. Red：只读 detect/history/owner contract tests。
8. Green：只读探针，并在当前 Mac 上复验。
9. Red：fixture guard 与显式写开关测试。
10. Green：隔离 fixture 创建、发送与精确停止。
11. 逐类为 approval 和 userInput 先增加失败测试，再实现真实版本映射。
12. Refactor：抽出版本化 Adapter；生成脱敏 fixtures 与最终支持矩阵。

当前实施和 red/green 记录见 [TDD 记录](../testing/2026-10-03-m0-tdd.md)，实测能力见 [兼容性报告](../compatibility/2026-10-03-m0.md)。规格仍为 Approved，M0 尚未完成全部验收。

### 8.1 当前实现的容量与版本约束

- IPC 单帧最大 8 MiB，pending request 最大 32 个。
- 事件缓冲最多 64 帧且总计不超过 16 MiB；先过滤其他 thread 的状态广播，消费后释放容量。
- 缓冲溢出、错误 source owner、未知 stream version、revision 缺口或格式错误使观察失败，不丢事件后继续报告成功。
- 私有控制的原始实测基线是 Desktop `26.930.31730` 和内置 Codex `0.160.0`。启动门禁已由 [0025](0025-minimum-version-compatibility.md) 改为最低版本判断：达到或高于该基线即可运行，高版本实际不兼容时由当前操作显式失败。
- 当前 probe 遇到状态缺口会 fail closed 并退出；自动重订阅尚未实现，后续 Agent 负责恢复。

### 8.2 已验证的交互回执限制

2026-10-03 的隔离 fixture 实测：向不存在的 `requestId` 发送 `thread-follower-submit-user-input`，owner 仍返回 `ok: true`。因此该回执不能证明回答已受理。未来响应映射必须先核对 live pending request，再结合处理后的状态/回显确认；证据不足仍按原协议报告 `unknown`。两端竞争时不得只凭请求消失推断“由本次响应处理”。本条细化原有 unknown 语义，不将补充回答能力标为通过。

每一个真实 Desktop 测试都必须有对应的确定性单元测试或 fixture contract test；真实测试不能替代 red 阶段。

### 8.3 补充回答探针细化（已批准 M0 范围内）

- 新建 `purpose=user-input` 的专用 fixture，仅允许用于提问的 `request_user_input`，禁止其他工具、文件读取、网络和配置变更；sandbox 保持 read-only，approvalPolicy 保持 never。普通 fixture 的无工具限制不变。
- 探针使用 fixture 当前模型，在该 fixture 的 turn 中指定 Plan mode，不更改全局或业务会话设置。
- 原生 request ID 保留 string/number 类型；只处理匹配 thread、turn、workspace 的 `item/tool/requestUserInput`。缺失、未知、重复问题 ID 和 secret 问题均拒绝。
- 本次测试包含两个问题：第一个回答预设选项，第二个回答预设之外的自由文本。当前模型提问工具要求每题提供选项，因此两题均带选项；不依赖 Schema 中可空的 options。回答以问题 ID 关联；选项用实际 label，不虚构 option ID。
- 写前重新获取 live snapshot，要求 request 与最初捕获值一致；消失或改变则不发送。IPC `ok` 只记录为 receipt；只有相同 request ID、turn ID 的 completed userInputResponse 回显与预期答案完全一致，且 pending 消失，才确认状态已反映该答案。
- 两个客户端竞争时，即使答案相同，也不能证明是哪一个调用造成结果；此探针不承诺请求级 exactly-once。不同答案的竞争和中断过期另行实测。
- 超时、未知请求或协议错误不自动重试回答；只对本次已知 fixture turn 做精确停止清理。报告不输出真实 ID、问题正文或会话内容。

## 9. 验收标准

命令拒绝验证最初使用 `purpose=command-decline` fixture，只能请求 `/usr/bin/true` 的一次性审批；实际请求没有 `decline`。经用户确认的后续 `cancel` 语义、独立验证和公开历史缺少嵌套 command item 的证据限制，见 [0002](0002-approval-denial-semantics.md)。允许路径和文件/权限审批仍未验证。

- 默认运行不产生 Codex turn、不停止任务、不批准请求、不改业务文件。
- 所有写测试只能命中 manifest 指定的隔离 fixture。
- 当前 Desktop 版本的能力矩阵可复现，并包含证据 fixture 或明确 reason。
- App Server 与 Desktop IPC 的证据和结论分开。
- 未验证的审批、回答与自动加载能力不会被标记为 supported。
- probe 结束后无残留连接；fixture 可归档但不删除业务数据。
- 仓库中不存在凭据、真实业务聊天正文和未脱敏 IPC dump。

## 10. 已确认选择与暂缓项

以下选择不改变产品范围，但会影响后续项目结构：

1. Relay、Desktop Agent 与 probe 使用同一个 Go module：`github.com/wu8685/Ariel`。
2. Web 使用 React + TypeScript + Vite，首版不引入 SSR。
3. 自有协议以 JSON Schema 为唯一机器可读定义，Go/TypeScript 类型由它生成或校验。
4. 首版 interaction 只承诺已实测的常用 approval 与 `requestUserInput`；未知 MCP elicitation 显示回电脑处理。
5. **TODO（用户明确必须补齐）**：手机点开历史会话时，自动让 Codex Desktop 加载原会话、取得 owner 并允许续聊，保持 threadId、cwd 和原执行环境；用户无需回电脑操作。安全自动加载能力暂缓实现，`needsDesktopOpen` 仅描述当前阶段限制，不能替代最终功能。不得通过独立 App Server 创建平行 executor。

2026-10-03 用户确认第 1–4 项，并进一步澄清第 5 项的目标是自动加载历史会话、无需电脑端人工操作。该能力暂缓实现但不取消；在获得 owner 之前始终禁止发送、停止及回答。其余 M0 工作按 TDD 推进。
