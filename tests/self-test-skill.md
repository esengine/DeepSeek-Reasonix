# Reasonix Relay Self-Test Skill

> 让 Reasonix 自己测试自己的 browser_* 工具。
> 前置条件：Chrome 扩展已连接并授权，至少 attach 了一个标签页。

## 测试流程

### 1. 检查连接状态
```tool
browser_status
```
预期：`running: true, state: "authorized"`

### 2. 导航到测试页
```tool
browser_navigate
url: "about:blank"
```
预期：返回 frameId，页面加载完成

### 3. 列出所有标签页
```tool
browser_list_pages
```
预期：返回标签页数组，包含 tabId、title、url

### 4. 列出已 attach 的标签页
```tool
browser_attached_pages
```
预期：返回当前 attach 的标签页列表

### 5. 读取页面内容
```tool
browser_read
```
预期：返回页面标题和文本内容

### 6. 读取交互元素
```tool
browser_read_dom
```
预期：返回按钮、链接、输入框等交互元素列表

### 7. 截图
```tool
browser_screenshot
format: "jpeg"
quality: 80
```
预期：返回 base64 图片数据

### 8. 执行 JS
```tool
browser_eval
expression: "JSON.stringify({title: document.title, url: location.href})"
```
预期：返回当前页面的 title 和 URL

### 9. 创建新标签页
```tool
browser_new_page
url: "https://example.com"
```
预期：返回新标签页的 tabId 和 URL

### 10. 切换标签页
```tool
browser_select_page
tab_id: <上一步返回的 tabId>
```
预期：CDP 目标切换到该标签页

### 11. 滚动
```tool
browser_scroll
direction: "down"
amount: 500
```
预期：页面滚动成功

### 12. 后退/前进
```tool
browser_go_back
```
预期：返回上一页
```tool
browser_go_forward
```
预期：返回下一页

### 13. 点击元素
```tool
browser_read_dom
```
先找可点击元素
```tool
browser_click
selector: "body"
```
预期：点击成功

### 14. 输入文本
```tool
browser_type
selector: "body"
text: "test input"
```
预期：输入成功

### 15. 按键操作
```tool
browser_press_key
key: "Escape"
```
预期：按键事件发送成功

### 16. 等待元素
```tool
browser_wait
selector: "body"
timeout: 3000
```
预期：找到元素

### 17. 调整视口
```tool
browser_resize
width: 1024
height: 768
```
预期：视口调整成功

### 18. 关闭标签页
```tool
browser_list_pages
```
先获取标签页列表
```tool
browser_close_page
tab_id: <上一步创建的标签页的 tabId>
```
预期：标签页关闭成功

## 验证清单

| # | 工具 | 测试结果 | 备注 |
|---|------|---------|------|
| 1 | browser_status | ✅ | 实测 authorized / disconnected 均正确返回 |
| 2 | browser_navigate | ✅ | 2026-08-21 实测 example.com 导航成功，返回 frameId |
| 3 | browser_list_pages | ✅ | 实测列出全部 17 个标签页 |
| 4 | browser_attached_pages | ✅ | 2026-08-21 实测返回 attach 列表 |
| 5 | browser_read | ✅ | 实测读取语雀 / example.com / 一起安AI / Harness 内容 |
| 6 | browser_read_dom | ✅ | 实测 example.com 返回 null（无交互元素，正常） |
| 7 | browser_screenshot | ✅ | 实测返回 base64 JPEG 图片数据 |
| 8 | browser_eval | ✅ | 实测返回 title 和 url |
| 9 | browser_new_page | ✅ | 实测创建标签页，不自动 attach（设计如此） |
| 10 | browser_select_page | ✅ | 实测在 attach 标签页间切换 CDP 目标 |
| 11 | browser_scroll | ✅ | 实测 example.com 向下滚动 500px 成功 |
| 12 | browser_go_back | ✅ | 2026-08-21 实测 navigate→go_back 回到原页面 |
| 13 | browser_go_forward | ✅ | 实测 navigate→go_back→go_forward 完整链路 |
| 14 | browser_click | ✅ | 实测 example.com body 点击成功 |
| 15 | browser_type | ✅ | 实测 example.com body 输入 "test input" 成功 |
| 16 | browser_press_key | ✅ | 实测 Escape 按键发送成功 |
| 17 | browser_wait | ✅ | 实测等待 body 元素出现成功 |
| 18 | browser_resize | ✅ | 实测视口调整为 1024×768 成功 |
| 19 | browser_close_page | ✅ | 实测关闭测试标签页成功 |
| 20 | browser_emulate | ⬜ | 新增，CDP Emulation.setDeviceMetricsOverride |
| 21 | browser_take_snapshot | ⬜ | 新增，CDP Accessibility.getFullAXTree |
| 22 | browser_drag | ⬜ | 新增，CDP Input.dispatchMouseEvent 组合 |
| 23 | browser_list_console_messages | ⬜ | 新增，扩展端缓存 Console 消息 |
| 24 | browser_list_network_requests | ⬜ | 新增，扩展端缓存 Network 请求 |

## 注意
- 部分工具（hover、handle_dialog、upload_file、fill_form）需要特定页面环境
- 建议用 test_page.html 做完整测试
- 测试前确保扩展已 attach 了一个标签页