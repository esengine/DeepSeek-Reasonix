// Run: tsx src/__tests__/bundle-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const projectTreeSource = readFileSync(resolve(here, "../components/ProjectTree.tsx"), "utf8");
const settingsEntrySource = readFileSync(resolve(here, "../components/SettingsPanelEntry.tsx"), "utf8");
const settingsSource = readFileSync(resolve(here, "../components/SettingsPanel.tsx"), "utf8");
const remoteWizardEntrySource = readFileSync(resolve(here, "../components/RemoteConnectWizardEntry.tsx"), "utf8");
const remoteWizardSource = readFileSync(resolve(here, "../components/RemoteConnectWizard.tsx"), "utf8");
const projectCreationSource = readFileSync(resolve(here, "../components/useProjectCreation.tsx"), "utf8");
const markdownSource = readFileSync(resolve(here, "../components/Markdown.tsx"), "utf8");
const localPathLinksSource = readFileSync(resolve(here, "../lib/localPathLinks.ts"), "utf8");
const i18nSource = readFileSync(resolve(here, "../lib/i18n.tsx"), "utf8");
const mainSource = readFileSync(resolve(here, "../main.tsx"), "utf8");
const stylesSource = readFileSync(resolve(here, "../styles.css"), "utf8");

console.log("\nbundle contract");

ok(
  !/import\s+\{[^}]*\}\s+from\s+["']\.\/lib\/sessionExport["']/.test(appSource),
  "App keeps session export code out of the initial chunk",
);
ok(
  appSource.includes('import("./lib/sessionExport")'),
  "App loads session export code on demand",
);
ok(
  !/import\s+\{[^}]*\}\s+from\s+["']\.\/components\/SettingsPanel["']/.test(appSource) &&
    !/import\s+\{[^}]*\}\s+from\s+["']\.\/components\/HistoryPanel["']/.test(appSource),
  "App keeps secondary drawers out of the initial chunk",
);
ok(
  appSource.includes('import("./components/SettingsPanelEntry")') &&
    appSource.includes('import("./components/HistoryPanel")'),
  "App loads secondary drawers on demand",
);
ok(
  !/import\s+\{\s*ProjectTree\s*\}\s+from\s+["']\.\/components\/ProjectTree["']/.test(appSource),
  "App keeps the project tree out of the first-paint bundle",
);
ok(
  appSource.includes('import("./components/ProjectTree")'),
  "App loads the project tree when the sidebar mounts",
);
ok(
  settingsEntrySource.includes('import "./CompactRatioSettings.css"') &&
    settingsEntrySource.includes('from "./SettingsPanel"') &&
    !settingsSource.includes('import "./CompactRatioSettings.css"'),
  "Settings CSS stays in the lazy production entry without breaking direct module tests",
);
ok(
  remoteWizardEntrySource.includes('import "./RemoteConnectWizard.css"') &&
    remoteWizardEntrySource.includes('from "./RemoteConnectWizard"') &&
    projectCreationSource.includes('import("./RemoteConnectWizardEntry")') &&
    !remoteWizardSource.includes('import "./RemoteConnectWizard.css"'),
  "Remote wizard CSS stays in its lazy production entry without breaking direct module tests",
);
ok(
  !appSource.includes("openAllHistory") &&
    !appSource.includes("cmd-history") &&
    !appSource.includes("sidebar.allHistory") &&
    !projectTreeSource.includes("onOpenProjectHistory") &&
    !projectTreeSource.includes("project-history"),
  "App has no dedicated history-page entry points",
);
ok(
  appSource.includes('id: "cmd-trash"') &&
    appSource.includes("openTrash") &&
    appSource.includes("paletteSessions.slice(0, 12)") &&
    projectTreeSource.includes('t("projectTree.searchPlaceholder")'),
  "Trash and existing session search remain available",
);
ok(
  /\.sidebar--workbench\s+\.sidebar__utility-row\s*\{[^}]*grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)/s.test(stylesSource),
  "Workbench footer distributes its three utility actions evenly",
);
ok(
  /\.app--creation\s+\.sidebar__nav,\s*:root\[data-theme-style\]\s+\.app--creation\s+\.sidebar__nav\s*\{[^}]*grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)/s.test(stylesSource),
  "Creation footer distributes search, trash, and settings evenly",
);
ok(
  !/import\s+\{[^}]*\b(?:MCPServersSettingsPage|SkillsSettingsPage|PluginsSettingsPage)\b[^}]*\}\s+from\s+["']\.\/CapabilitiesPanel["']/.test(settingsSource) &&
    !/import\s+\{[^}]*\bMemorySettingsPage\b[^}]*\}\s+from\s+["']\.\/MemoryPanel["']/.test(settingsSource),
  "SettingsPanel keeps secondary settings pages out of the first settings chunk",
);
ok(
  settingsSource.includes('import("./CapabilitiesPanel")') &&
    settingsSource.includes('import("./MemoryPanel")'),
  "SettingsPanel loads secondary settings pages on demand",
);
ok(
  !/from\s+["']qrcode\.react["']/.test(settingsSource),
  "SettingsPanel keeps QR rendering code out of the first settings chunk",
);
ok(
  settingsSource.includes('import("qrcode.react")'),
  "SettingsPanel loads QR rendering code on demand",
);
ok(
  !/from\s+["']react-markdown["']/.test(markdownSource) &&
    !/from\s+["']remark-gfm["']/.test(markdownSource) &&
    !/from\s+["']remark-math["']/.test(markdownSource) &&
    !/from\s+["']rehype-katex["']/.test(markdownSource) &&
    !/katex\/dist\/katex\.min\.css/.test(markdownSource),
  "Markdown wrapper keeps markdown/math vendor code out of the initial chunk",
);
ok(
  markdownSource.includes('import("./MarkdownRenderer")'),
  "Markdown wrapper loads markdown renderer on demand",
);
ok(
  !/String\.raw`[^`]*\(\?<!/s.test(localPathLinksSource),
  "Markdown local-path support avoids RegExp lookbehind required by newer WebKit",
);
ok(
  !/import\s+\{\s*zh\s*\}\s+from\s+["']\.\.\/locales\/zh["']/.test(i18nSource) &&
    !/import\s+\{\s*zhTW\s*\}\s+from\s+["']\.\.\/locales\/zh-TW["']/.test(i18nSource),
  "i18n keeps Chinese dictionaries out of the unconditional initial chunk",
);
ok(
  i18nSource.includes('import("../locales/zh")') &&
    i18nSource.includes('import("../locales/zh-TW")'),
  "i18n loads Chinese dictionaries on demand",
);
ok(
  mainSource.includes("await preloadDetectedLocale()"),
  "main preloads the detected locale before mounting React",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
