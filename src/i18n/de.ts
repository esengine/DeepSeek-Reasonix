import { EN } from "./EN.js";
import type { TranslationSchema } from "./types.js";

export const de: TranslationSchema = {
  ...EN,
  common: {
    ...EN.common,
    error: "Fehler",
    warning: "Warnung",
    loading: "Wird geladen...",
    done: "Fertig",
    cancel: "Abbrechen",
    confirm: "Bestätigen",
    back: "Zurück",
    next: "Weiter",
    tool: "Werkzeug",
    running: "läuft",
    noTurns: "(noch keine Turns)",
  },
  cli: {
    ...EN.cli,
    description: "DeepSeek-natives Agent-Framework, gebaut für Cache-Treffer und günstige Tokens.",
    continue: "Die zuletzt verwendete Chat-Sitzung fortsetzen, ohne die Auswahl anzuzeigen.",
    setup: "Interaktiver Assistent für API-Schlüssel und MCP-Server. Jederzeit erneut ausführbar.",
    chat: "Interaktive Ink-TUI mit Live-Cache- und Kostenanzeige.",
    run: "Eine einzelne Aufgabe nicht-interaktiv ausführen, Ausgabe wird gestreamt.",
    stats: "Nutzungsdashboard anzeigen.",
    doctor: "Gesundheitscheck mit einem Befehl.",
    code: "Code-Editor-Chat — Dateisystem-Werkzeuge mit Wurzel in <dir> (Standard: cwd), Coding-System-Prompt, v4-flash-Baseline.",
    commit: "Commit-Nachricht aus der gestagten Diff entwerfen.",
    sessions: "Gespeicherte Chat-Sitzungen auflisten oder nach Name anzeigen.",
    pruneSessions:
      "Inaktive Sitzungen ab N Tagen löschen (Standard 90). Mit --dry-run zur Vorschau.",
    events: "Kernel-Event-Log-Seite lesbar ausgeben.",
    replay: "Interaktive Ink-TUI zum Durchblättern eines Transkripts.",
    diff: "Zwei Transkripte in einer geteilten Ink-TUI vergleichen.",
    mcp: "Model-Context-Protocol-Hilfsprogramme — Server entdecken, Setup testen.",
    index: "Lokalen semantischen Suchindex erstellen (oder inkrementell aktualisieren).",
    version: "Reasonix-Version ausgeben.",
    update: "Nach einer neueren Reasonix-Version suchen und installieren.",
  },
  stats: {
    ...EN.stats,
    usageHint: "Führe `reasonix chat`, `reasonix code` oder `reasonix run <task>` aus – jeden Turn",
    usageDetail: "Hängt eine Zeile an das Log an; `reasonix stats` fasst sie zusammen.",
  },
  run: {
    ...EN.run,
    missingApiKey:
      "DEEPSEEK_API_KEY ist nicht gesetzt und stdin ist kein TTY (Nachfrage nicht möglich).\n" +
      "Setze die Umgebungsvariable oder starte einmal interaktiv `reasonix chat`, um einen Schlüssel zu speichern.\n",
  },
  sessions: {
    ...EN.sessions,
    emptyHint:
      "Noch keine gespeicherten Sitzungen – starte `reasonix chat` (Sitzungen werden automatisch gespeichert, außer mit --no-session).",
    listHeader: "Gespeicherte Sitzungen (~/.reasonix/sessions/):",
    inspectHint: "Ansehen:       reasonix sessions <name>",
    resumeHint: "Fortsetzen:    reasonix chat --session <name>",
    noSession: 'Keine Sitzung namens "{name}" (oder sie ist leer).',
    lookedAt: "Angesehen: {path}",
    noIdleSessions: "Keine Sitzungen seit >= {days} Tagen inaktiv. Nichts bereinigt.",
    wouldPrune: "Würde {count} Sitzung(en) bereinigen, die >= {days} Tage inaktiv sind:",
    dryRunHint: "Ohne --dry-run erneut ausführen, um wirklich zu löschen.",
    prunedCount: "{count} Sitzung(en) bereinigt, die >= {days} Tage inaktiv waren:",
    daysInvalid: "--days muss eine positive ganze Zahl sein (erhalten: {days}).",
  },
  ui: {
    ...EN.ui,
    tipShownOnce: "einmal angezeigt",
    modelOverride: "das Standardmodell überschreiben",
    noSession: "Sitzungsspeicherung für diesen Durchlauf deaktivieren",
    noMouseHint:
      "SGR-Mausverfolgung deaktivieren; stellt die native Auswahl per Ziehen und Rechtsklick wieder her",
    noProxyHint: "HTTPS_PROXY / HTTP_PROXY für diesen Durchlauf ignorieren; direkt verbinden",
    resumeHint: "die angegebene Sitzung fortsetzen (auch wenn inaktiv)",
    newHint: "Eine neue Sitzung erzwingen (--session / --continue ignorieren)",
    transcriptHint: "Pfad zum Speichern der JSONL-Ausgabe",
    budgetHint: "Sitzungs-USD-Obergrenze – warnt bei 80 %, verweigert den nächsten Zug bei 100 %",
    modelIdHint: "DeepSeek-Modell-ID (z. B. deepseek-v4-flash)",
    systemPromptHint: "den Standard-System-Prompt überschreiben",
    effortHint: "Reasoning-Effort – niedrig|mittel|hoch|maximal",
    sessionNameHint: "Sitzungsname (Standard: „default“)",
    ephemeralHint: "Sitzungsspeicherung für diesen Durchlauf deaktivieren",
    mcpSpecHint: "MCP-Server-Spezifikation (wiederholbar)",
    mcpPrefixHint: "Präfix für MCP-Toolnamen",
    noConfigHint: "Ignoriere bei diesem Durchlauf die Datei ~/.reasonix/config.json",
    effortHintShort: "Reasoning-Effort – niedrig|mittel|hoch|maximal",
    budgetHintShort: "Sitzungs-USD-Obergrenze",
    transcriptHintShort: "Pfad zum JSONL-Transkript",
    mcpSpecHintShort: "MCP-Server-Spezifikation (wiederholbar)",
    mcpPrefixHintShort: "Präfix für MCP-Toolnamen",
    dryRunHint: "anzeigen, was installiert würde, ohne es tatsächlich zu installieren",
    rebuildHint: "den Index komplett neu erstellen",
    embedModelHint: "Name des Einbettungsmodells",
    projectDirHint: "Projektstammverzeichnis",
    ollamaUrlHint: "Ollama-Server-URL",
    skipPromptsHint: "Bestätigungsaufforderungen überspringen",
    verboseHint: "Alle Metadaten der Sitzung anzeigen",
    pruneDaysHint:
      "Sitzungen löschen, die seit mindestens dieser Anzahl von Tagen inaktiv sind (Standard: 90)",
    pruneDryRunHint: "Liste auf, was gelöscht würde, ohne etwas zu entfernen",
    eventTypeHint: "Nach Ereignistyp filtern",
    eventSinceHint: "Beginne mit dieser Ereignis-ID",
    eventTailHint: "Nur die letzten N Ereignisse anzeigen",
    jsonHint: "Ausgabe als JSON",
    projectionHint: "Zeige den voraussichtlichen Zustand bei jedem Ereignis an",
    printHint: "Anzeige über stdout statt über die TUI",
    headHint: "Zeige nur die ersten N Ereignisse an",
    tailHint: "Nur die letzten N Ereignisse anzeigen",
    mdReportHint: "Erstelle einen Markdown-Diff-Bericht unter diesem Pfad",
    printHintTable: "Eine Tabelle auf die Standardausgabe ausgeben",
    tuiHint: "Öffne die interaktive TUI",
    labelAHint: "Bezeichnung für den linken Bereich",
    labelBHint: "Bezeichnung für den rechten Bereich",
    mcpListDescription: "Durchsuche das MCP-Register (offiziell → smithery → lokaler Fallback)",
    mcpInspectDescription:
      "die Spezifikationen eines MCP-Servers prüfen (Tools, Ressourcen, Eingabeaufforderungen)",
    mcpSearchDescription:
      "Suche in der MCP-Registrierung nach Servern, die einer Suchanfrage entsprechen",
    mcpInstallDescription:
      "Einen MCP-Server anhand seines Namens installieren (schreibt dessen Spezifikation in deine Konfiguration)",
    mcpBrowseDescription:
      "Interaktiver Marktplatz-Browser – tippe, um zu filtern, drücke die Eingabetaste, um zu installieren",
    mcpLocalHint: "Nur den mitgelieferten Offline-Katalog anzeigen",
    mcpRefreshHint: "den 24-Stunden-Cache umgehen und neu abrufen",
    mcpLimitHint: "Maximale Anzahl der anzuzeigenden Einträge",
    mcpPagesHint: "Lade gleich so viele Seiten (Standard: 1)",
    mcpAllHint: "Jede Seite laden (beim ersten Mal etwas langsam)",
    mcpMaxPagesHint:
      "Begrenze die Anzahl der Seiten, die bei der Suche durchsucht werden sollen (Standard: 20)",
    jsonHintCatalog: "Ausgabe als JSON",
    jsonHintReport: "Gib den Inspektionsbericht als JSON aus",
    modelOverrideFlash: "das Modell überschreiben (Standard: deepseek-v4-flash)",
    skipConfirmHint: "Die Bestätigungsabfrage überspringen",
    yoloHint:
      "Plan-Checkpoints für diesen Aufruf automatisch genehmigen (entspricht editMode=yolo, ohne die Konfiguration zu ändern)",
    welcome:
      "Starte jederzeit `reasonix`, um zu chatten – deine Einstellungen bleiben gespeichert.",
    taglineChat: "DeepSeek-nativer Agent",
    taglineCode: "DeepSeek-nativer Coding-Agent",
    taglineSub: "cache-first · flash-first",
    startSessionHint: "Tippe eine Nachricht, um deine Sitzung zu starten",
    inputPlaceholder: "Frag etwas... (tippe / für Befehle, @ für Dateien)",
    busy: "Denke nach...",
    thinking: "▸ denke nach...",
    undo: "Rückgängig",
    undoHint: "Drücke innerhalb von 5s zum Rückgängig-Machen",
    applied: "angewendet",
    rejected: "abgelehnt",
    noDashboard: "Automatisch gestartetes eingebettetes Web-Dashboard unterdrücken.",
    openDashboardHint:
      "Dashboard-URL sofort im Standard-Browser öffnen, sobald der Server bereit ist. Keine Wirkung bei --no-dashboard.",
    dashboardPortHint:
      "Dashboard auf einen festen Port (1–65535) festlegen. Stabil über Neustarts hinweg — erforderlich für SSH-Tunnel. Standard: ephemeral.",
    dashboardPortInvalid:
      "▲ --dashboard-port={value} wird ignoriert (muss eine ganze Zahl 1–65535 sein) — Rückfall auf ephemeral",
    dashboardAutoStartFailed:
      "▲ Dashboard-Autostart fehlgeschlagen ({reason}) — /dashboard versuchen oder --no-dashboard zum Unterdrücken",
    systemAppendHint:
      "Anweisungen an den Code-System-Prompt anhängen. Ersetzt NICHT den Standard-Prompt — wird danach eingefügt.",
    systemAppendFileHint:
      "Dateiinhalte an den Code-System-Prompt anhängen. Ersetzt NICHT den Standard-Prompt. UTF-8, relativ zu cwd oder absolut.",
    resumedSession:
      '▸ Sitzung "{name}" fortgesetzt mit {count} vorherigen Nachrichten · /new für frischen Start · /sessions zum Verwalten',
    newSession:
      '▸ Sitzung "{name}" (neu) — automatisch gespeichert während des Chattens · /sessions zum Umbenennen oder Löschen',
    ephemeralSession:
      "▸ ephemerer Chat (keine Sitzungspersistenz) — --no-session weglassen zum Aktivieren",
    restoredEdits:
      "▸ {count} ausstehende Edit-Block(s) aus einem unterbrochenen vorherigen Durchlauf wiederhergestellt — /apply zum Übernehmen oder /discard zum Verwerfen.",
    resumedPlan: "Fortgesetzter Plan · {when}{summary}",
  },
  code: {
    ...EN.code,
    workspaceConflict:
      "⚠ Arbeitsbereich enthält Dateien einer anderen Agent-Plattform ({platforms}). Reasonix Code kann sie als Projektinhalt lesen; starte mit --dir <dein-projekt> neu, falls das nicht gewünscht ist.\n",
    systemAppendEmpty: "--system-append ist leer — kein Prompt-Text wird angehängt\n",
    systemAppendFileReadError:
      'Fehler: kann --system-append-file "{filePath}" nicht lesen: {errorDetails}\n',
  },
  slash: {
    ...EN.slash,
    help: { ...EN.slash.help, description: "Vollständige Befehlsreferenz anzeigen" },
    status: { ...EN.slash.status, description: "Aktuelles Modell, Flags, Kontext und Sitzung" },
    effort: {
      ...EN.slash.effort,
      argsHint: "<niedrig|mittel|hoch|max>",
      description:
        "Reasoning-Effort-Grenze (low|medium|high|max); high ist der sichere Standard für vLLM/Azure",
    },
    model: {
      ...EN.slash.model,
      description: "DeepSeek-Modell-ID wechseln",
    },
    models: {
      ...EN.slash.models,
      description: "Verfügbare Modelle von DeepSeek /models abrufen",
    },
    language: {
      description: "Laufzeitsprache wechseln",
      argsHint: "<EN|zh-CN|de>",
      success: "Sprache auf Deutsch umgestellt.",
      unsupported: "Nicht unterstützter Sprachcode: {code}. Unterstützt: {supported}.",
    },
    budget: {
      ...EN.slash.budget,
      description:
        "Session-USD-Grenze — warnt bei 80 %, verweigert nächsten Turn bei 100 %. Standardmäßig aus. /budget allein zeigt Status.",
    },
    mcp: { ...EN.slash.mcp, description: "MCP-Server + Tools dieser Sitzung auflisten" },
    resource: {
      ...EN.slash.resource,
      description:
        "MCP-Ressourcen durchsuchen + lesen (kein Arg → URIs auflisten; <uri> → Inhalt abrufen)",
    },
    prompt: {
      ...EN.slash.prompt,
      argsHint: "[Name]",
      description:
        "MCP-Prompts durchsuchen + abrufen (kein Arg → Namen auflisten; <name> → Prompt rendern)",
    },
    memory: {
      ...EN.slash.memory,
      argsHint: "[Liste|<Name> anzeigen|<Name> vergessen|<Bereich> löschen – Bestätigen]",
      description: "Pinned Memory anzeigen / verwalten (REASONIX.md + ~/.reasonix/memory)",
    },
    skill: {
      ...EN.slash.skill,
      description:
        "Benutzer-Skills auflisten / ausführen (Projekt + benutzerdefiniert + global + builtin)",
    },
    hooks: {
      ...EN.slash.hooks,
      argsHint: "[Neu laden]",
      description:
        "Aktive Hooks auflisten (settings.json unter .reasonix/) · reload liest von Platte neu",
    },
    permissions: {
      ...EN.slash.permissions,
      argsHint:
        "[Liste|<Präfix> hinzufügen|<Präfix|N> entfernen|Löschen (Bestätigung erforderlich)]",
      description:
        "Shell-Allowlist anzeigen / bearbeiten (builtin schreibgeschützt · pro Projekt: ~/.reasonix/config.json)",
    },
    dashboard: {
      ...EN.slash.dashboard,
      argsHint: "[Stopp]",
      description: "Eingebettetes Web-Dashboard starten (127.0.0.1, token-gesichert)",
    },
    update: {
      ...EN.slash.update,
      description: "Aktuelle vs. neueste Version anzeigen + Upgrade-Befehl",
    },
    stats: {
      ...EN.slash.stats,
      description:
        "Sitzungsübergreifendes Kosten-Dashboard (heute / Woche / Monat / gesamt · Cache-Treffer · vs. Claude)",
    },
    cost: {
      ...EN.slash.cost,
      argsHint: "[Text]",
      description:
        "Ohne Text → Ausgaben letzter Turn (Kostenkarte); Mit Text → Kostenschätzung für als nächster Senden (worst-case + likely-cache)",
    },
    doctor: {
      ...EN.slash.doctor,
      description: "Gesundheitscheck (API / Config / API-Reichweite / Index / Hooks / Projekt)",
    },
    context: {
      ...EN.slash.context,
      description: "Context-Window-Aufschlüsselung (System / Tools / Log / Input)",
    },
    retry: {
      ...EN.slash.retry,
      description: "Letzte Nachricht kürzen & erneut senden (frischer Sample)",
    },
    compact: {
      ...EN.slash.compact,
      argsHint: "[Token]",
      description:
        "Überdimensionierte Tool-Ergebnisse + Tool-Call-Args im Log kürzen; Grenze in Tokens, Standard 4000",
    },
    cwd: {
      ...EN.slash.cwd,
      argsHint: "[Pfad]",
      description:
        "Workspace-Root mid-Session wechseln — FS-/Shell-/Memory-Tools neu ausrichten, Projekt-Hooks neu laden, @-Mention-Walker aktualisieren",
    },
    stop: {
      ...EN.slash.stop,
      description: "Aktuellen Modell-Turn abbrechen (getippte Alternative zu Esc)",
    },
    feedback: {
      ...EN.slash.feedback,
      description: "GitHub-Issue mit Diagnoseinfo öffnen (in Zwischenablage kopiert)",
    },
    about: { ...EN.slash.about, description: "Projektinfo — Version, Website, Repo, Lizenz" },
    keys: { ...EN.slash.keys, description: "Tastatur + Maus + Kopieren/Einfügen-Referenz" },
    plans: {
      ...EN.slash.plans,
      description: "Aktive + archivierte Pläne dieser Sitzung auflisten, neueste zuerst",
    },
    replay: {
      ...EN.slash.replay,
      description:
        "Archivierten Plan als schreibgeschützte Time-Travel-Schnappschuss laden (Standard: neuester)",
    },
    sessions: {
      ...EN.slash.sessions,
      description: "Gespeicherte Sitzungen auflisten (aktuelle mit ▸ markiert)",
    },
    title: {
      ...EN.slash.title,
      description: "Modell bitten, diese Sitzung anhand des Gesprächs umzubenennen",
    },
    qq: {
      ...EN.slash.qq,
      description:
        "QQ-Kanal verbinden, inspizieren oder trennen (erste Verbindung führt durch App-ID / App-Secret-Setup)",
    },
    setup: { ...EN.slash.setup, description: "Erinnert dich daran, `reasonix setup` auszuführen" },
    semantic: {
      ...EN.slash.semantic,
      description:
        "Semantic-Search-Status anzeigen — Index erstellt? Ollama installiert? Wie aktivieren?",
    },
    clear: {
      ...EN.slash.clear,
      description: "Nur sichtbaren Scrollback leeren (Log/Kontext bleibt)",
    },
    new: {
      ...EN.slash.new,
      description: "Frisches Gespräch beginnen (Kontext + Scrollback löschen)",
    },
    loop: {
      ...EN.slash.loop,
      argsHint: "<5s..6h> <Eingabeaufforderung>  ·  Stopp  ·  (keine Argumente = Status)",
      description:
        "Prompt automatisch alle <intervall> erneut senden, bis du etwas eingibst / Esc / /loop stop",
    },
    init: {
      ...EN.slash.init,
      description:
        "Projekt scannen und eine REASONIX.md-Baseline erstellen (Modell schreibt; mit /apply reviewen). `force` überschreibt vorhandene Datei.",
    },
    apply: {
      ...EN.slash.apply,
      description:
        "Ausstehende Edit-Blocks auf Platte schreiben (kein Arg → alle; `1`, `1,3` oder `1-4` → diese Teilmenge, Rest bleibt ausstehend)",
    },
    discard: {
      ...EN.slash.discard,
      description:
        "Ausstehende Edit-Blocks ohne Schreiben verwerfen (kein Arg → alle; Indizes → diese Teilmenge)",
    },
    walk: {
      ...EN.slash.walk,
      description:
        "Schrittweise durch ausstehende Edits gehen (git-add-p-Stil: y/n pro Block, a = Rest anwenden, A = AUTO umschalten)",
    },
    undo: { ...EN.slash.undo, description: "Letzten angewandten Edit-Batch rückgängig machen" },
    history: {
      ...EN.slash.history,
      description:
        "Jeden Edit-Batch dieser Sitzung auflisten (IDs für /show, rückgängig-Markierungen)",
    },
    show: {
      ...EN.slash.show,
      description: "Gespeicherte Edit-Diff ausgeben (ID weglassen für neuesten nicht-rückgängigen)",
    },
    commit: { ...EN.slash.commit, description: "git add -A && git commit -m ..." },
    checkpoint: {
      ...EN.slash.checkpoint,
      argsHint: "[Name|Liste|<ID> löschen]",
      description:
        "Jede Datei, die die Sitzung berührt hat, als Schnappschuss sichern (Cursor-artiger interner Speicher, nicht Git). /checkpoint allein listet auf.",
    },
    restore: {
      ...EN.slash.restore,
      description: "Dateien auf einen benannten Checkpoint zurücksetzen (siehe /checkpoint list)",
    },
    plan: {
      ...EN.slash.plan,
      argsHint: "[Ein|Aus]",
      description:
        "Schreibgeschützten Plan-Modus umschalten (Schreibzugriffe blockiert bis submit_plan + Genehmigung)",
    },
    mode: {
      ...EN.slash.mode,
      argsHint: "[Rezension|Auto|YOLO]",
      description:
        "Edit-Gate: review (Warteschlange) · auto (anwenden+rückgängig) · yolo (anwenden+auto-shell). Shift+Tab schaltet um.",
    },
    jobs: {
      ...EN.slash.jobs,
      description: "Hintergrund-Jobs auflisten, die mit run_background gestartet wurden",
    },
    kill: {
      ...EN.slash.kill,
      argsHint: "Bezeichner",
      description: "Hintergrund-Job nach ID beenden (SIGTERM → SIGKILL nach Gnadenfrist)",
    },
    logs: {
      ...EN.slash.logs,
      argsHint: "<id> [Zeilen]",
      description: "Ausgabe eines Hintergrund-Jobs anzeigen (Standard letzte 80 Zeilen)",
    },
    btw: {
      ...EN.slash.btw,
      argsHint: "<Frage>",
      description:
        "Kurze Randfrage stellen — wird von Grund auf beantwortet, nie zum Gesprächskontext hinzugefügt",
    },
    "search-engine": {
      ...EN.slash["search-engine"],
      description:
        "Web-Search-Backend wechseln — bing (Standard, funktioniert von CN ohne Proxy), searxng (selbst gehostet), metaso (kostenlos 100/Tag), tavily (kostenlos 1000/Monat), perplexity (AI-native) oder exa (AI-native)",
    },
    theme: {
      ...EN.slash.theme,
      argsHint: "[auto|dunkel|hell|mitternachtsblau|tiefblau|hoher Kontrast]",
      description: "Terminal-Theme anzeigen oder speichern. Ohne Argument öffnet die Auswahl.",
    },
    exit: { ...EN.slash.exit, description: "TUI beenden" },
  },
  wizard: {
    ...EN.wizard,
    languageTitle: "Sprache auswählen",
    languageSubtitle: "Aus der Systemsprache erkannt. Später mit /language wechselbar.",
    welcomeTitle: "Willkommen bei Reasonix.",
    apiKeyPrompt: "Füge deinen DeepSeek-API-Schlüssel ein, um loszulegen.",
    apiKeyGetOne: "Erhalte einen unter: https://platform.deepseek.com/api_keys",
    apiKeySavedLocally: "Lokal gespeichert unter {path}",
    apiKeyInputLabel: "Schlüssel > ",
    apiKeyPlaceholder: "sk-...",
    apiKeyInvalid:
      "Der Schlüssel wirkt zu kurz – füge den vollständigen Token ein (16+ Zeichen, keine Leerzeichen).",
    apiKeyChecking: "API-Schlüssel wird geprüft...",
    apiKeyRejected:
      "DeepSeek hat diesen API-Schlüssel abgelehnt. Füge einen gültigen Schlüssel ein oder brich das Setup mit Esc ab.",
    apiKeyCheckFailed:
      "Konnte diesen API-Schlüssel gerade nicht verifizieren ({message}). Überprüfe deine Netzwerkverbindung oder versuche es erneut.",
    apiKeyPreview: "Vorschau: {redacted}",
    themeTitle: "Theme auswählen",
    themeSubtitle: "Die Vorschau aktualisiert sich beim Navigieren. Später mit /theme änderbar.",
    themeSampleHeading: "Beispiel",
    themeFooter: "[↑↓] navigieren · [Enter] bestätigen · [Esc] abbrechen",
    themeCaption: {
      ...EN.wizard.themeCaption,
      dark: "Kühle dunkle Töne (Standard)",
      light: "Helle klare Ansicht",
      midnight: "Tokyo-Night-Palette",
      "deep-blue": "Tiefblau auf Schwarz",
      "high-contrast": "Barrierefreiheit",
    },
    mcpTitle: "Welche MCP-Server soll Reasonix für dich einrichten?",
    mcpUserArgsHint: "(du wirst {arg} bereitstellen)",
    mcpFooterMulti:
      "[↑↓] navigieren  ·  [Leertaste] umschalten  ·  [Enter] bestätigen  ·  [Esc] abbrechen  ·  leer = überspringen",
    mcpArgsTitle: "{name} konfigurieren",
    mcpArgsDirMissing: "Verzeichnis {path} existiert nicht.",
    mcpArgsDirCreateHint: "[Y/Enter] erstellen (mkdir -p) · [N/Esc] anderen Pfad eingeben",
    mcpArgsDirCreateFailed: "Konnte {path} nicht erstellen: {message}",
    mcpArgsRequiredParam: "Erforderlicher Parameter: ",
    mcpArgsEmpty: "{name} benötigt einen Wert — leere Zeichenkette erhalten.",
    mcpArgsNotADir: "{path} existiert, ist aber kein Verzeichnis.",
    reviewTitle: "Bereit zum Speichern",
    reviewLabelApiKey: "API-Schlüssel",
    reviewLabelLanguage: "Sprache",
    reviewLabelTheme: "Theme",
    reviewLabelMcp: "MCP",
    reviewMcpNone: "(keine)",
    reviewMcpServers: "{count} Server",
    reviewSavesTo: "Speichert nach {path}",
    reviewSaveError: "Konfiguration konnte nicht gespeichert werden: {message}",
    reviewFooter: "[Enter] speichern · [Esc] abbrechen",
    savedTitle: "▸ Gespeichert.",
    savedShellHint:
      'Shell-Befehle, die das Modell ausführen möchte, fragen jedes Mal nach – wähle »immer erlauben" in der Eingabeaufforderung, um diesen genauen Befehl für dieses Projekt auf die Whitelist zu setzen. Kein globales Allow-All-Flag (designbedingt).',
    savedFooter: "[Enter] zum Beenden",
    selectFooter: "[↑↓] navigieren · [Enter] bestätigen · [Esc] abbrechen",
    stepCounter: "Schritt {step}/{total} · ",
    exitHint: "/exit zum Abbrechen",
    themeSampleReasoning: "Denken",
  },
  themePicker: {
    ...EN.themePicker,
    header: "Theme",
    footer: "↑↓ auswählen · ⏎ bestätigen · Esc abbrechen",
    currentPref: "Aktuelle Einstellung",
    activeNow: "Jetzt aktiv",
    autoDesc: "REASONIX_THEME oder Standard verwenden",
  },
  planFlow: {
    ...EN.planFlow,
    approveCardTitle: "Plan genehmigen",
    approveCardMetaRight: "wartet",
    openQuestionsBanner:
      "▲ der Plan zeigt offene Fragen oder Risiken — wähle {refine}, um konkrete Antworten zu schreiben, bevor das Modell fortfährt.",
    openQuestionsHeader: "Offene Fragen / Risiken",
    truncatedBodyMore: "… {n} weitere Zeile oben im Scrollback",
    truncatedBodyMorePlural: "… {n} weitere Zeilen oben im Scrollback",
    picker: {
      ...EN.planFlow.picker,
      accept: "akzeptieren",
      acceptHint: "Jetzt ausführen, in Reihenfolge",
      refine: "verfeinern",
      refineHint: "Dem Agenten mehr Anweisungen geben, neuen Plan entwerfen",
      revise: "überarbeiten",
      reviseHint: "Plan inline bearbeiten vor der Ausführung (Schritte überspringen/neu ordnen)",
      reject: "ablehnen",
      rejectHint: "Verwerfen, Agent versucht von Grund auf neu",
    },
    refineFooter: "⏎ senden  ·  Esc zurück zur Auswahl",
    refineQuestionsHeading: "Beantworte diese oder beschreibe die gewünschte Änderung:",
    modes: {
      ...EN.planFlow.modes,
      approve: {
        ...EN.planFlow.modes.approve,
        title: "Genehmigen — letzte Anweisungen?",
        hint: "Beantworte Fragen aus dem Plan, füge Einschränkungen hinzu oder drücke einfach Enter zur Genehmigung.",
        blankHint: " (Enter ohne Text = ohne Zusatzanweisungen genehmigen.)",
      },
      refine: {
        ...EN.planFlow.modes.refine,
        title: "Verfeinern — was soll das Modell ändern?",
        hint: "Beschreibe, was falsch ist oder fehlt, oder beantworte Fragen aus dem Plan.",
        blankHint: " (Enter ohne Text = Modell wählt sichere Standardwerte für offene Fragen.)",
      },
      reject: {
        ...EN.planFlow.modes.reject,
        title: "Ablehnen — sag dem Modell warum (optional)",
        hint: "Sag dem Modell, was es an deinem Ziel falsch verstanden hat oder was du stattdessen möchtest.",
        blankHint:
          " (Enter ohne Text = ohne Erklärung abbrechen; das Modell fragt, was du möchtest.)",
      },
      "checkpoint-revise": {
        ...EN.planFlow.modes["checkpoint-revise"],
        title: "Überarbeiten — was soll sich vor dem nächsten Schritt ändern?",
        hint: "Umfangsänderung, Schritte überspringen, alternativer Ansatz — das Modell passt den Restplan an.",
        blankHint: " (Enter ohne Text = mit aktuellem Plan fortfahren.)",
      },
      "choice-custom": {
        ...EN.planFlow.modes["choice-custom"],
        title: "Benutzerdefinierte Antwort — schreibe, was passt",
        hint: "Freitext-Antwort. Das Modell liest sie wörtlich und fährt fort — keine Notwendigkeit, die aufgeführten Optionen zu treffen.",
        blankHint: " (Enter ohne Text = Modell fragen, was du eigentlich möchtest.)",
      },
    },
    checkpoint: {
      ...EN.planFlow.checkpoint,
      title: "Checkpoint — Schritt erledigt",
      continue: "Fortfahren — nächsten Schritt ausführen",
      continueHint: "Modell fährt mit dem nächsten Schritt fort.",
      finish: "Abschließen — zusammenfassen und beenden",
      finishHint:
        "Modell zeichnet den letzten Schritt auf und fasst den abgeschlossenen Plan zusammen.",
      revise: "Überarbeiten — Feedback vor dem nächsten Schritt geben",
      reviseHint: "Bleibe pausiert, tippe Anweisungen; Modell passt den Restplan an.",
      stop: "Anhalten — Plan hier beenden",
      stopHint: "Modell fasst zusammen, was getan wurde, und beendet.",
    },
    stepList: {
      ...EN.planFlow.stepList,
      counter: "{total} Schritte",
      counterSingular: "{total} Schritt",
      counterDone: "{done}/{total} erledigt ({pct}%) · {total} Schritte",
      counterDoneSingular: "{done}/{total} erledigt ({pct}%) · {total} Schritt",
    },
    noPlanSummary: "Noch kein Plan-Body übermittelt.",
    detailCollapsedHint: "Strg+P erweitert die vollständigen Plan-Details.",
    detailExpandedHint: "Strg+P klappt Details ein.",
    detailHeader: "Plan-Details",
    detailWindow: "Zeige Zeilen {start}-{end} von {total}",
    detailScrollHint: "Bild↑/Bild↓ scrollt Details · Pos1/Ende springt",
    reviseTitle: "Plan überarbeiten",
    reviseSteps: "{count} Schritte",
    reviseFooter:
      "↑↓ fokussieren  ·  Leertaste überspringen umschalten  ·  k/j verschieben  ·  ⏎ akzeptieren  ·  Esc abbrechen",
    riskMed: " mittel",
    riskHigh: " hoch",
    completeMsg: "▸ Plan abgeschlossen — alle {total} Schritt(e) erledigt · archiviert",
  },
  app: {
    ...EN.app,
    dashboardStopped: "▸ Dashboard gestoppt.",
    notedScopeProject: "Projekt",
    notedScopeGlobal: "global",
    commandFailed: "! Befehl fehlgeschlagen",
    btwFailed: "/btw fehlgeschlagen",
    walkCancelledRemaining: "▸ Walk abgebrochen — {count} Block(s) noch ausstehend.",
    walkCancelled: "▸ Walk abgebrochen.",
    editModeYolo:
      "▸ Edit-Modus: YOLO — Edits UND Shell-Befehle auto-ausführen. /undo macht Edits immer noch rückgängig. Vorsicht.",
    editModeAuto:
      "▸ Edit-Modus: AUTO — Edits werden sofort angewandt; drücke u innerhalb von 5s zum Rückgängigmachen (Leertaste pausiert den Timer). Shell-Befehle fragen weiterhin.",
    editModeReview: "▸ Edit-Modus: review — Edits warten auf /apply (oder y) / /discard (oder n)",
    rejectedEdit: "▸ Edit abgelehnt: {path}{context}",
    autoApprovingRest: "▸ Restliche Edits für diesen Turn werden automatisch genehmigt",
    flippedAutoSession: "▸ Für den Rest der Sitzung auf AUTO umgeschaltet (gespeichert)",
    flippedAutoWalk:
      "▸ Auf AUTO umgeschaltet — zukünftige Edits werden sofort angewandt. Walk beendet.",
    notedMemory: "▸ vermerkt ({scope}) — {verb} {path}",
    notedVerbCreated: "erstellt",
    notedVerbAppended: "Angehängt an",
    memoryWriteFailed: "# Speicherschreibfehler",
    verboseOn: "▸ Ausführlicher Modus an — vollständiges Reasoning + Tool-Ausgabe",
    verboseOff: "▸ Ausführlicher Modus aus — head/tail-Kürzung wiederhergestellt",
    steerInjected: "▸ Steuerung in Warteschlange — wird nach dem aktuellen Schritt hinzugefügt",
    steerCommandRejected: "▸ Befehle sind deaktiviert, während ein Turn gesteuert wird",
    btwUsage: "▸ /btw <Frage> — eine Randfrage stellen, ohne den Gesprächskontext zu verschmutzen.",
    btwHeader: "≫ btw",
    restoreCodeOnly: "▸ /restore ist nur im Code-Modus verfügbar",
    hookUserPromptSubmit: "UserPromptSubmit-Hook",
    hookStop: "Stop-Hook",
    atMentions: "▸ @mentions: {parts}",
    atUrl: "▸ @url: {parts}",
    atUrlFailed: "@url Erweiterung fehlgeschlagen",
    sessionTitleNoSession: "▸ Keine persistierte Sitzung aktiv, also nichts umzubenennen.",
    sessionTitleNoContent: "▸ Noch nicht genug Gesprächsinhalt, um diese Sitzung zu benennen.",
    sessionTitleNoTitle: "▸ Das Modell hat keinen brauchbaren Sitzungstitel zurückgegeben.",
    sessionTitleUpdated: '▸ Sitzungstitel aktualisiert: "{title}"',
    sessionTitleRenameFailed: '▸ Sitzung konnte nicht für Titel "{title}" umbenannt werden.',
    sessionTitleRenamed: '▸ Sitzung umbenannt in "{name}" — {title}',
    sessionTitleAutoRenamed: '▸ Automatisch benannte Sitzung "{name}" — {title}',
    workspaceSwitched: "▸ Arbeitsbereich gewechselt zu {root}",
    semanticRepointed: "▸ Semantic-Search umgeleitet nach {root}",
    semanticDisabledForRoot: "▸ Semantic-Search deaktiviert (kein kompatibler Index in {root})",
    semanticRebootstrapFailed: "▸ Semantic-Search-Neustart fehlgeschlagen: {reason}",
    denied: "▸ verweigert: {cmd}{context}",
    alwaysAllowed: '▸ "{prefix}" für {dir} dauerhaft erlaubt',
    runningCommand: "▸ führe aus: {cmd}",
    startingBackground: "▸ starte (Hintergrund): {cmd}",
    checkpointSaved:
      "⛁ Checkpoint gespeichert · {id} · {count} Datei(en) · /restore {id} zum Zurücksetzen",
    continuingAfter: "▸ fortgesetzt nach {label}{counter}",
    planStoppedAt: "▸ Plan angehalten bei {label}{counter}",
    revisingAfter: "▸ überarbeite nach {label} — {feedback}",
    historyScrollHint: " ↑ lese Verlauf · Ende / Bild↓ zurück zum Ende · ↓ eine Zeile vor",
    editHistoryTitle: "Edit-Verlauf (älteste zuerst):",
    editHistoryNoCodeMode: "Nicht im Code-Modus",
    editHistoryNoEdits: "Noch keine Edits in dieser Sitzung aufgezeichnet",
    editHistoryNoShowId:
      "Verwendung: /show [id] [pfad]   (ID weglassen für neueste; Pfad aus der Datei-Zusammenfassung)",
    editHistoryIdNotFound: "Kein Edit #{id} — /history ausführen für gültige IDs",
    editHistoryLookupFailed: "Unerwartet: History-Lookup fehlgeschlagen",
    editHistoryBatchNoFile: 'Batch #{id} enthält kein "{path}" — Dateien in diesem Batch: {files}',
    editHistoryNoEdits2: "Keine Edits in dieser Sitzung aufgezeichnet — /history ist leer",
    editHistoryStatusApplied: "angewandt",
    editHistoryStatusPartial: "TEILWEISE",
    editHistoryStatusUndone: "RÜCKGÄNGIG",
    editHistoryHelpShow:
      "/show <id>            → Zusammenfassung pro Datei    ·    /show <id> <pfad>  → vollständige Diff einer Datei",
    editHistoryHelpUndo:
      "/undo                 → neueste nicht-rückgängige   ·    /undo <id> [pfad]  → gezielten Batch oder Datei rückgängig machen",
    editHistoryAlreadyReverted:
      "(bereits rückgängig gemacht — /history zeigt den batch-level Status)",
    editHistoryRevertFile: "/undo {id} {path}  → nur diese Datei rückgängig machen",
    mcpFailed: "MCP {name} fehlgeschlagen",
    mcpWarn: "MCP {name} Warnung",
    unknownTheme: "Unbekanntes Theme: {name}\nVerfügbar: {choices}",
    themeSaved: "Theme gespeichert: {name}\nAktiv beim nächsten Start: {active}",
    noPendingEdits:
      "Nichts ausstehend — das Modell hat seit dem letzten /apply oder /discard keine Edits vorgeschlagen.",
    noMatchedApply:
      "▸ Keine Edits mit diesen Indizes gefunden — nichts angewandt. Verwende /apply ohne Argumente, um alle zu übernehmen.",
    noPendingDiscard: "Nichts ausstehend zum Verwerfen.",
    noMatchedDiscard: "▸ Keine Edits mit diesen Indizes gefunden — nichts verworfen.",
    blocksStillPending:
      "▸ {count} Edit-Block(s) noch ausstehend — /apply oder /discard zum Bereinigen.",
    nothingWritten: ". Nichts auf Platte geschrieben.",
    discardedCount: "▸ {count} ausstehende Edit-Block(s) verworfen",
    noEventsFor: 'Keine Ereignisse für Sitzung "{name}"',
    lookedAtFile: "Angesehen: {path}",
    sidecarHint:
      "(Sitzungen erstellen den Sidecar automatisch beim ersten Turn — wurde diese Sitzung bereits ausgeführt?)",
  },
  hooks: {
    ...EN.hooks,
    head: "Hook {tag} `{cmd}` {decision}{truncTag}",
    headWithDetail: "Hook {tag} `{cmd}` {decision}{truncTag}: {detail}",
    truncated: " (Ausgabe bei 256 KB gekürzt)",
    decisionBlock: "blockieren",
    decisionWarn: "warnen",
    decisionTimeout: "Timeout",
    decisionError: "Fehler",
  },
  summary: {
    ...EN.summary,
    status: "Zusammenfassung der gesammelten Informationen...",
    hallucinatedFallback:
      "(Modell hat gefälschte Tool-Call-Markup statt einer Prosa-Zusammenfassung ausgegeben — versuche /retry mit einer engeren Frage, oder /think zur Inspektion von R1s Reasoning)",
    failedAfterReason:
      "{label} und der Fallback-Summary-Aufruf sind fehlgeschlagen: {message}. Führe /clear aus und versuche es mit einer engeren Frage, oder erhöhe --max-tool-iters.",
  },
  loop: {
    ...EN.loop,
    budgetExhausted:
      "Sitzungsbudget erschöpft — ${spent} ausgegeben ≥ Grenze ${cap}. Erhöhe die Grenze mit /budget <usd>, schalte sie mit /budget off aus oder beende die Sitzung.",
    budget80Pct:
      "▲ Budget zu 80 % verbraucht — ${spent} von ${cap}. Der nächste oder übernächste Turn erreicht wahrscheinlich die Grenze.",
    proArmed:
      "⇧ /pro aktiviert — dieser Turn läuft auf deepseek-v4-pro (einmalig · deaktiviert nach dem Turn)",
    toolUploadStatus: "Tool-Ergebnis hochgeladen – Modell denkt vor der nächsten Antwort...",
    turnStartFoldStatus: "Turn-Start: Kontext nähert sich Grenze, komprimiere Verlauf...",
    turnStartFolded:
      "Turn-Start: Anfrage ~{estimate}/{ctxMax} Tokens ({pct}%) — {beforeMessages} Nachrichten → {afterMessages} komprimiert. Sende.",
    harvestStatus: "Planstatus wird aus dem Reasoning extrahiert...",
    repeatToolCallWarning:
      "Wiederholten Tool-Aufruf erkannt — lasse das Modell das Problem sehen und es mit einem anderen Ansatz erneut versuchen.",
    stormStuck:
      "Festgefahrene Wiederholungsschleife gestoppt — das Modell rief dasselbe Tool mit identischen Argumenten auf, selbst nach einem Selbstkorrektur-Hinweis. Versuche /retry, umformulieren oder schließe den zugrunde liegenden Blocker aus.",
    stormSuppressed:
      "{count} wiederholte Tool-Aufrufe unterdrückt — gleicher Name + Argumente 3+ Mal gesendet.",
    compactingHistoryStatus: "Komprimiere Verlauf{aggressiveTag}...",
    aggressiveTag: " (aggressiv)",
    foldedHistory:
      "Kontext {before}/{ctxMax} ({pct}%) — {beforeMessages} Nachrichten → {afterMessages} gefaltet (Zusammenfassung {summaryChars} Zeichen). Fahre fort.",
    aggressivelyFoldedHistory:
      "Kontext {before}/{ctxMax} ({pct}%) — {beforeMessages} Nachrichten → {afterMessages} aggressiv gefaltet (Zusammenfassung {summaryChars} Zeichen). Fahre fort.",
    forcingSummary:
      "Kontext {before}/{ctxMax} ({pct}%) — erzwinge Zusammenfassung aus dem Gesammelten. Führe /compact, /clear oder /new aus, um zurückzusetzen.",
  },
  errors: {
    ...EN.errors,
    contextOverflow:
      "Context-Überlauf (DeepSeek 400): Sitzungsverlauf ist {requested}, über dem Prompt-Limit des Modells (V4: 1M Tokens; legacy chat/reasoner: 131k). Meist ist ein einzelnes Tool-Ergebnis zu groß geworden. Reasonix begrenzt neue Tool-Ergebnisse auf 8k Tokens und heilt überdimensionierte Verläufe automatisch beim Sitzungsladen – ein Neustart behebt es oft. Falls es weiterhin überläuft, führe /new für einen frischen Start aus oder öffne /sessions und drücke [d], um diese Sitzung zu löschen.",
    contextOverflowTooMany: "Zu viele Tokens",
    auth401:
      "Authentifizierung fehlgeschlagen (DeepSeek 401): {inner}. Dein API-Schlüssel wird abgewiesen. Behebe mit `reasonix setup` oder `export DEEPSEEK_API_KEY=sk-...`. Erhalte einen unter https://platform.deepseek.com/api_keys.",
    balance402:
      "Kontoguthaben aufgebraucht (DeepSeek 402): {inner}. Lade auf unter https://platform.deepseek.com/top_up — der Panel-Header zeigt dein Guthaben, sobald es nicht Null ist.",
    badparam422: "Ungültiger Parameter (DeepSeek 422): {inner}",
    badrequest400: "Fehlerhafte Anfrage (DeepSeek 400): {inner}",
    concurrency429:
      "DeepSeek-Gleichzeitigkeitslimit erreicht (429): {inner}. Das Konto hat zu viele gleichzeitige Anfragen (Grenze: 500 für v4-pro, 2500 für v4-flash, summiert über alle API-Schlüssel des Kontos). Meist läuft ein weiterer Reasonix-Prozess mit demselben Schlüssel oder ein paralleler Subagent-Fan-out hat überzogen. Warte einige Sekunden und wiederhole, reduziere die Parallelität oder beantrage eine höhere Grenze unter https://platform.deepseek.com.",
    deepseek5xxHead:
      "DeepSeek-Dienst nicht verfügbar ({status}) — dies ist ein DeepSeek-seitiges Problem, nicht Reasonix. Bereits 4× mit Backoff wiederholt.",
    deepseek5xxReachable:
      " DeepSeek's Haupt-API hat auf unseren Health-Check geantwortet, aber /chat/completions schlägt fehl — partieller Ausfall auf ihrer Seite.",
    deepseek5xxUnreachable:
      " DeepSeek-API ist von deinem Netzwerk aus nicht erreichbar — könnte ein größerer DS-Ausfall oder ein lokales Netzwerkproblem sein.",
    deepseek5xxActionNetwork:
      " Versuche: (1) Netzwerk prüfen, (2) 30s warten und wiederholen, (3) Statusseite: https://status.deepseek.com.",
    deepseek5xxActionRetry:
      " Versuche: (1) 30s warten und wiederholen, (2) /model zum Modellwechsel, (3) Statusseite: https://status.deepseek.com.",
    upstream5xxHead:
      "Upstream-Dienst nicht verfügbar ({status}) bei {host} — der konfigurierte API-Endpunkt hat einen Serverfehler zurückgegeben, kein Reasonix-Fehler. Bereits 4× mit Backoff wiederholt.",
    upstream5xxActionRetry:
      " Versuche: (1) Prüfen, ob der lokale/Proxy-Modell-Server läuft, (2) warten und wiederholen, (3) /model zum Modellwechsel.",
    innerNoMessage: "(keine Nachricht)",
    reasonAborted:
      "[vom Benutzer abgebrochen (Esc) — fasse zusammen, was ich bisher gefunden habe]",
    reasonContextGuard:
      "[Context-Budget wird knapp — fasse zusammen, bevor der nächste Aufruf überläuft]",
    reasonStuck:
      "[festgefahren bei wiederholtem Tool-Aufruf — erkläre, was versucht wurde und was den Fortschritt blockiert]",
    labelAborted: "Vom Benutzer abgebrochen",
    labelContextGuard: "Context-Guard ausgelöst (Prompt > 80 % des Fensters)",
    labelStuck: "Festgefahren (wiederholter Tool-Aufruf durch Storm-Breaker unterdrückt)",
  },
  handlers: {
    ...EN.handlers,
    basic: {
      ...EN.handlers.basic,
      newInfo:
        "▸ neues Gespräch — {count} Nachricht(en) aus dem Kontext entfernt. Gleiche Sitzung, frische Grundlage.",
      newInfoArchived:
        '▸ neues Gespräch — {count} Nachricht(en) aus dem Kontext entfernt. Vorheriges Transkript als "{archived}" archiviert (sichtbar unter Sitzungen).',
      newInfoSystemReloaded:
        " · REASONIX.md / Projekt-Memory neu geladen (nächster Turn zahlt einen Cache-Fehler)",
      helpTitle: "Befehle:",
      helpShellTitle: "Shell-Kürzel:",
      helpShell: "  !<befehl>                 <befehl> im Sandbox-Root ausführen; Ausgabe kommt",
      helpShellDetail:
        "                             in die Konversation, sodass das Modell sie im nächsten Turn sieht.",
      helpShellConsent:
        "                             Kein Allowlist-Gate — vom Benutzer getippt = explizite Zustimmung.",
      helpShellExample: "                             Beispiel: !git status   !ls src/   !npm test",
      helpShellGateTitle: "Vom Modell aufgerufene Shell-Befehle (pro Aufruf Genehmigung):",
      helpShellGate:
        "  ↑↓ + ⏎                   jeder Aufruf zeigt eine Eingabeaufforderung mit \u00bbEinmal erlauben\u00ab / \u00bbImmer erlauben\u00ab",
      helpShellGateDetail:
        "                             / \u00bbAblehnen\u00ab. W\u00e4hle \u00bbImmer erlauben\u00ab, um diesen genauen",
      helpShellGatePolicy:
        "                             Befehlspräfix für dieses Projekt auf die Whitelist zu setzen. Kein globales Allow-All-Flag.",
      helpMemoryTitle: "Kurzzeit-Memory:",
      helpMemoryPin:
        "  #<notiz>                  <notiz> an <projekt>/REASONIX.md anhängen (commitierbar).",
      helpMemoryPinEx:
        "                             Beispiel: #findByEmail muss case-insensitive sein",
      helpMemoryGlobal:
        "  #g <notiz>                <notiz> an ~/.reasonix/REASONIX.md anhängen (global, niemals committed).",
      helpMemoryGlobalEx:
        "                             Beispiel: #g immer pnpm, nicht npm verwenden",
      helpMemoryPinBoth:
        "                             Beide werden in jedes zukünftige Sitzungs-Präfix eingefügt. Schneller als /memory.",
      helpMemoryEscape:
        "                             Verwende `\\#text`, um ein literales `#text` an das Modell zu senden.",
      helpFileTitle: "Dateiverweise (Code-Modus):",
      helpFile:
        "  @pfad/zu/datei            Dateiinhalt unter [Referenzierte Dateien] beim Senden einfügen.",
      helpFilePicker:
        "                             Tippe `@`, um die Auswahl zu öffnen (↑↓ navigieren, Tab/Enter auswählen).",
      helpUrlTitle: "URL-Verweise:",
      helpUrl:
        "  @https://example.com     URL abrufen, HTML entfernen, unter [Referenzierte URLs] einfügen.",
      helpUrlCache:
        "                             Gleiche URL zweimal in einer Sitzung wird nur einmal abgerufen (In-Mem-Cache).",
      helpUrlPunct:
        "                             Abschluss-Satzzeichen (./,/)) werden automatisch entfernt.",
      helpSessionsTitle: "Sitzungen (standardmäßig aktiviert, heißen 'default'):",
      helpSessionCustom:
        "  reasonix chat --session <name>   eine andere benannte Sitzung verwenden",
      helpSessionNone: "  reasonix chat --no-session       Persistenz für diesen Lauf deaktivieren",
      retryNone: "Nichts zu wiederholen — keine vorherige Benutzernachricht im Log dieser Sitzung.",
      retryInfo: '▸ wiederhole: "{preview}"',
      loopTuiOnly: "/loop ist nur in der interaktiven TUI verfügbar (nicht in run/replay).",
      loopStopped: "▸ Loop gestoppt.",
      loopNoActive: "Kein aktiver Loop zum Stoppen.",
      loopNoActiveHint:
        "Kein aktiver Loop. Starte einen mit `/loop <intervall> <prompt>` (z.B. /loop 30s npm test).\nWird abgebrochen bei: /loop stop · Esc · /clear /new · jeder benutzereingegebene Prompt.",
      loopStarted:
        '▸ Loop gestartet — »{prompt}" wird alle {duration} erneut gesendet. Tippe etwas (oder /loop stop) zum Abbrechen.',
      keysNeedsTui: "/keys benötigt einen TUI-Kontext (postKeys angeschlossen).",
      aboutHeader: "Reasonix v{version} — ein Cache-First-DeepSeek-Coding-Agent",
      aboutWebsiteLabel: "Webseite",
      aboutRepoLabel: "GitHub ",
      aboutLicenseLabel: "Lizenz",
      unknownCommand: "Unbekannter Befehl: /{cmd} — meintest du {list}?",
      unknownCommandShort: "Unbekannter Befehl: /{cmd}  (siehe /help)",
    },
    sessions: {
      ...EN.handlers.sessions,
      titleUnavailable: "/title ist nur in einer aktiven persistierten TUI-Sitzung verfügbar.",
      titleStarted: "▸ benenne Sitzung...",
      titleFailed: "▸ Sitzungstitel fehlgeschlagen: {reason}",
    },
    qq: {
      ...EN.handlers.qq,
      unavailable: "/qq ist in dieser Sitzung nicht verfügbar.",
      connecting: "QQ: verbinde...",
      connectFailed: "QQ-Verbindung fehlgeschlagen: {reason}",
      disconnecting: "QQ: trenne...",
      disconnectFailed: "QQ-Trennung fehlgeschlagen: {reason}",
      usage: "Verwendung: /qq connect [appId appSecret [sandbox]] | /qq status | /qq disconnect",
      promptAppId:
        "QQ-Setup: gib deine QQ-Open-Platform-App-ID ein, dann Enter. Tippe /cancel zum Abbrechen.",
      promptAppSecret:
        "QQ-Setup: gib dein QQ-Open-Platform-App-Secret ein, dann Enter. Tippe /cancel zum Abbrechen.",
      setupWaitingAppId: "Warte auf App-ID",
      setupWaitingAppSecret: "Warte auf App-Secret",
      setupCancelled: "QQ-Setup abgebrochen.",
      credentialsRequired: "QQ-App-ID und App-Secret sind erforderlich.",
      connected:
        "QQ im {mode}-Modus verbunden. Es wird bei zukünftigen Starts automatisch gestartet.",
      alreadyConnected: "QQ ist bereits im {mode}-Modus verbunden. Autostart ist aktiviert.",
      disconnected: "QQ getrennt. Autostart ist deaktiviert.",
      status:
        "QQ: {connected}, Autostart {enabled}, Anmeldedaten {configured}, App-ID {appId}, {sandbox}, Zugriff {access}, aktueller Modus {mode}.",
      statusSetup: "QQ: Setup läuft — {step}",
      stateConnected: "verbunden",
      stateDisconnected: "getrennt",
      stateEnabled: "aktiviert",
      stateDisabled: "deaktiviert",
      stateConfigured: "konfiguriert",
      stateNotConfigured: "Nicht konfiguriert",
      sandbox: "Sandbox",
      production: "Produktion",
      none: "keine",
      modeChat: "Chat",
      modeCode: "Code",
      accessOwner: "Besitzer {owner}",
      accessOwnerWithAllowlist: "Besitzer {owner}, Allowlist {count}",
      accessAllowlist: "Allowlist {count}",
      accessRuntime: "Erstabsender (nur zur Laufzeit, {owner})",
      accessOpen: "Offen (ungebunden)",
      lockAlreadyRunning:
        "QQ-Kanal läuft bereits in Prozess {pid}. Stoppe diesen Prozess, bevor du einen weiteren QQ-Kanal startest.",
      unauthorizedMessage:
        "QQ hat Nachricht von nicht autorisierter OpenID {openid} ignoriert. Aktueller Zugriff: {access}.",
      runtimeBound:
        "QQ hat diesen Lauf vorübergehend an den Erstabsender {openid} gebunden. Setze `qq.ownerOpenId` in der Konfiguration, um den Zugriff dauerhaft zu machen.",
      missingAppId: "QQ-App-ID erforderlich. Führe `/qq connect` zum Konfigurieren aus.",
      missingAppSecret: "QQ-App-Secret erforderlich. Führe `/qq connect` zum Konfigurieren aus.",
      authFailed:
        "QQ-Bot-Authentifizierung fehlgeschlagen — überprüfe deine App-ID und dein App-Secret.",
      readyTimeout:
        "QQ-Bot hat READY nicht innerhalb von 15s erhalten — überprüfe deine App-ID und dein App-Secret.",
    },
    admin: {
      ...EN.handlers.admin,
      doctorNeedsTui: "/doctor benötigt einen TUI-Kontext (postDoctor angeschlossen).",
      doctorRunning: "⚕ Doctor — führe Gesundheitschecks aus...",
      hooksReloadUnavailable:
        "/hooks reload ist in diesem Kontext nicht verfügbar (kein Reload-Callback angeschlossen).",
      hooksReloaded: "▸ Hooks neu geladen · {count} aktiv",
      hooksUsage:
        "Verwendung: /hooks            aktive Hooks auflisten\n       /hooks reload     settings.json-Dateien neu lesen",
      hooksNone: "Keine Hooks konfiguriert.",
      hooksDropHint:
        "Lege eine settings.json mit einem `hooks`-Schlüssel in einem der folgenden Pfade ab:",
      hooksProject: "  · {path} (Projekt)",
      hooksProjectFallback: "  · <projekt>/.reasonix/settings.json (Projekt)",
      hooksGlobal: "  · {path} (global)",
      hooksEvents: "Ereignisse: PreToolUse, PostToolUse, UserPromptSubmit, Stop",
      hooksExitCodes: "Exit 0 = bestanden · Exit 2 = blockieren (Pre*) · andere = warnen",
      hooksLoaded: "▸ {count} Hook(s) geladen",
      hooksSources: "Quellen: Projekt={project} · global={global}",
      updateCurrent: "Aktuell: reasonix {version}",
      updateLatestPending:
        "Neueste:  (noch nicht aufgelöst — Hintergrundprüfung läuft oder offline)",
      updateRetryHint:
        "hat einen frischen Registry-Abruf ausgelöst — versuche `/update` in ein paar Sekunden erneut,",
      updateRetryHint2:
        "oder führe `reasonix update` in einem anderen Terminal aus, um es synchron zu erzwingen.",
      updateLatest: "Neueste:  reasonix {version}",
      updateUpToDate: "Du bist auf dem neuesten Stand. Nichts zu tun.",
      updateNpxHint:
        "Du verwendest npx — der nächste `npx reasonix ...`-Start lädt automatisch die neueste Version.",
      updateNpxForce: "Um früher zu aktualisieren: `npm cache clean --force`.",
      updateUpgradeHint: "Zum Aktualisieren beende diese Sitzung und führe aus:",
      updateUpgradeCmd1: "  reasonix update           (interaktiv, --dry-run wird unterstützt)",
      updateUpgradeCmd2: "  {command}   (direkt)",
      updateInSessionDisabled:
        "Die Installation innerhalb einer Sitzung ist bewusst deaktiviert — der Installationsprozess würde",
      updateInSessionDisabled2:
        "die Darstellung dieser TUI beeinträchtigen und Windows kann die laufende Binärdatei sperren.",
      statsNoData: "Noch keine Nutzungsdaten.",
      statsEveryTurn:
        "Jeder hier ausgeführte Turn hängt einen Datensatz an — die Turns dieser Sitzung",
      statsWillAppear: "Werden im Dashboard angezeigt, sobald du eine Nachricht sendest.",
    },
    edits: {
      ...EN.handlers.edits,
      undoCodeOnly:
        "/undo ist nur innerhalb von `reasonix code` verfügbar — der Chat-Modus wendet keine Edits an.",
      historyCodeOnly: "/history ist nur innerhalb von `reasonix code` verfügbar.",
      showCodeOnly: "/show ist nur innerhalb von `reasonix code` verfügbar.",
      applyCodeOnly:
        "/apply ist nur innerhalb von `reasonix code` verfügbar (hier gibt es nichts anzuwenden).",
      discardCodeOnly: "/discard ist nur innerhalb von `reasonix code` verfügbar.",
      planCodeOnly:
        "/plan ist nur innerhalb von `reasonix code` verfügbar — der Chat-Modus blockiert keine Tool-Schreibzugriffe.",
      planOn:
        "▸ Plan-Modus EIN — Schreibwerkzeuge sind blockiert; das Modell MUSS `submit_plan` aufrufen, bevor etwas ausgeführt wird. (Das Modell kann auch eigenständig submit_plan für große Aufgaben aufrufen, selbst wenn der Plan-Modus aus ist — dieser Schalter ist die strengere, explizite Einschränkung.) Tippe /plan off zum Verlassen.",
      planOff:
        "▸ Plan-Modus AUS — Schreibwerkzeuge sind wieder aktiv. Modelle können weiterhin eigenständig Pläne für große Aufgaben vorschlagen.",
      modeCodeOnly: "/mode ist nur innerhalb von `reasonix code` verfügbar.",
      modeUsage: "Verwendung: /mode <review|auto|yolo>   (Shift+Tab schaltet auch um)",
      modeYolo:
        "▸ Edit-Modus: YOLO — Edits UND Shell-Befehle auto-ausführen ohne Nachfrage. /undo macht Edits immer noch rückgängig. Vorsicht.",
      modeAuto:
        "▸ Edit-Modus: AUTO — Edits werden sofort angewandt; drücke u innerhalb von 5s zum Rückgängigmachen, oder /undo später. Shell-Befehle fragen weiterhin.",
      modeReview: "▸ Edit-Modus: review — Edits warten auf /apply (oder y) / /discard (oder n)",
      commitCodeOnly:
        "/commit ist nur innerhalb von `reasonix code` verfügbar (benötigt ein Git-Repo als Wurzel).",
      commitUsage:
        'Verwendung: /commit "deine Commit-Nachricht"  — führt `git add -A && git commit -m "…"` in {root} aus',
      walkCodeOnly: "/walk ist nur innerhalb von `reasonix code` verfügbar.",
      checkpointCodeOnly:
        "/checkpoint ist nur innerhalb von `reasonix code` verfügbar — der Chat-Modus wendet keine Edits an.",
      checkpointNone:
        "Noch keine Checkpoints — `/checkpoint <name>` sichert jede Datei, die die Sitzung berührt hat. Später mit `/restore <name>` wiederherstellbar.",
      checkpointHeader: "◈ Checkpoints · {count} gespeichert",
      checkpointRestoreHint:
        "  /restore <name|id> · /checkpoint forget <id> · /checkpoint <name> zum Hinzufügen",
      checkpointForgetUsage: "Verwendung: /checkpoint forget <id|name>",
      checkpointNoMatch: '▸ kein Checkpoint gefunden für "{name}" — siehe /checkpoint list',
      checkpointDeleted: "▸ Checkpoint {id} gelöscht ({name})",
      checkpointDeleteFailed: "▸ Konnte {id} nicht löschen (bereits entfernt?)",
      checkpointSaveUsage:
        "Verwendung: /checkpoint <name>   (oder /checkpoint list zum Anzeigen vorhandener)",
      checkpointSavedEmpty:
        '▸ Checkpoint "{name}" gespeichert ({id}) — aber es wurden noch keine Dateien berührt, daher ist es eine leere Basislinie. Nach diesem Punkt vorgenommene Edits können rückgängig gemacht werden.',
      checkpointSaved:
        '▸ Checkpoint "{name}" gespeichert ({id}) — {files} Datei(en), {size} KB. Wiederherstellen: /restore {name}',
      restoreCodeOnly: "/restore ist nur innerhalb von `reasonix code` verfügbar.",
      restoreUsage: "Verwendung: /restore <name|id>   (siehe /checkpoint list für IDs)",
      restoreNoMatch: '▸ kein Checkpoint gefunden für "{target}" — versuche /checkpoint list',
      restoreInfo: '▸ "{name}" ({id}) wiederhergestellt von {when}',
      restoreWrote: "  · {count} Datei(en) zurückgeschrieben",
      restoreRemoved: "  · {count} Datei(en) entfernt (existierten zum Checkpoint-Zeitpunkt nicht)",
      restoreSkipped: "  ✗ {count} Datei(en) übersprungen:",
      cwdCodeOnly: "/cwd ist nur innerhalb von `reasonix code` verfügbar.",
      cwdUsage:
        "Verwendung: /cwd <pfad>   (aktuelles Root: {current}). Richtet Dateisystem-/Shell-/Memory-Tools auf <pfad> neu aus.",
      cwdUsageNoCurrent: "Verwendung: /cwd <pfad>   richtet den Workspace-Root auf <pfad> neu aus.",
    },
    model: {
      ...EN.handlers.model,
      modelHint:
        "Versuche deepseek-v4-flash oder deepseek-v4-pro — führe /models aus, um die Live-Liste abzurufen",
      modelUsage: "Verwendung: /model <id>   ({hint})",
      modelNotInCatalog:
        "Modell → {id}   (⚠ nicht im abgerufenen Katalog: {list}. Falls das falsch ist, wird der nächste Aufruf 400 geben — führe /models zum Aktualisieren aus.)",
      modelSet: "Modell → {id}",
      effortStatus: "Effort → {current}   (Auswahl: {list})",
      effortUsage:
        "Verwendung: /effort <{list}>   (high ist der sichere Standard; max ist eine DeepSeek-Erweiterung)",
      effortUsageNoMax: "Verwendung: /effort <{list}>",
      effortSet: "Effort → {effort}",
      budgetNoCap:
        "Kein Sitzungsbudget festgelegt — Reasonix wird weiterlaufen, bis du es stoppst. Setze eines mit: /budget <usd>   (z.B. /budget 5)",
      budgetStatus:
        "Budget: ${spent} von ${cap} ({pct}%) · /budget off zum Entfernen, /budget <usd> zum Ändern",
      budgetOff: "Budget → aus (keine Grenze)",
      budgetUsage:
        'Verwendung: /budget <usd>   (erhalten: "{arg}" — muss eine positive Zahl sein, z.B. /budget 5 oder /budget 12.50)',
      budgetExhausted:
        "▲ Budget → ${cap} aber bereits ${spent} ausgegeben. Der nächste Turn wird verweigert — erhöhe die Grenze, um fortzufahren, oder beende die Sitzung.",
      budgetSet:
        "Budget → ${cap}  (bisher: ${spent} · warnt bei 80 %, verweigert nächsten Turn bei 100 % · /budget off zum Entfernen)",
    },
    permissions: {
      ...EN.handlers.permissions,
      mutateCodeOnly:
        "/permissions add / remove / clear sind nur innerhalb von `reasonix code` verfügbar — sie bearbeiten die projektbezogene Allowlist (`~/.reasonix/config.json` projects[<root>].shellAllowed).",
      addUsage:
        'Verwendung: /permissions add <präfix>   (mehrere Tokens OK: /permissions add "git push origin")',
      addAlready: "▸ bereits erlaubt: {prefix}",
      addBuiltin:
        "▸ `{prefix}` ist bereits in der Builtin-Allowlist — kein projektspezifischer Eintrag nötig. (Builtin-Einträge sind immer aktiv.)",
      addInfo:
        "▸ hinzugefügt: {prefix}\n  → nächste `{prefix}`-Ausführung erfolgt ohne Nachfrage in diesem Projekt.",
      removeUsage:
        "Verwendung: /permissions remove <präfix-oder-index>   (z.B. /permissions remove 3, oder /permissions remove npm)",
      removeEmpty: "▸ keine Projekt-Allowlist-Einträge zum Entfernen.",
      removeIndexOob: "▸ Index außerhalb des Bereichs: {idx} (Projektliste hat {count} Einträge)",
      removeNothing: "▸ nichts zu entfernen.",
      removeBuiltin:
        "▸ `{prefix}` ist in der Builtin-Allowlist (schreibgeschützt). Builtin-Einträge können zur Laufzeit nicht entfernt werden — sie sind in die Binärdatei eingebrannt.",
      removeInfo: "▸ entfernt: {prefix}",
      removeNotFound:
        "▸ kein solcher Projekt-Eintrag: {prefix}   (versuche /permissions list, um zu sehen, was gespeichert ist)",
      clearAlready: "▸ Projekt-Allowlist ist bereits leer.",
      clearConfirm:
        "Es werden {count} Projekt-Allowlist-Einträg(e) für {root} gelöscht. Führe den Befehl mit dem Wort 'confirm' erneut aus: /permissions clear confirm",
      clearedNone: "▸ Projekt-Allowlist war bereits leer — nichts geändert.",
      cleared: "▸ {count} Projekt-Allowlist-Einträg(e) gelöscht.",
      usage:
        'Verwendung: /permissions [list]                   aktuellen Status anzeigen\n       /permissions add <präfix>            speichern (z.B. "npm run build")\n       /permissions remove <präfix-oder-N>    Eintrag entfernen\n       /permissions clear confirm           alle Projekteinträge löschen',
      modeYolo:
        "▸ Edit-Modus: YOLO  — jeder Shell-Befehl läuft automatisch, Allowlist wird umgangen. /mode review zum Reaktivieren der Nachfragen.",
      modeAuto:
        "▸ Edit-Modus: auto  — Edits auto-anwenden, Shell weiterhin durch Allowlist geschützt (oder ShellConfirm-Nachfrage bei nicht-allowlisteten).",
      modeReview:
        "▸ Edit-Modus: review — sowohl Edits als auch nicht-allowlistete Shell-Befehle fragen vor der Ausführung.",
      projectHeader: "Projekt-Allowlist ({count}) — {root}",
      projectNone1:
        '  (keine — wähle »immer erlauben" in einer ShellConfirm-Eingabeaufforderung, um einen hinzuzufügen,',
      projectNone2: "   oder `/permissions add <präfix>` direkt.)",
      projectNoRoot:
        "Projekt-Allowlist — (kein Projekt-Root; Chat-Modus zeigt nur Builtin-Einträge)",
      builtinHeader: "Builtin-Allowlist ({count}) — schreibgeschützt, fest eincompiliert",
      subcommands:
        "Unterbefehle: /permissions add <präfix> · /permissions remove <präfix-oder-N> · /permissions clear confirm",
    },
    dashboard: {
      ...EN.handlers.dashboard,
      notAvailable:
        "/dashboard ist in diesem Kontext nicht verfügbar (kein startDashboard-Callback angeschlossen).",
      stopNoCallback: "/dashboard stop: kein Stop-Callback angeschlossen.",
      notRunning: "▸ Dashboard läuft nicht.",
      stopping: "▸ Dashboard wird gestoppt...",
      alreadyRunning: "▸ Dashboard läuft bereits:",
      alreadyRunningHint:
        "Öffne es in einem beliebigen Browser. Tippe `/dashboard stop` zum Herunterfahren.",
      ready: "▸ Dashboard bereit:",
      readyHint: "127.0.0.1 only · token-gesichert. Tippe `/dashboard stop` zum Herunterfahren.",
      failed: "▸ Dashboard konnte nicht gestartet werden: {reason}",
      starting: "▸ starte Dashboard-Server...",
      copied: "▸ Dashboard-URL in Zwischenablage kopiert: {url}",
      tokenResetting: "▸ rotiere Dashboard-Token — starte Server neu...",
      tokenReset: "▸ Dashboard-Token rotiert. Neue URL:",
    },
    observability: {
      ...EN.handlers.observability,
      contextInfo:
        "Kontext: ~{total} von {max} ({pct}%) · System {sys} · Tools {tools} · Log {log}",
      compactStarting: "▸ falte ältere Turns in eine Zusammenfassung...",
      compactNoop:
        "▸ nichts zu falten — Log bereits klein oder aktuelle Turns allein überschreiten das Budget.",
      compactDone:
        "▸ {before} Nachrichten → {after} gefaltet (Zusammenfassung {chars} Zeichen). Fahre fort.",
      compactFailed: "▸ Falten fehlgeschlagen: {reason}",
      costNoTurn:
        "Noch kein Turn — `/cost` zeigt die Token- und Kostenaufschlüsselung des letzten Turns.",
      costNeedsTui: "/cost benötigt einen TUI-Kontext (postUsage angeschlossen).",
      costNoPricing:
        '▸ /cost: keine Preistabelle für Modell "{model}". Füge eine in telemetry/stats.ts hinzu.',
      costEstimate:
        "▸ /cost Schätzung · {model} · {prompt} Prompt-Tokens (sys {sys} + tools {tools} + log {log} + msg {msg})",
      costWorstCase:
        "  schlimmster Fall (vollständiger Fehlschlag): {input} Eingabe + ~{output} Ausgabe ({avg} Ø) ≈ {total}",
      costLikely:
        "  wahrscheinlich ({pct}% Session-Cache-Treffer): {input} Eingabe + ~{output} Ausgabe ≈ {total}",
      costLikelyCold:
        "  wahrscheinlich: entspricht worst case bis der Cache gefüllt ist (noch keine abgeschlossenen Turns)",
      statusModel: "  Modell   {model}",
      statusFlags: "  Flags   stream={stream} · effort={effort}",
      statusCtx: "  Kontext     {bar} {used}/{max} ({pct}%)",
      statusCtxNone: "  Kontext     noch keine Turns",
      statusCost: "  Kosten    ${cost} · Cache {bar} {pct}% · Turns {turns}",
      statusCostCold: "  Kosten    ${cost} · Turns {turns} (Cache wärmt sich auf)",
      statusBudget: "  Budget  ${spent} / ${cap} ({pct}%){tag}",
      statusSession: '  Sitzung "{name}" · {count} Nachrichten im Log (fortgesetzt {resumed})',
      statusSessionEphemeral: "  Sitzung (ephemer — keine Persistenz)",
      statusWorkspace:
        "  Arbeitsbereich {path} · beim Start festgelegt (mit --dir <pfad> neu starten zum Wechseln)",
      statusMcp: "  MCP     {servers} Server, {tools} Tools im Register",
      statusEdits: "  Edits   {count} ausstehend (/apply zum Übernehmen, /discard zum Verwerfen)",
      statusPlan: "  Plan    EIN — Schreibzugriffe blockiert (submit_plan + Genehmigung)",
      statusLifecycle: "  Lebenszyklus {mode}/{state} · {progress}{evidence}",
      lifecycleNoPlan: "Kein Plan",
      lifecycleEvidencePending: "Nachweis ausstehend",
      lifecycleRejected: "Lebenszyklus: {tool} blockiert in {state} — nächster: {next}",
      lifecycleEvidenceRejected:
        "Lebenszyklus: Schritt {stepId} benötigt Nachweis — nächster: {next}",
      lifecycleRepeatedRejected:
        "Lebenszyklus: wiederholte {tool}-Ablehnung — wiederhole nicht identische Argumente",
      statusModeYolo:
        "  Modus    YOLO — Edits + Shell auto-ausführen ohne Nachfrage (/undo macht immer noch rückgängig · Shift+Tab zum Umschalten)",
      statusModeAuto:
        "  Modus    AUTO — Edits werden sofort angewandt (u zum Rückgängigmachen innerhalb von 5s · Shift+Tab zum Umschalten)",
      statusModeReview:
        "  Modus    review — Edits warten auf /apply oder y (Shift+Tab zum Umschalten)",
      statusDash: "  Dash    {url} (im Browser öffnen · /dashboard stop)",
    },
    plans: {
      ...EN.handlers.plans,
      noSession:
        "Keine Sitzung angehängt — `/plans` ist pro Sitzung. Führe `reasonix code` in einem Projekt aus, um eine Sitzung zu erhalten.",
      activePlan:
        "▸ aktiver Plan{label} — {done}/{total} Schritt(e) erledigt · zuletzt bearbeitet {when}",
      activeNone: "▸ aktiver Plan: (keiner)",
      noArchives:
        "Noch keine archivierten Pläne für diese Sitzung — sie werden automatisch archiviert, wenn alle Schritte erledigt sind",
      archivedHeader: "Archiviert ({count}):",
      evidencePending:
        "  ! Nachweis ausstehend — aktueller Schritt benötigt Verifikation/Diff/Checkpoint/manuellen Nachweis",
      evidenceLine: "  Nachweis {stepId}: {summary}",
      archivedEvidenceLine: "    Nachweis: {summary}",
      replayNoSession:
        "Keine Sitzung angehängt — `/replay` ist pro Sitzung. Führe `reasonix code` in einem Projekt aus, um eine Sitzung zu erhalten.",
      replayNoArchives:
        "Noch keine archivierten Pläne für diese Sitzung — `/replay` wird aktiv, sobald ein Plan abgeschlossen ist (auto-archiviert wenn alle Schritte erledigt).",
      replayInvalidIndex:
        "Ungültiger Index — `/replay` akzeptiert 1..{max} (neuester = 1). Verwende `/plans`, um die Liste zu sehen.",
      archivedRow: "  ✓ {when}  {total} Schritt(e) · {completion}  {label}",
      completionComplete: "abgeschlossen",
      stopAborted:
        "▸ Plan gestoppt — Modell abgebrochen; tippe eine Folgenachricht, um fortzufahren oder eine neue Aufgabe zu starten.",
      doneUsage:
        "Verwendung: /plans done <stepId>  ·  /plans done all — manuelle Überschreibung, wenn das Modell vergessen hat, mark_step_complete aufzurufen",
      doneUnavailable: "/plans done ist nur innerhalb einer aktiven Sitzung verfügbar.",
      doneNoPlan: "Kein aktiver Plan — nichts als erledigt zu markieren.",
      doneNotInPlan:
        "Schritt `{id}` ist nicht im aktiven Plan. Führe /plans aus, um die Schritt-IDs zu sehen.",
      doneAlready: "Schritt `{id}` wurde bereits als erledigt markiert.",
      doneOk: "▸ Schritt `{id}` als erledigt markiert.",
      doneAllNoop: "Jeder Schritt ist bereits erledigt.",
      doneAllOk: "▸ {count} Schritt(e) als erledigt markiert.",
    },
    jobs: {
      ...EN.handlers.jobs,
      codeOnly: "/jobs ist nur innerhalb von `reasonix code` verfügbar.",
      killCodeOnly: "/kill ist nur innerhalb von `reasonix code` verfügbar.",
      logsCodeOnly: "/logs ist nur innerhalb von `reasonix code` verfügbar.",
      empty:
        "◈ Jobs · 0 laufend · 0 gesamt\n  (run_background startet einen — Dev-Server, Watcher, langlebige Skripte)",
      header: "◈ Jobs · {running} laufend · {total} gesamt",
      footer: "  /logs <id> tail · /kill <id> SIGTERM → SIGKILL",
      killUsage: "Verwendung: /kill <id>   (siehe /jobs für IDs)",
      killNotFound: "Job {id}: nicht gefunden",
      killAlreadyExited: "Job {id} bereits beendet ({code})",
      killStopping:
        "▸ stoppe Job {id} (Prozessbaum: SIGTERM → SIGKILL nach 2s Gnadenfrist; Windows: taskkill /T /F)",
      killStatus: "▸ Job {id} {status}",
      killStillAlive: "Nach SIGKILL noch am Leben (!) — melde dies als Fehler",
      logsUsage: "Verwendung: /logs <id> [zeilen]   (Standard letzte 80 Zeilen)",
      logsNotFound: "Job {id}: nicht gefunden",
      logsStatus: "[Job {id} · {status}]\n$ {command}",
      logsRunning: "Läuft · PID {pid}",
      logsExited: "Beendet {code}",
      logsFailed: "Fehlgeschlagen ({reason})",
      logsStopped: "gestoppt",
    },
    memory: {
      ...EN.handlers.memory,
      disabled:
        "Memory ist deaktiviert (REASONIX_MEMORY=off in der Umgebung). Entferne die Variable zur Reaktivierung — es werden weder REASONIX.md noch ~/.reasonix/memory-Inhalte eingefügt.",
      noRoot:
        "Kein Arbeitsverzeichnis in dieser Sitzung — `/memory` benötigt ein Root, um REASONIX.md aufzulösen. (Läuft in einer Test-Umgebung?)",
      listEmpty:
        "Noch keine Benutzer-Memories. Das Modell kann `remember` aufrufen, um einen zu speichern, oder du kannst Dateien manuell in ~/.reasonix/memory/global/ oder dem projektspezifischen Unterverzeichnis erstellen.",
      listHeader: "Benutzer-Memories ({count}):",
      listFooter: "Body anzeigen: /memory show <name>   Löschen: /memory forget <name>",
      showUsage: "Verwendung: /memory show <name>  oder  /memory show <scope>/<name>",
      showNotFound: "Kein Memory gefunden: {target}",
      showFailed: "Anzeige fehlgeschlagen: {reason}",
      forgetUsage: "Verwendung: /memory forget <name>  oder  /memory forget <scope>/<name>",
      forgetNotFound: "Kein Memory gefunden: {target}",
      forgetInfo:
        "▸ {scope}/{name} entfernt. Nächstes /new oder der nächste Start wird es nicht mehr sehen.",
      forgetFailed: "Konnte {scope}/{name} nicht entfernen (bereits weg?)",
      forgetError: "Entfernen fehlgeschlagen: {reason}",
      clearUsage: "Verwendung: /memory clear <global|project> confirm",
      clearConfirm:
        "Alle Memories im Bereich {scope} werden gelöscht. Führe den Befehl mit dem Wort 'confirm' erneut aus: /memory clear {scope} confirm",
      cleared: "▸ Bereich {scope} geleert — {count} Memory-Datei(en) gelöscht.",
      noMemory: "Kein Memory in {root} eingefügt.",
      layers: "Drei Ebenen sind verfügbar:",
      layerProject: "  1. {file} — commitierbares Team-Memory (im Repo).",
      layerGlobal: "  2. ~/.reasonix/memory/global/ — dein projektübergreifendes privates Memory.",
      layerProjectHash:
        "  3. ~/.reasonix/memory/<projekt-hash>/ — privates Memory dieses Projekts.",
      askModel: "Bitte das Modell, etwas zu `remember`, oder bearbeite die Dateien direkt.",
      changesNote:
        "Änderungen werden beim nächsten /new oder Start wirksam — der System-Prompt wird einmal pro Sitzung gehasht, um den Prefix-Cache warm zu halten.",
      subcommands:
        "Unterbefehle: /memory list | /memory show <name> | /memory forget <name> | /memory clear <scope> confirm",
      changesNoteShort:
        "Änderungen werden beim nächsten /new oder Start wirksam. Unterbefehle: /memory list | show | forget | clear",
    },
    mcp: {
      ...EN.handlers.mcp,
      noServers:
        'Keine MCP-Server angehängt. Führe `reasonix setup` aus, um welche auszuwählen, oder starte mit --mcp "<spec>". `reasonix mcp list` zeigt den Katalog. Hinweis: vom Modell aufgerufene Shell-Befehle werden pro Aufruf abgefragt (einmal erlauben / immer erlauben / ablehnen) — kein globales Allow-All-Flag.',
      toolsLabel: "  Tools     {count}",
      resourcesHint: "`/resource` zum Durchsuchen+Lesen",
      promptsHint: "`/prompt` zum Durchsuchen+Abrufen",
      awarenessOnly:
        "Der Chat-Modus verbraucht Tools aktuell; Ressourcen+Prompts werden hier zur Information angezeigt.",
      catalogHint:
        "Vollständiger Katalog: `reasonix mcp list` · tiefere Diagnose: `reasonix mcp inspect <spec>`.",
      fallbackServers: "MCP-Server ({count}):",
      fallbackTools: "Tools im Register ({count}):",
      fallbackChange: "Um diesen Satz zu ändern, beende und führe `reasonix setup` aus.",
      usageDisableEnable:
        "Verwendung: /mcp {action} <name>  ·  wähle einen in /mcp angezeigten Namen (anonyme Server können nicht nach Namen umgeschaltet werden).",
      usageReconnect:
        "Verwendung: /mcp reconnect <name>  ·  wähle einen in /mcp angezeigten Namen.",
      unknownServer: 'Unbekannter MCP-Server "{name}". Bekannt: {list}.',
      noneList: "(keine)",
      reconnectNoTui: "/mcp reconnect benötigt die interaktive TUI (postInfo nicht angeschlossen).",
      liveTab: "Live",
      marketplaceTab: "Marktplatz",
      tabHint: "Tab zum Umschalten",
    },
    init: {
      ...EN.handlers.init,
      codeOnly:
        "/init funktioniert nur im Code-Modus (es benötigt Dateisystem-Werkzeuge).\nFühre `reasonix code [pfad]` aus, um eine Sitzung zu starten, die im\nProjekt verwurzelt ist, das du initialisieren möchtest, und führe dann /init aus.",
      exists: "▸ REASONIX.md existiert bereits unter {path}",
      existsForce: "  /init force   von Grund auf neu generieren (überschreibt)",
      existsEdit: "  Oder bearbeite es von Hand — es ist nur Markdown. Die aktuelle Datei wird",
      existsPinned: "  bei jedem Start unverändert in den System-Prompt eingefügt.",
      info: "▸ /init — Modell scannt das Projekt und synthetisiert REASONIX.md.\n  Das Ergebnis landet als ausstehender Edit; mit /apply oder /walk reviewen.",
    },
    webSearchEngine: {
      ...EN.handlers.webSearchEngine,
      currentEngine: "Aktuelle Websuchmaschine: {engine}",
      endpoint: "SearXNG-Endpunkt: {url}",
      usageHeader: "Verwendung:",
      usageBing:
        "  /search-engine bing              Bing verwenden (Standard, funktioniert von CN ohne Proxy)",
      usageSearxng: "  /search-engine searxng            SearXNG verwenden (Standard-Endpunkt)",
      usageSearxngUrl:
        "  /search-engine searxng <url>      SearXNG mit benutzerdefiniertem Endpunkt",
      usageMetaso:
        "  /search-engine metaso              Metaso-API verwenden (100/Tag kostenlos, konfiguriere eigenen API-Schlüssel für mehr)",
      usageTavily:
        "  /search-engine tavily              Tavily-API verwenden (LLM-freundlich, kostenlos 1000/Monat — setze TAVILY_API_KEY oder tavilyApiKey in der Konfiguration; erhalte einen unter https://tavily.com)",
      usagePerplexity:
        "  /search-engine perplexity          Perplexity AI verwenden (AI-native Antwort + Quellenangaben — setze PERPLEXITY_API_KEY oder perplexityApiKey in der Konfiguration; erhalte einen unter https://perplexity.ai/settings/api)",
      usageExa:
        "  /search-engine exa                 Exa-API verwenden (AI-native Antwort + Quellenangaben, kostenlos 1000/Monat — setze EXA_API_KEY oder exaApiKey in der Konfiguration; registriere dich unter https://exa.ai)",
      alias: "Alias: /se",
      searxngInfo:
        "SearXNG ist eine selbst gehostete Metasuchmaschine (https://github.com/searxng/searxng).",
      searxngInstall: "Installiere mit:  docker run -d -p 8080:8080 searxng/searxng",
      switched: 'Websuchmaschine auf "{engine}" umgestellt.{note}',
      switchedSearxngNote: " Stelle sicher, dass SearXNG unter {endpoint} läuft.",
      switchedMetasoNote:
        " Es gibt ein tägliches Kontingent von 100 (konfiguriere einen eigenen API-Schlüssel für höhere Grenzen).",
      switchedTavilyNote:
        " Setze TAVILY_API_KEY oder `tavilyApiKey` in der Konfiguration; kostenlos 1000/Monat unter https://tavily.com.",
      switchedPerplexityNote:
        " Setze PERPLEXITY_API_KEY oder `perplexityApiKey` in der Konfiguration; erhalte einen unter https://perplexity.ai/settings/api.",
      switchedExaNote:
        " Setze EXA_API_KEY oder `exaApiKey` in der Konfiguration; registriere dich unter https://exa.ai.",
      keyNeeded:
        'Kein API-Schlüssel für "{engine}" konfiguriert.\n\n  1. Setze die {envVar}-Umgebungsvariable\n  2. Oder gib ihn inline an:  /search-engine {engine} <dein-schlüssel>\n  3. Oder füge "{engine}ApiKey" zu ~/.reasonix/config.json hinzu\n\nWiederhole dann /search-engine {engine}.',
      keySaved: " API-Schlüssel in der Konfiguration gespeichert.",
      confirmed:
        'Websuchmaschine auf "{engine}" gesetzt{detail}. Der nächste Assistenten-Turn übernimmt die Änderung.',
      confirmedDetail: " ({endpoint})",
    },
    skill: {
      ...EN.handlers.skill,
      listEmpty: "Keine Skills gefunden. Reasonix liest Skills von:",
      listProjectScope:
        "  · <projekt>/.reasonix/skills/<name>/SKILL.md  (oder <name>.md)  — Projekt-Bereich",
      listGlobalScope:
        "  · ~/.reasonix/skills/<name>/SKILL.md  (oder <name>.md)  — globaler Bereich",
      listProjectOnly: "  (Projekt-Bereich ist nur in `reasonix code` aktiv)",
      listFrontmatter: "Die Frontmatter jeder Datei benötigt mindestens `name` und `description`.",
      listInvoke:
        "Führe einen Skill aus mit `/skill <name> [args]` oder indem du das Modell bittest, `run_skill` aufzurufen.",
      listHeader: "Benutzer-Skills ({count}):",
      listFooter:
        "Anzeigen: /skill show <name>   Ausführen: /skill <name> [args]   Neu: /skill new <name>",
      listEmptyNewHint:
        "Erstelle einen mit: /skill new <name>  (Projekt-Bereich) — es gibt noch kein entferntes Register; du erstellst Skills direkt.",
      showUsage: "Verwendung: /skill show <name>",
      showNotFound: "Kein Skill gefunden: {name}",
      runNotFound: "Kein Skill gefunden: {name}  (versuche /skill list)",
      runInfo: "▸ führe Skill aus: {name}{args}",
      newUsage: "Verwendung: /skill new <name> [--global]",
      newCreated:
        "▸ Skill erstellt: {name}\n  {path}\n  bearbeite ihn, dann `/skill {name}` zum Ausführen",
      newError: "▲ /skill new fehlgeschlagen: {reason}",
      pathsHeader: "Skill-Pfade (Prioritätsreihenfolge):",
      pathsPriority:
        "Priorität: Projekt > benutzerdefinierte Pfade in Konfigurationsreihenfolge > global > builtin. Änderungen wirken sich auf den System-Prompt beim nächsten /new oder einer neuen Sitzung aus.",
      pathsUsage:
        "Verwendung: /skill paths [list]\n       /skill paths add <pfad>\n       /skill paths remove <pfad|N>",
      pathsAddUsage: "Verwendung: /skill paths add <pfad>",
      pathsRemoveUsage: "Verwendung: /skill paths remove <pfad|N>",
      pathsAdded: "▸ benutzerdefinierten Skill-Pfad hinzugefügt: {path}",
      pathsAlready: "▸ benutzerdefinierter Skill-Pfad bereits konfiguriert: {path}",
      pathsRemoved: "▸ benutzerdefinierten Skill-Pfad entfernt: {path}",
      pathsRemoveNotFound: "▸ kein benutzerdefinierter Skill-Pfad entspricht: {target}",
      pathsRestartHint:
        "Der System-Prompt der aktuellen Sitzung ist unverändert; führe /new aus oder starte eine neue Sitzung, um das Skills-Register zu aktualisieren.",
    },
  },
  statusBar: {
    ...EN.statusBar,
    turn: "Turn",
    cache: "Cache",
    spent: "ausgegeben",
    left: " übrig",
    slow: "langsam",
    disconnect: "trennen",
    reconnecting: "Verbinde neu…",
    approvingIn: "Genehmige in ",
    escToInterrupt: "Esc zum Unterbrechen",
    recordingGlyph: "Aufnahme",
    mb: " MB",
    evt: " Ereignis",
    editsLabel: "Edits:",
    mcpLoading: "MCP",
    ctx: "Kontext",
    shortcutsHint: "Strg+P Tastenkürzel",
  },
  editMode: {
    ...EN.editMode,
    plan: "PLAN-MODUS",
    yolo: "YOLO",
    auto: "AUTO",
    review: "REVIEW",
    writesGated: "   Schreibzugriffe blockiert · /plan off zum Verlassen",
    editsShellAuto: "Edits + Shell auto · /undo zum Rückgängigmachen",
    editsLandNow: "Edits werden sofort angewandt · u zum Rückgängigmachen",
    queuedApplyDiscard: "{count} in Warteschlange · y anwenden · n verwerfen",
    editsQueued: "Edits in Warteschlange · y anwenden · n verwerfen",
    shiftTabFlip: "   {mid} · Shift+Tab zum Umschalten",
    queuedDots: "In Warteschlange…",
  },
  composer: {
    ...EN.composer,
    placeholder: "Frag etwas  ·  / für Befehle  ·  @ für Dateien",
    waitingForResponse: "…warte auf Antwort…",
    hintSend: "senden",
    hintNewline: "Neue Zeile",
    hintClear: "leeren",
    hintScroll: "scrollen",
    hintHistory: "Verlauf",
    hintAbort: "abbrechen",
    hintQuit: "beenden",
    abortedHint: "Turn vom Benutzer abgebrochen · erneut Esc zum Leeren · ⏎ für eine Folgefrage",
    editorNoRawMode:
      "Externer Editor nicht verfügbar — stdin unterstützt Raw-Mode-Umschaltung auf diesem Terminal nicht",
    editorFailed: "Externer Editor:",
    editorMissing:
      "Kein $EDITOR / $VISUAL / $GIT_EDITOR gesetzt — exportiere einen (z.B. `export EDITOR=nano`) und versuche es erneut",
    editorExited: "Editor mit Code {code} beendet",
    typeaheadStaged: "▸ {count} Zeile(n) bereitgestellt · Esc zurückrufen",
    steerPlaceholder:
      "Tippe, um die aktuelle Aufgabe zu steuern — Befehle sind deaktiviert, solange beschäftigt",
    steerHint: "Senden — mid-Turn eingefügt",
    stashNothing: "Nichts zu speichern",
    stashSaved: "Gespeichert",
    stashRecall: "Abgerufen",
  },
  pathConfirm: {
    ...EN.pathConfirm,
    title: "Pfad außerhalb des Sandbox",
    subtitleRead: "{tool} möchte eine Datei AUSSERHALB des Projekt-Sandbox lesen",
    subtitleWrite: "{tool} möchte eine Datei AUSSERHALB des Projekt-Sandbox schreiben",
    awaiting: "wartet",
    denyTitle: "Ablehnen — Kontext angeben",
    optional: "optional",
    denyFooter:
      "Kontext eingeben  ·  ⏎ mit Grund absenden  ·  Esc überspringen (ohne Grund ablehnen)",
    pickFooter: "↑↓ auswählen  ·  ⏎ bestätigen  ·  Tab Kontext hinzufügen  ·  Esc abbrechen",
    allowOnce: "Einmal erlauben",
    allowOnceDesc: "Diesen Zugriff erlauben; das Verzeichnis für den Rest dieser Sitzung merken",
    allowAlways: "Immer erlauben",
    allowAlwaysDesc:
      "`{prefix}` für dieses Projekt merken (gespeichert in ~/.reasonix/config.json)",
    deny: "ablehnen",
    denyDesc: "Tab drücken, um dem Modell den Grund mitzuteilen",
    pathLabel: "Pfad",
    sandboxLabel: "Sandbox",
    allowPrefixLabel: "Präfix",
    promptTitleRead: "Pfadzugriff — lesen",
    promptTitleWrite: "Pfadzugriff — schreiben",
    actionAllowRead: "Lesen erlauben",
    actionAllowWrite: "Schreiben erlauben",
    actionAlwaysAllow: "Immer erlauben — {prefix}",
    actionDeny: "Ablehnen",
  },
  shellConfirm: {
    ...EN.shellConfirm,
    title: "Shell-Befehl",
    bgTitle: "Hintergrundprozess",
    subtitle: "Modell möchte einen Shell-Befehl ausführen",
    bgSubtitle: "Langlebiger Prozess — läuft nach Genehmigung weiter, /kill zum Stoppen",
    denyTitle: "Ablehnen — Kontext angeben",
    optional: "optional",
    denyFooter:
      "Kontext eingeben  ·  ⏎ mit Grund absenden  ·  Esc überspringen (ohne Grund ablehnen)",
    awaiting: "wartet",
    pickFooter: "↑↓ auswählen  ·  ⏎ bestätigen  ·  Tab Kontext hinzufügen  ·  Esc abbrechen",
    allowOnce: "Einmal erlauben",
    allowOnceDesc: "Diesen Befehl ausführen, beim nächsten Mal erneut fragen",
    allowAlways: "Immer erlauben",
    allowAlwaysDesc: "`{prefix}` für dieses Projekt merken",
    deny: "ablehnen",
    denyDesc: "Tab drücken, um dem Modell den Grund mitzuteilen",
    cwdLabel: "CWD",
    timeoutLabel: "Timeout",
    waitLabel: "warten",
    previewMore: "… {n} weitere Zeile ausgeblendet — Esc drücken, Modell bitten, sie aufzuteilen",
    previewMorePlural:
      "… {n} weitere Zeilen ausgeblendet — Esc drücken, Modell bitten, sie aufzuteilen",
    promptTitleRunCommand: "Befehl ausführen",
    promptTitleRunBackground: "Hintergrundbefehl ausführen",
    actionRunOnce: "Einmal ausführen",
    actionAlwaysAllow: "Immer erlauben — {prefix}",
    actionDeny: "Ablehnen",
  },
  editConfirm: {
    ...EN.editConfirm,
    footer:
      "[y/Enter] anwenden  ·  [n] mit Grund ablehnen  ·  [a] Rest anwenden  ·  [A] AUTO umschalten  ·  [↑↓/Leertaste] scrollen  ·  [Esc] abbrechen",
    newTag: "NEU",
    editTag: "BEARBEITET",
    linesCount: "-{removed} +{added} Zeilen",
    viewingRange: "Zeige {start}-{end}/{total}",
    denyFooter: "⏎ absenden  ·  Esc überspringen (ohne Grund ablehnen)",
    oldLabel: "  - alt",
    newLabel: "  + neu",
    sideBySide:
      "   nebeneinander · entfernte Zeilen links, hinzugefügte rechts · paarweise nach Versatz",
    linesAbove: "  ↑ {count} Zeile darüber  (↑/k oder Bild↑)",
    linesAbovePlural: "  ↑ {count} Zeilen darüber  (↑/k oder Bild↑)",
    linesBelow: "  ↓ {count} Zeile darunter  (↓/j oder Leertaste/Bild↓)",
    linesBelowPlural: "  ↓ {count} Zeilen darunter  (↓/j oder Leertaste/Bild↓)",
  },
  editPicker: {
    ...EN.editPicker,
    title: "Vorherige Nachricht bearbeiten",
    hint: "↑↓ auswählen · Enter zum Laden in den Composer · Esc abbrechen",
    empty: "Noch keine Benutzer-Turns — nichts zu bearbeiten",
    dismiss: "Esc zum Schließen",
    forked: "▸ bei Turn #{turn} abgezweigt — Puffer enthält den Originaltext",
  },
  sessionPicker: {
    ...EN.sessionPicker,
    header: " ◈ REASONIX · Sitzung auswählen ",
    title: "Sitzung auswählen — {workspace}",
    messages: "{count} Nachricht",
    messagesPlural: "{count} Nachrichten",
    turns: "{count} Turns",
    pickerHint:
      "↑↓ auswählen · / suchen · ⏎ öffnen · [n] neu · [d] löschen · [r] umbenennen · Esc beenden",
    empty: "  Noch keine gespeicherten Sitzungen in diesem Arbeitsbereich — drücke ",
    emptyNew: " um eine neue zu starten",
    renamePrompt: '  "{from}" umbenennen → ',
    renameHint: "  ⏎ Umbenennung bestätigen  ·  Esc abbrechen",
    searchPrompt: "  Sitzungen durchsuchen: /",
    searchHint: "  Tippen zum Filtern  ·  ⏎ Treffer öffnen  ·  Esc zurücksetzen",
    searchEmpty: "  Keine Sitzungen entsprechen dieser Suche",
    emptyHint: "  ⏎ neue Sitzung  ·  Esc beenden",
    justNow: "Gerade eben",
    minAgo: "Vor {count} Min",
    yesterday: "gestern",
    hoursAgo: "Vor {count}h",
    daysAgo: "Vor {count} Tagen",
  },
  workspacePicker: {
    ...EN.workspacePicker,
    header: " ◈ REASONIX · Arbeitsbereich auswählen ",
    title: "Arbeitsbereich auswählen — {workspace}",
    sessions: "{count} Sitzung",
    sessionsPlural: "{count} Sitzungen",
    current: "aktuell",
    pickerHint:
      "↑↓ auswählen · / suchen · ⏎ wechseln + Sitzung auswählen · Esc beenden · /cwd <pfad> fügt einen hinzu",
    empty:
      "  Noch keine bekannten Arbeitsbereiche — führe /cwd <pfad> einmal aus, um einen hinzuzufügen",
    searchPrompt: "  Arbeitsbereiche durchsuchen: /",
    searchHint: "  Tippen zum Filtern  ·  ⏎ wechseln + Sitzung auswählen  ·  Esc zurücksetzen",
    searchEmpty: "  Keine Arbeitsbereiche entsprechen dieser Suche",
  },
  modelPicker: {
    ...EN.modelPicker,
    header: " ◈ REASONIX · Einrichtung auswählen ",
    loading: "  ·  lade Katalog…",
    catalogEmpty: "  ·  Katalog leer — verwende bekannte Fallbacks",
    modelsAvailable: "  ·  {count} Modelle verfügbar",
    effortHeader: "    EFFORT  ·  Reasoning-Effort-Grenze",
    modelsHeader: "    MODELLE  ·  DeepSeek-kompatible IDs",
    effortDesc: {
      ...EN.modelPicker.effortDesc,
      low: "Am schnellsten — minimales Reasoning",
      medium: "ausgewogen",
      high: "Standard — sicher für vLLM / Azure",
      max: "DeepSeek-Erweiterung; von stock OpenAI / vLLM abgelehnt",
    },
    pickerFooter: "  ↑↓ auswählen  ·  ⏎ bestätigen  ·  [r] aktualisieren  ·  Esc abbrechen",
    currentLabel: "  · aktuell",
  },
  slashSuggestions: {
    ...EN.slashSuggestions,
    noMatch: "Kein Slash-Befehl entspricht diesem Präfix",
    backspaceHint: " — Rücktaste zum Bearbeiten, oder /help für die vollständige Liste",
    commandCount: "{count} Befehl",
    commandCountPlural: "{count} Befehle",
    aboveLabel: "   ↑ {count} darüber",
    belowLabel: "   ↓ {count} darunter",
    advancedHint: "  + {count} erweitert  ·  tippe einen Buchstaben zum Suchen",
    footerHint: "  ↑↓ navigieren · Tab / ⏎ auswählen · Esc abbrechen",
    groupChat: "CHAT",
    groupSetup: "SETUP",
    groupInfo: "INFO",
    groupSession: "SITZUNG",
    groupExtend: "ERWEITERN",
    groupCode: "CODE",
    groupJobs: "JOBS",
    groupAdvanced: "ERWEITERT",
    groupDetailSetup: "Modell + Kosten",
    groupDetailInfo: "Aktueller Zustand",
    groupDetailChat: "Tägliche Turn-Operationen",
    groupDetailExtend: "MCP, Memory, Skills",
    groupDetailSession: "Gespeicherte Sitzungen",
    groupDetailCode: "Edits + Pläne (Code-Modus)",
    groupDetailJobs: "Hintergrundprozesse (Code-Modus)",
    groupDetailAdvanced: "Selten oder einmalig",
  },
  atMentions: {
    ...EN.atMentions,
    loading: "lade…",
    entrySingular: "{count} Eintrag",
    entryPlural: "{count} Einträge",
    searching: "suche…",
    scanned: "gescannt",
    match: "Treffer",
    matches: "Treffer",
    forFilter: 'für "{filter}"',
    noMatch: 'Keine Dateien entsprechen "{filter}"',
    emptyDir: "Leeres Verzeichnis",
    scanning: "Durchsuche Verzeichnisbaum…",
    footerBrowse: "↑↓ navigieren · Tab in Ordner eintauchen · ⏎ einfügen · Esc abbrechen",
    footerBrowseSearch: "↑↓ navigieren · Tab / ⏎ als @pfad einfügen · Esc abbrechen",
    footerInsert: "↑↓ navigieren · Tab / ⏎ als @pfad einfügen · Esc abbrechen",
  },
  statsPanel: {
    ...EN.statsPanel,
    modePlan: "PLAN",
    modeYolo: "yolo",
    modeAuto: "auto",
    modeReview: "review",
    pro: "⇧ pro",
    budget: "  Budget  ",
  },
  welcomeBanner: {
    ...EN.welcomeBanner,
    workspace: "▸ Arbeitsbereich",
    relaunchHint: "  (mit --dir <pfad> neu starten zum Wechseln)",
    dashboard: "▸ Web",
  },
  ctxBreakdown: {
    ...EN.ctxBreakdown,
    title: "▣ Kontext",
    compactHint: "  /compact faltet (automatisch bei 50 %) · /new löscht Log",
    topTools: "  Top-Tool-Ergebnisse nach Kosten ({count}):",
    msg: "Nachr",
    turnLabel: "Turn",
  },
  startup: {
    ...EN.startup,
    codeRooted:
      '▸ reasonix code: verwurzelt in {rootDir}, Sitzung "{session}" · {tools} native Tool{s}{semantic}',
    ephemeral: "(ephemer)",
    semanticOn: " · Semantic-Search an",
  },
  doctorErrors: {
    ...EN.doctorErrors,
    unreadable: "{path} nicht lesbar — {message}",
    cannotList: "Kann nicht auflisten — {message}",
    parseFailed: "settings.json konnte nicht geparst werden — {message}",
    probeFailed: "Test fehlgeschlagen — {message}",
  },
  webErrors: {
    ...EN.webErrors,
    status:
      "web_search {status} — versuche: Das Such-Backend hat einen Fehler zurückgegeben; formuliere die Abfrage um oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    rateLimit429:
      "web_search 429 — versuche: 10s warten vor erneuter Abfrage oder Abfrage umformulieren; das Such-Backend hat das Rate-Limit für diesen Client erreicht",
    forbidden403:
      "web_search 403 — versuche: Das Such-Backend blockiert diesen Client; wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa oder warte und versuche es später erneut",
    serverError5xx:
      "web_search {status} — versuche: Öffne die Such-URL in einem Browser; falls sie lädt, ist dies vorübergehend und ein erneuter Versuch in 30s kann helfen",
    bingBlocked:
      "web_search: Bing-Anti-Bot-Seite — Rate-Limit erreicht oder blockiert — versuche: 30s warten und erneut versuchen, oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    bingNoResults:
      "web_search: 0 Ergebnisse, aber die Antwort sieht nicht wie eine echte leere Seite aus ({chars} Zeichen, erste 120: {preview}) — versuche: formuliere die Abfrage mit einfacheren Begriffen um oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    invalidEndpoint:
      'web_search: ungültiger SearXNG-Endpunkt "{endpoint}" — versuche: setze eine gültige URL mit /search-endpoint http://host:port',
    endpointMustBeHttp:
      "web_search: SearXNG-Endpunkt muss http(s) sein, {protocol} erhalten — versuche: setze eine gültige URL mit /search-endpoint http://host:port",
    cannotReach:
      "web_search: SearXNG-Server unter {endpoint} nicht erreichbar — versuche: SearXNG installieren und starten (https://github.com/searxng/searxng, z.B. `docker run -d -p 8080:8080 searxng/searxng`), oder wechsle zu einer anderen Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    searxngNoResults:
      "web_search: 0 Ergebnisse, aber SearXNG-Antwort sieht nicht wie eine leere Ergebnisseite aus ({chars} Zeichen) — versuche: formuliere die Abfrage mit einfacheren Begriffen um oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    metasoMissingKey:
      "web_search: Metaso benötigt einen API-Schlüssel — setze METASO_API_KEY oder konfiguriere einen mit /search-engine metaso <schlüssel>. Erhalte einen unter https://metaso.cn/search-api/playground",
    metasoDailyLimit:
      "web_search: Metaso-Tageslimit erreicht — setze METASO_API_KEY oder erhalte einen Schlüssel unter https://metaso.cn/search-api/playground",
    metasoUnauthorized:
      "web_search: Metaso-API-Schlüssel abgelehnt — überprüfe METASO_API_KEY oder erhalte einen unter https://metaso.cn/search-api/playground",
    metasoRateLimit:
      "web_search: Metaso-Rate-Limit erreicht — warte und versuche es erneut, oder erhalte einen eigenen API-Schlüssel unter https://metaso.cn/search-api/playground",
    metasoServerError:
      "web_search: Metaso-Serverfehler ({status}) — versuche es später erneut oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    metasoParseError:
      "web_search: Metaso hat unparsbare Antwort zurückgegeben (HTTP {status}) — versuche es später erneut",
    metasoApiError:
      "web_search: Metaso-API-Fehler (Code {code}: {message}) — versuche es später erneut",
    tavilyMissingKey:
      "web_search: Tavily-Backend benötigt einen API-Schlüssel — setze TAVILY_API_KEY-Umgebungsvariable oder `tavilyApiKey` in ~/.reasonix/config.json; kostenlose 1000/Monat-Registrierung unter https://tavily.com",
    tavilyUnauthorized:
      "web_search: Tavily-API-Schlüssel abgelehnt — überprüfe TAVILY_API_KEY oder erhalte einen unter https://tavily.com",
    tavilyRateLimit:
      "web_search: Tavily-Rate-Limit erreicht oder monatliches Kontingent überschritten — warte, wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa oder upgrade deinen Tavily-Plan",
    tavilyServerError:
      "web_search: Tavily-Serverfehler ({status}) — versuche es später erneut oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    tavilyParseError:
      "web_search: Tavily hat unparsbare Antwort zurückgegeben (HTTP {status}) — versuche es später erneut",
    perplexityMissingKey:
      "web_search: Perplexity-Backend benötigt einen API-Schlüssel — setze PERPLEXITY_API_KEY-Umgebungsvariable oder `perplexityApiKey` in ~/.reasonix/config.json; erhalte einen unter https://perplexity.ai/settings/api",
    perplexityUnauthorized:
      "web_search: Perplexity-API-Schlüssel abgelehnt — überprüfe PERPLEXITY_API_KEY oder erhalte einen unter https://perplexity.ai/settings/api",
    perplexityRateLimit:
      "web_search: Perplexity-Rate-Limit erreicht — warte und versuche es erneut, oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    perplexityServerError:
      "web_search: Perplexity-Serverfehler ({status}) — versuche es später erneut oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    perplexityParseError:
      "web_search: Perplexity hat unparsbare Antwort zurückgegeben (HTTP {status}) — versuche es später erneut",
    exaMissingKey:
      "web_search: Exa-Backend benötigt einen API-Schlüssel — setze EXA_API_KEY-Umgebungsvariable oder `exaApiKey` in ~/.reasonix/config.json; kostenlose 1000/Monat-Registrierung unter https://exa.ai",
    exaUnauthorized:
      "web_search: Exa-API-Schlüssel abgelehnt — überprüfe EXA_API_KEY oder erhalte einen unter https://exa.ai",
    exaRateLimit:
      "web_search: Exa-API-Rate-Limit erreicht oder monatliches Kontingent überschritten — warte oder upgrade unter https://exa.ai/pricing",
    exaServerError:
      "web_search: Exa-Serverfehler ({status}) — versuche es später erneut oder wechsle die Engine mit /search-engine bing|searxng|metaso|tavily|perplexity|exa",
    exaParseError:
      "web_search: Exa hat unparsbare Antwort zurückgegeben (HTTP {status}) — versuche es später erneut",
    fetchStatus:
      "web_fetch {status} für {url} — versuche: Bestätige, dass die URL im Browser aufgelöst wird; der Status deutet darauf hin, dass der Host eine Fehlerseite zurückgegeben hat",
    fetchRateLimit429:
      "web_fetch 429 für {url} — versuche: 10s warten vor erneuter Abfrage; der Host ratelimitet diesen Client",
    fetchForbidden403:
      "web_fetch 403 für {url} — versuche: Der Host blockiert diesen Client; die Seite erfordert möglicherweise eine Anmeldung oder blockiert Bots — verwende stattdessen web_search-Auszüge",
    fetchServerError5xx:
      "web_fetch {status} für {url} — versuche: Öffne die URL in einem Browser; falls sie lädt, ist dies vorübergehend und ein erneuter Versuch in 30s kann helfen",
    fetchTimeout:
      "web_fetch: Zeitüberschreitung nach {ms}ms für {url} — versuche: eine kürzere URL oder kleinere Inhalte; dies könnte ein langsames CDN sein, oder einmal erneut versuchen",
    fetchTooLarge:
      "web_fetch abgelehnt: content-length {len} Bytes überschreitet {cap}-Byte-Grenze ({url}) — versuche: eine andere URL mit kleineren Inhalten; diese Seite ist zu groß zum Abrufen",
    fetchBodyTooLarge:
      "web_fetch abgelehnt: Antwortbody überschritt {cap}-Byte-Grenze ({seen} Bytes gesehen) — versuche: eine andere URL mit kleineren Inhalten; diese Seite hat die Größenbeschränkung überschritten",
    fetchInvalidUrl:
      "web_fetch: URL muss mit http:// oder https:// beginnen — versuche: eine absolute http(s)-URL übergeben (die URL ist fehlerhaft oder verwendet ein nicht unterstütztes Schema)",
  },
  choiceConfirm: {
    ...EN.choiceConfirm,
    customLabel: "Eigene Antwort eingeben",
    customDesc:
      "Keine der Optionen passt — gib eine Freitext-Antwort ein. Das Modell liest sie wörtlich.",
    cancelLabel: "Abbrechen — Frage verwerfen",
    cancelDesc: "Modell stoppt und fragt, was du stattdessen möchtest.",
  },
  cardTitles: {
    ...EN.cardTitles,
    usage: "Nutzung",
    context: "Kontext",
    search: "Suche",
    subagent: "Subagent",
    reply: "Antwort",
    reasoning: "Reasoning",
    reasoningAborted: "Reasoning (abgebrochen)",
    reasoningEllipsis: "Reasoning…",
    error: "Fehler",
    doctor: "Doctor",
    you: "Du",
    task: "Aufgabe",
  },
  cardLabels: {
    ...EN.cardLabels,
    prompt: "Prompt",
    reason: "Grund",
    output: "Ausgabe",
    cache: "Cache",
    session: "Sitzung",
    balance: "Guthaben",
    turn: "Turn",
    system: "System",
    tools: "Tools",
    log: "Log",
    input: "Eingabe",
    topTools: "Top-Tools",
    logMsgs: "Log-Nachr",
    hitSingular: "{count} Treffer · {files} Datei",
    hitsPlural: "{count} Treffer · {files} Dateien",
    moreHitSingular: "⋮ +{count} weiterer Treffer",
    moreHitsPlural: "⋮ +{count} weitere Treffer",
    earlierLine: "⋮ {count} ausgeblendete Zeile (Strg+R für vollständige Ausgabe)",
    earlierLines: "⋮ {count} ausgeblendete Zeilen (Strg+R für vollständige Ausgabe)",
    hiddenLine: "⋮ {count} ausgeblendete Zeile",
    hiddenLines: "⋮ {count} ausgeblendete Zeilen",
    earlierStackLine: "⋮ {count} frühere Stack-Zeile ausgeblendet",
    earlierStackLines: "⋮ {count} frühere Stack-Zeilen ausgeblendet",
    agent: "Agent · {name}",
    response: "Antwort",
    writing: "Schreibe …",
    tok: "Tok",
    pilcrow: "¶",
    aborted: "abgebrochen",
    truncatedByEsc: "[durch Esc gekürzt]",
    rejected: "abgelehnt",
    exit: "Exit {code}",
    bytesIn: "{bytes} rein",
    elapsedSec: "{secs}s",
    stackTrace: "Stacktrace",
    retries: "Wiederholungen",
    reasoningLabel: "Reasoning · {count} ¶",
    runningLabel: "läuft",
    workingLabel: "arbeitet",
    defaultFooter: "↑↓ auswählen  ·  ⏎ bestätigen  ·  Esc abbrechen",
    applyAction: "[a] anwenden",
    skipAction: "[s] überspringen",
    rejectAction: "[r] ablehnen",
    levelOk: "OK",
    levelWarn: "Warn",
    levelFail: "FEHLGESCHLAGEN",
    checksLabel: "Prüfungen",
    passed: "bestanden",
    warnTag: "Warn",
    failTag: "Fehl",
    stepLabel: "Schritt",
    done: "erledigt",
    inProgress: "← in Bearbeitung",
    upcoming: "bevorstehend",
    resumed: "fortgesetzt · ",
    archive: "⏤ archivieren · ",
    more: "⋮ +{count} weitere",
    categoryUser: "Benutzer",
    categoryFeedback: "Feedback",
    categoryProject: "Projekt",
    categoryReference: "Referenz",
  },
  mcpHealth: {
    ...EN.mcpHealth,
    noData: "Keine Inspektionsdaten",
    healthy: "Gesund · {ms}ms",
    slow: "Langsam · {ms}ms",
    verySlow: "Sehr langsam · {ms}ms",
    slowToast: "⚠ MCP `{name}` langsam · {seconds}s p95 über die letzten {sampleSize} Aufrufe",
    emptyHint:
      "ℹ keine MCP-Server konfiguriert — versuche: `reasonix setup` zur erneuten Auswahl, oder `reasonix mcp install filesystem` · Shell-Befehle werden pro Aufruf abgefragt (einmal erlauben / immer erlauben / ablehnen), kein globales Allow-All",
  },
  denyContextInput: {
    ...EN.denyContextInput,
    description:
      "Sag dem Agenten, warum du abgelehnt hast. Der nächste Versuch sieht deinen Grund als zusätzlichen Kontext.",
  },
  cardStream: {
    ...EN.cardStream,
    scrollAbove: " ↑ {scroll} / {max} Zeile darüber",
    scrollAbovePlural: " ↑ {scroll} / {max} Zeilen darüber",
    scrollMore: " — {remaining} weitere",
    scrollPgUp: " · Bild↑ / Mausrad",
    scrollCopy: " · /copy aktiviert Kopiermodus",
  },
  slashArgPicker: {
    ...EN.slashArgPicker,
    noMatch: 'Keine Übereinstimmung für "{partial}"',
    keepTyping: " — tippe weiter, oder Rücktaste zum Bearbeiten",
    above: "   ↑ {hidden} darüber",
    below: "   ↓ {hidden} darunter",
    footer: "  ↑↓ navigieren · Tab / ⏎ auswählen · Esc abbrechen",
  },
  mcpMarketplace: {
    ...EN.mcpMarketplace,
    title: "MCP-Marktplatz",
    filter: "Filter: ",
    filterPlaceholder: "(tippen zum Filtern)",
    matchSingular: "{n} Treffer",
    matchPlural: "{n} Treffer",
    loading: "lade…",
    noEntries: "Keine Einträge",
    opening: "Öffne Registry…",
    cached: "· zwischengespeichert",
    exhausted: "· erschöpft",
    loadingMore: "Lade mehr…",
    allLoaded: "Alle Seiten geladen",
    fetchingDetail: "Hole Smithery-Details…",
    noInstallInfo:
      "Keine Installationsinfo für {name} - versuche `npx -y @smithery/cli install {name}`",
    alreadyInstalled: "Bereits installiert: {spec}",
    installed: "Installiert → {spec}",
    uninstalled: "{name} deinstalliert",
    installFailed: "Installation fehlgeschlagen: {message}",
    notInstalled: "Nicht installiert: {name}",
    bridged: "✓ {name} installiert - verbunden",
    bridgeFailed: "▲ {name} installiert - Verbindung fehlgeschlagen: {reason}",
    bridgeReloadFailed:
      "✓ {name} installiert - starte `reasonix code` neu zur Verbindung (Neuladen fehlgeschlagen: {message})",
    restartBridge: "✓ {name} installiert - starte `reasonix code` neu zur Verbindung",
    needsEnv: "  ·  benötigt Umgebungsvariable: {env}",
    badgeOfficial: "[off]",
    badgeSmithery: "[smt]",
    badgeLocal: "[loc]",
    footerHint:
      "Filter eingeben · ↑↓ auswählen · ⏎ installieren/umschalten · Bild↓ mehr laden · Esc schließen",
    specLine: "Spec: {runtime} {id} · {transport}",
    smitheryDetail: "(Smithery-Eintrag — Installationsdetails werden bei Enter abgerufen)",
    statusError: "Fehler: {message}",
  },
  mcpBrowser: {
    ...EN.mcpBrowser,
    title: "◈ MCP-Browser",
    empty:
      "Keine MCP-Server angehängt. Führe `reasonix setup` aus, um welche auszuwählen, oder starte mit --mcp.",
    serverCount: "{count} Server",
    footer: "↑↓ auswählen · [r] neu verbinden · [d] deaktivieren · Esc beenden",
  },
  mcpBrowse: {
    ...EN.mcpBrowse,
    noResources:
      "Keine Ressourcen auf einem verbundenen MCP-Server (oder keine Server verbunden). `/mcp` zeigt den aktuellen Satz.",
    readOne: "Lese einen: `/resource <uri>` — oder verwende Tab in der Auswahl.",
    noPrompts:
      "Keine Prompts auf einem verbundenen MCP-Server (oder keine Server verbunden). `/mcp` zeigt den aktuellen Satz.",
    fetchOne:
      "Rufe einen ab: `/prompt <name>` — Argumente werden noch nicht unterstützt; Prompts mit erforderlichen Argumenten geben einen Fehler vom Server zurück.",
    noServerForResource: 'Kein Server bietet Ressource "{name}"',
    resourceHint: "`/resource` ohne Argument listet verfügbare Ressourcen.",
    readFailed: "readResource fehlgeschlagen",
    noServerForPrompt: 'Kein Server bietet Prompt "{name}"',
    promptHint: "`/prompt` ohne Argument listet verfügbare Prompts.",
    fetchFailed: "getPrompt fehlgeschlagen",
  },
  mcpLifecycle: {
    ...EN.mcpLifecycle,
    handshake: "Handshake…",
    connected: "verbunden",
    failed: "fehlgeschlagen",
    disabled: "deaktiviert",
    reconnect: "Wiederverbinden…",
    initDetail: "initialisiere → tools/list → resources/list",
    reconnectDetail: "baue ab · neuer Handshake · liste Tools",
    disabledDetail: "via /mcp disable {name}",
    failedSetupHint:
      "→ führe `reasonix setup` aus, um diesen Eintrag zu entfernen, oder behebe das zugrunde liegende Problem (fehlendes npm-Paket, Netzwerk usw.).",
    failedSetupConfigHint:
      "→ führe `reasonix setup` aus, um fehlerhafte Einträge aus deiner gespeicherten Konfiguration zu entfernen.",
    abortedHint:
      "MCP-Start abgebrochen — {count} Server übersprungen. Führe /mcp aus, um es erneut zu versuchen, sobald du das zugrunde liegende Problem behoben hast.",
    toolsReady: "Tools bereit",
    warnLabel: "Warn",
  },
  checkpointPicker: {
    ...EN.checkpointPicker,
    title: "Checkpoint wiederherstellen — {workspace}",
    header: " ◈ REASONIX · Checkpoint auswählen ",
    empty: "  Noch keine Checkpoints in diesem Arbeitsbereich - siehe /checkpoint zum Erstellen",
    more: "     … {hidden} weitere",
    footer: "  ↑↓ auswählen  ·  ⏎ wiederherstellen  ·  [d] vergessen  ·  Esc beenden",
    footerEmpty: "  Esc beenden",
  },
  planReviseConfirm: {
    ...EN.planReviseConfirm,
    title: "Plan-Überarbeitung vorgeschlagen",
    metaRight: "−{removed}  +{added}  ·  {kept} behalten",
    updatedSummary: "Aktualisierte Zusammenfassung: {summary}",
    acceptLabel: "Überarbeitung annehmen — neue Schrittliste anwenden",
    acceptHint:
      "Ersetzt den Restplan durch die vorgeschlagenen Schritte. Erledigte Schritte bleiben unberührt.",
    rejectLabel: "Ablehnen — Originalplan behalten",
    rejectHint:
      "Vorschlag verwerfen. Modell fährt mit den ursprünglichen verbleibenden Schritten fort.",
  },
  diffApp: {
    ...EN.diffApp,
    title: "reasonix diff",
    turnLabel: "Turn {turn} ({current}/{total})",
    turnsAligned: "{count} Turns ausgerichtet",
    paneEmpty: "(keine Datensätze auf dieser Seite für diesen Turn)",
    kindMatch: "✓ Übereinstimmung",
    kindDiverge: "★ Abweichung",
    kindOnlyInA: "← nur in A",
    kindOnlyInB: "→ nur in B",
  },
  recordView: {
    ...EN.recordView,
    userPrefix: "Du » ",
    assistant: "Assistent",
    toolPrefix: "Tool<",
    argsLabel: "  Args: ",
    resultArrow: "  → ",
    error: "Fehler ",
    cache: "  · Cache ",
    toolCallOnly: "(nur Tool-Call-Antwort)",
    truncateExtra: "(+{extra} Zeichen)",
  },
  replayApp: {
    ...EN.replayApp,
    emptyTranscript: "Leeres Transkript",
    turnProgress: "Turn {current}/{total}",
    noRecords: "Keine Datensätze",
    untracked: "(nicht verfolgt)",
    churned: "(umgewandelt ×{count})",
  },
  builtinSkills: {
    ...EN.builtinSkills,
    explore:
      'Durchsuche die Codebasis in einem isolierten Subagenten — breit angelegte, schreibgeschützte Untersuchung, die eine destillierte Antwort zurückgibt. Am besten für: »Finde alle Stellen, die…", »Wie funktioniert X im gesamten Projekt", »Durchsuche den Code nach Y".',
    research:
      'Recherchiere eine Frage durch Kombination von Websuche + Codelesen in einem isolierten Subagenten. Am besten für: »Wird X-Feature von Bibliothek Y unterstützt?", »Was ist der kanonische Weg, Z zu tun?", »Vergleiche unsere Implementierung mit dem Standard".',
    review:
      "Überprüfe die ausstehenden Änderungen (aktueller Branch-Diff) in einem isolierten Subagenten — kennzeichnet Korrektheit, Sicherheit, fehlende Tests, versteckte Verhaltensänderungen; meldet Befund + pro-Problem datei:zeile. Schreibgeschützt; das übergeordnete Element entscheidet, was zu tun ist.",
    securityReview:
      "Sicherheitsfokussierte Überprüfung des aktuellen Branch-Diffs in einem isolierten Subagenten — kennzeichnet Injection/Authz/Secrets/Deserialisierung/Pfad-Traversal/Krypto-Probleme, mit Schweregrad. Schreibgeschützt. Verwende beim Ausliefern von Änderungen, die Auth, Eingabeanalyse, Datei-E/A oder externe Anfragen betreffen.",
    test: "Führe die Testsuite des Projekts aus, diagnostiziere Fehler, schlage SEARCH/REPLACE-Fixes vor, wiederhole bis grün (oder stoppe nach 2 Fixversuchen beim gleichen Fehler). Inline — läuft in der übergeordneten Schleife, sodass du die Edit-Blocks siehst und /apply verwenden kannst. Erkennt npm/pnpm/yarn/pytest/go/cargo.",
  },
  shortcutsHelp: {
    ...EN.shortcutsHelp,
    title: "Tastenkürzel",
    groupInput: "Eingabe",
    groupNavigation: "Navigation",
    groupSession: "Sitzung",
    groupSystem: "System",
    descEnter: "Nachricht senden",
    descShiftEnter: "Neue Zeile",
    descCtrlEnter: "Neue Zeile",
    descCtrlJ: "Neue Zeile",
    descCtrlU: "Eingabe leeren",
    descCtrlW: "Wort löschen",
    descCtrlP: "Tastenkürzel anzeigen/ausblenden",
    descCtrlX: "Im Editor öffnen",
    descArrows: "Eingabeverlauf",
    descPgUpDown: "Seite scrollen",
    descCtrlL: "Bildschirm leeren",
    descCtrlB: "Seitenleiste umschalten",
    descNewSession: "Neue Sitzung",
    descListSessions: "Sitzungen auflisten",
    descSwitchModel: "Modell wechseln",
    descSwitchEffort: "Reasoning-Effort wechseln",
    descSwitchTheme: "Theme wechseln",
    descCtrlC: "Beenden",
    descEsc: "Stoppen / Abbrechen",
    descCtrlR: "Ausführlich umschalten",
    descCtrlO: "Antwort erweitern (nur während Streaming)",
    descHelp: "Alle Befehle anzeigen",
    descShiftTab: "Edit-Modus wechseln",
    descAltS: "Eingabe speichern / abrufen",
  },
  mcpCli: {
    ...EN.mcpCli,
    bundledCatalog: "Mitgelieferte MCP-Server (Offline-Katalog):",
    justFetched: "Gerade abgerufen",
    cachedAge: "Zwischengespeichert, {age}",
    moreAvailable: "Mehr verfügbar",
    allLoaded: "Alle geladen",
    morePagesAvailable: "▸ mehr Seiten verfügbar — `reasonix mcp list --pages <n>` oder --all",
    installHint: "Installieren:  reasonix mcp install <name>",
    usageSearch: "Verwendung: reasonix mcp search <abfrage>",
    usageInstall: "Verwendung: reasonix mcp install <name>",
    noMatchesFor: 'Keine Treffer für "{q}" in {count} geladenen Einträgen ({source})',
    matchCount: '{count} Treffer für "{q}" in {source}-Registry ({loaded} durchsuchte Einträge):',
    moreLoaded: "… {count} weitere geladen — verwende `reasonix mcp search <abfrage>` zum Filtern",
    moreMatches: "… {count} weitere Treffer",
    installed: "Installiert: {spec}",
    noServerFound:
      'Kein MCP-Server namens "{target}" gefunden nach {pages} Seite(n) der {source}-Registry.',
    noServerTryMore: "Versuche: reasonix mcp install {target} --max-pages 100",
    noInstallMeta:
      'Konnte Installationsmetadaten für "{name}" nicht ableiten — versuche `npx -y @smithery/cli install {name}` direkt.',
    buildSpecFailed: "Kann Installationsspec für {name} nicht erstellen: {message}",
    alreadyInstalled: "Bereits installiert: {spec}",
  },
};
