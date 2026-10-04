# Ariel 规格索引

从本文件开始，Ariel 的研发文档只在本仓库维护。brain-spark 中的 `codex-remote-control` 目录视为历史归档，不再作为后续修改目标。

## 状态定义

| 状态 | 含义 |
| --- | --- |
| Draft | 正在讨论，不允许开始实现 |
| Approved | 用户已确认，可以进入 TDD |
| Implemented | 规格对应实现和测试已经完成 |
| Superseded | 已被新规格取代，保留供追溯 |

## 当前规格

| 编号 | 文档 | 状态 | 目标 |
| --- | --- | --- | --- |
| 0001 | [M0 兼容性探针](0001-m0-compatibility-probe.md) | Approved（第 5 项保留 TODO） | 在当前 Mac 上确认公开 App Server 与 Desktop IPC 的真实能力 |
| 0002 | [审批拒绝语义](0002-approval-denial-semantics.md) | Approved（command/file 已实测；权限请求待真实样例） | 区分拒绝操作与拒绝并停止本轮，按原生可用决定呈现 |
| 0003 | [M1 最小链路](0003-m1-relay-mock-web.md) | Implemented（同机链路已验；物理手机待验） | Relay、Mock Agent 与响应式 Web 的单用户局域网链路 |
| 0004 | [M2 真实 Desktop Agent](0004-m2-real-desktop-agent.md) | 实施中（用户授权连续推进） | 原历史、原 owner、自动加载、发送与精确停止 |
| 0005 | [M3 原会话交互响应](0005-m3-interactions.md) | 实施中（用户授权连续推进） | 审批与补充提问，按原生请求和结果确认 |
| 0006 | [M4 断线恢复与有界容量](0006-m4-recovery-capacity.md) | 实施中（用户授权连续推进） | 重连、权威状态重建、慢消费者与大历史 |
| 0007 | [Night 界面配色](0007-night-appearance.md) | Draft（待用户确认） | 深色背景、白色正文、蓝色高亮与各状态可读性 |

## 设计基线

`baseline/` 是 2026-10-03 从 brain-spark 迁入的 v0.1 设计与证据快照。它定义产品目标、架构、协议草案、Adapter 边界、里程碑和验收矩阵。后续如有冲突，以最新的 `Approved` 规格为准。

M1–M4 当前实测与未验边界见 [2026-10-04 测试记录](../testing/2026-10-04-m1-m4.md)。

## 规格拆分顺序

1. M0：兼容性探针与支持矩阵。
2. M1：自有 WebSocket 协议、Relay 与 Mock Adapter。
3. M2：历史读取、Desktop 实时订阅、发送与精确停止。
4. M3：审批与补充回答。
5. M4：断线恢复、有界资源与真实手机验收。
