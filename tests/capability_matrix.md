# Reasonix Relay vs Alma ChromeRelay — 能力对比矩阵

> 日期：2026-08-21
> 当前状态：✅ = 已实现 / ✨ = Reasonix 额外 / ❌ = 缺失

## 核心能力对比

| # | 能力 | Alma ChromeRelay | Reasonix Relay | 状态 |
|---|------|:-:|:-:|:--:|
| 1 | 连接状态查询 | ❌ | `browser_status` | ✨ **Reasonix 额外** |
| 2 | 导航到 URL | `ChromeRelayNavigate` | `browser_navigate` | ✅ 等价 |
| 3 | 点击元素 (CSS) | `ChromeRelayClick` | `browser_click` | ✅ 等价 |
| 4 | 输入文本 | `ChromeRelayType` | `browser_type` | ✅ 等价 |
| 5 | 读取页面文本 | `ChromeRelayRead` | `browser_read` | ✅ 等价 |
| 6 | 截图 | `ChromeRelayScreenshot` | `browser_screenshot` | ✅ 等价 |
| 7 | 执行 JS | `ChromeRelayEval` | `browser_eval` | ✅ 等价 |
| 8 | 列出所有标签页 | `ChromeRelayListTabs` | `browser_list_pages` | ✅ 等价 |
| 9 | 读取交互元素 | `ChromeRelayReadDom` | `browser_read_dom` | ✅ 等价 |
| 10 | 滚动页面 | `ChromeRelayScroll` | `browser_scroll` | ✅ 等价 |
| 11 | 后退 | `ChromeRelayBack` | `browser_go_back` | ✅ 等价 |
| 12 | 前进 | `ChromeRelayForward` | `browser_go_forward` | ✅ 等价 |
| 13 | 上传文件 | `ChromeRelayUpload` | `browser_upload_file` | ✅ 等价 |
| 14 | 选择标签页 | ❌ | `browser_select_page` | ✨ **Reasonix 额外** |
| 15 | 新建标签页 | ❌ | `browser_new_page` | ✨ **Reasonix 额外** |
| 16 | 关闭标签页 | ❌ | `browser_close_page` | ✨ **Reasonix 额外** |
| 17 | 按键操作 | ❌ | `browser_press_key` | ✨ **Reasonix 额外** |
| 18 | 悬停 | ❌ | `browser_hover` | ✨ **Reasonix 额外** |
| 19 | 等待元素出现 | ❌ | `browser_wait` | ✨ **Reasonix 额外** |
| 20 | 视口调整 | ❌ | `browser_resize` | ✨ **Reasonix 额外** |
| 21 | 对话框处理 | ❌ | `browser_handle_dialog` | ✨ **Reasonix 额外** |
| 22 | 批量填表单 | ❌ | `browser_fill_form` | ✨ **Reasonix 额外** |
| 23 | 列出已附加标签页 | ❌ | `browser_attached_pages` | ✨ **Reasonix 额外** |

## 架构差异

| 维度 | Alma ChromeRelay | Reasonix Relay |
|------|:-:|:-:|
| **通信协议** | MCP (Model Context Protocol) | 自定义 WebSocket + CDP |
| **工具注册方式** | MCP 工具声明 | Go 内置工具 (tool.RegisterBuiltin) |
| **扩展架构** | Chrome Extension + Debugger API | Chrome Extension + Debugger API |
| **端口** | 23001 | 23002 |
| **认证** | Token（存 MCP config） | Token（存 ~/.reasonix/browser-relay.json） |
| **多标签支持** | 单标签（通过 tabId 参数） | 多标签（attach/select/detach 模型） |
| **工具数量** | 12 个 | 23 个 |
| **Reconnect** | 无自动重连 | 指数退避自动重连 |
| **空闲清理** | 无 | 30 分钟无活动自动 detach |
| **语言** | TypeScript (MCP Server) | Go (built-in tools) |

## 关键差异点

### Reasonix 优势 (11 个额外能力)

1. **browser_status** — 快速检查连接状态，无需记住之前的连接状态
2. **browser_select_page** — 在已 attach 的标签页之间切换
3. **browser_new_page** — 直接创建新标签页
4. **browser_close_page** — 关闭指定标签页
5. **browser_press_key** — 按 Enter/Tab/Escape 等特殊键
6. **browser_hover** — 悬停触发 tooltip/dropdown
7. **browser_wait** — 等元素出现再操作，避免固定 delay
8. **browser_resize** — 响应式测试
9. **browser_handle_dialog** — 处理 alert/confirm/prompt
10. **browser_fill_form** — 一次填多个字段
11. **browser_attached_pages** — 只显示已 attach 的标签页（隐私控制）

### 实现细节差异

**browser_navigate**: Reasonix 多了 `document.readyState` 轮询等待页面加载完成（15s 超时），Alma 不等待。

**browser_click**: Reasonix 通过 `Input.dispatchMouseEvent` 发送真实点击事件，坐标精确到元素中心。Alma 通过 ChromeRelayClick 内部实现。

**browser_type**: Reasonix 通过 JS 直接设置 `el.value` + 触发 `input`/`change` 事件。Alma 通过 `ChromeRelayType` 内部实现。

**browser_screenshot**: 两者都支持 PNG/JPEG 格式和 quality 参数。

**browser_upload_file**: Reasonix 使用 CDP 的 `DOM.setFileInputFiles` 方法，需要先通过 `DOM.querySelector` 找到节点。Alma 通过 `ChromeRelayUpload` 内部实现。

## 测试结论

✅ **Reasonix Relay 完全覆盖了 Alma ChromeRelay 的 12 个工具能力**

✨ **额外拥有 11 个 Alma 没有的浏览器自动化能力**

⚠️ 注意事项：
- 多标签 attach/select 模型增加了隐私控制（用户显式 attach），但也增加了使用复杂度
- 部分工具（如 browser_upload_file）依赖 CDP 的 DOM 查询，在 XHTML/iframe 页面可能受限
- 需要确保 `browserrelay.DefaultServer` 在工具调用前已设置