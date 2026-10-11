# 2026-10-11 侧栏状态与会话操作兼容性探针

## 环境

- ChatGPT Desktop bundled Codex CLI：`0.160.0`
- 本机 PATH Codex CLI：`0.145.0-alpha.27`
- `codex-app-tools` bundled MCP server：`0.1.5`
- 探针只读取 schema、进程身份、工具目录握手结果；未修改真实会话。

## 结论

### 可启用：归档／撤销归档

bundled `0.160.0` 的稳定 App Server schema 包含：

- `thread/archive`：参数仅含 `threadId`，成功返回空对象；
- `thread/unarchive`：参数仅含 `threadId`，成功返回恢复后的 `thread`。

Ariel 使用 public App Server 调用，并在写入后分别读取 active 与 archived 索引。只有两边都证明状态已经切换时才返回 accepted；否则返回 unknown。实现中没有面向用户的 `thread/delete` 路径。

隔离探针在临时目录创建一条无 turn 会话，以 `thread/inject_items` 使其形成可归档的持久化 fixture，完成 archive → 双索引验证 → unarchive → 双索引验证，最后仅由测试清理逻辑删除 fixture；全程没有启动模型 turn。

### 不可启用：public App Server 置顶写入

当前 bundled `0.160.0` 的 `ThreadMetadataUpdateParams` 只有 `threadId` 与 `gitInfo`，没有 `isPinned`。因此不能按 `thread/metadata/update {isPinned}` 实现置顶；发送 schema 外字段会把未验证行为冒充成稳定能力。

只读 `thread/list` 仍可读取现有 pinned section，所以既有置顶展示不受影响。

### 暂不可启用：跨会话 active／unread 批量状态与 mark-read

Codex App 内部工具目录确实包含批量 thread 状态和 read-state 能力，但其 native pipe 是为已经由 Desktop 启动的单个 MCP 子进程分配的连接。对同一已占用 socket 启动第二个经过 schema 校验的 `codex-app-tools` client 时：

1. MCP initialize 成功；
2. 首次 `tools/list` 返回 `Codex app tools pipe closed`；
3. 没有取得工具目录，也没有执行任何会话读写。

这说明当前 Desktop 没有向独立 Ariel Agent 暴露可安全复用的 App Tools endpoint。直接接管已有 MCP 子进程的 stdin/stdout、结束原进程抢占 socket、读取 Desktop 内存或修改 SQLite 都会越过进程所有权和安全边界，因此没有采用。

## 产品降级

- `threadArchive`／`threadArchiveUndo` 可上报并启用；归档只对当前已订阅且 live snapshot 明确为 idle 的会话开放。
- `threadPin`、`threadStatus`、`threadReadState` 不上报；Web 不显示虚假的置顶操作、spinner 或未读蓝点。
- 当前会话本身仍由既有 live owner snapshot 展示运行状态；会话列表、历史、发送和队列不受影响。

## 后续解锁条件

满足下列任一条件后重新执行隔离探针：

1. public App Server schema 正式加入 `isPinned` 与全局 unread/read-state；
2. Desktop 为第三方 Adapter 提供多 client 或显式授权的 App Tools transport；
3. Codex 提供不依赖 Desktop 私有 pipe 的批量全局 thread 状态接口。

探针未通过前，不以逐会话查询、数据库扫描或抢占 Desktop 子进程作为降级方案。
