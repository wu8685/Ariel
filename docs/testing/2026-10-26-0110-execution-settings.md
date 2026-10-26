# 0110：按会话模型、推理强度与速度验收

## 范围

- 规格：[0110](../specs/0110-conversation-model-effort-and-speed.md)。
- 原生组合：Desktop `26.930.51102`、bundled Codex `0.160.0`。
- 原生写入仅在探针创建并授权的临时 fixture 内进行；报告不保存真实 thread／owner ID、工作区绝对路径、正文快照或凭据。
- 未验证版本由 CLI 隐藏新扩展，原会话能力不受影响；物理 iPhone／Safari 不在本机 Chromium 验收结论之内。

## TDD 证据

各组测试先加入并实际运行 red，再实现 green：

| 行为／发现 | Red | Green 修复 |
| --- | --- | --- |
| 动态模型与合法组合 | 缺少 catalog、patch 与校验实现导致测试失败 | 动态 model／effort／tier ID、有界分页、模态与组合校验 |
| native 下一轮设置 | 缺少原生读写 Adapter 导致测试失败 | 版本化 owner-targeted mutation、精确 applied 检查、follower 回读 |
| catalog 共享／watch | Service 方法不存在，测试失败 | 设备级 TTL／singleflight、复用 hot controller、只推实际变化 |
| 旧 Web capability | 旧 device projection 泄漏新增字段 | 对旧投影剔除新 capability，模型方法显式 opt-in |
| 公共 wire | 新设置请求与事件被原 schema 拒绝 | 独立、严格、白名单设置契约 |
| Web 控件／发送保护 | 控件不存在、App 不发模型请求 | 本会话选择、保存期间阻止发送、保留正文和草稿 |
| native `null` effort | 空字符串导致 native CAS 永远不匹配；显式 null 未清空旧值 | 保留 null 条件与清空语义 |
| 元信息性能 | Reducer 不提供独立设置投影 | 在既有解码中抽取最多 4 KiB 白名单标量，不携带历史、权限或 developer instructions |
| 不可验证 watch baseline | 已接受但格式错误的订阅没有释放 | 显式 unwatch；不能把 transport receipt 当设置确认 |
| 多客户端编辑冲突 | 打开的编辑被静默 rebase 到其他客户端的新 revision | 记录打开面板时的 revision，冲突要求重新读取 |
| StrictMode 旧 Agent | 共用 pending catalog 时旧 Agent 错误出现设置入口 | catalog loader 无组件局部副作用，失败向所有 consumer 传播；旧截图不需更新 |
| 离线显示 | 断线后已确认设置入口消失 | 保留只读投影，禁止离线写入 |
| warm retire 内存 | 设置 generation 额外引用已关闭 native owner | generation 不保存 Live 引用，关闭后不保留大历史 |
| revision ABA | A → B → A 复用旧内容 hash | 同 generation 内递增版本，owner 替换使用新 generation |
| 版本边界 | 未定义设置能力的独立版本 gate | 只对经过真实验证的组合启用；提供 opt-out／回滚开关 |
| bootstrap → Desktop 交接 | 真实 Service 接管测试失败：首轮标准速度被全局 Fast 覆盖 | 仅对显式初始化意图在首次接管前确认 native 设置；普通重连不写入，并发模型／强度变更不覆盖 |

## 真实 Codex 验证

```sh
ARIEL_TEST_SETTINGS_WRITE=1 go test ./internal/desktopagent \
  -run 'TestRealConversationExecutionSettings|TestRealBootstrapExecutionSettings' \
  -v -count=1 -timeout=5m
```

1. 新建有 guard 的 no-tools fixture；模型与选项从真实 catalog 获取。
2. 用不同于 seed 的模型，分别写入 Fast、标准与恢复继承；匹配 native metadata，再验证实际下一 turn 的 model／effort／tier 参数。
3. 当前 turn 运行期间修改下一轮 effort；当前 turn 参数保持不变，排队消息执行时采用新设置。
4. 关闭并重新打开 follower，确认设置仍存在。
5. 在同一隔离 fixture 上运行真实 Relay、Agent 与 WebSocket：Web 请求设置更新，由 native owner 接受，再经 follower → Service → Agent → Relay 推送给 Web watcher。
6. 新会话通过真实 Service bootstrap 使用指定 model／effort／标准速度；原生 thread/start 回读、首轮完成，以及自动迁移到 Desktop owner 后的三项设置校验通过。
7. 写入后没有调用完整历史加载来等待设置确认，也没有修改全局配置。

## 浏览器与回归

- Web unit 覆盖每会话／草稿隔离、动态选项、图片兼容、native 推送、冲突、断线、错误订阅清理与 StrictMode。
- 真实 Chromium 的手机 viewport／touch 与桌面 viewport 覆盖面板宽度、滚动、焦点返回、保存禁发、正文／草稿保留、跨会话旧事件 fencing 与 catalog 只读取一次。
- 手机设置面板有固定 screenshot baseline；既有 UI baseline 未为本功能重录。
- Relay Go 测试通过真实 WebSocket 验证旧 Agent 不收到新方法、旧 Web 不收到新事件，以及断开时释放设置订阅。
- 最终全量结果在本记录的交付结果小节补充；开发中一次并行 suite 的真实 Relay fixture 启动探针超时，单独复跑通过，不将该次运行称为全绿。

## 交付结果

| 验证 | 最终结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| modelsettings／Desktop IPC／Desktop Agent／Relay／protocol `-race` | 通过 |
| Web unit | 32 个文件、274 项通过 |
| Web production build | 通过；保留既有非阻断 chunk-size 提示 |
| 真实 Chromium 回归 | 51 项通过（最终完整运行使用 3 workers） |
| 真实 Codex 隔离验证 | 两项 gated 测试通过；包含真实 Relay／Agent／native 双向链路与排队设置继承 |
| 物理 iPhone／Safari、其他 Codex 版本 | 待单独验证，不由 Chromium／当前版本结果外推 |
| 生产部署 | 本轮未执行 |
