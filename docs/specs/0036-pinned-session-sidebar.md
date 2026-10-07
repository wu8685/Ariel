# 0036：置顶会话独立展示并从项目分组去重

- 状态：Implemented（自动测试、production build、官方文档与 bundled Codex 0.160.0 只读兼容检查已验；0.160.0 尚不暴露 `isPinned`，升级到支持该字段的 bundled Codex 后自动生效）。
- 前置：0032 以会话 `cwd` 生成项目分组；0034 让项目默认收起。
- 原则：Codex 仍是置顶状态和排序的 SSOT，Ariel 只读取并投影，不建立自己的置顶数据。
- 接口依据：[Codex App Server 官方文档](https://developers.openai.com/codex/app-server) 的 `thread/list.isPinned` 与 thread `isPinned` 字段。

## 输入与兼容适配

1. Desktop Agent 通过官方 App Server 的 `thread/list` + `isPinned: true` 读取置顶会话，并以 `updated_at` 倒序展示；Web 协议只暴露统一的可选布尔字段 `isPinned`。
2. Adapter 同时解码普通 `thread/list`／`thread/search` 返回的 `isPinned` 字段；只有原生字段明确为 `true` 时才认定置顶。
3. 若当前 bundled Codex 不支持 `isPinned` 筛选或返回项缺少可靠的正向标记，普通会话列表仍必须可用；Adapter 不猜测置顶状态，也不读取 Desktop 私有数据库。
4. 初次加载最多读取 100 个置顶会话；超过该有界容量时返回显式容量错误，不无限分页或建立本地数据库。

## 用户可见行为

1. 存在置顶会话时，左侧菜单最上方显示独立的“置顶”区域，位于所有项目分组之前。
2. 置顶区域内保持 Codex 返回的顺序；置顶会话可直接点击进入，不需要先展开项目。
3. 已进入置顶区域的会话不得再次出现在它所属的项目分组中；项目会话计数也只统计未置顶会话。
4. 项目选择器仍从全部可见会话推导项目目录，因此某项目即使当前所有会话都已置顶，也仍可用于“新建会话”。
5. 搜索时只对当前搜索结果做同样的置顶优先与项目去重；不把不匹配搜索词的置顶会话强行混入结果。
6. 没有置顶会话时不显示空的“置顶”区域，现有项目分组行为保持不变。

## 分页、去重与状态边界

- 首次非搜索列表请求把原生置顶结果与普通最近会话页合并；相同 `threadId` 只返回一次。
- Web 追加后续页时继续按 `threadId` 去重，防止原生普通分页再次包含已预取的置顶会话。
- 置顶状态不写入 LocalStorage，不提供 Ariel 自有的置顶／取消置顶操作；刷新时重新从 Codex 获取。
- 会话选择、订阅、发送、队列、项目展开状态和历史分页语义不变。

## 错误处理

- 置顶筛选返回“方法不存在”“参数不支持”，或疑似忽略筛选且结果缺少 `isPinned: true` 时，降级为普通列表，不阻断会话导航。
- 其他原生错误按既有 `thread.list` 错误边界返回，避免展示可能错误的置顶状态。
- 任何缺少可靠原生证据的会话按未置顶处理。

## TDD 与验收

1. App Server 单测验证置顶筛选使用 `isPinned: true`、`updated_at` 与倒序，并拒绝缺少正向置顶标记的结果。
2. Desktop Agent 单测验证首屏置顶优先、与普通页按 `threadId` 去重、unsupported 降级和有界容量。
3. 协议生成测试验证 `Thread.isPinned` 为可选布尔字段，旧的 live snapshot 仍兼容。
4. Web 单测验证置顶区域先于项目、置顶会话不重复进入项目、项目计数正确、后续分页去重且 pinned-only 项目仍出现在新建会话选择器。
5. 使用本机 bundled Codex 0.160.0 做只读验证，只记录能力与数量，不记录真实会话正文、标题或 ID。
6. 全量 Web tests、production build、Go race tests 和 `go vet` 通过后，推送并重启本地 Ariel，以 LAN health endpoint 验证部署。

## 非目标

- 不在 Ariel 中新增置顶、取消置顶或拖拽置顶顺序的写操作。
- 不展示 Codex 的其他自定义 sidebar section。
- 不改变项目身份仍由规范化完整 `cwd` 决定的规则。
