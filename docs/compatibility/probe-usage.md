# 兼容性探针操作

## 只读探针

```sh
go run ./cmd/probe detect
go run ./cmd/probe history --thread <明确的本机会话ID>
go run ./cmd/probe owner --thread <明确的本机会话ID> --watch 3s
```

`detect` 检查环境并握手，不启动 Desktop。`history` 通过独立 App Server 读取分页与历史；`owner` 发现 Desktop owner，临时订阅并在退出时取消订阅。输出不含聊天正文和实际 ID。

可选参数：`--app`、`--codex-binary`、`--ipc-socket`。PATH CLI 不作为静默替代。`--watch` 范围为大于 0、最多 30 秒。

## 隔离写测试

这些命令会创建测试聊天或执行测试 turn，需要显式 `--write-enabled`。不得用于真实业务聊天。

```sh
mkdir -p .local
go run ./cmd/probe fixture create --manifest .local/fixture.json --write-enabled
```

创建命令在系统临时目录下创建专属 workspace，设置只读 sandbox 和禁止审批提权的策略，保存权限为 0600 的 manifest，再执行一个禁止使用工具的短回复。manifest 路径不能覆盖已有文件。命令失败时先检查已保存身份及历史，不重复执行未知结果的提交。

随后在 Desktop 打开该测试聊天。当前自动加载尚未实现，这是测试前置操作，不是最终手机用户流程。

```sh
go run ./cmd/probe fixture exercise --manifest .local/fixture.json --write-enabled
go run ./cmd/probe fixture stale-interaction --manifest .local/fixture.json --write-enabled
```

`exercise` 只在 fixture 为空闲且没有 pending request 时发出一个短回复和一个精确停止测试；最后通过独立 App Server 核对真实 turn 状态。

`stale-interaction` 只在空闲 fixture 上对明确不存在的补充问题提交空答案，用于验证原生回执的语义限制。它不会批准任何真实请求。

完成后归档测试聊天，保留所需的本地 manifest；不要将 manifest、目录标记或实际会话正文提交到 Git。

## 补充问题测试

普通 fixture 明确禁止工具，不能用它测试提问。创建专用 fixture：

```sh
go run ./cmd/probe fixture create --purpose user-input --manifest .local/input.json --write-enabled
# 在 Desktop 打开这个测试聊天并确认 owner 已加载后：
go run ./cmd/probe fixture user-input --manifest .local/input.json --write-enabled
```

仅允许提问工具，保持 read-only / never，使用 fixture 当前模型的 Plan mode。两题分别响应实际选项 label 和选项外的自由文本；不采用默认预选答案。返回结果分别报告 receipt、精确回显、完成状态和独立历史核对。

原生 `ok` 不等于已受理；请求消失或改变时本地拒绝提交。完成后再尝试同一请求只验证本地 guard，不向 Desktop 重发。双客户端不同答案竞争使用另一专用 fixture：

```sh
go run ./cmd/probe fixture create --purpose user-input --manifest .local/input-race.json --write-enabled
# 在 Desktop 打开这个测试聊天并确认 owner 后：
go run ./cmd/probe fixture user-input-race --manifest .local/input-race.json --write-enabled
```

该命令只按相同 request ID、turn ID 的最终精确回显归因，两个 native `ok` 不等于两个答案都被受理。测试使用隔离只读会话，不面向业务会话。

中断过期测试可使用已有、已确认 seed 的专用 user-input fixture：

```sh
go run ./cmd/probe fixture user-input-interrupt --manifest .local/input-race.json --write-enabled
```

探针等待真实 pending 后精确停止该 turn，刷新确认 interrupted 且无 pending，再验证旧答案被本地拒绝；独立历史复核 exact turn。它不会把旧答案发给 Desktop。

## 命令审批只拒绝探针

```sh
go run ./cmd/probe fixture create --purpose command-decline --manifest .local/decline.json --write-enabled
# 先在 Desktop 打开 fixture 并验证 owner，再运行：
go run ./cmd/probe fixture command-decline --manifest .local/decline.json --write-enabled
```

仅该 fixture 使用 read-only / on-request；请求 `/usr/bin/true` 的审批，但只在原生明确提供 decline 时才发送 decline。该命令没有 accept、cancel、会话授权或规则授权入口。当前实测请求只提供 accept / 结构化决定 / cancel，因此会报告失败并精确停止测试 turn；不能把它当成完整审批支持。

已确认的 cancel 语义使用另一份专用 fixture：

```sh
go run ./cmd/probe fixture create --purpose command-cancel --manifest .local/cancel.json --write-enabled
# 在 Desktop 打开此测试聊天并确认 owner 后：
go run ./cmd/probe fixture command-cancel --manifest .local/cancel.json --write-enabled
```

它只在原生请求提供 cancel 时提交，随后核对 live exact turn 与独立历史都为 interrupted。若出现异常，输出是否动用了清理 interrupt；该项为 true 时不能把持久化中断归因于 cancel。公开历史缺少嵌套 command item 时单独报告，不伪造 item 结论。

## 忙时发送测试

用普通无工具 fixture（`fixture create` 不带 purpose），在 Desktop 打开后执行：

```sh
go run ./cmd/probe fixture busy-submit --manifest .local/fixture.json --write-enabled
```

探针从第一个客户端启动长 turn，确认它正在运行，再从第二个客户端提交短消息，随后只对已知 turn ID 做精确停止。结果分别报告第二次 native 回执、是否返回同一 turn ID、停止结果；不能把 accepted 推断为第二条消息已持久化。该测试仅用于隔离 fixture；生产发送入口须在 busy 时拒绝提交。

## 错误与容量说明

单帧 8 MiB，事件缓冲 64 帧 / 16 MiB，pending request 32 个。超限或状态缺口时停止观察，需重新运行只读探针取得快照。写操作不自动重试。

`not_submitted` 只表示该次请求确定未发送；若某个多步骤测试更早的步骤已经执行，仍应查看 fixture 历史核对整轮结果。测试报告不能仅根据退出码判断所有步骤都未执行。
