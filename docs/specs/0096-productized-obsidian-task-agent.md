# 0096｜Obsidian 面板任务到 Codex Agent 的产品化链路

- 状态：Implemented（2026-10-18，自动测试与真实公网 E2E 通过）
- 前置规格：0091、0095
- 目标版本：Ariel 当前主干的下一次本机与公网部署

## 1. 用户场景

用户在 Obsidian 的 `tasks/kanban.base` 三列面板中，于 `TODO` 列点击“新建”，输入任务名并补充任务描述、成功条件或评论。无需手工运行脚本，Ariel 应自动把这张卡片交给 `agent_host` 指定主机上的 Codex Agent。

任务被 Codex 接受后必须先进入 `WIP`，并在任务文件中记录稳定的 Ariel 主机 ID、Codex `threadId` 与领取时间。此后用户或其他参与者在同一条评论时间线上追加内容，Ariel 应把更新续投给同一个 Codex thread，而不是创建第二个负责人。Codex 完成或受阻后，状态和评论继续写回同一任务文件。

## 2. 范围

### 2.1 本规格实现

1. 兼容 Obsidian Bases 原生“新建”生成的最小卡片：Ariel 自动补齐标准 frontmatter、正文区块、稳定 `task_id` 和默认 `agent_host`。
2. 将 Obsidian task source 作为 Ariel 的持久启动配置，而不是依赖临时环境变量。
3. 为投递增加本地 durable journal；Ariel 或网络重启后恢复已接受的 native turn，不重复创建 Codex thread。
4. `TODO` 任务先执行 claim-only turn；claim fact 验证成功后才写回 `WIP`、`agent_id` 和 `claimed_at`，随后在同一 thread 执行任务。
5. `WIP` 文件发生新的人工编辑或追加评论时，续投到已有 `agent_id`。Ariel 自己写入的阻塞评论不得形成反馈循环。
6. 在当前 `wuke-mac-mini` 上安装持久配置，并使公网 Relay 的任务权限包含该 Obsidian task store。
7. 用真实 Obsidian 卡片、真实公网 Relay 与真实 Codex Desktop 做端到端验收；创建的 thread 必须能在 Codex App 中读取。

### 2.2 不在本规格内

- 多用户权限、团队级审计与云数据库。
- 物理第二主机验收；协议仍保留跨主机路由能力。
- 将 Agent 的完整 transcript 复制进 Obsidian。任务文件只记录有界状态评论与 native identity。
- 自动提交或 push Agent 对其他仓库造成的代码变更；任务正文需明确授权其业务动作。

## 3. 任务文档契约

标准业务元数据：

| 字段 | 必填 | 语义 |
| --- | --- | --- |
| `name` | 是 | 任务名称；Bases 新建时可由文件名补齐 |
| `date_created` | 是 | `YYYY-MM-DD` |
| `date_modified` | 是 | 最近一次 Ariel 受控写入日期 |
| `tags` | 是 | 至少包含 `task` |
| `status` | 是 | 仅 `TODO`、`WIP`、`DONE` |
| `task_id` | 是 | Ariel 生成的稳定 ID |
| `agent_host` | 是 | 稳定 Ariel `deviceId`，不得使用 IP |
| `agent_id` | WIP/DONE 必填 | Codex native `threadId` |
| `claimed_at` | WIP/DONE 必填 | RFC 3339 领取时间 |
| `workspace` | 否 | 绝对工作目录；缺省为 vault 根目录 |

正文至少包含：

1. `## 任务描述`
2. `## 成功条件（选填）`
3. `## 评论`

评论保持 append-only。每条评论包含稳定 comment marker、时间、评论人和内容；类型与关联可选。Ariel 不覆盖既有评论。

## 4. 状态机

```text
TODO + agent_host + no agent_id
  -> claim command
  -> native receipt + verified claim intent
  -> WIP + agent_id + claimed_at
  -> work command on the same thread

WIP + human revision
  -> feedback/work command on the same thread

work intent = done
  -> DONE + completion comment

work intent = blocked
  -> stay WIP + blocked comment
  -> wait for a later human revision
```

文件 revision 是乐观并发控制依据。任何在授权与写回之间发生的用户编辑都使旧命令失效，Ariel 不覆盖该编辑。

## 5. Durable delivery journal

Journal 位于 vault 的 `.ariel/tasks/state.json`，权限为仅本用户可读写，并由 vault `.gitignore` 排除。它只保存传输恢复信息，不成为第二套业务状态：

- 当前命令及其 task revision；
- 已接受的 native `threadId` / `turnId`；
- Ariel 自己刚写入且已处理的 revision；
- schema version。

重启后：

- 没有 native receipt 的 pending command 可用同一 command/client message identity 重试；
- 已有 native receipt 的命令只进入 tracking recovery，不再调用 `thread/start`；
- terminal fact 成功写回后清理 pending；
- journal 损坏时 fail closed，不扫描或覆盖任务文件，并输出可定位错误。

## 6. 启动配置

`scripts/ariel.sh up agent` 新增可持久化参数：

```text
--obsidian-task-vault <absolute-path>
--task-allowed-roots <comma-separated-absolute-paths>   # 可选
--task-allowed-managers <comma-separated-device-ids>    # 可选
```

指定 vault 即显式启用 tasks。默认执行 root 为 vault，默认 manager allowlist 为本机 `deviceId`。保存后的普通 `up` 和 launchd 重启必须继续带出等价环境配置；状态输出只显示功能是否启用与 store ID，不泄露 token。

## 7. 安全约束

- vault、task file、workspace 都必须经过绝对路径、canonical path 和 allowlist 校验。
- 不跟随 `tasks/list` 下的 symlink；不允许任务逃逸允许目录。
- Relay 必须同时授权 management store、execution target 和 execute capability。
- task token、journal、native identity 不进入 Git 或日志正文。
- Agent prose、工具明细和私有路径不经 Relay 写回；只传播既有 bounded fact。

## 8. 验收标准

### 自动测试

- [x] Bases 最小卡片被规范化为完整任务模板，默认 host 为当前稳定 `deviceId`。
- [x] claim 成功前任务不进入 `WIP`；成功后同一 thread 执行 work。
- [x] receipt 后重启 source 只恢复 tracking，不创建第二个 thread。
- [x] blocked 写回不会自触发；人工追加评论会续投同一 thread。
- [x] stale revision、symlink、越界 workspace、损坏 journal 均 fail closed。
- [x] startup 配置持久化、默认值、重复 setup 与 credential 隔离有测试覆盖。
- [x] `go test ./...`、`go test -race ./...`、构建通过；既有独立 vet 缺陷单列。

### 真实链路

- [x] 在 Obsidian `TODO` 列点击“新建”生成验收任务。
- [x] Ariel 自动补齐模板与 `agent_host: wuke-mac-mini`。
- [x] 任务自动进入 `WIP`，写回一个真实 Codex `threadId`。
- [x] 该 thread 在 Codex App 中可读取，且 work turn 与 claim turn 属于同一 thread。
- [x] 追加一条人工评论后，同一 thread 收到后续 turn。
- [x] harmless 验收任务进入 `DONE`，任务时间线保留领取、阻塞、人工反馈与完成评论。
- [x] 本机 launchd Agent 和公网 `/healthz` 均正常。

## 9. 回滚

1. 停止本机 launchd Agent。
2. 恢复上一版 Desktop Agent deployment 与公网容器。
3. 从保存的旧 `config.json` 恢复任务配置；不删除 vault 任务或 journal。
4. 重启旧 Agent 并验证普通 Ariel 会话能力。
