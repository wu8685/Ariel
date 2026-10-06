# 0025 最低版本兼容门禁测试记录

- 日期：2026-10-06
- 当前环境：macOS `14.8.7`、ChatGPT Desktop `26.930.51102`、内置 Codex CLI `0.160.0`
- 最低版本：Desktop `26.930.31730`、Codex CLI `0.160.0`
- 范围：版本门禁、证据标记、运行期兼容错误、只读真实探针和 LAN 启动；未对业务会话执行写操作

## TDD

1. Red：原精确白名单拒绝更高 Desktop／CLI，且 native shape 错误只返回通用 `RESYNC_REQUIRED`。新增测试实际失败。
2. Green：增加数字／语义版本比较和三态证据；等于最低值为 `verified`，高于最低值为 `unverified`，低于、缺失、畸形或低于正式版的预发布为 `unsupported`。
3. Green：启动器、Desktop Agent、owner probe 与 fixture probe 统一调用最低版本检查；高版本只警告，不阻断。显式 binary 与 bundle 的精确一致检查保持不变。
4. Green：native method／schema／IPC protocol 错误显式映射为 `PROTOCOL_UNSUPPORTED`；已有 lost receipt 测试仍确认写调用只发送一次、结果保持 `unknown`。

## 自动验证

- `GO111MODULE=on go test -race ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `npm test`：13 个文件、97 项测试通过。
- `npm run build`：通过；Vite 仅报告既有的单 chunk 大小提示。Node `24.13.0` 低于部分间接依赖声明的 `24.15.0`，npm 给出 engine warning，但安装、测试和构建均成功。
- race 模式下，既有超大 live stream 测试实际需要约 1.5 秒完成；只将测试等待预算从 1 秒放宽到 5 秒，超限后返回 `HISTORY_TOO_LARGE` 的产品行为未改变。

## 当前 Desktop 与 LAN 验证

- `probe detect`：`ipcProfileStatus=unverified`，最低版本字段正确，bundle binary 匹配，socket 存在且 IPC initialize 成功。
- `probe history`：两页只读历史请求成功，未输出或保存会话正文。
- Relay 在当前 LAN 地址监听，`/healthz` 与页面均返回 HTTP 200；Desktop Agent 与 Relay 建立连接，并记录“高于最低版本、未逐版本验证”的提示。
- 当前 Codex 执行环境会清理脱离命令会话的后台子进程，因此本次用两个受控前台会话完成进程存活验证；仓库启动器和 managed-process 生命周期的自动测试通过。该限制属于本次执行宿主，不作为 Ariel 后台启动行为的失败证据。
