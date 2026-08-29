---
name: design-ui
description: 根据 DESIGN.md 设计系统生成 UI 代码 — 加载令牌、构建组件、验证
---

# Design UI — 从 DESIGN.md 生成 UI 代码

你的任务：生成符合项目 DESIGN.md 设计系统的 UI 代码（HTML/CSS、React 或指定的框架）。

## 工作流程

1. **找到 DESIGN.md** — 按顺序查找：
   - `.reasonix/DESIGN.md`
   - `./DESIGN.md`
   - 项目根目录下任何以 `DESIGN.md` 结尾的文件

   如果都没有，用 `web_fetch` 从 [awesome-design-md](https://github.com/VoltAgent/awesome-design-md) 下载一个。

2. **读取 DESIGN.md** — 用 `@DESIGN.md` 或 `read_file` 加载完整文件。重点关注：
   - 色板（语义名称 + 十六进制值）
   - 字体层级（字族、字号、字重）
   - 间距/圆角令牌
   - 组件定义（按钮、卡片、输入框、导航）
   - 注意事项（设计红线）

3. **判断需求** — 用户在问：
   - 单个页面/组件？→ 直接构建
   - 完整站点？→ 先规划页面结构，再构建组件树
   - 只是设计探索？→ 生成预览 HTML

4. **生成代码** — 产出可用于生产环境的 UI：
   - 使用 DESIGN.md 色板中的精确十六进制值
   - 遵循字体层级（display-xl → caption）
   - 应用间距刻度和圆角令牌
   - 匹配组件定义（按钮形状、卡片样式）
   - 遵守每条 "Don't" 规则

5. **写入文件** — 用 `write_file` 创建：
   - `ui/index.html` 或对应框架的文件
   - 包含 CSS 链接或内联样式

6. **验证** — 如果有浏览器/构建工具可用，提供视觉验证。

## 设计约束

- 每个颜色**必须**来自 DESIGN.md 色板 — 绝不凭空造十六进制值
- 组件形状**必须**匹配 `components:` 节的令牌
- 字体**必须**使用定义的 `typography` 层级令牌
- 如果 DESIGN.md 说 "Don't"，就别做
- 用 CSS 自定义属性（`--color-primary`、`--spacing-lg`）映射 DESIGN.md 令牌到运行时值，使映射关系显式可追溯

## 参数格式

```
design-ui arguments: "<要构建的内容描述>, design=<design.md路径>"
```

如果不传 `design`，默认搜索 `PROJECT_ROOT/DESIGN.md` 然后是 `.reasonix/DESIGN.md`。
