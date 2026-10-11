# 0066：运行中输入焦点与会话 chrome 稳定性

- 状态：Implemented（Web 单测／真实 Chromium／全量回归已验；物理 iPhone／Safari 待部署后复验）
- 来源：物理 iPhone／Safari 中，会话正在处理时在 composer 输入文字，每次输入都会使 composer 与会话 Header 被历史阅读折叠逻辑收起，导致无法连续输入。
- 关联：[0045 阅读历史时自动收起会话 Header](0045-collapse-conversation-header-while-reading-history.md)、[0057 手动滚动恢复会话 chrome](0057-manual-scroll-restores-conversation-chrome.md)。
- 范围：Web 端 composer focus、移动 viewport 与 transcript 折叠判定；不改变发送、排队、streaming 或历史分页协议。

## 已确认事实与根因判断

1. 公网 `www.kan.chat` 当前 `/assets/index-Cx4ShtH9.js` 已包含 `28c1ece` 的 keyboard viewport 防折叠逻辑，因此问题不是该修复尚未部署。
2. 现有逻辑只有在 `visualViewport.offsetTop + height < window.innerHeight - 80` 时才把键盘标为 active。现有自动测试固定 `window.innerHeight = 844`，仅缩小 `visualViewport.height`，因此总能命中保护分支。
3. 我不确定目标 iPhone 当时的 Safari viewport 数值；但若 `window.innerHeight` 与 `visualViewport.height` 一起缩小，现有键盘判断会返回 false。composer 每次输入的自适应测量、React 重排或同时到达的 streaming 内容会改变 transcript 几何并产生 scroll event，现有代码会把这个无用户手势的 scroll 当成“离开最新消息”，进而设置 `historyChromeCollapsed=true`。这一解释同时符合“输入仍聚焦”“每次输入触发”和“运行中更易出现”三个现象。
4. 修复不再让输入稳定性依赖具体 Safari viewport 数值，而以 focus 和明确的 transcript 手势作为行为边界。

## 行为与不变量

1. composer textarea 获得 focus 且会话仍在跟随最新消息时，进入 composer editing session。在该 session 内，下列非用户 transcript 操作不得触发历史阅读折叠：
   - 输入、删除、中文输入法 composition 与 textarea 自适应高度变化；
   - streaming thread update、队列状态更新及 React 重排；
   - `visualViewport`／`window` resize 或 scroll，以及地址栏、软键盘造成的可视高度变化。
2. editing session 内发生上述布局 scroll 时，Web 保持 Header 与 composer 展开、草稿值、focus 和 caret，并把 transcript 继续锚定在最新消息。
3. 用户在 transcript 上作出明确的向历史方向 wheel／touch 手势时，即使 composer 仍处于 focus 或软键盘仍可见，也允许进入既有历史阅读模式并折叠 chrome；本修复不得让用户失去主动查看历史的能力。
4. textarea blur、会话／设备切换、断线或 selection generation 改变时结束 editing session，不把旧 focus 状态带入其他会话。
5. `visualViewport` 检测继续只负责可视区域高度适配；它不再作为“允许 composer 被折叠”的唯一安全条件。
6. 已处于历史阅读模式时，程序不得仅因 textarea focus 状态伪造“回到最新”。恢复仍须通过既有的明确回到底部操作；正常点击可见 composer 获得 focus 的场景除外。

## TDD 与验收

1. Red：模拟 iPhone viewport 中 `window.innerHeight` 与 `visualViewport.height` 同步从 844 缩到 500；运行中 thread 聚焦 textarea 后逐字符输入并触发 transcript scroll，断言每一步 chrome 均展开，草稿、focus 和 caret 均保留。
2. Red：在每次字符输入之间注入连续 `thread.update` streaming 事件，断言 composer 不折叠且 draft 不丢失。
3. Red：同一 editing session 中对 transcript 发出明确的向历史方向 touch／wheel，再产生 scroll，断言既有历史阅读折叠仍生效。
4. Red：blur 或切换会话后，不再沿用旧 editing session 的 scroll 抑制状态。
5. 真实 Chromium 390×844 fixture 模拟同步收缩的 inner／visual viewport、逐字符输入与 streaming 更新；同时断言 `data-history-collapsed=false`、textarea 可见且 focused、草稿完整、composer 位于可视 viewport 内。
6. 运行 Web 全量单测、production build 与全部 Playwright 回归。Chromium 模拟只能证明真实浏览器 viewport／touch 行为；部署后仍需物理 iPhone／Safari 复验。

## 实现结果

- composer 聚焦且仍跟随最新消息时，不再依赖 Safari 的 keyboard viewport 高度差识别；没有明确 transcript 手势的布局 scroll 会重新锚定最新消息并保持 chrome 展开。
- wheel／touch 手势保留 500 ms intent grace，允许软键盘仍开启时主动进入历史阅读，避免 focus 保护吞掉真实滚动。
- 新增同步收缩 `innerHeight`／`visualViewport`、逐字符输入、连续 streaming 和聚焦状态下主动浏览历史的回归；2026-10-11 的 176 个 Web 单测、production build 与 22 个真实 Chromium 用例全部通过。
