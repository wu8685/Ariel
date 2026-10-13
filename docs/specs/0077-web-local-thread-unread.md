# 0077：Web 本地会话未读标记

- 状态：Implemented（Web 195 tests、production build、真实移动 Chromium 29 tests 与截图已验；物理 iPhone／Safari 待用户复验）
- 来源：在 Codex 原生跨会话 unread transport 暂不可用时，用户确认先忽略“处理中”，仅在会话菜单展示由当前 Web 浏览器维护的 unread 蓝点。
- 关联：[0062 Web 端有界会话历史缓存](0062-bounded-web-history-cache.md)、[0067 侧栏会话运行与未读状态](0067-sidebar-thread-status.md)、[0070 Agent 只读请求自愈与设备会话列表缓存](0070-agent-read-timeout-and-thread-list-cache.md)。
- 范围：Web 本地已读水位、侧栏蓝点和有界批量刷新；不增加 Relay／Desktop Agent 协议，不接入 Codex 私有 App Tools pipe，不显示跨会话“处理中”。

## 语义与边界

1. 蓝点表示：同一浏览器 origin、同一 `deviceId + threadId` 的最新 `updatedAt` 晚于该浏览器最后一次成功查看的水位。
2. 这不是 Codex 原生跨端 read-state：在 Codex Desktop、另一台手机或另一个浏览器查看，不会清除当前浏览器的蓝点；清除站点数据后重新建立基线。
3. 首次观察某个 thread 时以当前 `updatedAt` 建立已读基线，不把已有历史会话全部标成未读。
4. 标题修改、置顶等由当前 Web 明确发起并取得 accepted 回执的 metadata 变化不得制造 unread。
5. `updatedAt` 缺失、非法或倒退时不产生 unread；能力异常时隐藏蓝点，不影响会话列表、历史与写操作。

## 已读与未读行为

1. 未选中 thread 的批量索引版本前进后显示一个可访问名称为“有新结果”的蓝点。
2. 选中 thread 只有同时满足以下条件才更新已读水位：
   - 第一份身份匹配的 snapshot 已渲染，或后续 live update 已渲染；
   - 页面为 visible；
   - 用户处于最新消息位置，而不是阅读旧历史；
   - device、thread 和当前 selection 仍一致。
3. 仅点击行、订阅尚未成功、页面 hidden、同步失败或用户停留在旧历史时均不得清除 unread。
4. 用户主动“回到最新”后，在当前内容完成渲染时清除 unread。
5. pinned 区、项目分组和搜索结果中的同一 thread 共用同一水位。

## 本地持久化与容量

1. 只在 `localStorage` 保存版本化结构：`deviceId`、`threadId`、已观察版本、已读版本和最近访问时间；不保存标题、cwd、正文、摘要、凭据或 Agent epoch。
2. 最多保留 1024 个 thread，序列化上限 256 KiB；超过限制按最近访问时间淘汰。解析失败、quota 异常或 storage 不可用时 fail open，仅不显示 unread。
3. 写入采用完整版本化对象替换；未知版本不迁移、不猜测，重新建立基线。

## 批量刷新

1. 继续复用现有 `thread.list`，每次刷新仍是一条批量请求，不按 thread 查询。
2. 首次列表、设备切换、手动刷新和会话列表重新打开时刷新；页面 visible、Relay ready、设备在线、非搜索且会话列表可见时，每 15 秒后台刷新一次。
3. 后台刷新不得重叠，不显示整页 loading，不清空现有列表；失败时保留现有列表和 unread，不弹阻塞性错误。
4. 页面 hidden、Agent 离线、搜索中或移动端侧栏关闭时停止定时刷新；恢复后允许一次立即刷新。

## TDD 与验收

1. Red：本地 store 测试覆盖首次基线、版本前进、已读、倒退／非法时间、设备隔离、1024 条淘汰、损坏／quota storage 降级。
2. Red：Web 测试覆盖未选中更新显示蓝点；仅点击、hidden、旧历史不清除；snapshot 渲染且 visible、回到最新后清除。
3. Red：metadata accepted 不产生 unread；pinned／项目／搜索重复行状态一致。
4. Red：15 秒刷新为单个 `thread.list`，慢请求不重叠，后台刷新不进入列表 loading；hidden／搜索／侧栏关闭时停止。
5. 真实移动 Chromium fixture 覆盖首次无蓝点、后台版本前进显示蓝点、打开并渲染后消失；断言关键几何关系且保存截图。物理 iPhone／Safari 留给部署后用户复验。
6. 运行 Web 全量测试、production build 和全部 Playwright 回归。

## 回滚

- 删除 Web unread store 与蓝点渲染即可；协议、Relay、Desktop Agent 和 Codex 会话状态均不受影响。
- 未来 Codex 提供经验证的原生批量 unread/read-state transport 时，另写规格迁移；不得把本地水位静默冒充原生状态。
