# 0040：兼容 bundled Codex 的原生置顶分区

- 状态：Implemented（Adapter TDD、bundled Codex 0.160.0 只读探针、Web 单测、390×844 Chromium 截图、全量 race tests 与 production build 已验；物理 iPhone/Safari 待用户复验）。
- 修复：0036 只读取较新 App Server 的 `isPinned` 字段；本机 bundled Codex 0.160.0 实际通过保留的 `Pinned` thread section 保存置顶状态，因此 Web 永远收不到置顶标记。
- 原则：Codex App Server 仍是 SSOT；Ariel 不读取 Desktop 私有数据库，也不建立自己的置顶数据。

## 兼容读取

1. 优先尝试较新 App Server 的 `thread/list` + `isPinned: true`，保留前向兼容。
2. 若服务端拒绝该筛选，或忽略筛选且没有返回可靠的 `isPinned: true`，改用 bundled 0.160.0 已公开在生成 schema 中的 thread section 协议：
   - `thread/list.sectionId = 01984de2-8f74-7c91-a3b2-5c5e937cf318`（Codex 保留的 `Pinned` section）；
   - `sortKey = section_position`，保持 Codex 侧栏原始顺序；
   - `useStateDbOnly = true`，只读 App Server 状态库，不扫描或修改 rollout。
3. section 结果中每个会话都必须明确返回同一 `section.id`；否则 fail closed，不把普通会话误判为置顶。
4. 两种原生能力均不可用时，仍降级为普通会话列表，不阻断导航。
5. 沿用 0036 的首屏 100 条上限、去重、项目分组移除和搜索行为。

## 验收

1. Adapter 单测先失败，再验证现代字段路径、0.160.0 section fallback、原生顺序与错误边界。
2. 对本机 bundled Codex 做只读探针：只记录置顶数量、section ID 和字段集合，不记录标题、正文或 thread ID。
3. Web 单测与真实浏览器 fixture 显示“置顶”区域位于项目之前，且置顶会话不会重复进入项目。
4. 全量 Web tests、production build、Go race tests、`go vet` 通过；推送后重启本地 Ariel，并报告 LAN 地址与 6 位连接码。

## 非目标

- 本次不新增 Ariel 自有的置顶／取消置顶写操作。
- 不展示普通自定义 section，也不改变 Codex 中的置顶顺序。
