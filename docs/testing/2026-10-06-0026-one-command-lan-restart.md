# 0026 一键局域网重启测试记录

- 日期：2026-10-06
- 范围：默认网卡私有 IPv4 发现、预检顺序、配置与凭据不变量、受管进程重启、真实 LAN 只读验收
- 数据边界：未打印 token／PIN，未输出会话标题或正文，未执行任何 Codex 写操作

## TDD

1. Red：`restart-local` 命令、私有 IPv4 发现、仅更新网络配置和可注入重启编排均不存在；新增测试实际编译失败。
2. Green：覆盖 `10/8`、`172.16/12`、`192.168/16`，拒绝公网、loopback、link-local、IPv6、畸形地址、无默认网卡和系统命令失败。
3. Green：覆盖未初始化、remote 模式、目标端口占用、部分受管状态、预检先于停止、配置在 start 前落盘，以及 token／PIN 字节不变。
4. Refactor：抽出 `buildConfigured` 与 `startConfigured`，让普通 `up` 和 `restart-local` 共享同一 Desktop 检查、构建、进程、ready file 与回滚边界。

## 自动验证

- `GO111MODULE=on go test -race ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `npm test`：13 个文件、97 项测试通过。
- `npm run build`：通过；Vite 仅报告既有的单 chunk 大小提示。

## 真实局域网验收

- 在已经运行的 local 配置上执行 `GO111MODULE=on ./scripts/ariel.sh restart-local`，自动发现当前默认网卡的私有 IPv4，并沿用保存端口。
- 构建完成后按 Agent→Relay 顺序停止旧进程；新 Relay／Agent PID 均与重启前不同。
- 重启前后 Agent token 与 Web PIN 字节相同；设备 ID、设备名称和端口保持不变。
- `status` 显示 Relay 在线、Agent 已握手且 Codex 就绪；HTTP health 返回 200。
- WebSocket `hello`、`device.list` 和只读 `thread.list(limit=10)` 成功，设备 `agentOnline=true`、`codexReady=true`，历史有下一页。
