# 0024：侧栏搜索原始 Codex 会话

- 状态：Implemented（用户在 2026-10-06 明确授权；自动测试、真实 Codex 只读搜索和本机部署已验证，物理手机触摸操作待用户复验）。
- 背景：Codex App Server 0.160.0 的生成类型包含 `thread/search`，结果为 `thread`、`snippet` 和 opaque `nextCursor`；本机只读调用已返回合法空结果。该方法需要 `experimentalApi`，Ariel 现有 App Server 连接已启用。公开文档列出的 `thread/list.searchTerm` 仅匹配标题，不足以覆盖消息内容，因此不用它替代完整搜索。

## 用户可见行为

1. 已连接时，左侧会话列表上方有紧凑搜索框，手机和桌面都可使用。输入关键字后暂停约 250 ms，Ariel 在当前选中设备的原始 Codex 会话中搜索；不只筛选已加载的最近 50 条。空查询显示原有最近会话。
2. 搜索结果沿用会话标题、目录与更新时间，并显示 Codex 提供的匹配摘要。摘要只按普通文本显示，不解析 HTML／Markdown，也不写入持久存储或日志。点击结果仍按现有流程自动加载原始会话，并在手机收起侧栏；搜索状态不干扰已打开的会话和草稿。
3. 每页最多 50 条。搜索结果有下一页时，用“加载更多”继续取同一查询的 opaque cursor；清空、修改查询、切换设备、重新连接或刷新时，不得混入旧查询／旧设备的迟到响应。清空搜索后恢复最近会话列表。
4. 搜索仅覆盖当前设备的未归档、交互型原始会话，保持与当前列表一致。是否命中及排序以本机 Codex `thread/search` 为准；Ariel 不复制、索引或长期保存会话正文。

## 输入、输出与协议

- Web→Relay→Agent：复用 `thread.list`，可选 `searchTerm`，去掉首尾空白后长度 1–128；无此参数走原 `thread/list`，有值走 Codex `thread/search`。`limit` 1–100、`cursor` 是当前查询专用的 opaque 值。Relay 只路由并校验，不解析搜索语义。
- Agent→Web：原 `threads`／`nextCursor` 响应；搜索结果的每个 thread 可带 `searchSnippet`（最长 400 字符，纯文本）。Agent 不把 turn/item 正文或未经截断的大结果附在列表项上。
- Codex 调用：`thread/search`，`sortKey: updated_at`、`sourceKinds: [cli, vscode, appServer]`、`archived: false`，沿用原索引视图的范围。结果经现有 `NormalizeStored` 转为 Ariel Thread；不得写回原始会话。

## 错误与边界

- 输入为空只加载原列表；超过 128 字符不发送超长请求。无匹配显示“没有找到匹配会话”，不误报设备离线。搜索中显示进度，失败保留已显示结果，明确提示可重试。
- 本机 Codex 不支持 `thread/search` 或拒绝 experimental API 时，显示“当前 Codex 版本不支持会话内容搜索”；不悄悄降级成仅标题匹配。普通最近列表仍可用。
- 切换设备、查询或重新连接后，旧响应不得覆盖新结果；分页请求不得使用其他查询的 cursor。搜索摘要属于会话内容，沿用现有 Relay 认证与传输限制；不进入浏览器 storage。
- 搜索可能扫描较多历史：Relay 对搜索列表请求给原生调用至多约 35 秒，Web 等待 40 秒；普通会话列表仍用原短超时。

## TDD 与验收

1. Red：App Server adapter 测试 `thread/search` 参数、结果／摘要、游标和无效输入；Desktop Service／Agent 测试搜索结果归一化、截断和不支持错误。
2. Red：协议测试 `searchTerm` 边界；Web 测试查询 debounce、内容命中摘要、空结果、清空恢复、搜索分页、迟到响应隔离、错误和移动侧栏点选。
3. Green 后跑 Go 与 Web 全量测试、构建；用当前本机 Codex App Server 的隔离 fixture 做只读搜索验证。物理手机视觉与触摸操作由用户复验。

## 实测记录（2026-10-06）

- 本机 Codex App Server `0.160.0`：未启用 `experimentalApi` 时拒绝 `thread/search`，启用后返回符合生成类型的分页结果；用隔离历史中的唯一消息标记搜索命中 2 条，未打印或保存消息正文。
- Go adapter、Desktop Service、Relay、Mock Agent、Web 与协议均经历 red→green；全量 Go race／Web 测试和 Web 构建通过。`go vet ./...` 通过。
- 一键脚本重建并重启本机 Relay／Agent，`status` 显示双方就绪；LAN 地址直连 HTTP 200。未执行物理手机视觉／触摸复验。
