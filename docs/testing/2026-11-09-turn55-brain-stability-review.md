# Turn 55 Brain 稳定性、架构 Review 与预发验证

日期：2026-11-09

分支：`fix/ariel-turn55-review-stability`

基线：`main@ba54502d`

## 生产失败的只读取证结论

- 失败容器本身正常启动和退出，Relay 日志没有 Brain 分阶段信息。
- 失败数据库 integrity check 为 `ok`，三个临时 Bot 的 greeting 分别约 116–123 秒。
- Brain rollout 元数据表明 thread/context 准备约 16–19 秒，主要耗时是之后约 100 秒的首轮 reasoning。
- 结构化提问场景中，Brain 在消息后约 6.7 秒调用 `ask_user`，但额外生成
  `questions[].type=single-select`；严格 decoder 返回 `INVALID_ARGUMENT`。因此没有成功工具事件进入 Relay，
  也没有 question card 可供数据库写入或 Web 渲染。
- 本机预发前额外发现：Codex 0.145 已移除 `view_image` feature 名，旧的固定 `--disable` 列表会让
  app-server 在启动时立即退出。现改为按 binary 的 feature inventory 构造参数，并在 inventory 不可用时
  fail closed。

取证过程未修改生产容器、生产数据库、生产 Agent 或现有 Codex thread；记录中不保留凭据、完整 thread id
或用户原始提示。

## 自动门禁

| 门禁 | 结果 |
|---|---|
| `go test ./... -count=1` | 通过 |
| `go test -race ./... -count=1` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| `web/npm test` | 通过，47 files / 419 tests |
| `web/npm run test:ui` | 通过，189 Chromium tests |
| `web/npm run build` | 通过；仅有既有的大 chunk warning |
| `git diff --check` | 通过 |
| 真实 app-server dynamic tool gate | 通过；process 0.177s，thread ensure 5.335s，首个 tool call 约 5.65s，turn complete 6.621s |

## 本地 Relay + Agent + 真实 Brain 预发冒烟

环境由测试自行创建临时 Relay、Puck DB、Brain workspace、执行 fixture 和新 thread；没有连接生产。测试
串行运行，一次仅存在一个临时 Bot，用后删除。

| 场景 | 结果 | 观测耗时 |
|---|---|---|
| Full Access | 通过；创建 1 个 task、0 个 authorization，并稳定观察 180 秒 | create/ensure 2.007s；greeting 6.010s（Brain turn 4.317s）；首个 task card 0.200s；稳定窗口合计约 180.2s |
| 每次询问 | 通过；首问、以后都允许、自动允许、撤销后再问均符合预期，计数依次为 0/1、1/1、2/1、2/2 | create/ensure 2.811s；greeting 8.010s（另一次完整跑为 6.010s）；权限全流程 7.479s |
| `ask_user` 结构化卡 | 通过；得到 canonical pending question card，单问题且含两个选项 | 4.017s |
| 删除 Bot | 通过；删除后 `puck.list` 不再出现 | Full Bot 0.026s；Ask Bot 含执行 thread 收尾 11.415s |
| 引用回复 | 通过；引用 assistant 文本持久化；引用 task card 后保持一个 task，未重复创建 | 34.093s |

真实 Brain 两次创建的 greeting 均在约 6–8 秒可见，相比失败样本的 116–123 秒显著下降。冒烟期间某次
引用 task card 的 native continuation 返回 `RESYNC_REQUIRED`，Brain 随后查询状态并保持 task 数为 1；
场景最终通过。该现象支持 review 中“为 RESYNC reason 增加指标、审视 follower 恢复”的后续建议。

## 交付判断

本分支具备“可部署候选”的自动门禁和本地预发证据，但 Turn 55 明确不部署。正式部署前仍应先处理或
接受 review 中 P0 风险，尤其是 Puck event channel 静默 drop 与数据库降级不兼容。
