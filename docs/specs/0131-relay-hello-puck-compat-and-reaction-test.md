# 0131：Relay 握手向后兼容与 Puck reaction 测试修复建议

- 状态：Draft（2026-11-04；修复建议，尚未批准或实施）。
- 来源：旧 Desktop Agent 持续报告 `invalid Relay acknowledgement`；更新候选的 Go 全量测试被 Puck reaction 用例阻塞。
- 分析基线：旧运行代码 `911a45b1e8d8684eb3de4fa359ab62d1cab971b1`；候选官方 main `f9769b4001360e965b3843f19e29993cd864b37f`。
- 关联：0108、0109、0130 Puck P0。
- 实施位置：包含 0130 的最新 main。当前旧工作区仅新增本建议文档，不据此覆盖未提交的 native follower 修复。

## 1. 问题与证据边界

### 1.1 握手兼容缺陷

1. 候选 `internal/relay/server.go` 在 `PuckState` 非空时向 `hello.ok` 附加 `puck`，当前条件未区分 Agent／Web，也没有接收方能力协商。启动代码在 Puck 关闭时也会传入非空的 `off`，因此“功能关闭”不等同于该条件不成立。
2. 旧 `protocol/v1.schema.json` 的 `helloOK` 使用 `additionalProperties: false`，允许的字段没有 `puck`。
3. 旧 `internal/desktopagent/agent.go` 对回包先执行 `protocol.Validate`；失败统一返回 `invalid Relay acknowledgement`，不会执行 `OnReady`。
4. 协议仍为 `v: 1`。新 schema 将字段声明为 optional，只能让新接收端接受旧消息，不能让已运行的旧接收端接受新字段。

### 1.2 Puck 测试误判

失败用例：`TestModulePersistsRealTurnDurationStepsUnreadAndReactions`。

1. 用例按 `messageId` 写入 reaction，最后却读 `history[0].Reaction`。
2. 新建助理会先创建 greeting；`clearPending` 不删除已持久化 greeting。
3. `Module.now` 固定时钟；用户消息未传 `CreatedAt`，Store 回退真实 `time.Now()`。
4. `Store.Messages` 按 `created_at,id` 排序；第一条不保证是目标用户消息。
5. 目标用户消息的 reaction 正确保存，但 `history[0]` 是无 reaction 的 assistant 消息。

这是测试定位错误。不能改生产 reaction 写入、强行改历史排序，或删除 greeting 来让断言变绿。

### 1.3 网络问题另行处理

本规格不修改代理、DNS、Clash、证书或 Git 全局配置。

## 2. 范围

- 修改 Relay 握手发送与契约测试、Puck 失败测试、Web 状态类型／归一化及必要测试辅助。
- 新 Agent 对过渡回包的接收兼容保留；不新增 capability、不升级协议 major、不迁移凭据或数据库。
- **本规格不授权公网 Relay／Web 部署、本机 Agent 替换或服务重启。** 只做代码修复 + 测试 + PR。

## 3. 推荐修复：收敛发送字段

1. 移除 Relay 向未协商 Agent／Web 的 `hello.ok` 附加 `puck`；`PuckState` 为空／off／ready／unavailable／degraded 时都必须满足旧契约。
2. 基础字段保持 `type`、`v`、`connectionId`、`relayEpoch`；`sessionToken` 仅按既有 Web session 条件发送，绝不发给 Agent。
3. 不以 `puckBrain` 等推断接收端能力。
4. Web 继续用 `GET /api/auth/status` 的 `puck`；修正 `web/src/auth.ts` 类型（wire 有 off／unavailable／ready／degraded），归一化：缺省或 off → UI disabled；ready → ready；unavailable／degraded → UI unavailable 并保留原因。未知非法不伪造 ready。
5. 不推荐仅改成 `role=web`（旧 Web 同样严格）。

### 3.2 接收端继续兼容过渡回包

保留 schema 对有界 optional `puckStatus` 的接收；已知 `puck` 仍严格校验；不为兼容而全局允许 unknown fields。

## 4. reaction 测试

按目标 `messageId` 在 history 中查找唯一消息再断言 reaction；保留耗时／步骤／未读等原断言；不删 greeting、不硬编码下标、不改 SQL 排序。

## 5–6. 不变量与验收

见完整用户规格：H1–H5、P1–P4、E1。旧契约从 `911a45b…` 冻结。

验证命令：

```bash
go test ./internal/puck -run '^TestModulePersistsRealTurnDurationStepsUnreadAndReactions$' -count=20
go test ./...
go test -race ./protocol ./internal/relay ./internal/puck ./internal/codex/desktopipc ./internal/desktopagent ./cmd/desktop-agent
go vet ./...
go build -trimpath -buildvcs=true ./cmd/desktop-agent
```

Web：`npm test`、`npm run build` 及相关 Chromium。

## 7. 实施入口

| 文件 | 动作 |
| --- | --- |
| `internal/relay/server.go` | 收敛 acknowledgement 发送形状 |
| `internal/relay/puck_status_test.go` 与旧契约 fixture | 覆盖 Puck 开启时旧 Agent／旧 Web |
| `protocol/puck_contract_test.go` | 保留过渡解码与负向校验 + 冻结旧契约 |
| `internal/puck/module_test.go` | 按目标消息 ID 验证 reaction |
| `web/src/auth.ts`、`client.ts`、`app.tsx` 及相关测试 | HTTP wire 类型与 UI 归一化 |

顺序：冻结旧契约并复现 H1／H2 → 最小修复发送端 → H3／H4／H5 → reaction 断言 P1–P4 → 全量／race／vet／构建 → E1。

## 8. 发布边界

本轮**不要**部署公网 Relay／Web，**不要**替换本机 Agent。只 commit、push、开 PR。交付记录含修复前后复现、旧契约来源、测试结果、commit；规格文档可写入 `docs/specs/0131-...md` 并保持 Draft／建议态，勿标 Implemented。
