# 0054：面向 Agent App 的中立产品定位

- 状态：Implemented（145 个 Web 单测、11 个真实 Chromium 用例、品牌规范截图与 production build 已验）。
- 范围：根 README、公开架构入口、Web metadata、PWA Manifest、品牌规范、安装指南和规格索引中的产品范围说明。
- 不涉及：运行时协议、当前 Codex Adapter、历史规格、兼容性证据和测试记录中的事实表述。
- 覆盖：[0051 面向使用者的精简 README](0051-readable-project-readme.md)中的 Codex 专用定位和架构入口。

## 问题

当前 README 将 Ariel 定义为“在手机浏览器中接续 Mac 上的 Codex Desktop 会话”，Web metadata 与品牌规范也把产品直接绑定到 Mac／Codex。macOS + Codex Desktop 是当前唯一已经实现和验证的 Adapter，但不是 Ariel 的长期产品边界；把实现现状写成产品定义，会无意中限制后续接入其他桌面平台和 Agent App。

## 用户结果

首次接触 Ariel 的读者应先理解它是“浏览器与桌面端 Agent App 之间的远程会话中继器”，然后清楚看到当前发行版只支持 macOS + Codex Desktop。定位保持开放，支持声明保持诚实。

## 文档分层

1. 根 README、Web metadata、PWA Manifest 与品牌场景文案使用 provider-neutral 的产品定位，不在标题或一句话介绍中绑定 Mac／Codex。
2. 新增公开架构总览，使用 Browser → Relay → Desktop Agent → Adapter → Agent App 的分层模型；目标 Agent App 保持会话和执行状态的 SSOT。
3. README 单列“当前支持范围”，明确当前唯一已实现组合为 macOS + Codex Desktop，并说明这是实现矩阵而不是产品边界。
4. 快速开始、安装指南、兼容性文档继续准确写明当前 Adapter 的 macOS／Codex 前提，不暗示其他平台已经可用。
5. 历史 specs、baseline、测试记录和兼容性证据保持原文；其中的 Mac／Codex 是当时真实目标和证据，不做追溯性改写。
6. 品牌规范把六条路线解释为多个 Agent App 会话端点，不改变 Logo 几何、路线数量、资产或使用规则。

## 架构边界

1. Web 与 Relay 只依赖 Ariel 自有协议，不直接调用某个 Agent App 的私有接口。
2. Desktop Agent 通过 Adapter 将统一会话操作映射到具体 Agent App；不同平台或产品的接入应新增 Adapter，而不是把 provider 特例扩散到 Web／Relay。
3. 当前代码和 v1 协议仍有 `codexReady`、`internal/codex` 等实现耦合；公开架构须把它们标记为后续泛化项，不能把目标架构写成已经完成。
4. 新 Adapter 必须独立声明能力、版本门槛、安全权限和失败语义，不能从 Codex Adapter 的实测结论外推兼容性。

## 验收

1. 文档测试先约束 README 的一句话定位为“在浏览器中完成与桌面端 Agent App 的远程会话。”，并确认现有文案失败。
2. README 和新架构总览同时包含 provider-neutral 架构与 macOS + Codex Desktop 当前支持矩阵。
3. Web title、description、Open Graph、Twitter 与 PWA Manifest 的产品定位不再包含 Mac／Codex。
4. 品牌规范和展示样例不再把六条路线限定为多个 Codex App，但路线数量与批准资产不变。
5. 所有 README 相对链接可解析；运行文档专项测试、Web 全量单测和 production build。

## 实现结果

1. README 的一句话定位已改为“在浏览器中完成与桌面端 Agent App 的远程会话。”，核心说明和 Mermaid 架构使用 Browser → Relay → Desktop Agent → Agent App Adapter → Agent App 分层。
2. README 新增当前支持矩阵，明确 macOS + Codex Desktop 是当前唯一已实现组合，不是产品边界；快速开始和安全边界继续如实描述当前 Codex Adapter。
3. 新增 `docs/architecture/overview.md`，定义通用 Adapter 职责、不变量、能力子集、当前矩阵和仍未完成的 provider 解耦工作；README 不再把早期 Codex 专用 baseline 作为长期架构入口。
4. Web title、description、Open Graph、Twitter 与 PWA Manifest 已统一为 “Ariel — Agent 联络中继器” 和 provider-neutral 描述。
5. 品牌规范中的六条路线已改为多个 Agent App 会话端点；Logo 资产和路线数量未改变，真实浏览器截图基线已人工检查并更新。
6. 安装指南、兼容性报告和规格索引增加当前 Adapter 范围说明；历史 specs、baseline 和测试证据保留原有 Mac／Codex 事实，不做机械替换。
7. GitHub 仓库 description 和 homepage 当前为空，没有遗留的 provider-specific 外部文案需要修改。
