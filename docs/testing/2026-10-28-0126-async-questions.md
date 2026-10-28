# 0126 异步问题：读取展示与写入门禁

日期：2026-10-28。对应已批准 spec 0126、Issue #56。

## 交付边界

原生问题、选项、已接受答案可读；异步回答写入仍 **unverified**，生产 Web／Relay／Agent 门禁关闭。同步补充回答原链路不变，Chilling 不参与，也未重启。不能将只读卡片的 mock／浏览器绿灯写成异步答案真实受理。

## Red → green

1. Adapter 测试先失败，随后补 `agentMessage.questions` 投影、稳定身份、内容指纹、逐题选项与 free text 形状、终态。仅 accepted steering／原生 user message 中精确身份和题目匹配的答案可标已回答。
2. public wire opt-in 测试先被旧 schema 拒绝，再增加独立 sidecar 与参数。旧 thread／interaction 形状不变；响应保持可选扩展，新字段不发送给未 opt-in 的 Web 或未协商的旧 Agent。
3. 实际 Agent＋Relay＋WebSocket 集成测试复现：只有初始 snapshot、正文不再变化时，后续协商不能补发问题而超时。补保初始最近视图后通过，不重建 follower。六个 Web 共享一份 native follower，设普通文本合并为一小时，题目变化仍即时送达。
4. 缓存实际计算 sidecar 字节，整体 replacement 同时 rotate；非法 sidecar 局部提示而非令会话断线。过期 request／退休 Agent 的能力回执不能启用扩展；先收到 snapshot 再收到 touch 回执也能协商。
5. Web 测试覆盖显式选择不自动提交、本地跳过与恢复、页面内草稿重挂载、指纹变化、未验证只读、终态、非法／超限数据。部分历史先复现无法解析，以及覆写已回答状态，再修复并保留 native 精确答案。
6. 既有 follower 全量测试遇到 receipt／IPC 关闭竞争；新增确定性测试先复现已知 reducer 超限原因被覆盖，随后在调用前与 receipt 失败分支优先保留已记录的原因，不扩大容量或重放请求。
7. 既有文件 quota 浏览器测试实际失败：独立聊天历史恢复后侧栏关闭，旧用例误要求菜单默认展开。首次尝试条件开菜单仍暴露恢复时序竞争；最终用“列出时选择主会话、已恢复时点击无副作用标题”确定主会话就绪，保留 quota 提示、文件可读及刷新后无文件 Tab，另增加刷新前后 checkpoint 必为空的断言。

## 浏览器与布局

Chromium 320／390／1280px，隔离 WebSocket；问题在原消息处显示，几何边界不溢出，不禁用普通 composer，不新建 Agent subscription。覆盖本地跳过、恢复、原生已回答增量替换及主草稿保留，零 native mutation。三张截图已检查，属于真实浏览器 viewport／touch 验收，不是物理 iPhone／Safari。

## Native evidence

- 安装组合：Desktop 26.930.51102／bundled Codex 0.160.0。
- 专用临时 workspace＋manifest guard，实际 native thread/cwd 逐一核对；read-only／never；不使用业务会话或真实问题做写测试。
- 初次 owner 尚未 ready，discovery 超时；独立历史确认只有 seed、没有测试 turn 后，确认 owner 就绪再开展一次新的隔离测试，未重发未知的 mutation。
- 原生测试完成，没有 `agentMessage.questions`，回复 `ASYNC_TOOL_UNAVAILABLE`；独立历史确认 seed 与测试均为 completed。没有提交任何异步答案，不证明 steer 写入、答案回显或竞争结果。
- 读取当前业务会话只在内存检查结构，不保存、提交或上传正文、实际 ID、owner、快照或凭据。
- 发布前额外用生产 `OpenFollower` 对当前超长会话做只读 cold attach，两次均在初始化路径命中 `HISTORY_TOO_LARGE`；未更改 64 MiB reduced frame／256 MiB wire／有界缓冲，也未获得 exact 失败字节数，不能断言单帧超过某个大小。这使当前会话的真实生产冷接入 smoke 未通过，另记 #64；为避免重启丢失现有热连接与 Relay cache，暂缓本机和公网部署，保留现有进程。单测／隔离三端通过不能代替这个未通过项。

## 最终自动结果与发布状态

- Web 单测 377 项及 production build 通过；完整 Chromium 157 项通过，三个问题卡片宽度已检查截图。
- Go 全量、vet、Relay／Agent／IPC race 通过；问题集成 30 轮、协商／旧 Agent 30 轮、follower 原因保留 100 轮及 quota 浏览器 10 轮通过。
- README、指南、架构与 approved spec 已更新。异步写入跟踪 #63；冷接入容量失败跟踪 #64，代码只 push PR，不因本报告而宣称已上线或已支持异步回答。

## 复验命令

```sh
go test ./...
go vet ./...
go test -race ./internal/relay ./internal/desktopagent ./internal/codex/desktopipc
go test ./internal/desktopagent -run TestAgentQuestionOptInRepublishes -count=30
go test ./internal/codex/desktopipc -run 'TestFollower.*Preserves' -count=100
cd web
npm test
npm run build
npm run test:ui
npm run test:ui -- e2e/file-workspace.spec.ts -g 'storage quota failure' --repeat-each=10
```

当前发布不包含 SVG PR 的未合并变更、PWA 通知、侧边新会话或未验证的异步回答 mutation；物理手机与用户最终验收仍待本人确认。
