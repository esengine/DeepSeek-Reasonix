"use strict";

// Role labels come from the platform's own catalogue, which follows the
// system locale rather than the language the interface was set to, so they are
// stated here for the two languages the interface draws.
const WORDS = {
  zh: {
    edit: "编辑",
    undo: "撤销",
    redo: "重做",
    cut: "剪切",
    copy: "复制",
    paste: "粘贴",
    selectAll: "全选",
    pasteAndMatchStyle: "粘贴并匹配样式",
    delete: "删除",
    substitutions: "替换",
    showSubstitutions: "显示替换",
    smartQuotes: "智能引号",
    smartDashes: "智能破折号",
    textReplacement: "文本替换",
    speech: "语音",
    startSpeaking: "开始朗读",
    stopSpeaking: "停止朗读",
  },
  en: {
    edit: "Edit",
    undo: "Undo",
    redo: "Redo",
    cut: "Cut",
    copy: "Copy",
    paste: "Paste",
    selectAll: "Select All",
    pasteAndMatchStyle: "Paste and Match Style",
    delete: "Delete",
    substitutions: "Substitutions",
    showSubstitutions: "Show Substitutions",
    smartQuotes: "Smart Quotes",
    smartDashes: "Smart Dashes",
    textReplacement: "Text Replacement",
    speech: "Speech",
    startSpeaking: "Start Speaking",
    stopSpeaking: "Stop Speaking",
  },
};

function wordsFor(lang) {
  return WORDS[lang] ?? WORDS.en;
}

// The edit commands as roles carrying explicit labels: roles do the clipboard
// work, the labels follow the interface language.
function editItems(lang, can) {
  const w = wordsFor(lang);
  const on = (flag) => (can ? { enabled: !!can[flag] } : {});
  return [
    { role: "undo", label: w.undo, ...on("canUndo") },
    { role: "redo", label: w.redo, ...on("canRedo") },
    { type: "separator" },
    { role: "cut", label: w.cut, ...on("canCut") },
    { role: "copy", label: w.copy, ...on("canCopy") },
    { role: "paste", label: w.paste, ...on("canPaste") },
    { type: "separator" },
    { role: "selectAll", label: w.selectAll, ...on("canSelectAll") },
  ];
}

// The context menu's shape, kept apart from the platform so it can be checked
// without one. editFlags is the page's own account of what is possible at the
// click: this only decides what to offer.
function contextTemplate(params, lang) {
  if (!params.isEditable && !params.selectionText) return [];
  return editItems(lang, params.editFlags ?? {});
}

// Everything the platform's own Edit menu carries, with the same roles, so
// only the words change.
function editMenuTemplate(lang) {
  const w = wordsFor(lang);
  const items = editItems(lang);
  const at = items.findIndex((i) => i.role === "selectAll");
  items.splice(at - 1, 1);
  items.splice(at - 1, 0,
    { role: "pasteAndMatchStyle", label: w.pasteAndMatchStyle },
    { role: "delete", label: w.delete });
  items.push(
    { type: "separator" },
    {
      label: w.substitutions,
      submenu: [
        { role: "showSubstitutions", label: w.showSubstitutions },
        { type: "separator" },
        { role: "toggleSmartQuotes", label: w.smartQuotes },
        { role: "toggleSmartDashes", label: w.smartDashes },
        { role: "toggleTextReplacement", label: w.textReplacement },
      ],
    },
    {
      label: w.speech,
      submenu: [
        { role: "startSpeaking", label: w.startSpeaking },
        { role: "stopSpeaking", label: w.stopSpeaking },
      ],
    },
  );
  return { label: w.edit, submenu: items };
}

function applicationMenuTemplate(lang) {
  return [{ role: "appMenu" }, editMenuTemplate(lang), { role: "windowMenu" }];
}

// Only the Edit menu follows the interface language; the application and window
// menus are roles and follow the OS. Menu is injected so this runs without Electron.
function menuInstaller(Menu, platform = process.platform) {
  let installedLang = null;
  return function installApplicationMenu(languageOf = () => "en") {
    if (platform !== "darwin") {
      Menu.setApplicationMenu(null);
      return null;
    }
    const lang = languageOf();
    if (lang === installedLang) return Menu.getApplicationMenu();
    installedLang = lang;
    const menu = Menu.buildFromTemplate(applicationMenuTemplate(lang));
    Menu.setApplicationMenu(menu);
    return menu;
  };
}

module.exports = { contextTemplate, editMenuTemplate, applicationMenuTemplate, menuInstaller };
