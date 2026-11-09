# 0126 app-server stderr 本地诊断 sink

- 状态：Approved
- 日期：2026-11-09

## 规格

- Codex app-server stderr 写入本机 user cache 下的 Ariel diagnostics；不进入 Relay、Web、数据库或网络。
- Agent 进程内所有 app-server 共用一个 sink，单文件上限 5MiB，保留 3 个轮转文件；目录权限 0700，文件权限 0600。
- 按行处理，疑似 `token`、`secret`、`password`、`api key`、`Authorization` 与 `Bearer` 值替换为 `[REDACTED]`。
- 单行最多保留 16KiB，超出部分截断，避免异常 stderr 形成大内存 buffer。
- sink 初始化或写入失败不得阻止 app-server 启动；失败时降级为 discard。
- 不改变 app-server stdout JSON-RPC、公开协议或上传行为。

## 验收

- 测试验证敏感值不出现在文件中。
- 小尺寸 fixture 验证轮转文件数量和单行截断有界。
- appserver package race test 通过。
