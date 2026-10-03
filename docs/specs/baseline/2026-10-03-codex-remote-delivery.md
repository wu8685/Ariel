---
title: "Codex 远程交互系统实施与验收"
created: 2026-10-03
tags: [codex, remote-control, delivery, testing]
type: experiment
source: "[[2026-10-03-codex-remote-requirements]]"
project: "Codex 远程交互系统 待命名"
---

# Codex 远程交互系统实施与验收

## 交付边界

本轮交付是知识库中的 spec 和既有验证材料，不是已经可运行的三端系统。实际代码将在另一台 macOS 电脑的独立 Monorepo 中开发，项目名和目录尚未确定。

当前 spec 是 v0.1：已确认需求可作为实现约束；通信字段、技术栈和降级方式是明确标记的提案。开始写依赖私有接口的实现之前，必须在目标电脑复验，特别是手机审批与补充回答。

## 建议工程结构

建议 Relay 和 Desktop Agent 使用 Go，Web 使用 TypeScript；前端框架在建项目时确定。以下路径均相对于未来代码仓库，不是在 brain-spark 中创建服务代码。

```text
<project>/
  cmd/relay/                   Relay 可执行入口
  cmd/desktop-agent/           Agent 可执行入口
  internal/relay/              注册 路由 订阅 心跳
  internal/agent/              生命周期 并发控制 归一化
  internal/codex/              HistoryReader 和 DesktopController 接口
  internal/codex/desktopipc/   版本化私有 IPC Adapter
  internal/codex/mock/         可重复测试的模拟后端
  web/                        响应式页面
  protocol/                   自有协议 Schema 与测试样例
  tests/fixtures/             脱敏协议快照和审批样例
  tools/probes/               显式目标 读写隔离的兼容性探针
  docs/specs/                 规格迁移后的代码侧位置
  docs/compatibility/         目标版本复验记录
```

业务协议只有一份机器可读定义，生成或验证 Go 与 TypeScript 类型，避免手工维护两套不一致字段。不要为首版引入数据库、Redis、消息队列或多服务编排平台。

## 配置与运行约定

建议配置项：

| 组件 | 参数 |
| --- | --- |
| Relay | listenAddress、token、allowedOrigins、心跳与容量上限 |
| Agent | relayURL、token、deviceId、deviceName、codexBinary、codexHome、ipcSocket |
| Web | 从当前页面来源推导 Relay 地址；用户输入 token，页面内存保管 |

配置示例使用占位符，不提交真实 token、Codex 认证文件和用户会话。Agent 的稳定 deviceId 属于配置，不是业务持久化；重启生成 agentEpoch。用户本机已登录的 Codex 负责实际模型访问。

最终 README 应给出：构建命令、两进程启动命令、Web 访问地址、macOS 权限与防火墙检查、常见错误、停止服务方式。可以先支持手动启动，不要求后台守护进程安装器。跨电脑复制 spec 不等于迁移原电脑会话。

## 里程碑

| 阶段 | 实施内容 | 退出条件 |
| --- | --- | --- |
| M0 兼容性探针 | 检测目标版本；复验历史、owner、发送和停止；验证自动加载、审批和回答 | 有准确支持矩阵与 fixtures；未通过的能力明确，不谎报完成 |
| M1 最小链路 | Relay 双向连接、设备状态、Mock Adapter、Web 会话列表与详情 | 手机经 Relay 看见 Mock 会话，完成鉴别、切换和消息路由 |
| M2 真实续聊 | 历史读取、IPC 订阅、文字发送、渐进回复、精确停止 | 真实已有会话在两端同步，同一 threadId，无多余 executor |
| M3 手机交互 | 审批卡片、补充问题表单、请求生命周期、两端同时处理 | 常用审批允许与拒绝、选项与自由回答、过期请求均通过 |
| M4 故障恢复 | 断线重连、进程重启、提交未知、限流和容量、响应式细节 | 验收矩阵通过，可在目标局域网持续手动使用 |

M1 的 Mock 开发可以与 M0 未决接口研究交错进行；真实控制和审批不得在兼容性未知时假装实现。M2 只是中间成果，手机审批和回答属于首版必要能力，不能在 M2 后宣称 MVP 已全部完成。

自动加载旧会话是能力探针，不要求先做手机页面；手机点选旧会话的完整体验属于 M2。若自动加载失败，需按需求文档讨论并记录“在电脑先打开”的降级。

## 验收矩阵

所有执行类测试用隔离 fixture，不向用户正在工作的聊天注入测试指令。每一项都记录环境版本、输入、观察到的证据与结果，不能只写“页面看起来正常”。

| 编号 | 场景 | 验收标准 |
| --- | --- | --- |
| A01 | 两端连 Relay | Agent 与 Web 都主动发起连接；电脑没有为手机开放控制端口 |
| A02 | 会话列表 | 至少跨两页，同名会话可按项目和时间区分，过滤范围表述准确 |
| A03 | 原有会话 | 同一 threadId 历史可读、可续聊；不是偷偷创建副本 |
| A04 | 未加载旧会话 | 自动加载成功并可控，或明确 needsDesktopOpen；不能只读却显示可发送 |
| A05 | 双向同步 | 手机发送和 Desktop 直接发送都能在已订阅 Web 看见 |
| A06 | 渐进回复 | 同一 item 随流更新，无重复段落；完成、失败、停止状态分开 |
| A07 | 发送确认 | Relay 收到不显示已发送；Codex 接受后带真实 turnId |
| A08 | 忙时草稿 | 不自动排队；结束不自动发送；两 Web 同时发不产生隐形队列 |
| A09 | 精确停止 | 只能停止 expectedTurnId；旧按钮不能停止新 turn |
| A10 | 命令与文件审批 | 足够的命令/cwd/diff 上下文；允许和拒绝均到达原请求 |
| A11 | 权限请求 | 展示真实申请范围，不把单次授权扩大为长期或全局权限 |
| A12 | 补充回答 | 原问题、选项、自由输入和多问题映射正确，不代选或丢答案 |
| A13 | 请求竞争与过期 | 电脑/手机先处理都只生效一次；旧卡片不能再次提交 |
| A14 | 待审批时断线 | 重连重新取 live requests；已处理不复活，仍待处理可继续 |
| A15 | 丢失响应 | 发送/审批发出后丢响应显示 unknown，不自动重试或重复执行 |
| A16 | Relay 与 Agent 分别重启 | 无数据库亦可重建视图；旧 epoch/stream 不混入，待处理请求重新确认 |
| A17 | Desktop 退出与重启 | agentOnline 和 codexReady 分开，不自动拉起 Desktop，不显示假在线 |
| A18 | 缺失 patch 与慢消费者 | 发现缺口或缓冲溢出后重同步；内存有界，不继续展示伪一致状态 |
| A19 | 大历史与大 diff | 完整显示或明确超限；不静默截断历史，不在关键信息缺失时允许审批 |
| A20 | 未支持交互与协议升级 | 可见错误、合理降级，未知方法不执行，不绕过审批 |
| A21 | 基础连接鉴别 | 错 token/Origin/重复 Agent/畸形与超大帧被拒绝；正文不落日志 |
| A22 | 手机真实使用 | 手机浏览器竖屏、键盘弹出、滚动、切后台再回来均可恢复；不承诺后台通知 |
| A23 | 无业务持久化 | Relay/Agent 不生成会话库、离线队列或正文日志；重启后视图来自 Codex |

运行中自动继续、原生 Desktop 自己的队列、特殊 turn 状态若与本协议冲突，记录为兼容性差异，不用 UI 掩盖。并发保证需要来自真实 owner 行为，而不是只测 Mock。

## 自动测试分层

- 单元：IPC 拆包/粘包、长度限制、响应关联、patch reducer、revision 缺口、规范化、审批映射和过期检查。
- 协议：Go/TypeScript 共用样例校验；结果 unknown 与 rejected 区分；不认识的字段和版本处理。
- 集成：Mock Agent 连接 Relay，两个 Web 同时订阅、写入、断开；队列与内存上限。
- E2E：真实 macOS Desktop、真实手机 LAN；涵盖发送、停止、审批、回答和断线恢复。

原始探针源码是调查参考，不是自动测试替代品。正式测试必须断言实际结果；当前原始 probe 部分路径只记录失败而没有完整断言，不能只以退出码判断所有检查通过。

## TODO

- [ ] 确定产品名、仓库名和代码目录。➕ 2026-10-03
- [ ] 确定 Go 与 TypeScript 提案及前端框架，选版本并写 lockfile。➕ 2026-10-03
- [ ] 在目标 Mac 完成 M0，尤其是未加载会话、手机审批与补充回答。➕ 2026-10-03
- [ ] 对 M0 无法自动加载的结果，确认手动打开的首版降级是否可接受。➕ 2026-10-03
- [ ] 根据真实审批 Schema 定稿 interaction 映射与能力矩阵。➕ 2026-10-03
- [ ] 讨论并确认自有协议、容量上限及特殊 MCP 请求范围。➕ 2026-10-03
- [ ] 代码项目建立后明确 spec 唯一维护位置，知识库保留索引或版本快照。➕ 2026-10-03
- [ ] 公网部署前单独安排安全加固，当前不做公网发布。➕ 2026-10-03

## 给下一台电脑上的开发 Agent

> 先完整阅读本目录 README 及五份 spec，区分已确认需求、实现提案和未验证能力。先检查本机 macOS 与 Codex Desktop 版本，读取本机项目指令。不要把独立 App Server 的历史访问误当作 Desktop runtime 接管；不要复用附件里的固定 threadId、owner 或机器路径。Relay 与 Desktop Agent 不建业务数据库，二者均通过主动连接 Relay 组织通信。首版包括已有会话续聊、停止、手机审批和补充回答，不包括新建会话。先做 M0 与 Mock，再做纵向链路；未通过审批和回答不能宣称 MVP 完成。项目名和代码目录未定时先请用户指定，不要在知识库中搭正式服务项目。

## 相关规格

[[2026-10-03-codex-remote-requirements]]、[[2026-10-03-codex-remote-architecture]]、[[2026-10-03-codex-remote-protocol]]、[[2026-10-03-codex-remote-adapter]]。
