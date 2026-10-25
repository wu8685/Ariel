# Obsidian Task Source 运行手册

> 对应规格 [0095](../specs/0095-obsidian-task-source-and-codex-claim.md) 与 [0096](../specs/0096-productized-obsidian-task-agent.md)。功能默认关闭；不要让 Obsidian source 与 Chilling 同时管理同一 task collection。

## 1. 任务卡

```yaml
status: TODO
task_id:
agent_host: <Ariel Desktop Agent deviceId>
agent_id:
claimed_at:
```

- `agent_host` 是 Ariel 配置中的稳定 `deviceId`，不是 IP 或 hostname。
- `task_id` 为空时，source 首次扫描会填充 UUID。
- `agent_id` 在领取成功后回写 native Codex `threadId`。
- `workspace` 可选；缺省在 vault 根目录执行，显式填写时仍须命中 executor allowlist。

Obsidian Bases 在分组列内点击“新建”时只会生成最小 frontmatter。Ariel 首次扫描会补齐文件名对应的 `name`、日期、`tags`、`task_id`、默认 `agent_host` 和三个标准正文区块。已经是完整模板、但故意留空 `agent_host` 的任务不会被自动领取。

## 2. Relay 配置

Relay 必须显式启用 tasks，并在私有 task credential 文件中分别授权：

- source device：允许管理 `obsidian-*` task store，并限定可投递的 execution targets；
- executor device：`execute: true`；
- M=E 时同一 device 同时具有两类权限。

Relay 仍不读 vault，也不持久化任务正文。

## 3. 持久配置（推荐）

```sh
scripts/ariel.sh up agent \
  --relay-url wss://relay.example/ws \
  --device-id <stable-device-id> \
  --device-name <name> \
  --token-file /private/task-agent-token \
  --obsidian-task-vault /absolute/path/to/vault
```

指定 vault 会显式启用 tasks，并默认：

- `task-allowed-roots = vault`；
- `task-allowed-managers = device-id`；
- Bases 原生新卡的 `agent_host = device-id`。

需要在其他目录执行时，用 `--task-allowed-roots` 传逗号分隔的现存绝对目录。配置保存在 `.local/runtime/config.json`，凭据仍只从私有文件导入，不写入配置。

## 4. 直接运行 Source Desktop Agent

```sh
cmd/desktop-agent \
  --enable-tasks \
  --tasks-manager-only \
  --obsidian-task-vault /absolute/path/to/vault
```

也可用 `ARIEL_OBSIDIAN_TASK_VAULT` 设置 vault。Source 要求 `<vault>/tasks/list/` 存在，启动时会解析 symlink 并固定绝对路径。

## 5. Executor Desktop Agent

```sh
cmd/desktop-agent \
  --enable-tasks \
  --task-allowed-managers <source-device-id> \
  --task-allowed-roots /absolute/allowed/workspace
```

Executor 必须通过 Ariel 现有 official-provider 与 native compatibility gate。未验证版本只保留管理／读恢复能力，不开始 Codex turn。

## 6. 领取与恢复顺序

1. Source 扫描 `TODO + agent_host + unclaimed`。
2. Executor 创建 native thread，首 turn 只返回绑定 task/revision 的 claim intent。
3. Source CAS 回写 `WIP` / `agent_id` / `claimed_at` 并追加评论。
4. Source 重读确认后，才向同一 thread 发送 work command。
5. `done` intent 将任务更新为 `DONE`；`blocked` 保留 `WIP` 并追加阻塞评论。
6. accepted receipt 持久化在 `<vault>/.ariel/tasks/state.json`；进程重启后只恢复 native turn tracking，不重复创建 thread。
7. blocked 写回的 revision 被标记为已处理；只有后续人工编辑／评论产生新 revision，才会续投同一 thread。
8. 文件产生新 revision 时，journal 中同一 task 的旧投递会被淘汰；旧 turn 即使迟到也不能覆盖新内容。

`.ariel/` 是传输恢复状态，必须加入 vault `.gitignore`。它不是第二套业务状态，不应手工编辑或同步到远端。

## 7. 故障排查

| 现象 | 检查 |
| --- | --- |
| 任务一直 `TODO` | `agent_host` 是否等于 Relay 中授权的 `deviceId`；source/executor 是否 online/ready。 |
| 已建 thread 但未变 `WIP` | claim turn 是否返回 exact JSON；revision 是否被人工编辑改变。 |
| 任务保留 `WIP` | 查看评论中的阻塞记录，并用 `agent_id` 打开对应 Codex thread。 |
| 重启后不再投递 | 检查 `.ariel/tasks/state.json` 权限与 JSON；损坏时 source 会 fail closed，不覆盖任务。 |
| `OFFICIAL_PROVIDER_UNVERIFIED` | 不要绕过 gate；先完成当前 Desktop/Codex 版本的兼容性验证。 |
