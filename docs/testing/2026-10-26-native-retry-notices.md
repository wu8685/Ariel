# 原生 retry／error 展示验收

- 关联：Issue #19、spec 0113；与 Issue #17 的输入框模型入口调整一起交付。
- 根因：`normalizeTurn` 未处理 native `error`，其 `message/willRetry` 被丢弃，未知类型 fallback 将每次重试显示为 `[error 项目]`。Web 原样逐条渲染。

## 原生兼容性证据

只读检查本机安装包，未修改真实会话或网络条件：

- Desktop `26.930.51102` / build `13100`。
- ASAR SHA-256：`a159b8f5b78ed1ba89fc70d5c8448d822a46c4fc2a4a9f18ec348f3cc2f6b8c9`。
- bundled `app-shared` 的 turn status 索引仅将 `type=error && !willRetry` 归为 latestTurnError；`remote-conversation-page` 把 error notification 转为含 `message/willRetry/errorInfo/additionalDetails` 的 item；thread-turn entries 另有 reconnect error 的折叠逻辑。
- 因此 `error` 并不等于最终失败。截图里的网络状态无法单独证明 Web↔Relay、Agent↔Relay 或 Codex 上游哪一段故障；本修复不改变连接行为。
- 原生网络故障未人为注入真实业务会话；兼容性证据来自已安装 bundle，端到端状态演进使用隔离 native fixture。

## Red → green

1. 先实际运行 Go 测试：重复 retry、恢复、terminal、最终错误、缺失／非法 flag、缺失 message、有界中文和图片省略均在旧代码失败。
2. App unit 在旧 UI 上因没有重试状态角色失败；实际 Agent + Relay + Chromium 用例在旧实现上同样失败，页面仍是 error 占位。
3. Adapter 合并当前 turn 中的 retry，保留最新有效项；native 进展或 terminal 消除它。最终错误和未知状态保留，不通过本地 timer 推断恢复。
4. 使用现有 system text 的固定可读前缀做展示分类，不添加 v1 Item 字段，不依赖新 capability。UI 分类仅决定颜色和可访问角色，不改变发送权限或状态来源。用户／assistant 文本不按此前缀分类。
5. review 补充失败用例：普通 plan 文本恰好与 retry 前缀相同也不能被删除。过滤改为跟踪由原生 error 明确产生的 item，而非扫文本删除。
6. 重试状态不会与“思考中”重复；错误原文按 literal text 展示，附加堆栈不转发、inline 图片数据省略、4 KiB UTF-8 安全上限。

## 验证结果

| 验证 | 结果 |
| --- | --- |
| Go 全量、vet | 通过 |
| Agent／Relay race | 通过 |
| Web unit | 32 文件、291 项通过 |
| 全量 Chromium | 58 项通过 |
| 跨层 retry 专项 | 实际 Agent + Relay + 手机 viewport；重试合并、恢复、最终失败、未知 flag、草稿、无横向溢出通过 |
| 截图 | 新增 retry 灰色／error 红色两张局部基线；原有基线未改 |
| production build | 通过；保留既有 Mermaid 大 chunk 提示 |
| 协议 | v1 schema 未变；规范化 error item 只含原有 itemId／role／text |
| 物理 iPhone／Safari | 待用户复验，不从 Chromium 结果推定 |

部署与发布的版本、进程、容器和公网验收结果在对应 Issue 记录，不持久化凭据或真实正文。
