# 会话引用功能架构文档

> GitHub Issue: https://github.com/esengine/DeepSeek-Reasonix/issues/3185

## 1. 功能需求

在当前会话中使用 `@` 引用其他会话的聊天记录作为内容发送给 AI。

### 1.1 用户场景

- 用户在当前会话中想引用之前会话的对话内容
- 用户想让 AI 参考之前的对话上下文
- 用户想合并多个会话的讨论结果

### 1.2 预期行为（P0-MVP）

```
输入 @
→ 显示原有菜单（文件/目录列表）
→ 菜单顶部新增 "past:chats" 选项
→ 选择 past:chats
→ 切换到历史会话列表
→ 选择一个会话
→ Composer 上方显示"已引用会话"
→ 发送时把该会话历史内容附加到当前 prompt/context
```

---

## 2. 现有代码结构分析

### 2.1 核心文件

| 文件 | 作用 |
|------|------|
| `desktop/frontend/src/components/Composer.tsx` | 输入框组件，包含 @ 功能 |
| `desktop/frontend/src/components/FileMenu.tsx` | @ 文件菜单组件 |
| `desktop/frontend/src/components/HistoryPanel.tsx` | 历史会话面板 |
| `desktop/frontend/src/lib/bridge.ts` | 前后端通信接口 |
| `desktop/frontend/src/lib/types.ts` | 类型定义 |

### 2.2 现有 @ 功能实现

**Composer.tsx 第 257-337 行** 实现了文件引用功能：

```typescript
// --- @ file references ---
const atRaw = useMemo(() => {
  const m = /(?:^|\s)@([^\s]*)$/.exec(text);
  return m ? m[1] : null;
}, [text]);

// 文件匹配结果
const atMatches = useMemo(() => {
  // 过滤本地目录和搜索结果
}, [atRaw, atFrag, entries, searchEntries]);

// 菜单模式判断
const menuMode: "slash" | "slasharg" | "at" | null = ...;

// 渲染文件菜单
{menuMode === "at" && <FileMenu items={atMatches} ... />}
```

### 2.3 已有的会话 API（可复用）

```typescript
interface AppBindings {
  // 会话列表
  ListSessions(): Promise<SessionMeta[]>;

  // 会话操作（可复用读取历史）
  PreviewSession(path: string): Promise<HistoryMessage[]>;
}
```

---

## 3. P0-MVP 实施方案

### 3.1 设计思路

在现有的 `@` 菜单中添加 "past:chats" 选项，而不是创建新的 `@session:` 语法。

**菜单结构：**
```
@
├── 📁 past:chats        ← 新增：选择后显示历史会话列表
├── 📁 src/
├── 📁 docs/
├── 📄 README.md
└── ...
```

### 3.2 实施路线

```
第一步：后端加搜索接口
    ↓
第二步：前端 bridge.ts 暴露接口
    ↓
第三步：在 @ 菜单中添加 "past:chats" 选项
    ↓
第四步：选择 past:chats 后切换到会话列表
    ↓
第五步：选择会话后添加到引用区域
    ↓
第六步：发送时附加会话上下文
```

### 3.3 最小改动文件清单

```
desktop/frontend/src/lib/types.ts      — 添加 SessionReference 类型
desktop/frontend/src/lib/bridge.ts     — 添加 SearchSessions API
desktop/frontend/src/components/Composer.tsx — 扩展 @ 菜单逻辑
desktop/frontend/src/components/FileMenu.tsx — 扩展菜单支持会话项
desktop/app.go                         — 添加 SearchSessions 方法
desktop/sessions.go                    — 实现会话搜索逻辑
```

### 3.4 类型定义

```typescript
// types.ts
export interface SessionReference {
  path: string;
  title: string;
  preview?: string;
  turns?: number;
  createdAt?: number;
  lastActivityAt?: number;
  messages?: HistoryMessage[]; // P0 先不存，发送时再拉取
}
```

### 3.5 API 设计

```typescript
// bridge.ts
interface AppBindings {
  // 新增：搜索会话
  SearchSessions(query: string): Promise<SessionMeta[]>;

  // 已有：读取会话历史（复用）
  PreviewSession(path: string): Promise<HistoryMessage[]>;
}
```

### 3.6 前端逻辑修改

**Composer.tsx 修改：**

```typescript
// 1. 添加状态
const [showPastChats, setShowPastChats] = useState(false);
const [pastChats, setPastChats] = useState<SessionMeta[]>([]);
const [sessionRefs, setSessionRefs] = useState<SessionReference[]>([]);

// 2. 修改 @ 菜单渲染
{menuMode === "at" && (
  showPastChats ? (
    // 显示会话列表
    <SessionMenu
      items={pastChats}
      activeIndex={active}
      onPick={pickSession}
      onHover={setActive}
    />
  ) : (
    // 显示文件列表（原有逻辑）
    <>
      <button
        className="slashmenu__item slashmenu__item--special"
        onMouseDown={() => {
          setShowPastChats(true);
          app.ListSessions().then(setPastChats);
        }}
      >
        <MessageSquare size={13} />
        <span className="slashmenu__name">past:chats</span>
        <span className="slashmenu__desc">引用历史会话</span>
      </button>
      <FileMenu items={atMatches} ... />
    </>
  )
)}

// 3. 选择会话后的处理
const pickSession = (session: SessionMeta) => {
  // 添加到引用区域
  setSessionRefs(prev => [...prev, {
    path: session.path,
    title: session.title || session.preview || "Untitled",
    preview: session.preview,
    turns: session.turns,
    createdAt: session.createdAt,
    lastActivityAt: session.lastActivityAt,
  }]);

  // 重置状态
  setShowPastChats(false);
  setText(""); // 清空输入框
};

// 4. 发送时附加会话上下文
const handleSubmit = async () => {
  let context = "";

  if (sessionRefs.length > 0) {
    context = "以下是用户引用的历史会话上下文：\n\n";
    for (const ref of sessionRefs) {
      const messages = await app.PreviewSession(ref.path);
      const limited = limitMessages(messages, 30, 20000);
      context += formatSessionContext(ref.title, limited);
    }
    context += "\n\n当前用户问题：\n";
  }

  onSubmit(context + text);
};
```

### 3.7 限制策略

```
最多引用最近 30 条消息
或最多 20k 字符
超出部分截断，并提示"已截断"
```

### 3.8 发送时的消息格式

```
以下是用户引用的历史会话上下文：

[会话：修复登录 bug]
用户：...
助手：...
用户：...

当前用户问题：
...
```

---

## 4. 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                      Composer.tsx                           │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  @ 菜单逻辑                                          │   │
│  │  - 原有：文件/目录列表                               │   │
│  │  - 新增：past:chats 选项（在菜单顶部）              │   │
│  └─────────────────────────────────────────────────────┘   │
│                           │                                 │
│                           ▼                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  菜单渲染                                            │   │
│  │  - showPastChats=false → FileMenu + past:chats 按钮 │   │
│  │  - showPastChats=true  → SessionMenu (会话列表)     │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                      bridge.ts                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  新增 API:                                          │   │
│  │  - SearchSessions(query): Promise<SessionMeta[]>   │   │
│  │                                                     │   │
│  │  复用 API:                                          │   │
│  │  - ListSessions(): Promise<SessionMeta[]>          │   │
│  │  - PreviewSession(path): Promise<HistoryMessage[]> │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    desktop/app.go                           │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  新增方法:                                          │   │
│  │  - SearchSessions(query string) []SessionMeta      │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## 5. UI 设计

### 5.1 @ 菜单（showPastChats=false）

```
┌─────────────────────────────────────────────────────────────┐
│  @  ← 用户输入                                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  💬 past:chats        引用历史会话                   │   │
│  ├─────────────────────────────────────────────────────┤   │
│  │  📁 src/                                             │   │
│  │  📁 docs/                                            │   │
│  │  📄 README.md                                        │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  [输入消息...]                                    [发送]    │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 会话列表（showPastChats=true）

```
┌─────────────────────────────────────────────────────────────┐
│  @past:chats  ← 用户输入                                    │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  💬 项目架构设计讨论 - 2026-06-04                   │   │
│  │  💬 数据处理方案 - 2026-06-03                       │   │
│  │  💬 API 接口设计 - 2026-06-02                       │   │
│  │  ← 返回文件列表                                     │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  [输入消息...]                                    [发送]    │
└─────────────────────────────────────────────────────────────┘
```

### 5.3 引用区域

```
┌─────────────────────────────────────────────────────────────┐
│  📎 引用的会话:                                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  📄 项目架构设计讨论 (8 轮)               [×]       │   │
│  └─────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  [输入消息...]                                    [发送]    │
└─────────────────────────────────────────────────────────────┘
```

---

## 6. P1 后续优化（暂不实施）

- 选择单条/多条消息
- hover 预览会话详情
- 截断/缓存优化
- 国际化翻译
- 搜索会话功能

---

## 7. 验收标准

### P0 验收

- [ ] 输入 `@` 显示菜单，包含 "past:chats" 选项
- [ ] 选择 "past:chats" 显示历史会话列表
- [ ] 选择会话后显示在引用区域
- [ ] 可以删除引用的会话
- [ ] 发送时正确附加会话上下文
- [ ] 引用内容限制在 30 条消息或 20k 字符内
- [ ] 超出部分截断并提示

### 测试用例

- [ ] 无历史会话时的行为
- [ ] 引用超大会话时的截断
- [ ] 与现有 @ 文件引用同时使用
- [ ] 在不同主题下的显示效果
