# 0046：登录页使用彩色“风之信使”主标

- 状态：Superseded（彩色资产选择保留，固定 320px 显示尺寸由 0048 调整）。
- 范围：未连接状态的 Ariel 登录页、桌面与手机真实浏览器截图、品牌使用映射。
- 覆盖：[0044 正式“风之信使”Logo 系统](0044-wind-messenger-logo-system.md)中“登录页使用 white 单色主版”的产品调用点；其他尺寸规则、导航微标和资产约束保持不变。
- 后续：[0048 登录页彩色 Logo 恢复紧凑尺寸](0048-compact-color-login-brand-mark.md)保留本规格的彩色资产选择，并恢复原黑白 Logo 的响应式显示尺寸。

## 用户结果

打开 Ariel、尚未连接 Relay 时，登录页品牌区域展示正式彩色“风之信使”主版，不再展示反白单色版。导航栏、favicon、会话加载态等紧凑位置继续使用既有微标，不随本规格改变。

## 实现契约

1. 登录页只引用仓库内 `/brand/ariel-logo-wind-messenger-color.png`，不得引用设计源绝对路径。
2. 使用 `ArielLogo` 的 `variant="color"`，显示尺寸为 320px，满足彩色主版的最小显示宽度；保持 1:1 与 `object-fit: contain`。
3. 不使用 CSS `filter`、遮罩、裁切、底板、阴影或额外装饰改变正式资产。
4. Logo 继续作为装饰图，因为同一页面 Masthead 已显示 Ariel 品牌文字；保持 eager/high-priority 加载。
5. 390×844 手机视口不得产生横向滚动，连接码输入和连接按钮仍可通过正常纵向滚动到达。

## 验收

1. 先让桌面品牌 E2E 从 mono/white 预期改为 color/master 并失败，再修改产品调用点。
2. 桌面 1440×900 断言彩色资源、320×320 几何、透明背景和既有 Masthead 微标，并更新登录页截图。
3. 手机 390×844 断言彩色资源、无横向滚动、表单可达，并保存完整登录页截图。
4. 运行 Web 全量单测、全部 Playwright 用例和 production build；物理 iPhone/Safari 待用户复验。

## 实现结果

1. 登录 Hero 改为 `ArielLogo size={320} variant="color"`，实际只加载 `/brand/ariel-logo-wind-messenger-color.png`。
2. Masthead、侧栏、favicon 和会话加载态仍使用既有微标，没有扩大彩色主版的使用范围。
3. 桌面 1440×900 与手机 390×844 截图均已更新；手机页面无横向溢出，连接码输入和连接按钮可正常滚动到达。
