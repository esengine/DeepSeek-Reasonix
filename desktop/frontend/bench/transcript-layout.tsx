import { useLayoutEffect, useMemo, useState, type CSSProperties } from "react";
import { createRoot } from "react-dom/client";
import { ChatPaneRegion, type ChatPaneTranscriptInput } from "../src/app-shell/ChatPaneRegion";
import { DockLauncher } from "../src/components/DockLauncher";
import { initialState, type Item } from "../src/lib/useController";
import { applyConversationWidth } from "../src/lib/conversationWidth";
import { LocaleProvider, useT } from "../src/lib/i18n";
import { installDesktopHostStub } from "../src/__tests__/desktopHostStub";
import "../src/styles.css";

type Options = {
  layout: "creation" | "workbench";
  width: "standard" | "full";
  sidebar: boolean;
  dock: boolean;
  launcher: boolean;
  long: boolean;
  turns: number;
  text: string | null;
  streaming: boolean;
  shell: boolean;
};
declare global {
  interface Window {
    transcriptLayoutFixture: {
      configure: (next: Partial<Options>) => void;
      revision: number;
      copied: string[];
      command: string;
    };
  }
}

// Only the backend boundary is stubbed: layout, launcher, projection, windowing
// and message/code rendering below are the production components and stylesheet.
installDesktopHostStub({
  ReportFrontendDiagnostic: async () => {},
  GitBranches: async () => [],
  WorkspaceChanges: async () => [],
  ToolResultForTab: async () => null,
});
const noAction = () => {};
const copied: string[] = [];
const command = "printf '%s\\n' \"C:\\中文\\file.txt\"\r\n  echo " + "long-command-".repeat(120) + "END_OF_COMMAND";
Object.defineProperty(navigator, "clipboard", { configurable: true, value: {
  writeText: async (text: string) => { copied.push(text); },
} });
const commands = {
  onPrompt: noAction, onFork: undefined, onDeliveryContinue: undefined, onAcceptDelivery: undefined,
  onOpenChanges: undefined, onOpenVerification: undefined, onEditPrompt: undefined,
  onRewind: undefined, onLoadOlderHistory: undefined, onSurfacePaintReady: undefined,
};
function Fixture() {
  const t = useT();
  const [options, setOptions] = useState<Options>({
    layout: "creation", width: "full", sidebar: true, dock: false, launcher: false, long: true, turns: 2, text: null, streaming: false, shell: false,
  });
  const [revision, setRevision] = useState(0);
  const items = useMemo(() => Array.from({ length: options.turns }, (_, index): Item[] => [
    { kind: "user", id: "user-" + index, text: "USER MESSAGE MUST REMAIN VISIBLE " + index },
    options.shell
      ? { kind: "tool", id: "tool-" + index, name: "bash", status: "done", readOnly: false,
        args: JSON.stringify({ command }), output: "OUTPUT_STAYS_VISIBLE\n" + "abcdefghij".repeat(60) }
      : { kind: "assistant", id: "answer-" + index, text: options.text ?? ("\`\`\`text\n" + (options.long && (options.turns < 100 || index === 10) ? "abcdefghij".repeat(60) : "short") + "\n\`\`\`"), reasoning: "", streaming: options.streaming },
  ]).flat(), [options.long, options.turns, options.text, options.streaming, options.shell]);
  useLayoutEffect(() => {
    document.documentElement.dataset.themeStyle = "graphite";
    document.documentElement.dataset.theme = "light";
    document.documentElement.dataset.platform = "windows";
    applyConversationWidth(options.width);
    window.transcriptLayoutFixture = {
      configure: next => { setOptions(current => ({ ...current, ...next })); setRevision(value => value + 1); },
      revision,
      copied,
      command,
    };
  }, [options.width, revision]);
  const transcript: ChatPaneTranscriptInput = {
    state: { ...initialState, items, running: options.streaming }, items, tabId: "layout-fixture", geometrySessionKey: "layout-fixture",
    footerHeight: 100, invocationMetadata: undefined, surfaceCommitToken: undefined,
    liveStore: undefined, transcriptHydrating: false, navigationDataReady: true, readOnly: false,
    controllerReady: true, hydratePlaceholderActive: false, clearContextPending: false,
    creation: options.layout === "creation", availability: { kind: "ready", source: "history" },
    rewind: { stateActive: false, committing: false },
  };
  return <div className={"app app--windows app--windows-frameless app--" + options.layout} data-fixture-revision={revision}>
    <div className={`layout${options.sidebar ? "" : " layout--sidebar-collapsed"}${options.dock ? " layout--workspace-open" : ""}`}
      style={{ "--sidebar-expanded-width": options.layout === "creation" ? "236px" : "300px", "--workspace-width": "300px" } as CSSProperties}>
      <header className="topicbar">Transcript layout fixture</header>
      <aside className="sidebar">Sidebar</aside>
      <div className="chat-pane">
        <ChatPaneRegion transitioning={false} t={t} imDetail={null} remote={undefined} commands={commands}
          transcript={transcript} onRetryHistory={async () => {}}
          launcher={options.launcher ? <DockLauncher tabId="layout-fixture" scopeKey="layout-fixture"
            workspaceRoot="" visible onSelect={noAction} overlay={options.dock} /> : undefined} />
        <footer className="footer"><div className="composer-wrap"><textarea aria-label="Draft" defaultValue="Draft remains inside the chat pane" /></div></footer>
      </div>
      {options.dock && <aside className="workbench-dock" style={{ gridColumn: 3, gridRow: 2 }}>Workspace</aside>}
    </div>
  </div>;
}
createRoot(document.getElementById("root")!).render(<LocaleProvider><Fixture /></LocaleProvider>);
