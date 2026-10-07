# 0048：登录页彩色 Logo 恢复紧凑尺寸

- 状态：Implemented（138 个 Web 单测、9 个真实 Chromium 用例、桌面与手机截图、production build 已验；物理 iPhone/Safari 待用户复验）。
- 范围：未连接登录首页的彩色“风之信使”Logo、桌面与手机真实浏览器回归。
- 覆盖：[0046 登录页使用彩色“风之信使”主标](0046-color-login-brand-mark.md)中的固定 320px 显示尺寸；彩色资产选择与其他品牌调用点保持不变。

## 用户结果

登录首页继续展示正式彩色 Logo，但恢复到此前黑白 Logo 的视觉尺寸，不再因为固定 320px 图形挤压标题、连接表单或手机首屏布局。

## 资产决策

正式资产中没有单独的“彩色紧凑版”。登录页继续只加载 `/brand/ariel-logo-wind-messenger-color.png`，以 CSS 显示尺寸缩放到原黑白 Logo 使用的 `clamp(128px, 12vw, 160px)`；不复制新文件，不用 CSS filter 生成颜色，也不改动源图片。

## 实现契约

1. `ArielLogo` 使用 `variant="color"`，保持彩色正式资产与 1:1 宽高比。
2. 显示尺寸恢复为 `clamp(128px, 12vw, 160px)`：1440px 桌面视口为 160px，390px 手机视口为 128px。
3. Logo 继续作为装饰图并保持 eager/high-priority 加载。
4. 390×844 手机视口不得出现横向滚动；连接码输入与连接按钮保持可见、可达。
5. README 的 480px 彩色首图、导航微标、favicon 与其他调用点不随本规格改变。

## 验收

1. TDD：先把桌面与手机 E2E 几何预期改为 160px／128px，并确认当前 320px 实现失败。
2. 修改产品调用点后，运行桌面和手机登录页真实 Chromium 用例，更新两张截图基线。
3. 运行 Web 全量单测、全部 Playwright 用例和 production build；物理 iPhone/Safari 待用户复验。

## 实现结果

1. 登录页继续加载正式彩色主版，显示尺寸恢复为原黑白 Logo 使用的 `clamp(128px, 12vw, 160px)`。
2. 1440×900 Chromium 实测为 160×160px；390×844 Chromium 实测为 128×128px，手机页面没有横向溢出，连接码表单正常可达。
3. 桌面与手机截图基线已更新；Web 全量 138 项单测、9 项真实 Chromium 用例和 production build 均通过。
