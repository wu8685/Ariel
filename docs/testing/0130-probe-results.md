# 0130 Puck：P0 第 0 步写探针结果（2026-11-03，Mac mini，codex-cli 0.160.0）

环境：用户 Mac mini（`wu8685`），Codex Desktop 内置 `codex-cli 0.160.0`（`/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex`），模型 `gpt-5.6-sol`，effort low；共用用户 `~/.codex`（未用独立 `CODEX_HOME`）。全局配置（只读）：`approval_policy="never"`、`sandbox_mode="danger-full-access"`。
所有 app-server 由探针以 `<codex> app-server -c notify=[] -c model_reasoning_effort="low" [...] --listen stdio://` 启动（`-c`／`--disable` 只作用于本进程，未改 `config.toml`）。工作目录仅 `~/ariel-probe-0130/{brain-cwd,exec-cwd}`；未触碰任何既有 thread，未进入用户仓库。
探针脚本与原始结果未入库（保存在维护者工作区 `ariel-0130-probe/`：`scripts/harness.py` 为 app-server JSON-RPC stdio 客户端，`logs/*.result.json` 为原始结果）。

## P1 常驻 brain + dynamicTools 往返（PASS）
- `thread/start {dynamicTools:[ariel_echo], developerInstructions, sandbox:read-only, approvalPolicy:never}` → turn 中收到 `item/tool/call {tool:"ariel_echo", arguments:{text:"ping"}, callId:"exec-…", namespace:null}`，回复 `{contentItems:[{type:"inputText",text}],success:true}`，模型复述 `MARKER-7Q3Z`。
- kill 进程 → 新进程 `thread/resume` → 再次调用工具成功（`MARKER-R2D9`）：**工具随持久 thread 保存**（state DB 有 `thread_dynamic_tools` 表）。
- **重要发现**：不带参数的 `thread/resume` 返回 `sandbox: dangerFullAccess`，并且**仅 resume 就把 state DB 的 `sandbox_policy` 改写为 `{"type":"disabled"}`**（P4 再次确认）；`approvalPolicy` 则保留 thread 自身值。即 sandbox 不随 thread 持久，resume 时回落到全局默认。

## P2 Q27 Desktop 可见性与并发
- 普通 thread：`source:"vscode"`、`originator = clientInfo.name`，写入 `~/.codex/sessions` rollout 与共享 `state_5.sqlite`；Desktop 所用的 `thread/list`（`useStateDbOnly:true`）**能列出**它。
- `threadSource:"chatgpt_hidden"`：被持久化（DB 列 `thread_source='chatgpt_hidden'`；`thread/read`、`thread/resume` 均返回该值）；Desktop 侧边栏构建代码（app.asar）对 `threadSource===chatgpt_hidden`、有 `parentThreadId`、`ambient_suggestions`、`pull_request_fix_automation` 及 source `exec` 返回 null。
- **但** 0.160 的 `thread/list` 响应里所有 thread 的 `threadSource` 均为 `null`（`useStateDbOnly` true/false 都一样），而 Desktop 列表用的就是 `thread/list`。因此 hidden thread 是否真的从 Desktop 侧边栏消失**无法由协议层确认**——若 Desktop 只依赖 list 字段，它会显示出来。需用户肉眼确认一次（UI 未打开，按约束）。
- `ephemeral:true`：无 rollout（`path:null`），不在 list 中，但新进程 `thread/resume` 报错 `no rollout found` → 不能作为 brain（丢失恢复能力）。
- `serviceName`／`originator`：Desktop 不按此过滤。
- 并发：Desktop 运行期间两个进程共用 `~/.codex`，全部 stderr 日志无 `locked`／`SQLITE_BUSY`／conflict；thread 创建、resume、delete 均正常。（只证明短时低频无冲突。）
- 独立 `CODEX_HOME`：未测试（需复制登录凭据、刷新令牌轮换风险，且不必要）。

## P3 Q28 关闭 Brain 内置本地能力
默认进程：模型可用 `exec_command`／`write_stdin`／`apply_patch`／`view_image`／`image_gen`／`web.run`／goals／MCP 资源／`collaboration.*`（子 agent）／`mcp__cua_repl` 等；让它跑 `pwd` → 出现 `commandExecution "/bin/zsh -c pwd"` 并成功（即 read-only 沙箱下 Brain 仍能执行读命令）。
二分结果（同一提示：先调 `ariel_echo` 再尝试 `pwd`）：
| 变体 | 动态工具 | shell |
|---|---|---|
| A `--disable shell_tool --disable unified_exec` + `web_search="disabled"` + `include_apply_patch_tool=false` + 4 个全局 MCP `enabled=false` | ✅ | NO_SHELL，无 commandExecution |
| B = A + 关 view_image/image_generation/goals/browser_use*/computer_use/in_app_browser/sleep_tool/multi_agent | ✅ | NO_SHELL |
| C = B + `--disable code_mode_host` | ❌（模型看不到 ariel_echo） | NO_SHELL |
| D = B + 关 apps/plugins/tool_suggest/skill_search | ✅ | NO_SHELL |
结论：**可以只留动态工具**，推荐 D 组合；**不能关 `code_mode_host`**（0.160 中动态工具经它暴露，关掉即失效）。模型自报工具列表里仍有 `functions.exec`（code mode）、`request_user_input`、`collaboration.*`（关了 multi_agent 仍自报，未实测能否 spawn）——属残余面，需在实现时以版本门禁 + 事件审计（出现 `commandExecution`／`collabAgentToolCall` 等即告警并中断）兜底。

附带发现：Ariel Agent 进程环境里 `ARIEL_DEVICE_NAME` 是非 UTF-8 字节串，被继承后 codex 的 shell 工具 `std::env` unwrap **panic**（"shell tool crashed"）。Agent 启动 app-server 时必须清洗环境（丢弃非 UTF-8 值或修正该变量编码）。

## P4 Q23 执行 thread 策略
- `thread/start {cwd:exec-cwd, approvalPolicy:on-request, sandbox:workspace-write}` → 返回 `workspaceWrite{writableRoots:[],networkAccess:false}`；DB `sandbox_policy` 为受限文件系统（root 只读、cwd/tmp 可写、`.git`/`.codex`/`.agents`/`.aws` 只读），`approval_mode=on-request`。
- turn：`touch inside.txt` 成功；`touch ~/ariel-probe-0130/outside.txt` 被沙箱拒绝（文件不存在），模型未发起提权审批请求（serverRequests 为空）。
- kill → 新进程裸 `thread/resume`：`approvalPolicy:on-request`（保留），`sandbox:dangerFullAccess`，DB 立即改写为 `disabled`。
- 再 resume 后在 `turn/start` 携带 `approvalPolicy:on-request` + `sandboxPolicy:{type:workspaceWrite,…}`：沙箱重新生效（inside OK、outside FAILED），DB 恢复为 workspace-write 受限策略。
- **含义**：任何不带沙箱参数的客户端恢复（包括 Desktop 自动加载，与 2026-10-04 m1-m4 观察一致）都会把该 thread 的沙箱降到全局 `danger-full-access` 并写回持久设置；逐 turn `sandboxPolicy` 有效且会成为新的持久值。审批策略在 thread 上持久。
- Desktop IPC follower `start-turn` 接管场景：见 P6；已运行隔离 fixture，但无法取得兼容读回，结论为 **unverified**。

## P6 Q23 Desktop follower 显式 policy（UNVERIFIED，fail closed）
- P0 实施第 0 步新增受 manifest／guard／临时 cwd 三重保护的 `probe fixture execution-policy`。探针只接受自己刚创建的 `purpose=execution-policy` fixture，发送一条要求字面回复且禁止工具的 turn；wire 显式携带 `approvalPolicy:on-request`、`approvalsReviewer:user`、`sandboxPolicy:{type:workspaceWrite,writableRoots:[fixture cwd],networkAccess:false}`，并要求从 follower owner 的 `latestThreadSettings` 读回一致设置。
- 实机环境：Desktop 26.930.51102、Codex 0.160.0；fixture 创建与 seed 通过，Desktop follower policy probe 返回 `IPC protocol incompatible`，未取得可证明 `on-request + workspaceWrite` 生效的 owner 读回。该结果不能区分 Desktop 拒绝显式字段与当前私有 follower profile／快照结构不兼容，故结论是 **unverified**，不是“策略已失效”。
- 决策：按 spec §4.4 第 4 条和 B10 fail closed。当前版本不得凭 `executionPolicy` 能力自动派发或续投；无法完成逐 turn policy 强制并读回时返回 `EXECUTION_POLICY_UNENFORCEABLE`，创建 B10 单次授权卡片。后续 Codex／Desktop 版本只有在该探针重新通过后才可开启自动派发。
- 清理：探针通过 `thread/delete` 删除唯一 fixture thread，删除 manifest 与 `mktemp` cwd；state DB 中该临时 cwd 记录为 0；探针命令启动的 app-server 均随命令退出。未读取、resume、归档或写入任何既有 thread，未修改全局 Codex 配置。

## P5 Q25 时延（热进程，trivial turn）
| 样本 | turn/start→首个 agent delta | →turn/completed |
|---|---|---|
| 1 | 2543 ms | 2674 ms |
| 2 | 2352 ms | 2928 ms |
| 3 | 1545 ms | 1750 ms |
含一次动态工具调用的 turn 约 12 s。冷启动（resume 后首轮）未单独计时。

## 清理
- 12 个探针 thread 全部 `thread/delete` 成功；之后 list（含 archived）为空，state DB 中 cwd 在探针目录或 originator=ariel_probe 的行为 0，`~/.codex` 下无对应 rollout 文件。ephemeral thread 无持久化。
- 启动过的全部 app-server（18 个）均已退出；`pgrep ariel-probe|ariel_probe` 无结果。
- `~/ariel-probe-0130/` 与临时 tgz 已删除；未创建独立 `CODEX_HOME`；未改 `config.toml`。
- 可能残留：Codex 自身日志库（如 `logs_*.sqlite`）里的运行日志行，未处理。

## Q27 目视确认（2026-11-03 22:06–22:2x UTC+8）
- 新建 `ariel-probe-hidden`（`01a1a1bb-9196-7452-b54f-30b0d728565e`，`threadSource:chatgpt_hidden`）与对照 `ariel-probe-visible`（`01a1a1bb-b88f-7f42-9d9c-7d20b4f39c3a`），read-only + never，各跑一轮 "ok"。
- 用户在 Codex Desktop 中确认：**两个都出现在侧边栏** → 0.160 下 `chatgpt_hidden` 不能隐藏。用户接受为 v1 已知外观行为。
- 清理：两者 `thread/delete` 成功，`thread/read` 报 not loaded，state DB 0 行，无 rollout；`~/ariel-probe-0130` 已删，无探针进程。
