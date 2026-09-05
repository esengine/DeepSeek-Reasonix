import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const appSource = readFileSync(new URL("../App.tsx", import.meta.url), "utf8");
const stylesSource = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const heartbeatStyles = readFileSync(new URL("../custom/features/heartbeat/heartbeat.css", import.meta.url), "utf8");
const terminalWarmthSource = readFileSync(new URL("../lib/useWarmTerminalPanel.ts", import.meta.url), "utf8");

assert.match(appSource, /const chatSurfaceVisible = !automationView;/, "one page projection gates every chat-owned surface");
assert.match(appSource, /surfaceWorkspacePanelMaximized = chatSurfaceVisible && workspacePanelOpen && workspacePanelMaximized/, "stored workspace maximization is presentation-only on automation");
assert.match(appSource, /terminalSurfaceOpen = chatSurfaceVisible && terminalPanelOpen && !remoteSurfaceActive/, "stored terminal state remains intact while its surface is hidden");

assert.match(appSource, /automationView \? "app--automation" : ""/, "root marks automation before lazy Heartbeat mounts");
assert.match(appSource, /!automationView \? "layout--terminal-drawer-open" : ""/, "automation removes the terminal grid row");
// Resizer visibility and all layout entry points are exercised by automation-regions.test.tsx.
assert.match(appSource, /Boolean\(activeTabId\) && !automationView/, "automation disables the hidden-chat close shortcut");
assert.match(heartbeatStyles, /\.app--automation \.skip-to-composer\s*\{\s*display:\s*none;/s, "automation removes the hidden composer focus target");
assert.match(appSource, /if \(automationView\) \{\s*setMainView\("chat"\);\s*setTerminalPanelOpen\(true\)/s, "terminal toggle leaves automation and opens the terminal");
assert.doesNotMatch(appSource, /sidebar__quick-action--active/, "Workbench does not add a second automation entry beside New session");

assert.match(stylesSource, /\.layout--automation \.terminal-drawer\s*\{\s*display:\s*none;/s, "hidden terminal cannot occupy or receive input");
assert.match(heartbeatStyles, /\.automation-surface > \.heartbeat-page\s*\{[^}]*flex:\s*1;[^}]*width:\s*100%;/s, "shared automation page fills its shell");
assert.match(heartbeatStyles, /\.layout--sidebar-collapsed \.automation-surface\s*\{[^}]*padding-top:\s*44px/s, "collapsed automation reserves one shared titlebar safe area");
assert.match(heartbeatStyles, /\.app--darwin \.automation-sidebar-toggle\s*\{[^}]*left:\s*96px/s, "macOS sidebar recovery stays clear of traffic lights");
assert.match(stylesSource, /\.layout--automation:not\(\.layout--sidebar-collapsed\)[^}]*grid-template-columns:\s*var\(--sidebar-expanded-width\)/s, "minimum-width automation can restore its sidebar");

assert.match(heartbeatStyles, /container:\s*heartbeat-page \/ inline-size/, "Heartbeat responds to its real content width");
assert.match(heartbeatStyles, /@container heartbeat-page \(max-width:\s*559px\)/, "narrow editor mode starts below 560px");
assert.match(heartbeatStyles, /\.heartbeat-split--detail-open \.heartbeat-split__left,[\s\S]*\.heartbeat-split--detail-open \.heartbeat-split__divider\s*\{\s*display:\s*none;/s, "narrow editor hides the list and divider without remounting");

assert.match(terminalWarmthSource, /if \(open\) setMounted\(true\)/, "terminal content remains mounted while presentation is hidden");
assert.match(terminalWarmthSource, /if \(!open \|\| !visible\) \{\s*setFitEnabled\(false\)/s, "terminal fitting pauses while automation hides it");

console.log("automation surface layout: 20 passed");
