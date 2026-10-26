# 0108 追加回归：旧 WebSocket 回调污染新连接

- 关联：Issue #11；既有规格 [0108](../specs/0108-auth-and-protocol-failure-separation.md) 与 [0109](../specs/0109-web-wire-backward-compatibility.md)。
- 本次是已批准连接恢复／认证分类契约的实现修复，不新增协议或改变安全边界。

## 线上只读检查与归因边界

用户截图显示“页面版本与 Relay 不兼容”，并反馈随后又跳回登录页。检查时公网 Relay／Web 为 `v0.8.4`，对应 `150c8e6`；容器 healthy、RestartCount 为 0，公开健康检查通过，HTML 使用 no-store。

这些检查没有取得用户当次故障的客户端消息来源，不能证明每次登录跳转都由同一个原因造成，也不能把“未重启”当作 Cookie 永不失效的证据。以下是确实存在、已确定性复现并修复的代码缺陷，不冒充该次物理 Safari 抓包。

## 确定性复现

旧实现只在 close 回调和终止协议连接时检查 socket 身份，open／message 没有同样的检查。

1. 一个连接还在等待 hello 时，被前台恢复的 wake 或重新 connect 替换。
2. 旧连接晚到的合法 hello 仍修改共享 ready／token 状态，提前把新连接标为已握手。
3. 新连接自己的合法 hello 到达后，被当作重复握手，触发 incompatible。
4. 旧 open 还可能覆盖当前 helloTimer；旧 event／response 可以越过 transport 边界，进入当前页面或完成当前 pending request。显式 disconnect 后，晚到 hello 也能产生幽灵 ready。

四项客户端测试先实际运行 red，分别暴露以上状态污染。真实 Chromium 中使用隔离 WebSocket fixture，故意延迟旧连接的 hello 回调，在它被替换后、最新连接握手前释放；旧代码再次出现与用户截图相同的红色协议不兼容提示，正文停止更新，测试失败。

## 最小修复

- open 与 message 在任何解析、发包、状态或计时器操作前校验当前 socket 身份；已退役 transport 的回调直接忽略。
- hello deadline 只有仍属于当前且尚未完成握手的连接才可关闭连接。
- 当前连接的 JSON／schema／时序校验保持严格；当前 unauthorized、真实协议错误、普通断线的既有分类不变。
- 不修改 Passkey／Cookie、认证有效期、Relay／Agent 协议或业务数据，不自动重放 mutation，不记录消息正文或凭据。

## 验收结果

| 验证 | 结果 |
| --- | --- |
| 四项新客户端回归 | 先 red 后 green；客户端 suite 18 项通过 |
| Chromium 竞态复现 | 修复前红色 incompatible、无法恢复新正文；修复后保留正文／草稿并自动看到新正文，发送重新可用，无新 Passkey ceremony |
| 竞态重复执行 | 5／5 通过 |
| Web 全量 unit | 31 个文件、269 项通过 |
| 浏览器全量 | 最终 1 worker 完整运行 51 项通过；既有截图基线不变 |
| production build | 通过；保留既有非阻断 chunk-size 提示 |
| Go 全量／vet | 通过 |

首轮 3 workers suite 有一项真实 Relay fixture 在测试启动阶段未通过 health 探针，尚未运行浏览器行为断言；该项单独复跑通过，随后最终单 worker 完整 suite 通过。没有把失败的并行运行说成全绿，也没有放宽断言或更新截图掩盖失败。

本轮生产仅做只读诊断，未部署、未修改任何真实 Codex 会话。物理 iPhone／Safari 及用户实际断网／后台场景仍须在发布后复验。
