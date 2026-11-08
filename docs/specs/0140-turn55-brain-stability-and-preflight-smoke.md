# 0140：Turn 55 Brain 稳定性与预发冒烟

- 状态：Approved（2026-11-09；用户在 Turn 55 明确要求诊断并修复生产冒烟失败、沉淀可配置预发冒烟工具；本稿只细化该已授权范围）
- 日期：2026-11-09
- 关联：0130（Puck Brain）、0136（结构化卡契约）、0138（授权规则）、0139（引用回复）

## 1. 问题与证据

Turn 54 在 `ba54502d` 上的生产冒烟暴露出两个独立问题：

1. 新建 Bot 的 greeting 首轮长时间停留在 `greeting_processing`。失败 DB 与本机 Codex rollout
   元数据显示，三个临时 Bot 的 greeting 分别约 123 s、116 s、120 s；其中上下文初始化约
   16–19 s，主要时间消耗在无需复杂推理的 greeting 首轮模型 reasoning。
2. 结构化提问没有生成 question card。Brain 实际调用了 `ask_user`，但受用户措辞中的
   `single-select` 影响，多生成 `questions[0].type="single-select"`；0136 的严格 decoder
   拒绝该字段，Brain 随后改用普通文本并结束。

Relay 容器在故障窗口没有记录 Puck／Brain 阶段耗时，Desktop Agent 又丢弃 Brain app-server
stderr，因此仅凭服务日志无法区分初始化、模型推理、tool schema 与写回阶段。

## 2. 目标

1. greeting 明确使用低 reasoning effort；普通 Brain turn 继续使用 Bot 的既有配置。
2. Desktop Agent 为 Brain ensure、turn start、tool call／result 记录脱敏的阶段、耗时、结果与
   错误类别，不记录 prompt、tool arguments、完整 thread／assistant id 或凭据。
3. `ask_user` 兼容已经实证的 `type=single-select|multi-select` 模型漂移：分别归一化为
   `multiSelect=false|true`；若与显式 `multiSelect` 冲突则拒绝。其他未知字段仍按 0136 拒绝。
4. 将仓库内 opt-in 的 `cmd/relay/puck_brain_e2e_test.go` 扩为可复用预发门禁；它自行启动隔离
   本地 Relay 与 Agent，串行执行五项场景，一次只存在一个临时 Bot，并在每项后删除。
5. 冒烟输出只含场景结果、耗时、计数和脱敏错误，不输出凭据、完整原生 thread id 或用户提示。
6. Brain 启动前读取当前 Codex binary 的 feature inventory，只为该版本仍支持的本地 surface 传
   `--disable`；已移除的 feature 不得继续作为启动参数，避免 Codex 升级后 app-server 启动即退出。

## 3. 输入与输出

### 3.1 Brain greeting

- `puck.create` 仍同步 ensure Brain thread，并返回既有 wire shape。
- greeting `puck.brain.turn.start` 显式传 `effort="low"`。
- greeting processing、完成与 fallback 语义保持不变，不新增协议字段。

### 3.2 `ask_user` 兼容归一化

- canonical schema 不新增字段，toolset version 不变。
- decoder 在严格未知字段检查前，只消费 question object 上的可选兼容字段 `type`：
  - `single-select` → `multiSelect=false`；
  - `multi-select` → `multiSelect=true`；
  - 其他值、非字符串、与显式 `multiSelect` 冲突 → `INVALID_ARGUMENT.details[]`。
- 归一化后的持久卡片仍只包含 0136 canonical 字段，不回显 `type`。

### 3.3 预发冒烟工具

必填输入：Codex binary；可选输入为本机隔离 fixture 使用的 IPC socket。工具自行创建临时 Relay
URL、临时 PIN、目标 Desktop Agent id、Puck DB 与 fixture，生产域名、device id、session key 路径
不得硬编码，也不接受生产凭据。

五项串行场景：

1. Full Access：创建任务且不产生授权卡；
2. 每次询问：批准“以后都允许”、验证自动允许、撤销后再次询问；
3. `ask_user`：生成 canonical 结构化 question card；
4. 删除 Bot：删除后 `puck.list` 不再出现；
5. 引用回复：分别引用 assistant 文本与 task card，后者续投既有 task 且不新建 task。

每项记录总耗时；创建阶段另记录 ensure/create、greeting、首个 tool/card 延迟。任一场景失败时整体
退出非零，但继续清理当前测试 Bot。工具默认拒绝在未显式传入目标、认证或 fixture 时运行。

## 4. 不变量与边界

1. 不改变 Puck toolset version、Relay wire protocol、DB schema、授权 owner 或 Codex SSOT。
2. 不修改真实用户会话；执行任务只允许落在调用方提供的隔离 fixture。
3. 不连接生产作为本轮最终验证；本地 Relay、独立 Agent 配置和临时 Puck DB 与常驻实例隔离。
4. 不在日志、JSON summary、测试 fixture、文档或 commit 中持久化 token、session key、完整 thread id
   或用户原始提示。
5. Bot 容量不足时 fail fast，不删除非本次前缀创建的 Bot。
6. 无法取得 Codex feature inventory 时 Brain fail closed，不能为了启动成功而静默开放 shell、browser
   或 multi-agent 等本地 surface。

## 5. 错误处理

- Brain 阶段日志将错误归类为稳定 code；原始 app-server 错误只在本地 debug sink 可用时保留，默认
  不转发到 Relay。
- `ask_user` 兼容归一化失败返回现有结构化 `INVALID_ARGUMENT`，不生成裸 JSON 消息。
- 冒烟超时必须指出阶段；清理失败单独报告，不能覆盖主失败。

## 6. TDD 与验收

1. Red：复现 `questions[].type=single-select` 被拒；复现 greeting 未传低 effort；复现 Brain 阶段
   缺少可观测耗时；为 smoke 配置校验、顺序、单 Bot 生命周期与脱敏 summary 写失败测试。
2. Green：最小实现通过上述测试；再重构共享的 websocket client 与场景 runner。
3. 既有 0136 strict-schema、0138 授权、0139 引用回复测试必须保持通过。
4. 完整门禁：`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`、Web unit、
   Playwright UI、production build、`git diff --check`。
5. 门禁后用隔离本地 Relay + Desktop Agent + 真实 Brain 跑五项预发冒烟；全部通过才可标记
   “可部署候选”。本轮无论结果如何均不实际部署。
