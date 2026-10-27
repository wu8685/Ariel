# 文件能力指南已迁移

当前文件能力已内置到 Ariel Desktop Agent，不再要求启动 Chilling backend。请使用[内置文件能力配置](file-context.md)；本页保留旧链接的迁移入口。

- 新配置使用 `--enable-file-context`，目录与图片分别显式开启。
- 旧 `--enable-chilling-file-context`／`ARIEL_ENABLE_CHILLING_FILE_CONTEXT` 仅是兼容别名，`ARIEL_CHILLING_ENDPOINT` 已不参与文件链路。
- Chilling 仅是可选看板／任务扩展，不是会话、模型设置或文件阅读的运行前提；不要为迁移文件功能删除或停止现有 Chilling 数据与任务服务。

历史设计与当时验收仍留在原记录中，现行归属与安全边界以[规格 0119](../specs/0119-self-contained-desktop-file-context.md)为准。
