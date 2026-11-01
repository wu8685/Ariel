# 0129：旧正文回收与 follower 自恢复验收

- 日期：2026-11-01；Issue #68；规格 [0129](../specs/0129-recent-native-state-and-follower-self-recovery.md) 已获用户确认。
- macOS／Apple M4，Go 测试、隔离 Unix IPC、production Agent／Relay 与真实 Chromium。没有向真实业务会话发送、停止、审批或回答。
- 本记录区分合成缺陷复现、真实 native 兼容检查与目标设备效果，不以其中一项替代另一项。

## Red → Green

| 行为 | 实际 Red | Green／测试入口 |
| --- | --- | --- |
| 回收窗口外正文而不重排原生地址 | 旧 follower 导出 52,429,453 bytes，仍含约 50 MiB 历史正文 | `recent_state_test.go` 的 production Follow 路径、50 MiB reducer 容量用例；最近两 turn／旧 ID 均保留 |
| 旧 turn 回滚成为最近内容时不能伪造空正文 | 旧实现仍返回可用视图，无法体现新回收契约 | 回收后回滚需 fresh baseline，绝不发布已回收正文为完整内容 |
| 单会话自动恢复 | 关闭 native follower 后，保持 Agent／Relay 连接，无 promotion；3 秒未收到后续 snapshot | `TestCacheNativeFollowerAutomaticallyRecoversWithoutPromotion` 收到新 stream 的终态 |
| 删除数组项真正释放引用 | 底层数组仍引用已删除对象 | `TestReducerRemovalClearsArrayBackingReferences` 验证清空尾槽 |
| 只读 timeout 不误报 mutation unknown | TIMEOUT 被分类成 OUTCOME_UNKNOWN；真实 response 也被 CallError outcome 覆盖 | `TestCacheRecoveryBackoffAndPermanentReasons`／`TestCacheReadTimeoutReplyNeverSuggestsMutationUnknown` |
| 回收子树也必须校验 patch path 类型 | false／null／负数／小数 path component 可绕过内部 marker | `TestRetiredPayloadDoesNotBypassPatchPathValidation` 必须失效并要求新基线 |
| canonical 共享实体保护 | 较早的 reference 先回收正文，随后最近 reference 被误判缺失 | `TestRecentWindowPreservesSharedCanonicalEntity` 先确定全部必要身份，再回收 |
| 取消发生在订阅完成与安装之间 | 留下 loading placeholder，后续 rewatch 永远复用它 | `TestCacheCancelledAttachDoesNotLeaveLoadingPlaceholder` 验证取消后清理自身占位 |

测试 fixture 最初的 response 字段、事件预算配置错误已先纠正，再取得上表有效 Red；不把 fixture 协议错误当业务缺陷证据。mutation pin 新 API 的首次测试还经历编译 Red，其后有正常行为与幂等释放断言。

## 原版本的完整浏览器复现

在独立临时 clone 固定 main `898b7c8`（v0.13.2），只复制新增的隔离 native peer／HTTP fixture，不修改旧运行逻辑，构建正常二进制；使用环境变量 `ARIEL_RECENT_FIXTURE_BINARY` 让**相同浏览器测试**指向旧二进制：

```sh
npm run test:ui -- e2e/native-recent-recovery.spec.ts --workers=1
```

结果为两项预期失败：

1. `residentBytes=54,002,512`，不满足回收后小于 2 MiB；这个数是 native JSON 表示，不是 RSS。
2. native revision gap 后 30 秒没有最终回复。浏览器、Relay 长连接没有被断开，也未点击 promotion。

新实现同两项通过：约 50 MiB 历史正文＋tool／image 的 wire frame 仍为 50–64 MiB，Agent 回收后投影小于 2 MiB；推送正文、完成状态和草稿正确。revision gap 后只多一次 native attach／load，无新增 WebSocket、页面导航、promotion 或 mutation。完整 156 项 Chromium 回归亦通过，覆盖文件阅读／原文／Diff／引用、文件 Tab 恢复、工具摘要、草稿、队列及认证恢复。

最初给 50 MiB 浏览器 fixture 加 `-race` 时，逐字节 parser 的 shadow memory 令 fixture 进程 RSS 约 1.8 GB，首次读取超过测试窗口。容量／时延浏览器验收改为正常 production build，**未扩大生产 timeout 或任何字节限制**；并发正确性使用独立 Go race 测试验证。50 MiB reducer 容量用例仍保留，真实完整 Unix wire 由正常浏览器 fixture 验证，不能把它们混称为 50 MiB 全链路 race 验收。

## 成本与内存

`BenchmarkFollowerRecentStatePatch` 同机、相同合成正文，3 次 iteration；仅测 steady-state reducer 小 patch，不代表网络／首次接入时延：

| 历史正文 | 旧实现 ns/op／B/op | 新实现 ns/op／B/op |
| --- | --- | --- |
| 1 MiB | 4,072,681／8,434,048 | 12,667／5,992 |
| 50 MiB | 192,894,722／274,059,592 | 13,625／5,992 |

关键不是用微基准承诺用户端倍数，而是旧正文增大不再导致每批小 patch 重建整份 JSON。未知 metadata、仍 active／pending／验证中的内容，以及旧身份索引仍有实际成本，不宣称恒定内存或无限历史。

独立测试二进制运行 50 MiB reducer 容量用例，`/usr/bin/time -l` 记录 maximum RSS 304,611,328 bytes（约 290.5 MiB），无 swap，wall 1.56 秒。包含 fixture JSON 构造、解码及瞬时副本，不是生产 Agent 总 RSS 或常驻状态上限；也不与 Relay 1 GiB 容器限额混用。

## 恢复及安全回归

- 确定性 timer 验证退避、取消、旧 timer 拒绝、显式 promotion 重开、三次无进展熔断；相同 snapshot 不重置预算，较新 owner revision／终态才恢复预算。
- 一个会话报告原生队列 OVERLOADED 时，另两个会话继续产出 seq=2 更新；只重建故障会话。容量 admission 失败不当作事件队列过载去无限重试。
- active／pending turn 不回收；写请求的必要历史在原生核对期间 pin，释放幂等，unknown 不重放。
- 已有 revision／owner／cwd／原生身份、大小／HTML 字符／未知 JSON、修改文件入口与文件权限测试继续有效；文件来源仍独立通过 App Server 只读获取，不依赖被回收正文。
- 首轮未留完整日志的关键 race 组合出现失败，截断输出不足以确定原因，不将其解释为网络问题或当作通过；之后启用完整 JSON 留档重新验收。恢复专项 race 20 次、独立 Agent 全量 race 已通过，最终冻结源码后的组合结果另记于下方。

## 真实 native 与未通过项

1. 当前超长业务会话仅做只读检查：普通 follower overflow 后 large retry 仍返回 `projected output exceeds 67108864`。reader 已拒绝，reducer 尚未得到基线；本规格不解决该首次入站限制，继续由 #64 跟踪。没有抬高限额或循环重试来掩盖它，也没有 exact 原始 wire 字节数可报告。
2. 已有隔离 Codex fixture 的只读 cold attach 通过，规范化最近两个 turn，导出约 11.5 KiB；只记录大小，不保存真实正文。未据此声称超限业务会话恢复。
3. 另一台 Mac 尚未直接访问／升级，运行版本、RSS、过载分类及实际效果仍为 unverified。物理 iPhone／Safari 未验，Chromium touch viewport 不冒充物理设备。

## 最终验收与发布

Go 全量／vet、命令构建与 Linux amd64 Relay 构建、Web 371 项单测、production build、156 项真实 Chromium 均通过。关键包组合 race：IPC 43.261 秒、Relay 3.589 秒、Agent 177.367 秒通过；最后取消分支修正后，又以最终源码跑完整 Agent race（154.916 秒）及缓存／恢复专项 race 20 次（23.891 秒）通过，完整 JSON 日志无 DATA RACE／失败事件。最终 Chromium 全量复跑仍为 156 项通过。

用户已授权发布本次修复，计划版本 v0.13.3；README 先更新，再合并、打 tag／push，最后更新本机 launchd Agent 与公网 Docker Relay/Web。发布前再跑回收／自动恢复的真实 Unix IPC→Agent→Relay→Chromium 用例，部署后核验容器 health、公开资源、PID／ready marker、连接及缓存发布；实际部署结果写入对应 GitHub Release，不将计划当作已完成。

另一台 Mac 未提供访问方式，不能由公网部署代替其 Agent 更新。当前超限业务会话的 #64 仍保持开放，不把本规格的阶段性通过写成目标会话故障已排尽；本次不合入未完成的异步回答或 SVG 分支。
