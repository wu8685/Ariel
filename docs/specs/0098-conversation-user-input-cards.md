# 0098：会话内用户输入卡片

- 状态：Approved（2026-10-20，用户已确认）
- 依据：[0005 原会话交互响应](0005-m3-interactions.md)、[0076 新会话首条消息 App Server bootstrap](0076-local-draft-first-turn-bootstrap.md)
- 影响范围：Desktop Agent 的 `requestUserInput` 规范化、公共 interaction schema、Web 会话交互卡片和真实浏览器回归
- 不影响范围：普通消息输入框、审批语义、任务看板／chilling task、Codex 原生 request 的权威归属

## 1. 现状与目标

Ariel 已存在 `item/tool/requestUserInput → pendingInteractions → interaction.respond` 链路，并验证过选项和自由文本回答；本规格不新建第二套提问协议。

当前 Web 只把每题呈现为带 `datalist` 的普通输入框，未保留 Codex 提供的短标题和选项说明，也没有明确区分建议选项与自定义回答。目标是将已有能力补成适合桌面和手机直接操作的输入卡片，同时继续由当前 Codex owner 判定请求是否有效、回答是否被接受。

## 2. 输入与公共模型

Desktop Agent 只接受当前 owner snapshot 中明确存在的 `item/tool/requestUserInput`：

- request 必须绑定 exact thread、turn、request ID，且 turn 仍为 `inProgress`。
- 支持一至三题；每题必须有稳定 `id` 和非空 `question`。
- 可选保留原生 `header`。
- 可选建议项保留 `label` 与 `description`；不把 description 拼进最终答案。
- 没有建议项时仍支持自由文本。
- `isSecret=true` 沿用现有 fail-closed 行为：Web 不收集答案，只提示回 Desktop 处理。
- 未知字段或不支持的交互类型不能被猜测映射为可回答卡片。

公共 schema 在保持旧字段可读的前提下扩展 question：

```text
id: string
header?: string
question: string
options?: [{ label: string, description?: string }]
```

Web 必须兼容旧 Agent 仍发送 `options: string[]` 的过渡形态；新 Agent 统一发送结构化选项。

## 3. Web 交互

1. 待回答卡片显示在当前会话正文之后，并保持现有 `NEEDS YOUR INPUT` 层级。
2. 每题显示短标题、完整问题和原生顺序。
3. 有建议项时，用适合触摸的单选按钮展示 label 与 description；不依赖原生 `datalist`。
4. 每题始终允许选择“其他”，选择后展开自由文本输入。无建议项时直接显示自由文本输入。
5. 多题必须全部有非空答案后才能提交；空白回答不提交。
6. 一次点击只发送一次包含全部题目的 `interaction.respond(decision=answer)`；提交期间整张卡片禁用并显示处理中状态。
7. 已选答案仅存在当前页面内存，不写 durable history cache、localStorage 或 Relay 日志。
8. owner exact echo 确认后卡片消失，turn 继续；另一 Web 客户端通过 live update 同步消失。
9. 切换会话后返回，如果 owner 仍有同一个 pending request，卡片重新出现；当前页面内同一 interaction 的未提交答案可以保留，刷新页面不承诺保留。
10. 普通 composer、queue 和审批卡片沿用既有门禁，不因本规格引入新的自动发送或自动选择。

## 4. 错误与竞态

- 请求已在 Desktop 或另一 Web 处理：返回 stale/rejected，移除或重新同步旧卡片，不重放回答。
- 提交后断线、超时或只有 transport receipt：显示“结果不确定”，立即核对 owner；不自动再次发送。
- owner snapshot、thread/cwd/turn/request identity 或问题内容发生变化：旧 interaction ID 失效，旧答案不能套用到新卡片。
- pending request 消失但没有 exact answer echo：不能宣称回答成功。
- Agent／Relay 重连：重新从 owner 当前 snapshot 建立卡片，不从 Web 缓存恢复权威 pending 状态。

## 5. TDD 顺序

### Red

1. Go normalizer 测试证明当前会丢失 header 和 option description。
2. protocol 测试证明当前 schema 拒绝结构化 options。
3. Web 测试证明当前只能使用 `datalist`，没有触摸友好的建议项与“其他”输入。
4. 浏览器 fixture 覆盖两题、建议项、自定义回答、提交中禁用、一次请求和切换会话恢复。

### Green

扩展最小公共模型和 normalizer，复用既有 `interaction.respond` 与 exact echo 确认链路，实现输入卡片。

### Refactor

将卡片状态与答案映射从 `App.tsx` 中收敛到 interaction 模块；不改审批、queue 或普通消息发送语义。

## 6. 验收标准

1. Go／protocol／Web 单测全部通过，production build 通过。
2. 390×844 touch viewport 和桌面真实 Chromium 均完成行为与截图回归。
3. 浏览器 fixture 证明多题只产生一次 `interaction.respond`，答案 ID 与原生问题完全匹配。
4. 隔离 Codex fixture 在当前安装版本上真实触发一次 `request_user_input`：Web 提交一个建议项和一个自由文本，owner exact echo 匹配，原 thread 继续并完成；不向业务会话发送内容。
5. Agent／Relay 重连和第二浏览器同步至少完成自动 fixture 回归；不得把仅 transport `ok` 写成成功。
6. 物理 iPhone／Safari 未在本机完成时明确留给用户复验。
