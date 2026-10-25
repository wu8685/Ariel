# 0095：Obsidian Task Source、稳定主机路由与 Codex 领取 POC

- 状态：Implemented（2026-10-18；隔离 M=E 真实 native POC 已验，物理跨主机待验）
- 日期：2026-10-18
- 关联：[0091 Chilling Task Board 与后台路由](0091-chilling-task-board-and-background-routing.md)

## 1. 目标

用一张无副作用任务证明以下链路：

1. `brain-spark/tasks/list/` 中的 Markdown 是任务权威数据；
2. 一台启用 Obsidian Task Source 的 Ariel Desktop Agent 定时扫描本地同步后的 vault；
3. 它通过 Ariel 现有的 `task.dispatch` 把任务路由到 `agent_host` 指定的 Desktop Agent；
4. 目标 Desktop Agent 创建本机 native Codex thread，该 thread 在 Codex App 可见；
5. Codex 先完成 claim-only turn；Ariel 将领取意图返回 source，由唯一 vault writer 将卡片 CAS 改为 `WIP`、回写该 Codex `threadId` 并追加评论。领取确认后才投递真正任务。

POC 不新建第二套 task state machine，不将 Obsidian 任务复制进 Relay 数据库，不使用 Hetairoi 或 ahsir 作为必经中介。

## 2. Obsidian 接入方式

本 POC 不把“Obsidian API”假定为已有的云端 vault CRUD 服务。当前 vault 也没有安装 Local REST API community plugin。

因此固定为一个可替换的 source adapter：

- POC：`filesystem` adapter 读写 Desktop Agent 所在主机的本地 vault；Obsidian Sync 只负责把 Markdown 同步到该主机。
- 后续：若明确采用 Local REST API 或其他受信 API，仅替换 adapter，不改任务契约和 Ariel 路由协议。

Source 运行在指定的管理 Desktop Agent，而非远程 Relay；该 source 是 vault 的唯一自动写 owner。Relay 不获取 vault 路径、不持有 Obsidian token，也不持久化任务正文。目标执行主机可以与管理主机不同，不需要挂载 vault；所有 task 写回经受限业务消息返回 source 执行。

## 3. 任务元数据

```yaml
---
name: "Example"
date_created: 2026-10-18
date_modified: 2026-10-18
tags: [task]
status: TODO
task_id:
agent_host: mac-studio-primary
agent_id:
claimed_at:
---
```

| 字段 | 契约 |
| --- | --- |
| `status` | 仅允许 `TODO` / `WIP` / `DONE`，与 Kanban 三列一一对应。 |
| `task_id` | 任务稳定唯一 ID。可留空；source 首次扫描时使用 UUID 填充。改名或移动文件不得改变它。 |
| `agent_host` | 目标 Ariel Desktop Agent 的稳定 `deviceId`，长期保存在 Ariel 配置中。禁止使用 IP、hostname、`connectionId` 或 `agentEpoch`。 |
| `agent_id` | 领取后负责此任务的 native Codex `threadId`。它既是唯一执行标识，也是 Codex App 打开对话的依据。 |
| `claimed_at` | 成功领取时间，ISO-8601，带时区。 |

`agent_id` 不用来表示 Ariel Desktop Agent；Desktop Agent 的身份是 `agent_host/deviceId`。POC 不再增加与 `agent_id` 重复的 `codex_thread_id`。

## 4. 发现与路由

Source 每次扫描只选择同时满足以下条件的文件：

1. 位于允许的 vault 根目录下 `tasks/list/**/*.md`；
2. `status == TODO`；
3. `agent_host` 非空，且是当前 Relay 中已授权的执行 `deviceId`；
4. `agent_id` 和 `claimed_at` 均为空；
5. frontmatter 可解析，正文包含 `## 任务描述`。

扫描只产生可验证的有界 command；路由复用 0091 的 manager/executor 身份、allowlist、`task.validate` 与 `task.dispatch`。Relay 不做离线队列；目标主机离线时任务保持 `TODO`，后续扫描再试。

去重键为 `task_id + task revision + operation kind`。revision 由规范化后的任务内容摘要计算，不使用 mtime 单独判定。配置层拒绝多个 source 同时持有同一 vault 的自动写权；任何重试仍以 source 写入 `WIP` 的 compare-and-swap 为唯一领取成功依据。

## 5. 两阶段领取 gate

为严格满足“Codex Agent 在处理前先改为 WIP”，启动不能把领取和真正任务放在同一个无结构 prompt 中。

### Phase A：claim-only

1. 目标 Desktop Agent 先创建 native Codex thread，取得 `threadId`；这需要将当前已证实的 native `thread/start` 独立暴露给受限 task runtime，不用已立即启动 turn 的 `BootstrapThread`；
2. 首个 turn 是 claim-only：它获得 task 身份、摘要、当前 revision 和领取契约，不获得真正任务指令；
3. Codex 只能返回结构化 claim intent，包含 action / `task_id` / revision；`agent_host` 来自 Relay 已认证路由，`threadId` 来自 native receipt，不信任模型自报。目标 Desktop Agent 不直接修改 vault；
4. source Desktop Agent 收到该 intent 后，以 compare-and-swap 检查 `TODO + 目标 deviceId + 未领取 + revision 一致`，然后原子替换文件：
   - `status: WIP`；
   - `agent_id: <threadId>`；
   - `claimed_at: <timestamp>`；
   - `date_modified: <today>`；
   - 在 `## 评论` 末尾追加领取评论。
5. 冲突、已领取或主机不匹配时，不改文件，返回 stale/busy。

### Phase B：work

Source 重新读取 task，只有同时看到 `status == WIP` 且 `agent_id == threadId` 才发出 work command；目标 Desktop Agent 再按 0091 验证新鲜授权，然后向同一 thread 发送真正任务内容。任一验证失败都不执行任务。

进展、决策和阻塞都追加到共享评论时间线。自动评论附带隐藏的稳定 comment ID，防止重试重复追加；人工评论不强制填 ID。

`DONE` 表示任务成功条件已满足。若用户仍要求所有完成都经人工确认，可在后续版本增加独立验收字段；POC 不增加第四个 Kanban 状态。

## 6. Codex App 可见性

目标 Desktop Agent 必须复用 Ariel 现有 native Codex Adapter，不得启动独立、不可见的临时 Agent runtime。验收必须基于行为证据：

1. receipt 含 exact `threadId` 和 claim turn ID；
2. Codex App 任务列表中可找到该 thread；
3. `codex://threads/<threadId>` 能打开同一对话；
4. Phase B 进入同一 thread，不新建第二个 thread。

## 7. 开关与安全边界

- 新增显式 source 开关，默认关闭；只允许一个 task backend/source 持有同一 vault 的写 owner，且 Obsidian source 与 Chilling task backend 不能同时管理同一 task collection。
- vault 根路径必须是本机显式 allowlist 的绝对路径；任务文件解析 symlink 后仍必须位于该根目录。
- 只允许写回被正确绑定的 task 文件，不提供通用文件或 Obsidian API proxy。
- POC 任务不发布、不 push、不发外部消息，不涉及凭据；不用“Ariel 维护”这张可能导致部署的卡做首个测试。
- brain-spark 仅本地，不配置 remote，不 push。

## 8. 失败语义

| 失败点 | 结果 |
| --- | --- |
| 路由前／目标离线 | 保持 `TODO`，无 `agent_id`，后续扫描重试。 |
| thread 创建结果未知 | 不盲目补建；按稳定 command ID 核对 native 状态。 |
| claim turn 失败 | 保持 `TODO`；不进入 Phase B。 |
| claim 已成功但 receipt 丢失 | 重读 frontmatter；`agent_id` 匹配时恢复同一 thread，不再领取。 |
| Phase B 失败 | 保持 `WIP`，追加 `阻塞` 评论，不自动回退 `TODO`。 |
| task 或主机绑定已变更 | 旧 command 验证失败，不开始新 turn。 |

## 9. POC 验收

1. 另一 `agent_host` 的任务不被错路由。
2. 空 `task_id` 被填充且后续扫描保持不变。
3. 未变更的 `TODO` 卡在重复扫描中只产生一个逻辑 claim。
4. 目标 Desktop Agent 创建的 exact thread 在 Codex App 可打开。
5. Phase A 只领取，先将 task 改为 `WIP` 并追加评论。
6. 只在验证 `agent_id == threadId` 后开始 Phase B。
7. 完成后追加结果评论并改为 `DONE`；后续扫描不再投递。
8. 冲突领取只有一个胜者；不产生重复评论或第二个执行 thread。
9. Relay 不持久化 vault 正文，未授权设备、目录和文件都无法通过 source 访问。

## 10. 实现顺序（待批准后）

严格按 red → green → refactor：

1. 先写 task parser/schema 测试：必填字段、枚举、稳定 ID、非法 YAML、多文件隔离。
2. 先写 discovery 测试：仅选 `TODO + deviceId + unclaimed`，内容 revision 去重。
3. 先写安全文件写测试：原子替换、CAS 冲突、symlink 逸出、评论 append 及 comment ID 去重。
4. 先写协议测试：Obsidian source 映射为已有 typed task frame，禁止通用 proxy。
5. 先写两阶段执行测试：claim turn 完成前 work turn 为零，绑定不匹配时 fail closed。
6. 先写断线、unknown、重试和竞争测试。
7. 完成最小实现，跑 Go unit/race/vet 与现有 Ariel 回归。
8. 最后用专用无害 POC task 验证真实 Codex App 可见性；不部署、不重启现有服务，除非用户另行要求。

## 11. 已确认的决策

2026-10-18 用户已确认以下三点：

1. `agent_host` 的值固定为 Ariel `deviceId`；
2. `agent_id` 直接使用 Codex `threadId`，不再另存 `codex_thread_id`；
3. POC 用本地 filesystem adapter 替代尚未证实的 Obsidian 云 API，Ariel 负责定时发现和跨主机路由。
