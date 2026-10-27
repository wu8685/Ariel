# 0121 原生状态编码膨胀与重连误报修复

状态：Implemented（2026-10-27；用户明确要求直接修复并部署，作为 0104／0107 既有行为的缺陷修复；发布另记）

证据：[测试记录](../testing/2026-10-27-0121-native-state-reconnect.md)，含实际 owner 只读复验、Red → Green 与未验边界。

补充：[生产限额全链路回归](../testing/2026-10-27-0121-native-wire-regression.md)（Issue #41），含旧版本与独立回退复现。

## 问题与证据

真实只读 owner 探测在 Reducer 返回 `normalized snapshot bytes 67940311 exceed 67108864`，Web 却显示“事件顺序发生变化”。该错误不是 Relay 重启或登录失败；需验证 JSON 的默认 HTML 转义是否将有效原生 JSON 在重新编码时无谓扩大。

## 输入、输出与不变量

- 输入仍是版本化 Adapter 收到的 JSON snapshot／patch；输出仍为完整、UTF-8 合法、受原有字节上限约束的 JSON owner state 与同一 revision。
- 传输 JSON 不嵌入 HTML，重编码不应仅因 `<`／`>`／`&` 的 HTML 转义扩大状态；字符串、引号、反斜线、控制字符和精确数值必须保持相同语义。
- snapshot 与 patch 共用一致的安全编码；保留现有 frame／wire／事件／controller／Relay 字节限制，不放大内存限制、不取消结构或 revision 校验。
- 补齐 0104 的已批准 lazy 边界：兼容大帧超过普通 8 MiB 阈值时，即使低于 64 MiB resident limit，也应对已验证图片／tool payload 做既有 streaming projection；小帧保持原快路径。仅去除已验证字段，未知内容保留或超限拒绝，不扩大可剔除字段范围。被剔除 payload 内可包含任意合法 JSON 类型，不因其内部 `type` 字段不是字符串而误判传输 envelope。
- 真正超限必须同时使 Reducer 失效并可被 `errors.Is(..., ErrFrameTooLarge)` 识别；兼容原 `ErrResyncRequired` 检测，使既有大帧 fallback 与 `HISTORY_TOO_LARGE` 分类生效。乱序与无效结构仍属于 resync，不伪装成容量错误。
- Web 对 `RESYNC_REQUIRED` 使用实时状态暂不可确认的中性说明，不暗示用户必须刷新整页；正文和草稿继续保留。真实超限沿用只读提示，不自动重试或重放 mutation。
- 不增加轮询、订阅、native 方法、后台任务或 Chilling 依赖，不更改 mutation owner、安全边界与通信 schema。

## TDD 与验收

1. 先新增并实际运行失败测试：含大量 HTML 特殊字符的合法 snapshot 与 patch 在限额内可用；真实超限同时属于 frame-too-large 与 resync；初始 snapshot 超限与 patch 增长均不保留可用状态。
2. 真实 IPC socket fixture 验证 Follow 能接受原生限额内的特殊字符正文，再消费 patch；不通过直接构造已归一化 Web 快照替代 Adapter 测试。
3. Web 单测与真实 Chromium 验证 stale／resync 文案、内容和草稿保留、显式重连恢复，不产生会话写请求；既有终态／缓存／双 turn／五秒更新回归保持通过。
4. 修复后复跑实际 Codex owner 只读探测，记录状态大小及观察结果，不保存原始快照／真实会话正文；未通过时不得宣称根因已解决。
5. Go 全量／race／vet、Web 单测／build／真实浏览器及真实 Docker 构建通过后 push、tag 并部署本机 Agent 与公网 Relay/Web，保留身份、凭据、回滚与看板关闭状态。

物理 iPhone／Safari 仍需用户复验；原生只读验证不冒充发送、停止或审批能力验收。

## 补充回归契约（Approved，2026-10-27）

用户要求补充能复现本次故障的回归测试，不改变运行时行为或安全边界：

- 隔离 Unix socket 模拟 native owner，但通过生产 `OpenFollower`、framing、projection、Reducer、Agent、Relay 与真实 Chromium，不再只在假 Live 中缩小 Reducer 限额。
- 合成大于 8 MiB、小于 64 MiB 的合法 JSON，包含已验证 tool／image lazy 字段及必须保留的未知 HTML 字符字段；旧编码与旧 projection 组合会越过真实 64 MiB resident cap。保留字段内容必须不变，lazy 字段必须剔除，不能以删除未知内容或调高限额通过测试。
- 首次打开可见正文，连续 patch 可见增量和完成状态，草稿保留、发送恢复可用、无需刷新页面或额外 attach；native mutation 请求数必须为零。
- 在隔离源码副本恢复旧行为，实际运行新增测试得到失败；当前源码通过。大帧在运行时生成，不提交正文大文件或真实快照。
