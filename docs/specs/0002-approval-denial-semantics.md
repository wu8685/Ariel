# 0002：审批拒绝语义

- 状态：Approved（2026-10-03 用户确认按原生能力、保持与本地 Codex 体验一致）；cancel 隔离验证已完成。
- 创建日期：2026-10-03
- 背景：M0 已批准的审批验证发现原生可选决定存在语义差异。

## 实测事实

当前 Desktop 的隔离 `/usr/bin/true` 审批请求为 `item/commandExecution/requestApproval`，身份、cwd、turn、item 均可识别。该请求的 `availableDecisions` 为 `accept`、一个结构化决定、`cancel`，没有 `decline`。结构化决定的具体内容未输出、未使用，不能将其当成单次许可。

当前内置 CLI 生成的 Schema 分别描述：

- `decline`：拒绝此操作，Agent 继续本轮。
- `cancel`：拒绝此操作，同时中断本轮。

最初的 decline-only 探针发现本次请求不提供 `decline`，因此没有提交决定，只用 exact-turn interrupt 清理 fixture。随后新增独立 fixture 提交原生提供的 `cancel`：owner 返回回执，pending request 移除，实时 canonical history 中原 turn 为 `interrupted`，独立 App Server 读取同一 turn 也为 `interrupted`，清理 interrupt 未触发。隔离会话的本地执行记录显示该嵌套工具调用结果为 `aborted by user`。

公开 `thread/read` 没有保存嵌套 `exec_command` 的 commandExecution item，因此不能宣称通过公开历史核对了该 item 的 declined 状态。探针将“原 turn 已中断”和“公开历史有无 command item”分别输出；当前后者为 false。此限制不影响本次 turn 停止的实测结论。

## 已确认行为

1. 手机端跟随当前原生请求的实际可选集合，不承诺每个请求都有“仅拒绝、继续本轮”。
2. 对已验证的 `decline` 显示“拒绝此操作”；对已验证的 `cancel` 显示“拒绝并停止本轮”，不得共用含糊的按钮文案。
3. 公共协议区分决定及后果，暂拟 `deny` 与 `deny_and_stop`；最终字段纳入 M1 JSON Schema。旧稿的单一 `deny` 不静默映射成中断。
4. 不增加 session grant、永久规则授权或自动批准。允许按钮仍须具备完整操作上下文和已验证的单次许可语义。
5. 未验证的决定不可用；请求消失、owner 改变、revision 变化或提交结果不明时，不重放，不猜测成功。

## 测试与证据

- Red：按原生集合限制决定；拒绝/取消绝不走授权分支；变更、过期请求拒绝提交。
- Red：`cancel` 与 `decline` 的映射不同；请求回执和实际终态分别记录。
- Green：只对 manifest 指定的 command fixture 提交 cancel，不执行被请求的命令。
- 真机核对：request 移除、原 turn 实时和持久化终态、fixture 本地执行记录；公开历史缺少嵌套 command item 的限制已记录。
- 测试返回 `receiptOK=true`、`pendingCleared=true`、`liveTurnStopped=true`、`observedTurnStatus=interrupted`、`cleanupInterrupted=false`；独立历史再次确认同一 turn `interrupted`。

用户已确认上述原则。结论限于当前 Desktop / CLI 版本、已识别的命令审批类型和独立 fixture；文件变更与权限审批仍需各自验证。
