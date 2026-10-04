# MVP 验收矩阵逐项审计（2026-10-04）

依据 [基线 A01–A23](../specs/baseline/2026-10-03-codex-remote-delivery.md)、[M1–M4 实测](2026-10-04-m1-m4.md)与[浏览器回归](2026-10-04-browser-regression.md)。`通过`仅代表对应范围有直接证据；`部分`表示实现或测试存在，但验收场景尚未完整复现。本表不是功能完成声明。

| 编号 | 结论 | 当前证据与缺口 |
| --- | --- | --- |
| A01 两端主动连接 | 通过（本机） | Relay／Agent 与 Web 已在当前实例联机；Relay 未向手机开放 Desktop 控制端口。 |
| A02 会话列表 | 通过（本机浏览器） | 真实列表从 50 条加载至 100 条；同名 fixture 行同时显示 cwd 和时间。 |
| A03 原会话续聊 | 通过（隔离 fixture） | 同一 threadId 的历史、发送及原持久化结果已核对。 |
| A04 未加载自动接续 | 通过（隔离 fixture） | Web 点选后 deep link 自动加载原 owner；不需电脑端手动打开。 |
| A05 双向同步 | 部分 | 手机路径的 Web→Desktop 已实测；Desktop UI 直接发送→Web 尚无实际 UI 操作证据。本任务尝试读取 Codex Desktop 界面时，Computer Use 明确拒绝操作 `com.openai.codex`，因此不能代替用户完成该 UI 动作。 |
| A06 渐进回复 | 通过（隔离 fixture） | Web 看到运行中到完成／停止变化；稳定 item ID 与无重复段落有回归测试。 |
| A07 发送确认 | 通过（隔离 fixture） | 原 owner 中精确核对 turnId、clientId 和文本；缺证据时返回 unknown。 |
| A08 忙时草稿／无隐形队列 | 部分 | 两个 WebSocket 客户端经测试 Relay／Agent 同时启动，严格一次 accepted、一次 `TURN_BUSY`；真实 Desktop 双客户端并发实验出现不可寻址占位，仍缺原生成功路径证据。 |
| A09 精确停止 | 通过（隔离 fixture） | Web 发 expectedTurnId，Desktop 原 turn 中断；旧 turn 拒绝的测试存在。 |
| A10 命令／文件审批 | 通过（隔离 fixture） | 允许与拒绝已走真实 owner；文件字节与拒绝后不存在均独立核对。 |
| A11 权限请求 | 未完成真实验收 | 协议、Adapter 和 UI 测试存在；当前 Desktop 的 `request_permissions_tool` 未启用，无原生待审批样例。 |
| A12 补充回答 | 通过（隔离 fixture） | 两问题、选项／自由文本、原 owner 回显和持久化终态已核对。 |
| A13 请求竞争与过期 | 部分 | 双 IPC 客户端竞争和本地 stale 判定已实测；Desktop UI 与手机同时处理未实测。 |
| A14 待处理时断线 | 通过（隔离 fixture） | 真实 user-input 在 Agent、Relay 分别重启后仍可提交；命令与文件待审批卡片也分别经过独立 Relay／Agent 重启，从 owner live request 恢复。拒绝后卡片消失，独立历史确认终态；文件拒绝后目标文件不存在。 |
| A15 丢失响应 | 通过（分层故障注入） | 真实 Desktop 隔离 fixture 中，发送与命令拒绝均在 owner 已确认后注入丢回执，Agent 报 unknown 且不重放；独立 follower 核对消息身份或请求消失。真实 WebSocket 测试覆盖 Agent 断线后 Relay 报 unknown、不重放；Web 测试覆盖草稿／审批卡片保留至 owner 更新。不是物理手机单次端到端定点丢包实验。 |
| A16 Relay／Agent 重启 | 通过（隔离 fixture） | 同页保留选择并重订阅，旧视图清空；待回答问题恢复有真实证据。 |
| A17 Desktop 退出／重启 | 部分 | 健康检查和子 App Server 退出已测；承载业务会话的 Desktop 未做破坏性退出实验。 |
| A18 patch 缺口／慢消费者 | 通过（自动／loopback） | IPC revision 缺口与 Web stream 序号缺口均触发重同步，不沿用旧视图；Follower 失效、缓冲容量和超时有自动回归。真实 loopback WebSocket 背压注入中，慢 Web 被写超时关闭并释放订阅，另一个订阅者仍收到快照。未把该测试称为物理弱网验收。 |
| A19 大历史／大 diff | 部分 | 超限显式错误和审批上下文有自动测试，普通真实文件 diff 已验；补测发现直接审批曾绕过快照容量门禁，现已修复并证明超大 diff 不会到达 owner。极大真实历史／diff 未端到端复现。 |
| A20 未支持交互／协议升级 | 部分 | 除 Schema／未知决定测试外，真 WebSocket 证明未来版本 Agent 握手被拒且不假在线；Web 证明未支持交互有说明而无决定按钮。跨真实 Codex Desktop 版本升级未做。 |
| A21 基础连接鉴别 | 通过（本机及自动测试） | PIN／Agent 口令隔离、Origin、重复 Agent、帧上限和锁定均有测试；浏览器连接真实 Relay。 |
| A22 物理手机 | 部分 | 用户已确认同 LAN、手机 PIN 登录和设备出现；完整历史、侧栏、输入、审批、键盘、后台恢复待复验。 |
| A23 无业务持久化 | 通过（当前 Ariel 进程） | Relay／Agent 源码无会话库、离线队列或正文日志写盘；当前两进程均无可写普通文件 FD，stdout／stderr 指向终端。Codex 自身的原始历史持久化不属于 Ariel 副本。 |

## 下一步的真实门槛

1. [0011 手机 UI 规格](../specs/0011-compact-mobile-conversation-header.md)仍是 Draft。用户确认后才能按 SDD/TDD 实施，随后需浏览器与物理手机复验。
2. 物理手机完整操作需要用户在手机上执行或提供可操作的设备会话；不能用 390×844 桌面视口替代。
3. Desktop UI 主动消息、双端竞争和 Desktop 重启需要隔离 fixture 与不影响业务会话的操作窗口。当前不能把双 IPC 客户端异常实验当成成功证据；Codex Desktop UI 控制在本任务被工具安全边界拒绝，A05 的 UI 主动消息需用户在隔离 fixture 中手动发送并观察 Web。
4. 原生权限请求需要可用的 Desktop 能力或版本；不擅自开启全局 feature，不把协议测试当成真实审批通过。

其余 `部分` 项仍可用有界故障注入、自动测试及隔离 fixture 继续推进；每次须记录具体输入与可核对结果。

A23 的运行时检查是 2026-10-04 对当前 Relay／Agent 进程的 `lsof` 快照，不能证明未来版本或所有依赖都绝不会写文件；源码边界仍需随变更回归。
