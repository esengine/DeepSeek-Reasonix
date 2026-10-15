# 可复制的 DOM 选择器与操作序列

所有片段都在 `chrome/chrome_script_execute` 里跑（CDP `Runtime.evaluate`，`awaitPromise` 已开启）。

> ⚠️ 本文里的 class 名（`localVariableSetRowButton`、`pageRowButton`、`.ublptto`、`properties_panel`）都是 **Figma 打包后的内部混淆名**，只对当前版本有效，升级后随时会变。它们是"够用就行的探针"，不是 API 契约。失效时见文末「选择器失效时的降级顺序」。

## 打开文件

```
chrome/chrome_page_navigate  { url: "<figma 链接>", waitForReady: true, waitTimeoutMs: 30000 }
```

带 `view=variables` 直接进变量面板；去掉则进画布。Community 文件免登录。

## 变量面板

**枚举集合**

```js
[...document.querySelectorAll('button[data-testid="localVariableSetRowButton"]')]
  .map(b => ({ t: b.textContent.replace(/\s+/g,' ').trim() }))
// → [{t:"Border15 variables15"},{t:"Colors37 variables37"},{t:"Typography29 variables29"}]
```

**切集合 + 读全表（含 mode 数判定）**

```js
(async () => {
  const btns = [...document.querySelectorAll('button[data-testid="localVariableSetRowButton"]')];
  const t = btns.find(b => b.textContent.includes('Colors'));
  if (!t) return 'collection not found';
  t.click();
  await new Promise(r => setTimeout(r, 1600));
  const table = document.querySelector('[data-testid="variables-modal-table"]');
  return JSON.stringify([...table.querySelectorAll('tbody tr')].map(r => {
    const name = (r.querySelector('th')?.innerText || '').replace(/\s+/g,' ').trim();
    const vals = [...r.querySelectorAll('input')].map(i => i.value);
    return `${name || '<group>'} | inputs=${vals.length} | ${JSON.stringify(vals)}`;
  }));
})()
```

判读规则：

| 一行里的 input 数 | 含义 |
|---|---|
| 1 | 单一 mode |
| 2 | 两个 mode（典型 Light / Dark 两列） |
| 4 | 两个 mode，且值带透明度（hex、alpha、hex、alpha） |

**读表头列名**（确认是不是 Light/Dark）

```js
[...document.querySelectorAll('.variablesModalTableHeader > *')]
  .map(e => (e.innerText||'').replace(/\s+/g,' ').trim())
```

## 页面与图层

**枚举页面**

```js
[...document.querySelectorAll('button')]
  .filter(x => (x.className||'').toString().includes('pageRowButton'))
  .map(p => (p.innerText||'').replace(/\s+/g,' ').trim())
```

**单页结构（不入画布）** — 左侧面板整体文本

```js
(document.querySelector('[class*=left_panel_container--panel]') || {}).innerText
  ?.replace(/\s+/g,' ').slice(0, 2000)
```

**切页 + zoom to fit（不依赖快捷键）**

```js
(async () => {
  const ps = [...document.querySelectorAll('button')]
    .filter(x => (x.className||'').toString().includes('pageRowButton'));
  const t = ps.find(p => /Inline card/.test(p.innerText || ''));
  if (!t) return 'page not found';
  t.click();
  await new Promise(r => setTimeout(r, 2200));

  const zb = [...document.querySelectorAll('button')]
    .find(b => /^\s*\d{1,3}%\s*$/.test((b.innerText||'').trim()));
  if (!zb) return 'no zoom button';
  zb.click();
  await new Promise(r => setTimeout(r, 700));
  const fit = [...document.querySelectorAll('div,button,li,span')]
    .find(e => /^Zoom to fit/.test((e.innerText||'').trim()));
  if (!fit) { document.body.click(); return 'no fit item'; }
  fit.click();
  await new Promise(r => setTimeout(r, 1200));
  return 'ok';
})()
```

**连续放大**（zoom 档位：8% → 13% → 25% → 50% → 100% → 200%）

```js
(async () => {
  const zb = () => [...document.querySelectorAll('button')]
    .find(b => /^\s*\d{1,3}%\s*$/.test((b.innerText||'').trim()));
  const out = [];
  for (let i = 0; i < 5; i++) {
    const z = zb(); if (!z) break;
    z.click(); await new Promise(r => setTimeout(r, 800));
    const zi = [...document.querySelectorAll('div,button,li,span')]
      .find(e => /^Zoom in/.test((e.innerText||'').trim()) && (e.innerText||'').trim().length < 30);
    if (!zi) { out.push('menu item missing'); break; }
    zi.click(); await new Promise(r => setTimeout(r, 900));
    out.push((zb()?.innerText || '?').trim());
  }
  return JSON.stringify(out);
})()
```

## 选中节点 + 读右侧面板

图层行是 `[role="gridcell"]`，文本在 `.ublptto`。⚠️ 合成事件常被拒（见 SKILL.md 陷阱 2），**优先真实点击**。

如果必须用 DOM 探位置（拿 viewport 坐标，再乘 dpr 交给真实点击）：

```js
(() => {
  const row = [...document.querySelectorAll('[role="gridcell"]')]
    .find(r => /Inline card \/ Desktop web/.test((r.innerText||'').trim()));
  if (!row) return 'nf';
  const r = (row.querySelector('.ublptto') || row).getBoundingClientRect();
  return JSON.stringify({ x: Math.round(r.x + r.width/2), y: Math.round(r.y + r.height/2) });
})()
```

**读右侧属性面板**

```js
[...document.querySelectorAll('[class*=properties_panel]')]
  .map(x => (x.innerText||'').replace(/\s+/g,' ').trim())
  .filter(t => t.length > 60)
  .slice(-1)[0]
```

选中不同对象时这里会给出：Frame / Page 属性、Position（X/Y/Rotation）、Auto layout（Flow、`W … Hug`、Gap、Padding、Clip content）、Appearance、Text styles / Effect styles / Export。

## 切 Design / Prototype 标签

标签是 `SPAN`，`click()` 会冒泡到父级。

```js
(async () => {
  const tab = [...document.querySelectorAll('span,div,button')]
    .find(e => /^Prototype$/.test((e.innerText||'').trim()) && e.getBoundingClientRect().y < 60);
  if (!tab) return 'nf';
  tab.click();
  await new Promise(r => setTimeout(r, 1500));
  return 'ok';
})()
```

`Prototype` 面板里读到 `Prototype settings` + `Creating a connection` / `Running your prototype` 这类帮助文案，说明**当前没有交互**。

## 截图

```
chrome/chrome_page_screenshot { tabId, savePng: true, background: true, width: 1600, height: 900 }
```

- `background: true` 时窗口最小化也能采集
- 返回 `fullPath`，用 `view_image` 看
- 多标签场景先 `chrome/chrome_tab_activate`，否则可能截到别的标签

## 恢复被最小化的窗口（真实点击的前置条件）

```powershell
Add-Type -Namespace Win -Name U -MemberDefinition '[DllImport("user32.dll")] public static extern bool ShowWindow(System.IntPtr hWnd, int nCmdShow); [DllImport("user32.dll")] public static extern bool SetForegroundWindow(System.IntPtr hWnd);'
Get-Process chrome -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | ForEach-Object {
  [void][Win.U]::ShowWindow($_.MainWindowHandle, 9)
  [void][Win.U]::SetForegroundWindow($_.MainWindowHandle)
}
```

会抢前台，属于对用户桌面的打扰：能不用就不用，用了要在最终回答里说明。

## 选择器失效时的降级顺序

class / `data-testid` 变了不要硬刚，按这个顺序往下退，每退一级都先在 Console 手验一次：

| 级 | 手段 | 例 |
|---|---|---|
| 1 | `data-testid` | `[data-testid="variables-modal-table"]` |
| 2 | 稳定属性 | `[role="gridcell"]`、`button[aria-selected]`、`[aria-label*="Zoom"]` |
| 3 | 可见文案匹配 | `[...document.querySelectorAll('span,div,button')].find(e => /^Zoom to fit$/.test(String(e.innerText).trim()))` |
| 4 | 结构关系 | 从已读到的表头倒推：`table.querySelector('tbody tr')` 找不到就找 `[role="row"]` |
| 5 | 真实坐标 | 用 `getBoundingClientRect()` 拿 viewport 坐标 → 乘 dpr → `chrome/chrome_computer_act` 点击（陷阱 5） |

```js
// 探针：一次列出候选，避免反复猜
(() => {
  const hit = (sel) => document.querySelectorAll(sel).length;
  return JSON.stringify({
    gridcell: hit('[role="gridcell"]'),
    testids: [...new Set([...document.querySelectorAll('[data-testid]')].map(e => e.getAttribute('data-testid')))].slice(0, 40),
    panels: [...document.querySelectorAll('[class]')].map(e => e.className.toString()).filter(c => /properties_panel|left_panel_container/.test(c)).slice(0, 5)
  });
})()
```

文案层的兜底也要写死一条：整层都取不到时，**降级为截图 + 目测**（标"目测，未复核"），不要为了凑一个数字去猜。

