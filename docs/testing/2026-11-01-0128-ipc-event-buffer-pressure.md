# 0128：IPC 事件积压修复验收

日期：2026-11-01。已批准 spec 0128；Issue #66。基于 main `088874f`，不包含异步问题 PR #65。

## 证据边界

用户提供另一台 Mac 的 146 MB 记录文件、约 40–50 MB 同步和多次 `OVERLOADED` 诊断。本轮核验的是代码及隔离合成链路，没有直接访问目标 Mac、读取其日志或更新其 Agent；不能宣称该主机已恢复。该问题与 #64 的 `HISTORY_TOO_LARGE` 分开，不因队列变大就声称单帧容量问题也已解决。

## Red → green

1. 先修改生产选项契约并添加暂停消费者的实际 IPC 测试。旧配置实际失败：1024 条小事件、2 × 40 MiB、2 × 64 MiB 三种合法突发均在排空前返回 `ErrOverloaded`。
2. 最小调整 production large profile 为 1024 条／128 MiB，三种突发全部按序排空；顺序 reader barrier 与之后的普通 RPC 正常返回。显式旧 65 条和 2 × 40 MiB 用例继续确认原故障能复现；新 1025 条和 3 × 48 MiB 超限仍 fail closed。
3. 共享池／释放／并发测试先因缺少 API 而编译失败，再实现 `EventByteBudget` 与 Client 的原子入队。以缩小的等价池测精确容量、无预留、跨 Client 拒绝且不影响既有连接、出队复用、重复 Close、Close／入队／出队竞争、初始化拒绝后清理。另用真实 128 MiB production 队列验证全额计费及排空归零。
4. `failLocked` 同时记录错误、清空队列、释放本 Client 的预算，避免已读取事件在 Close 后重新入队。容量错误用可 `errors.Is(ErrOverloaded)` 识别的安全类型；callback 在解锁与失效后执行，只携带容量指标。生产日志覆盖三种原因，不保留内容或真实身份。
5. 暂停 follower reducer，IPC reader 持续排队 128 批顺序 patches。旧 64 条 profile 明确失败；新 profile 中 RPC receipt 先正常到达，恢复后所有数组项顺序、最终回复、completed／idle 均正确。不新增 follow、complete-history 或 mutation；owner／revision gap 原有拒绝测试保留。
6. production large follower 仍共享现有 state admission 的 512 MiB reservation，8 个 nominal 64 MiB reservation 的既有测试不变；新事件池独立按实际排队计费，不把其上限冒充总体 RSS 限额。
7. 首轮 race 中，测试把 reader barrier RPC 发在整批大数据之前，race 的合成帧处理计入原有 30 秒生产 RPC timeout，三条大字节用例实际失败为 deadline exceeded，而非队列溢出。修正 fixture：在整批数据之后发送 discovery 控制帧并等待其回执，再发最多 5 秒的普通 RPC；生产 30 秒超时没有修改，条数／字节／顺序／超载断言均保留。长批次的测试传输保护 deadline 为 3 分钟，不把 race 插桩吞吐当作生产性能承诺。

## 合成内存测量

macOS、本机单个测试进程、非 race binary；同时包含合成发送端、真实 IPC Client／projection 和 Go runtime，不是独立生产 Agent RSS：

```sh
go test -c -o .local/0128-desktopagent.test ./internal/desktopagent
/usr/bin/time -l .local/0128-desktopagent.test \
  -test.run '^TestProductionLargeIPCBurstPreservesEveryQueuedEvent/128MiB-byte-boundary$' -test.v
```

- 真实保留并排空 134,217,728 queued bytes，2 条事件；共享池前后计量正确。
- 最终 fixture wall time 2.69 秒，maximum resident set size 356,040,704 bytes（约 339.5 MiB），peak memory footprint 346,637,104 bytes，0 swaps。
- 不能据此推算另一台 Mac、并发多个大会话、真实 reducer 或生产 Agent 的峰值。未进行真实五会话并发 native 验证，未调整已有 owner state／wire／Relay 限额。

## 回归命令

```sh
go test ./...
go vet ./...
go test -race ./internal/codex/desktopipc ./internal/desktopagent ./internal/relay
go test ./internal/codex/desktopipc -run 'TestEventBudgetClose|TestFollowerPaused' -count=30
go test ./internal/desktopagent -run 'TestProductionFollowerInitializationFailure|TestIPCEventPressure' -count=30
cd web
npm test
npm run build
npm run test:ui
```

## 最终结果与交付边界

- Go 全量、vet、Desktop Agent build 通过；IPC／Desktop Agent／Relay 全量 race 通过，Desktop Agent race 运行约 154 秒。
- 共享池边界、关闭竞争、连续 follower patch 专项 race 重复 30 轮通过；生产初始化失败清理／脱敏日志重复 30 轮通过。
- Web 371 单测、production build、完整 Chromium 154 项通过。其中真实隔离 Unix IPC → Agent → Relay → Chromium 用例确认初次正文、后续回复、completed／idle、发送门禁和草稿保留，无额外 attach／history load／mutation。此浏览器用例本身不是暂停大帧队列压力测试；字节积压由上面的独立真实 IPC 测试覆盖。
- 无新增 native API、UI、轮询、自动重放、持久会话数据或 Chilling 依赖；文档、构建产物和测试不代表任何服务已更新。
- 本轮仅提交／push 修复 PR，不打 tag、不合并发布、不重启本机／公网服务或其他 Mac。目标 Mac 的实际吞吐、峰值和断线频率仍为 `unverified`，需要升级其 Desktop Agent 后只读复验；如果持续超载，仍应看到明确错误，不能承诺单纯加缓冲能解决所有慢消费。

## 后续发布预检（2026-11-01）

用户随后明确要求部署。部署前先确认公网仍为 v0.13.1、Docker healthy、1 GiB／只读 rootfs，以及本机 launchd 身份与关闭看板的参数不变；旧实例未被预检重启。

新代码对本机当前会话执行生产 `OpenFollower` 的真实只读接入，约 10.28 秒完成 small → large 过程；完整 owner state 为 66,563,636 bytes，规范化 recent view 为两个 turn，大小校验通过。只在内存检查，不保存／上传正文或真实 ID，不做 native mutation。它证明此次检查时该会话可冷接入，不能证明此前 #64 的唯一原因，也不能替代另一台 Mac 的验证。后续合并、tag 与双端更新由本次部署授权执行；实际完成状态以发布验收为准，不从本节预检推定。
