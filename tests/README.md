# Reasonix Relay E2E Test Suite

## 快速开始

```bash
# 1. 启动 Reasonix Wails 应用（或确保已有的进程在运行）
# 2. 浏览器扩展已连接并授权
# 3. 运行测试
go test -v ./tests/ -run TestAllToolsRegistered
```

## 测试分组

### 1. 结构测试（无需扩展连接）
```bash
go test -v ./tests/ -run TestAllToolsRegistered
go test -v ./tests/ -run TestAlmaEquivalency
go test -v ./tests/ -run TestReasonixExtraTools
go test -v ./tests/ -run TestServerLifecycle
go test -v ./tests/ -run TestToolSchemaValidation
```

### 2. E2E 测试（需要真实扩展连接）
```bash
go test -v ./tests/ -run TestBrowserStatus
go test -v ./tests/ -run TestBrowserNavigate
# ... 等
```

### 3. 手动测试脚本
```bash
cd tests
bash tools_test.sh
```

## 测试架构

```
tests/
├── e2e_test.go           # Go 集成测试套件
├── test_page.html        # 测试页面（覆盖所有交互场景）
├── capability_matrix.md  # 能力对比矩阵
├── tools_test.sh         # 手动测试脚本
└── README.md             # 本文件
```

## 测试页面包含的测试区域

| # | Section | 测试的工具 | 测试内容 |
|---|---------|-----------|---------|
| 1 | sec-read | browser_read, read_dom | 页面文本读取、交互元素识别 |
| 2 | sec-click | browser_click | 点击按钮、点击计数器 |
| 3 | sec-type | browser_type | 输入框填值 |
| 4 | sec-form | browser_fill_form | 批量填表单 + 下拉框 |
| 5 | sec-key | browser_press_key | Enter 提交 |
| 6 | sec-hover | browser_hover | 悬停触发 tooltip |
| 7 | sec-scroll | browser_scroll | 滚动容器 |
| 8 | sec-wait | browser_wait | 等待动态元素出现 |
| 9 | sec-dialog | browser_handle_dialog | Alert/Confirm/Prompt |
| 10 | sec-upload | browser_upload_file | 文件上传 |
| 11 | sec-resize | browser_resize | 视口调整 |
| 12 | sec-eval | browser_eval | JS 执行 |

## 与 Alma ChromeRelay 的等价性验证

所有 12 个 Alma ChromeRelay 工具在 Reasonix 中都有对应实现。
Reasonix 额外拥有 11 个 Alma 没有的工具（详见 capability_matrix.md）。