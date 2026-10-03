# Codex Desktop 接入证据附件

本目录保存 2026-10-03 在原 macOS 电脑进行的实测记录。它不是新的运行环境，也不包含业务聊天内容、认证文件或完整应用源码。

| 附件                                                               | 用途                                   |
| ---------------------------------------------------------------- | ------------------------------------ |
| [Codex Desktop 会话接入实测](2026-10-03-codex-desktop-test-results.md) | 当时的实验报告脱敏版，保留测试版本和行为结果             |
| [App Server 探针源码](2026-10-03-codex-app-server-probe-source.md)   | 标准 App Server 历史读取与 fixture 创建探针脱敏源码 |
| [Desktop IPC 探针源码](2026-10-03-codex-desktop-ipc-probe-source.md) | Desktop 私有 IPC 发现、订阅、发送与停止探针脱敏源码     |
| [ASAR 只读检查脚本源码](2026-10-03-codex-asar-inspector-source.md)       | 只读读取已安装 ASAR 的定位辅助脚本                 |

## 使用限制

报告和源码均以 `.md` 文档保存；代码块不是可直接执行的交付物。原机器用户名、用户目录、默认业务聊天 ID、测试 fixture ID 与临时目录已替换为占位符。另一台电脑仍须改为显式配置和显式测试目标，再运行新探针。

probe 的 seed 会创建并执行测试会话，ipc-probe 的 exercise 会发送并停止任务。阅读归档文件不代表授权对任意真实会话执行这些操作。新探针应默认只读，写操作需明确开关，且仅限隔离 fixture。

原临时目录中的 test-thread.json 未复制，避免将原机器的 fixture 身份当作另一台电脑可操作的目标。原生成 schema 也未搬运；目标机器应通过其匹配版本的 `codex app-server generate-json-schema --help` 确认用法后重新生成。报告中的临时路径仅描述当时环境，已脱敏。

探针没有完整 patch reducer、健壮断线清理、所有请求的错误断言或生产级兼容机制。保留脱敏结构用于追溯；正式 Adapter 不能直接照抄上线。

回到 [规格入口](../README.md)，或查看 [[2026-10-03-codex-remote-adapter]]。
