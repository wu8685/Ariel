# Streaming 正文默认 5 秒合并验收

- 关联：Issue #15、spec 0100 §4.1；用户明确指定将原有 1 秒可调默认值改为 5 秒。
- 不改变 IPC／wire、双 turn cache、连接数量或 owner authority；不新增轮询。

## Red → Green

1. 先添加并实际执行生产默认间隔与虚拟时钟测试；旧默认为 1 秒，且交互卡片新增时未立即推送，两项实际失败。
2. 先运行真实 Chromium + Go Agent／Relay fixture：生产默认配置下，1.4 秒时正文已提前更新，5 秒 cadence 断言失败。
3. 将生产默认值改为 5 秒，并把 bounded pendingInteractions 的稳定 hash 纳入结构变化判断；文本仍只保留一个 latest-only pending snapshot，不复制历史或添加 Web 计时器。
4. 虚拟时钟验证首帧即时、1 秒不发布纯文本、固定 5 秒窗口发送最新尾部、交互新增／同 ID 内容变化／移除即时、完成即时以及旧 timer 不重放被覆盖的内容。
5. 真实浏览器验证细粒度 native 更新已被 Agent 消费但 Web 仍保留原正文；约 5 秒后只显示最新文本，完成在 2 秒门槛内到达，草稿和发送状态正确。支持单独 --grep 运行，不依赖前一条测试状态。

## 回归与边界

- Go 全量、vet 与 Desktop Agent／Relay race 通过。
- Web unit：286 项通过。
- 真实 Go Agent／Relay 浏览器专项：2 项通过；5 秒 cadence 专项连续 3 次通过。
- 全量 Chromium viewport／touch 回归：57 项通过（1 worker），既有截图基线不变。
- 当前 native owner 仅使用隔离 fixture，不改变真实 Codex 业务会话；本次没有修改 native Adapter。
- 物理 iPhone／Safari 未测试。5 秒是正常调度、网络畅通下的正文合并窗口，不是端到端延迟 SLA；首屏／结构变化／完成／交互仍走即时路径。
- 本次仅提交代码，不在未获新部署请求时重启公网 Relay 或本机 Agent。
