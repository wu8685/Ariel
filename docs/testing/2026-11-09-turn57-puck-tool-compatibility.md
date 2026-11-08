# Turn 57：Puck 生产工具参数兼容验证

- 日期：2026-11-09
- 分支：`fix/puck-ask-user-prod-compat`
- 基线：`main@27315bd`
- 范围：仅本地隔离 Relay、Desktop Agent、真实 Brain 与临时 Puck DB；未部署、未连接生产写路径

## 根因与复现

生产 Desktop Agent 默认通过 `probe.BundledBinary` 选择 ChatGPT bundle 内的 Codex；显式
`ARIEL_CODEX_BINARY` 只有在版本与 bundle 完全一致时才允许覆盖。Turn 56 的预发使用独立
0.145.0-alpha.27，而生产实际使用 0.160.0，因而没有覆盖版本相关的 dynamic tool 参数漂移。

本轮用生产同一路径的 `codex-cli 0.160.0` 复现。`ask_user` 首次调用的脱敏 shape 为：

```text
object {
  question: string,
  options: array<object { label: string, value: string }>,
  multiple: boolean
}
```

0136 decoder 只接受 `multiSelect`，因此以 `field=multiple`、`reason=additional property is not
allowed` 拒绝。参数值、用户文本和完整 thread id 均未记录。0.145.0 的既有实证 shape 则是
`questions[].type = single-select | multi-select`；两版的兼容漂移不同。

在只加入脱敏诊断和明确重试说明、尚未加入 `multiple` 归一化时，0.160 首次调用被拒，Brain 约
2.75s 后用 canonical shape 重试成功，`ask_user` 总耗时 8.025s。这证明失败发生在 Relay strict
decoder，不是 dynamic tool 未注册、Desktop Agent 没有转发或 Web 没有渲染。

## 修复

1. Relay 对三个结构化工具输出有界脱敏诊断：工具名、字段路径、reason、expected，以及只含 key、
   JSON type、array length 的 shape；scalar 值永不进入诊断。
2. Agent 从 Relay tool result 中只提取上述 typed diagnostic，写入 `brain-tool-result` 结构化日志；
   不记录完整 result content。Bot 首次拒绝显示安全 notice。
3. `ask_user` 同时覆盖 0.145 的 `questions[].type` 与 0.160 的 `multiple:boolean`，并窄兼容已知
   question/options/custom alias、字符串或对象 options；归一化后再执行原有严格校验。
4. `share_result` 与 `request_authorization` 对已知等价 alias 做相同 canonicalization；handler 向
   store／authorization owner 只传 canonical payload。冲突 alias、未知字段、不明确 kind／locator、
   非绝对 cwd 等仍拒绝。
5. 同一 Brain turn／tool 第一次无法归一化时要求只重试一次；第二次仍失败时明确要求停止重试，并
   持久化 `question`／`authorization`／`share_result` 对应的友好终态卡。授权降级不代表已授权。
6. Desktop Agent 在最终解析 binary 后记录绝对路径和版本，普通 Adapter 与 Brain 共用该路径。
   预发工具要求 `ARIEL_PRODUCTION_CODEX_BINARY` 与 `ARIEL_CODEX_BINARY` 解析到同一文件且版本一致，
   否则启动前失败。

## 自动门禁

| 门禁 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `go test -race ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| Web unit | 47 files／420 tests 通过 |
| Web Playwright | 首轮 188／189；唯一 `relay-live-cache` 在 5s 内未找到发送按钮，定向重跑 426ms 通过 |
| Web production build | 通过；仅保留既有 chunk-size warning |
| `git diff --check` | 通过 |

Playwright 的首轮失败场景不经过 Puck 代码，定向重跑一次通过；记录为既有时序抖动，不隐藏。

## 0.160.0 隔离预发

binary parity gate 验证 production 与 smoke 均使用 ChatGPT bundle 的同一 Codex 0.160.0 executable。

| 场景 | 结果 | 耗时 |
|---|---|---:|
| Full Access | 通过；单 task／零授权保持完整 3 分钟稳定窗口 | 3m0.425s |
| 每次询问 | 通过；首次授权、以后都允许、自动允许、撤销后再次询问均符合预期 | 380ms |
| `ask_user` | 通过；0.160 的 `multiple:boolean` shape 首次调用即成功 | 4.026s |
| 删除 Bot | 通过；Full Bot 29ms，ask Bot 105ms | 29ms／105ms |
| 引用回复 | 通过；assistant 引用持久化，task-card 引用沿用原 task 且未新建 task | 36.103s |

附加阶段数据：Full create/ensure 142ms、greeting 6.012s、首个 task/card 177ms；ask create/ensure
121ms、greeting 6.009s、首个 authorization card 13ms。两个临时 Bot 均已删除。

## 结论

本地证据支持将该分支作为部署候选：已复现并修复 0.160 参数漂移，0.145 兼容回归保留，完整门禁
与五项 0.160 预发均通过。本轮按要求不部署；是否部署仍由用户另行决定。
