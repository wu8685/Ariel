# 0076 首条 turn bootstrap compatibility gate

- 日期：2026-10-13
- Codex Desktop bundle：`0.160.0`
- 规格：[0076 本地新会话草稿与首条 turn bootstrap](../specs/0076-local-draft-first-turn-bootstrap.md)
- 结论：Passed

## 验证范围

使用隔离临时目录和无副作用 fixture，直接启动 Desktop bundle 内的 Codex App Server，验证以下链路：

1. 同一进程顺序执行 `thread/start`、`turn/start`，并核对 exact thread ID、turn ID 与 `clientUserMessageId`。
2. 消费首轮 streaming notification，直到 exact `turn/completed`。
3. 对隔离长时间命令执行 exact `turn/interrupt`，确认终态为 `interrupted`。
4. 对命令 approval server request 回复一次 `decline`，确认请求数为一且没有副作用。
5. 在 App Server 公布的 Plan collaboration mode 下处理一次 `request_user_input`；model 由 `model/list` 动态选择，未写死版本相关名称。
6. 终态后分别用同一 owner 和独立 App Server 读回 thread；关闭 bootstrap owner 后，通过 Desktop deep link 接管，再由 Ariel owner probe 观察 snapshot。

## 结果

| 检查 | 结果 |
|---|---|
| stream | passed |
| interrupt | passed |
| approval decline | passed，server request 恰好 1 次 |
| request user input | passed，server request 恰好 1 次 |
| independent persistent read | passed |
| Desktop owner adoption | passed |

生产 `AppServerBootstrapper` 完成后，又使用同一 bundled binary 运行了仓库内的隔离 integration fixture：本地临时 cwd → 首条真实 turn → streaming 终态 → `clientUserMessageId` 核对 → 归档清理，结果通过。该回归同时发现并修正了两个只在真实事件速率下暴露的问题：notification 不能逐条同步阻塞 `thread/read`，以及 macOS 临时目录必须先 canonicalize `/var`／`/private/var`。

所有 fixture 均已归档。该 gate 证明 0076 的生产方案在当前 bundle 上可行，但不替代产品实现的 TDD、浏览器回归和版本升级后的重复验证。
