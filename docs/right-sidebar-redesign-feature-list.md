# feat/right-sidebar-redesign 功能清单（PR 描述草稿）

> 用户维度说明：右侧面板从「一个固定大面板」升级为「轻量标签页工作台」，
> 文件路径展示更清晰、复制更省事。

## 一、右侧面板：图标启动器 + 标签页

1. **全新入口**：右侧面板收起时是一个悬浮小工具条（文件 / 改动 / 概览三个入口），点击展开对应内容，不再常驻占用屏幕空间
2. **顶部标签页**：展开后上方一排标签页，每个标签带独立关闭按钮，右下角「+」可随时添加新标签（文件 / 改动 / 概览）
3. **打开的文件变成标签**：文件列表点开文件 → 标签页显示文件名；连续切换文件时只更新当前标签内容，不会刷出满屏标签
4. **标签可拖动排序**：按住标签拖到任意位置调整顺序（持久化，重启保留）
5. **标签右键菜单**（类 VS Code）：
   - 用默认应用打开文件
   - 「打开方式」二级菜单选择其他程序（如 Sublime Text）
   - 另存为 / 复制路径（相对+绝对）/ 复制文件内容 / 在文件管理器中显示
   - 关闭 / 关闭其他标签页 / 关闭右侧标签页
6. **细节修正**：切到「改动」视图不会误改已打开的文件标签；标签顺序跟随操作顺序，新标签出现在最后

## 二、文件路径面包屑

7. **完整路径展示**：显示完整路径（项目 › 目录 › 文件名），字体更大，文件名颜色加深突出
8. **点击复制**：点击任意一段路径即复制该段路径
9. **框选复制**：框选面包屑复制出规范格式（如 `DeepSeek-Reasonix/go.mod`，斜杠分隔）
10. **长路径不截断**：超长时横向滚动查看，默认停在最右侧（文件名可见），隐藏滚动条

## 三、提交列表（21 个，基于 upstream/main-v2）

```
2faff2199 feat(desktop): drag to reorder dock tabs
068da1832 style(desktop): tighten dock tab horizontal padding
4aa284027 fix(desktop): dock tabs render in store insertion order
384c78ff8 style(workspace): deepen the last breadcrumb segment (file name)
532e60bc8 style(workspace): breadcrumb hover without background
8133e5660 refactor(desktop): drop icons from dock file-tab context menu
64e932988 fix(desktop): pin submenu chevron to the right edge of its menu item
c01481930 feat(workspace): clickable breadcrumbs copy path; dock menu Open With submenu
160cc1a7a fix(workspace): right-align breadcrumb before first paint and after fonts settle
12bc767d7 fix(workspace): breadcrumb overflows horizontally with scrollbar hidden
f41bfb0c4 feat(workspace): breadcrumb shows full project-relative path, drop file icon, bump font
75ae2baa2 feat(workspace): selectable breadcrumb copies project-relative path
9fad0da68 feat(desktop): context menu for dock file tabs
5cf32c795 fix(desktop): never rewrite dock file tabs from non-file view reports
eb5938692 refactor(desktop): dock mirrors current preview as a single file tab
c9f39519f feat(desktop): mirror open files as dock tabs; polish launcher/add-menu
385144395 fix(desktop): keep upstream dock header chrome; anchor add menu to the + button
bcc6859a0 feat(desktop): replace activity bar with a floating dock launcher
e988dc1ba feat(desktop): keep the activity bar always visible
d5611c606 feat(desktop): match the plan's exclusive activity-bar interaction
7a2e67307 feat(desktop): redesign right dock into activity bar + tab container
```

## 四、验证

- 前端 store 单测 31/31、tsc、eslint、bundle budget 全绿
