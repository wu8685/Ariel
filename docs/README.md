# Ariel 项目文档

[返回项目介绍](../README.md)

README 用于快速了解项目；这里按使用目的组织详细说明。当前实现为 macOS＋Codex Desktop，其他平台和 Agent App 不在已验证支持范围内。

## 开始使用

| 你要做什么 | 阅读入口 |
| --- | --- |
| 了解功能、使用方式与限制 | [功能与使用指南](guide/features.md) |
| 在 Mac 安装，或把 Agent 连接到已有 Relay | [安装、配置与排障](operations/agent-install.md) |
| 开启文件浏览、原文／Diff、阅读模式和图片 | [内置文件能力配置](operations/file-context.md) |
| 在服务器部署 Relay＋Web | [Docker 部署](operations/container-deployment.md)／[ECS 一键部署](operations/ecs-single-node-deployment.md) |
| 配置公网认证与安全边界 | [Passkey 部署](operations/public-passkey-deployment.md) |
| 使用 Kubernetes | [Kubernetes 部署](operations/kubernetes-deployment.md) |

## 理解系统

- [架构总览](architecture/overview.md)：组件职责、多路复用连接、增量数据流、缓存与认证边界，以及当前支持矩阵。
- [Codex 兼容性报告](compatibility/2026-10-03-m0.md)：私有接口的能力与实测边界；历史最低版本结论不能替代当前版本验收。
- [探针使用](compatibility/probe-usage.md)：只读诊断与隔离 fixture；不要向真实工作会话做写测试。

## 开发与维护

- [协作规范](../AGENTS.md)：规格、TDD、浏览器验收、发布与安全要求。
- [功能规格索引](specs/README.md)：按已批准规格理解行为；Draft 不代表已实现。
- [测试与验收记录](testing/)：保留每次验证的环境、结果和未验项，不将旧结果视为对所有版本的保证。
- [开发待办](TODO.md)：跟进计划与尚未完成的能力。

## 可选扩展与历史文档

Chilling 仅承载可选看板／任务业务；Ariel 会话、模型设置和文件能力不依赖它。当前 Web 不提供看板入口，已有任务集成说明仅供维护者按需参考：[任务方案](specs/0091-chilling-task-board-and-background-routing.md)／[Obsidian 任务源](operations/obsidian-task-source.md)。

[旧 Chilling 文件指南](operations/chilling-file-context.md)保留迁移指引。历史规格、兼容性实验和验收记录按当时事实保留；现行文件归属以 [0119](specs/0119-self-contained-desktop-file-context.md) 为准。
