# 0072 新会话首屏最近四回合测试记录

- 日期：2026-10-11
- 规格：[0072 新会话首屏最近四回合](../specs/0072-four-turn-initial-history-window.md)
- 范围：Desktop Agent 首屏 recent window、Web 自动补齐目标与手动旧页 page size；不写真实会话。

## Red → Green

先修改测试并实际运行，失败结果分别证明：

1. live owner 的 12-turn fixture 仍返回 `turn-03`…`turn-12` 共 10 个；
2. stored history 首次仍向 App Server 请求 `limit: 10`；
3. Web 在已有 1 个 turn 时仍用 `limit: 10` 自动补齐，而不是只请求缺少的 3 个。

实现把 `recentTurnLimit` 改为 4，并新增独立的 10-turn history page limit。转绿后：

- live owner 与 stored history 都只返回 `turn-09`…`turn-12`；
- 分段首屏按实际缺口请求，本 fixture 为 `limit: 3`；
- 手动“加载更早消息”仍为 `limit: 10`。

## 自动与浏览器回归

- Go 全包：通过；
- `go vet ./...`：通过；
- `go test -race ./...`：通过；
- Web：23 个测试文件、187 项测试通过；
- production build：通过；
- Playwright：新增 390×844 隔离 WebSocket fixture，验证 `[3, 10]` 两次请求边界；完整第二轮 26/26 通过。

第一轮完整 Playwright 中既有“returning to latest restores the chrome once without oscillation”滚动时序用例单次失败；该用例单独重跑通过，第二轮完整 26/26 也通过。新增 4-turn 用例两轮均通过。物理 iPhone／Safari 的实际加载耗时留待公网部署后由用户复验。
