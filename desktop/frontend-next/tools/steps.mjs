// The walk both sweeps take through the interface, and the settings tour that
// follows it. It lives here because a step is a claim about what the window
// can be driven to show, and two copies of that claim drift: the i18n sweep
// was still opening settings by an English aria-label months after the button
// stopped carrying one.
//
// Every step names the control it wants by the intent the interface declares
// on it, never by what the button says or where it sits: the labels move with
// the interface language, and the sweep runs in English against a window a
// person reads in Chinese. Picking by shape is how `.sesstitle` — a class that
// had not existed for months — went on "succeeding" into the rename button
// beside the row it meant to open.
export const act = (a, v) => `[data-action="${a}"]` + (v ? `[data-value="${v}"]` : "");
const click = (sel, pick = "[0]") => `(()=>{const b=[...document.querySelectorAll('${sel}')]${pick};if(!b)return false;b.click();return true})()`;
const type = (t) => `(()=>{const ta=document.querySelector('.compose textarea');if(!ta)return false;ta.focus();const s=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(ta),'value').set;s.call(ta,${JSON.stringify(t)});ta.dispatchEvent(new Event('input',{bubbles:true}));return true})()`;
export const STEPS = [
  ["main", null],
  // The busiest session, because a transcript with turns in it is the only one
  // that renders tool cards, plans and approvals.
  ["session-open", click(act("session.open"), `.sort((a,b)=>(+(b.querySelector('.sessmeta')?.textContent.match(/\\d+/)?.[0]||0))-(+(a.querySelector('.sessmeta')?.textContent.match(/\\d+/)?.[0]||0)))[0]`)],
  ["tab-task", click(act("pane.view", "task"))],
  ["tab-flow", click(act("pane.view", "flow"))],
  ["picker-model", click(act("model.select"))],
  ["picker-effort", `(()=>{document.body.click();return ${click(act("reasoning.effort"))}})()`],
  ["picker-approvals", `(()=>{document.body.click();return ${click(act("tool-approval.mode"))}})()`],
  ["policy", `(()=>{document.body.click();return ${click(act("chrome.policy"))}})()`],
  ["plan-on", `(()=>{document.body.click();return ${click(act("plan.mode"))}})()`],
  ["slash-palette", type("/")],
  ["at-files", type("@")],
  ["clear-composer", `(()=>{const ok=${type("")};document.body.click();return ok})()`],
  ["send-turn", `(()=>{const ok=${type("Run the tests and fix what fails")};if(!ok)return false;return ${click(act("session.send"), ".filter(b=>!b.classList.contains('sug'))[0]")}})()`],
  ["turn-mid", `(()=>true)()`],
  ["turn-late", `(()=>true)()`],
  ["turn-end", `(()=>true)()`],
  ["account", click(act("chrome.account"))],
];
// Settings is walked by index rather than by name: the nav labels carry counts
// and translate, and only their order is stable.
export const SETTINGS_OPEN = `document.querySelector('${act("chrome.settings")}')?.click()`;
export const SETTINGS_COUNT = `document.querySelectorAll('.prefs-nav button').length`;
export const settingsTab = (i) =>
  `(()=>{const b=document.querySelectorAll('.prefs-nav button')[${i}];if(!b)return "";b.click();return b.id||b.textContent.trim().slice(0,12)})()`;

// Putting the window into a theme takes a reload, not an attribute: a pack
// writes its palette to root.style, which outranks any `data-theme` set by
// hand and does not move until the window repaints. Setting the attribute
// alone leaves a pack's light inks under a dark stylesheet — a state nothing
// renders, which a contrast probe then reports as ten failures.
export const chooseTheme = (t) => `localStorage.setItem("rx-theme", ${JSON.stringify(t)})`;
