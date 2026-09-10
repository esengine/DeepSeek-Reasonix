import type { BrowserDownloadState } from "./browserHost";
import { useI18n, type Locale } from "./i18n";

// Panel copy stays inside the lazy browser chunk: the shared dictionaries are
// ratcheted per locale chunk (check-bundle-budget) and this surface only
// exists in the Electron shell.
const en = {
  panel: "Browser",
  tabs: "Browser tabs",
  newTab: "New tab from address",
  untitled: "Untitled",
  temporary: "Temporary",
  loading: "Loading",
  closeTab: (title: string) => `Close ${title}`,
  address: "Address",
  addressPlaceholder: "Enter an address",
  back: "Back",
  forward: "Forward",
  reload: "Reload",
  stop: "Stop loading",
  zoomIn: "Zoom in",
  zoomOut: "Zoom out",
  zoomReset: (percent: number) => `Reset zoom (${percent}%)`,
  devTools: "Toggle DevTools",
  takeover: "You are controlling this page; the agent is paused",
  takeControl: "Take over",
  resume: "Resume",
  errorTitle: "This page could not be loaded",
  errorDetail: (code: number, description: string) => `${description} (${code})`,
  retry: "Retry",
  emptyTitle: "No pages open",
  emptyHint: "Enter an address to open a page for this task.",
  downloads: "Downloads",
  clearDownloads: "Clear finished downloads",
  downloadState: {
    progressing: "Downloading",
    completed: "Completed",
    cancelled: "Cancelled",
    interrupted: "Failed",
  } satisfies Record<BrowserDownloadState, string>,
  actionFailed: (message: string) => `Browser: ${message}`,
};

export type BrowserCopy = typeof en;

const zh: BrowserCopy = {
  panel: "浏览器",
  tabs: "浏览器标签页",
  newTab: "用地址栏内容新建标签页",
  untitled: "未命名",
  temporary: "临时",
  loading: "加载中",
  closeTab: (title) => `关闭 ${title}`,
  address: "地址",
  addressPlaceholder: "输入网址",
  back: "后退",
  forward: "前进",
  reload: "重新加载",
  stop: "停止加载",
  zoomIn: "放大",
  zoomOut: "缩小",
  zoomReset: (percent) => `重置缩放（${percent}%）`,
  devTools: "开关开发者工具",
  takeover: "你正在操作此页面；代理已暂停",
  takeControl: "接管",
  resume: "恢复",
  errorTitle: "无法加载此页面",
  errorDetail: (code, description) => `${description}（${code}）`,
  retry: "重试",
  emptyTitle: "没有打开的页面",
  emptyHint: "输入网址，为当前任务打开页面。",
  downloads: "下载",
  clearDownloads: "清除已完成的下载",
  downloadState: { progressing: "下载中", completed: "已完成", cancelled: "已取消", interrupted: "失败" },
  actionFailed: (message) => `浏览器：${message}`,
};

const zhTW: BrowserCopy = {
  panel: "瀏覽器",
  tabs: "瀏覽器分頁",
  newTab: "以網址列內容新增分頁",
  untitled: "未命名",
  temporary: "暫時",
  loading: "載入中",
  closeTab: (title) => `關閉 ${title}`,
  address: "網址",
  addressPlaceholder: "輸入網址",
  back: "上一頁",
  forward: "下一頁",
  reload: "重新載入",
  stop: "停止載入",
  zoomIn: "放大",
  zoomOut: "縮小",
  zoomReset: (percent) => `重設縮放（${percent}%）`,
  devTools: "切換開發人員工具",
  takeover: "你正在操作此頁面；代理已暫停",
  takeControl: "接管",
  resume: "繼續",
  errorTitle: "無法載入此頁面",
  errorDetail: (code, description) => `${description}（${code}）`,
  retry: "重試",
  emptyTitle: "沒有開啟的頁面",
  emptyHint: "輸入網址，為目前任務開啟頁面。",
  downloads: "下載",
  clearDownloads: "清除已完成的下載",
  downloadState: { progressing: "下載中", completed: "已完成", cancelled: "已取消", interrupted: "失敗" },
  actionFailed: (message) => `瀏覽器：${message}`,
};

export function browserCopy(locale: Locale): BrowserCopy {
  return locale === "zh" ? zh : locale === "zh-TW" ? zhTW : en;
}

export function useBrowserCopy(): BrowserCopy {
  return browserCopy(useI18n().locale);
}
