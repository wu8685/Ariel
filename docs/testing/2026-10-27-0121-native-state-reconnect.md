# 2026-10-27：原生状态编码与打开会话重连故障

## 根因与实际验证

Issue #39；按用户直接授权的 [0121](../specs/0121-native-state-encoding-reconnect.md) 修复，不修改内存上限，不新增轮询／订阅或重放 mutation。

1. 真实只读 owner 探测在旧实现失败：`normalized snapshot bytes 67940311 exceed 67108864`。Desktop owner 存在，但 Reducer 未能建立可用 snapshot。
2. `json.Marshal` 默认把 `<`、`>`、`&` 转义为六字节序列，导致合法 JSON 在重编码后越过 resident limit；该数据仅在 JSON 传输内使用，不嵌入 HTML。
3. 真正超限被包装成 `ErrResyncRequired`，Web 将其显示为“事件顺序发生变化”；Follower 在关闭 IPC 的竞态中还可能把 Reducer 原因覆盖成通用 `ErrClosed`。
4. streaming projection 仅在超过 expanded resident limit 时启用，因此略低于 64 MiB 的兼容大帧仍保留已批准 lazy 的图片／tool payload。仅关闭 HTML 转义后的真实状态为 66,864,354 bytes，距上限很近。
5. 修复后对超过普通 8 MiB 快路径的大帧执行原有已验证 projection，未知正文不删除；不扩大可剔除字段。略过 detail 内部任意合法 `type` 值，不将其误当 envelope 字符串 enum。
6. 同一实际会话只读探测通过：owner found、snapshot seen，收到 1 个 snapshot 和 1 批 patch，state 为 **55,906,587 bytes（约 53.3 MiB）**，低于原有 64 MiB 上限。会话仍在增长，不将这份统计当成固定大小或全部设备的验收。

原始 snapshot、正文、真实 thread／owner ID 和本机路径均未保存进仓库。本机 Desktop 版本高于 profile 最低基线，探针仍输出 `unverified` 版本提醒；上述成功只证明该版本本次 owner read／follow，不替代发送、停止、审批及其他设备的兼容性验证。

## 实际 Red → Green

| 用例 | Red | Green |
| --- | --- | --- |
| snapshot／patch HTML 字符 | 不足 2 KiB 的 fixture 被转义成 5,448 bytes，Reducer 拒绝 | 非 HTML 转义编码，文本／控制字符／反斜线／精确大整数语义保持 |
| 容量分类 | envelope／snapshot／patch 超限只有 resync | 同时可识别 `ErrResyncRequired` 与 `ErrFrameTooLarge`，状态立即失效 |
| IPC Follow | 有效限额内原生 frame 因编码膨胀无法 attach | 真实 `net.Pipe` framing／Follow／patch 成功 |
| 关闭竞态 | Reducer 超限被 `IPC connection closed` 掩盖 | `Refresh` 优先返回已记录的 typed Reducer 原因 |
| Agent 错误映射 | resident overflow 显示 RESYNC_REQUIRED | 映射 HISTORY_TOO_LARGE，仍沿用现有安全只读行为 |
| 低于 resident cap 的大 payload | 9 MiB 图片和 tool 直接绕过 projection | 两类均剔除；未知 9 MiB 字段逐字保留，原 256 MiB wire cap 不变 |
| Web 失败说明 | stale cache 仍显示“事件顺序变化” | 中性实时连接提示；既有 watcher 原位 promotion，正文和草稿保留 |
| 真实 Agent→Relay→浏览器 | 注入可容纳的特殊字符 native JSON 后没有新正文，进入只读 | fixture 中仅替换 native owner，真实 Reducer／Service／sender／Relay／浏览器显示新正文，草稿保留、零额外 attach，无需刷新 |

浏览器 E2E 的 native fixture 将 limit 缩小以稳定复现同类容量比率，不向真实工作会话写入。底层 IPC 用例验证实际 framing 和 Reducer，不用预构造 Web cache snapshot 冒充 Adapter 验收。

## 回归与交付

- Web：40 文件、318 项通过；production build 通过，既有 chunk-size 警告不变。
- Go：`go test ./...`、`go test -race ./...`、`go vet ./...` 通过，包括 streaming projection 内存约束与未知大字段 fail-closed。
- 浏览器专项：4 项通过，覆盖本次编码故障、失效 follower 原位恢复、迟到失败 fencing、5 秒正文合并与即时终态。
- Chromium 全量：99 项通过，原有截图无变更；物理 iPhone／Safari 未验，不把 viewport／touch 称为物理设备测试。
- 实际发布另记 Release；升级公网 Relay/Web 也必须同步升级相关设备 Desktop Agent。当前可直接更新的本机不等于用户所有 Mac，其他设备未取得连接方式时不能宣称已部署。

保留设备身份、凭据、session key、只读容器、1 GiB 限制与回滚版本；看板关闭，Chilling 不动。
