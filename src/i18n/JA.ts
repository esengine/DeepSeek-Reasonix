import type { TranslationSchema } from "./types.js";

export const JA: TranslationSchema = {
  common: {
    error: "エラー",
    warning: "警告",
    loading: "読み込み中...",
    done: "完了",
    cancel: "キャンセル",
    confirm: "確認",
    back: "戻る",
    next: "次へ",
    tool: "ツール",
    running: "実行中",
    noTurns: "(まだターンがありません)",
  },
  cli: {
    description:
      "DeepSeekネイティブのエージェントフレームワーク — キャッシュヒットと低コストトークンのために設計。",
    continue: "最後に使用したチャットセッションをピッカーなしで再開します。",
    setup: "対話型ウィザード — APIキー、MCPサーバー。いつでも再実行して再設定できます。",
    code: "コード編集チャット — <dir>（デフォルト: cwd）をルートとするファイルシステムツール、コーディング用システムプロンプト、v4-flash ベースライン。",
    chat: "ライブキャッシュ/コストパネル付きの対話型 Ink TUI。",
    run: "単一タスクを非対話的に実行し、出力をストリーミングします。",
    stats: "使用状況ダッシュボードを表示します。",
    doctor: "ワンコマンドのヘルスチェック。",
    commit: "ステージされた差分からコミットメッセージを生成します。",
    sessions: "保存されたチャットセッションを一覧表示、または名前で詳細を表示します。",
    pruneSessions:
      "アイドル状態がN日以上の保存セッションを削除します（デフォルト90）。--dry-run でプレビュー。",
    events: "カーネルイベントログのサイドカーを整形表示します。",
    replay: "トランスクリプトをスクラブする対話型 Ink TUI。",
    diff: "2つのトランスクリプトを分割ペインの Ink TUI で比較します。",
    mcp: "Model Context Protocol ヘルパー — サーバーの検出、セットアップのテスト。",
    version: "Reasonix のバージョンを表示します。",
    update: "新しい Reasonix をチェックしてインストールします。",
    index: "ローカルのセマンティック検索インデックスを構築（または増分更新）します。",
  },
  stats: {
    usageHint: "`reasonix chat`、`reasonix code`、または `reasonix run <task>` を実行するたびに",
    usageDetail: "ログに1行追加され、`reasonix stats` で集計されます。",
  },
  run: {
    missingApiKey:
      "DEEPSEEK_API_KEY が設定されておらず、stdin が TTY ではありません（プロンプトを表示できません）。\n" +
      "環境変数を設定するか、`reasonix chat` を一度対話的に実行してキーを保存してください。\n",
  },
  sessions: {
    emptyHint:
      "保存されたセッションはまだありません — `reasonix chat` を実行してください（--no-session を付けない限り自動保存されます）。",
    listHeader: "保存されたセッション (~/.reasonix/sessions/):",
    inspectHint: "詳細表示:  reasonix sessions <name>",
    resumeHint: "再開:   reasonix chat --session <name>",
    noSession: '"{name}" という名前のセッションはありません（または空です）。',
    lookedAt: "参照: {path}",
    noIdleSessions: "{days}日以上アイドルのセッションはありません。削除はありません。",
    wouldPrune: "{count}件のセッション（{days}日以上アイドル）を削除します:",
    dryRunHint: "--dry-run なしで再実行すると実際に削除されます。",
    prunedCount: "{count}件のセッション（{days}日以上アイドル）を削除しました:",
    daysInvalid: "--days は正の整数である必要があります（{days} が指定されました）。",
  },
  ui: {
    welcome: "Run `reasonix` any time to start chatting — your settings are remembered.",
    taglineChat: "DeepSeekネイティブエージェント",
    taglineCode: "DeepSeekネイティブコーディングエージェント",
    taglineSub: "キャッシュファースト · フラッシュファースト",
    startSessionHint: "メッセージを入力してセッションを開始",
    inputPlaceholder: "何でも聞いてください... (/ でコマンド、@ でファイル)",
    busy: "考え中...",
    thinking: "▸ 考え中...",
    undo: "元に戻す",
    undoHint: "5秒以内に u を押すと元に戻せます",
    applied: "適用済み",
    rejected: "拒否されました",
    noDashboard: "自動起動の埋め込みWebダッシュボードを抑制します。",
    openDashboardHint:
      "サーバーの準備ができ次第、デフォルトブラウザでダッシュボードURLを開きます。--no-dashboard が設定されている場合は何もしません。",
    dashboardPortHint:
      "ダッシュボードを固定ポート (1～65535) に固定します。再起動後も安定 — SSHトンネルに必要。デフォルト: エフェメラル。",
    dashboardPortInvalid:
      "▲ --dashboard-port={value} を無視します（1～65535の整数である必要があります）— エフェメラルにフォールバック",
    dashboardAutoStartFailed:
      "▲ ダッシュボードの自動起動に失敗しました ({reason}) — /dashboard を試すか、--no-dashboard で抑制してください",
    systemAppendHint:
      "コードシステムプロンプトに指示を追加します。デフォルトプロンプトを置き換えるのではなく、その後ろに追加します。",
    systemAppendFileHint:
      "コードシステムプロンプトにファイルの内容を追加します。デフォルトプロンプトは置き換えません。UTF-8、cwdからの相対パスまたは絶対パス。",
    resumedSession:
      '▸ セッション "{name}" を再開しました（{count}件の既存メッセージ）· /new で新規開始 · /sessions で管理',
    newSession:
      '▸ セッション "{name}" (新規) — チャットに応じて自動保存 · /sessions で名前変更または削除',
    ephemeralSession:
      "▸ エフェメラルチャット（セッション永続化なし）— --no-session を外すと有効になります",
    restoredEdits:
      "▸ 中断された前回の実行から {count} 件の保留中編集ブロックを復元しました — /apply でコミット、/discard で破棄。",
    resumedPlan: "プランを再開しました · {when}{summary}",
    tipEditBindings: {
      topic: "編集ゲートのキーバインド",
      sections: [
        {
          rows: [
            { key: "y / n", text: "保留中の編集を承認または破棄" },
            {
              key: "Shift+Tab",
              text: "review ↔ AUTO を切り替え（永続化; AUTO では即時適用）",
            },
            { key: "u", text: "最後に自動適用されたバッチを取り消し（5秒のバナー表示中）" },
          ],
        },
      ],
      footer: "現在のモードは下部ステータスバーに表示 · /keys で全リファレンス",
    },
    tipMouseClipboard: {
      topic: "マウス + クリップボード",
      sections: [
        {
          rows: [
            { key: "ドラッグ", text: "テキストを選択 — ターミナルネイティブ、修飾キー不要" },
            {
              key: "右クリック",
              text: "ターミナルネイティブのメニュー（Windows Terminal等での貼り付け/コピー）",
            },
            {
              key: "ホイール",
              text: "チャット履歴をスクロール（Web/クラウド/SSHターミナルでも動作）",
            },
            {
              key: "↑ / ↓",
              text: "プロンプト履歴（複数行ドラフトでは行単位カーソル）— Ctrl+P / Ctrl+N でも可",
            },
            {
              key: "PgUp / PgDn",
              text: "チャット履歴をスクロール（マウスホイールもここにルーティング）",
            },
          ],
        },
      ],
      footer: "/keys でキーボード+マウスの全リファレンスを表示",
    },
    keysReference: {
      topic: "Reasonix キー + マウス リファレンス",
      sections: [
        {
          title: "キーボード",
          rows: [
            { key: "Enter", text: "プロンプトを送信" },
            { key: "Shift+Enter", text: "プロンプトに改行を挿入" },
            {
              key: "↑ / ↓",
              text: "前/次のプロンプト履歴 · 複数行ドラフトではカーソル上下",
            },
            { key: "Ctrl+P / Ctrl+N", text: "↑ / ↓ のreadlineエイリアス" },
            { key: "Ctrl+A / Ctrl+E", text: "現在行の先頭/末尾にジャンプ" },
            { key: "Ctrl+W", text: "カーソル前の単語を削除" },
            { key: "Ctrl+U", text: "プロンプト全体をクリア" },
            { key: "Tab", text: "@メンション補完 · フォルダ展開 · スラッシュコマンド確定" },
            { key: "Shift+Tab", text: "編集ゲート: review ↔ AUTO モード切替" },
            { key: "Esc", text: "ピッカーを閉じる · 実行中のモデルターンを中断" },
            {
              key: "Ctrl+C",
              text: "実行中のモデルターンを中断（コピーではありません — クリップボード参照）",
            },
            { key: "PgUp / PgDn", text: "チャット履歴を1ページずつスクロール" },
            { key: "End", text: "チャットを最新行にジャンプ" },
            {
              key: "Ctrl+R",
              text: "詳細モード切替 — 推論・ツール出力を全文表示、head/tail省略なし",
            },
          ],
        },
        {
          title: "マウス",
          rows: [
            {
              key: "ホイール",
              text: "チャット履歴をスクロール（Web/クラウド/SSHターミナルでも動作）",
            },
            { key: "ドラッグ", text: "テキストをネイティブ選択 — 直接コピー可能、修飾キー不要" },
            {
              key: "右クリック",
              text: "ターミナルネイティブ（Windows Terminal等での貼り付けメニュー）",
            },
          ],
        },
        {
          title: "コピー / 貼り付け",
          rows: [
            { key: "テキスト選択", text: "ドラッグで選択 — ターミナルネイティブ（修飾キー不要）" },
            {
              key: "コピー",
              text: "Ctrl+Shift+C (Win/Linux) · Cmd+C (macOS) — または端末が自動コピーする場合もあります",
            },
            { key: "貼り付け", text: "Ctrl+V または Ctrl+Shift+V (Win/Linux) · Cmd+V (macOS)" },
            {
              key: "ブラケット貼り付け",
              text: "複数行貼り付けは1ブロックとして保持 — 中間の改行で自動送信されません",
            },
          ],
        },
        {
          title: "編集ゲート（コードモード）",
          rows: [
            { key: "y / n", text: "レビューモーダルで保留中の編集を承認または破棄" },
            { key: "Shift+Tab", text: "review ↔ AUTO を切り替え（セッション間で永続化）" },
            { key: "u", text: "最後に自動適用されたバッチを取り消し（5秒のバナー表示中）" },
          ],
        },
      ],
      footer:
        "ほとんどの端末でホイールスクロールが動作します（Web/クラウド/SSH含む）— SGRマウストラッキングはデフォルトで有効で、ネイティブのドラッグ選択や右クリックを妨げません。--no-mouse でオプトアウト。",
    },
    tipShownOnce: "一度だけ表示",
    modelOverride: "デフォルトモデルを上書き",
    noSession: "この実行のセッション永続化を無効化",
    noMouseHint: "SGRマウストラッキングを無効化; ネイティブのドラッグ選択と右クリックを復元",
    noProxyHint: "この実行ではHTTPS_PROXY / HTTP_PROXYを無視; 直接接続",
    resumeHint: "指定されたセッションを強制再開（アイドル状態でも）",
    newHint: "強制的に新規セッション（--session / --continue を無視）",
    transcriptHint: "JSONLトランスクリプトの書き込み先パス",
    budgetHint: "セッションのUSD上限 — 80%で警告、100%で次のターンを拒否",
    modelIdHint: "DeepSeekモデルID（例: deepseek-v4-flash）",
    systemPromptHint: "デフォルトのシステムプロンプトを上書き",
    effortHint: "推論努力 — low|medium|high|max",
    sessionNameHint: "セッション名（デフォルト: 'default'）",
    ephemeralHint: "この実行のセッション永続化を無効化",
    mcpSpecHint: "MCPサーバー指定（繰り返し可）",
    mcpPrefixHint: "MCPツール名にこの文字列をプレフィックスとして付与",
    noConfigHint: "この実行では ~/.reasonix/config.json を無視",
    effortHintShort: "推論努力 — low|medium|high|max",
    budgetHintShort: "セッションUSD上限",
    transcriptHintShort: "JSONLトランスクリプトパス",
    mcpSpecHintShort: "MCPサーバー指定（繰り返し可）",
    mcpPrefixHintShort: "MCPツール名プレフィックス",
    dryRunHint: "実際にインストールせずにインストール内容を表示",
    rebuildHint: "インデックスをゼロから再構築",
    embedModelHint: "埋め込みモデル名",
    projectDirHint: "プロジェクトルートディレクトリ",
    ollamaUrlHint: "OllamaサーバーURL",
    skipPromptsHint: "確認プロンプトをスキップ",
    verboseHint: "セッションメタデータを完全表示",
    pruneDaysHint: "この日数以上アイドルのセッションを削除（デフォルト90）",
    pruneDryRunHint: "実際に削除せずに削除対象を一覧表示",
    eventTypeHint: "イベントタイプでフィルタ",
    eventSinceHint: "このイベントIDから開始",
    eventTailHint: "最後のN件のイベントのみ表示",
    jsonHint: "JSONとして出力",
    projectionHint: "各イベントでの投影状態を表示",
    printHint: "TUIの代わりにstdoutに出力",
    headHint: "最初のN件のイベントのみ表示",
    tailHint: "最後のN件のイベントのみ表示",
    mdReportHint: "markdown差分レポートをこのパスに書き出し",
    printHintTable: "stdoutにテーブルを出力",
    tuiHint: "対話型TUIを開く",
    labelAHint: "左ペインのラベル",
    labelBHint: "右ペインのラベル",
    mcpListDescription: "MCPレジストリを閲覧（official → smithery → ローカルフォールバック）",
    mcpInspectDescription: "MCPサーバー仕様を検査（ツール、リソース、プロンプト）",
    mcpSearchDescription: "クエリに一致するMCPレジストリのサーバーを検索",
    mcpInstallDescription: "名前でMCPサーバーをインストール（設定に仕様を書き込み）",
    mcpBrowseDescription: "対話型マーケットプレイスブラウザ — 入力でフィルタ、Enterでインストール",
    mcpLocalHint: "バンドルされたオフラインカタログのみ表示",
    mcpRefreshHint: "24時間キャッシュをバイパスして再取得",
    mcpLimitHint: "表示する最大エントリ数",
    mcpPagesHint: "このページ数を先行読み込み（デフォルト1）",
    mcpAllHint: "全ページを読み込み（初回実行時は低速）",
    mcpMaxPagesHint: "検索時に走査する最大ページ数（デフォルト20）",
    jsonHintCatalog: "JSONとして出力",
    jsonHintReport: "検査レポートをJSONとして出力",
    modelOverrideFlash: "モデルを上書き（デフォルト: deepseek-v4-flash）",
    skipConfirmHint: "確認プロンプトをスキップ",
    yoloHint:
      "この実行でプランチェックポイントを自動承認（editMode=yolo 相当、設定は変更しません）",
  },
  code: {
    workspaceConflict:
      "⚠ ワークスペースに別のエージェントプラットフォームのファイル ({platforms}) が含まれています。Reasonix Code がそれらをプロジェクトコンテンツとして読み取る可能性があります。意図しない場合は --dir <your-project> で再起動してください。\n",
    systemAppendEmpty: "--system-append が空です — プロンプトテキストは追加されません\n",
    systemAppendFileReadError:
      'エラー: --system-append-file "{filePath}" を読み取れません: {errorDetails}\n',
  },
  slash: {
    help: { description: "全コマンドリファレンスを表示" },
    status: { description: "現在のモデル、フラグ、コンテキスト、セッション" },
    effort: {
      description:
        "reasoning_effort の上限 (low|medium|high|max); high は vLLM/Azure で安全なデフォルト",
      argsHint: "<low|medium|high|max>",
    },
    model: { description: "DeepSeekモデルIDを切り替え", argsHint: "<id>" },
    models: { description: "DeepSeek /models から取得した利用可能なモデル一覧" },
    theme: {
      description: "ターミナルテーマ設定を表示または保存。引数なしでピッカーを開きます。",
      argsHint: "[auto|dark|light|midnight|deep-blue|high-contrast]",
    },
    language: {
      description: "表示言語を切り替え",
      argsHint: "<EN|zh-CN|de|ja>",
      success: "言語を日本語に切り替えました。",
      unsupported: "未対応の言語コードです: {code}。対応: {supported}。",
    },
    budget: {
      description:
        "セッションUSD上限 — 80%で警告、100%で次のターンを拒否。デフォルトはオフ。/budget 単体で現在の状態を表示",
      argsHint: "[usd|off]",
    },
    mcp: { description: "このセッションに接続されたMCPサーバーとツールを一覧表示" },
    resource: {
      description: "MCPリソースの閲覧と読み取り（引数なし → URI一覧; <uri> → 内容取得）",
      argsHint: "[uri]",
    },
    prompt: {
      description: "MCPプロンプトの閲覧と取得（引数なし → 名前一覧; <name> → プロンプト表示）",
      argsHint: "[name]",
    },
    memory: {
      description: "ピン留めメモリの表示/管理 (REASONIX.md + ~/.reasonix/memory)",
      argsHint: "[list|show <name>|forget <name>|clear <scope> confirm]",
    },
    skill: {
      description: "ユーザースキルの一覧/実行（プロジェクト + カスタム + グローバル + ビルトイン）",
      argsHint: "[list|paths|show <name>|<name> [args]]",
    },
    hooks: {
      description:
        "アクティブなフックを一覧 (.reasonix/ 下の settings.json) · reload でディスクから再読込",
      argsHint: "[reload]",
    },
    permissions: {
      description:
        "シェル許可リストの表示/編集（ビルトインは読取専用 · プロジェクト毎: ~/.reasonix/config.json）",
      argsHint: "[list|add <prefix>|remove <prefix|N>|clear confirm]",
    },
    dashboard: {
      description: "埋め込みWebダッシュボードを起動 (127.0.0.1, トークン認証)",
      argsHint: "[stop]",
    },
    update: {
      description: "現在のバージョンと最新バージョン + アップグレード用シェルコマンドを表示",
    },
    stats: {
      description:
        "セッション横断のコストダッシュボード（今日/週/月/全期間 · キャッシュヒット · Claude比較）",
    },
    cost: {
      description:
        "引数なし → 前回ターンの消費 (Usageカード); テキスト付き → そのテキストを送信した場合のコスト見積もり（最悪ケース + キャッシュヒット想定）",
      argsHint: "[text]",
    },
    doctor: { description: "ヘルスチェック (api / config / api-reach / index / hooks / project)" },
    context: { description: "コンテキストウィンドウの内訳を表示 (system / tools / log / input)" },
    retry: { description: "最後のメッセージを切り詰めて再送（新規サンプル）" },
    compact: {
      description:
        "ログ内の大きすぎるツール結果とツール呼び出し引数を縮小; トークン数の上限、デフォルト4000",
      argsHint: "[tokens]",
    },
    cwd: {
      description:
        "セッション中にワークスペースルートを切り替え — fs / shell / memory ツールの向き先を変更、プロジェクトフックを再読込、@メンションのウォーカーを更新",
      argsHint: "[path]",
    },
    stop: { description: "現在のモデルターンを中断（Escの入力代替）" },
    feedback: { description: "診断情報をクリップボードにコピーしてGitHub Issueを作成" },
    about: { description: "プロジェクト情報 — バージョン、ウェブサイト、リポジトリ、ライセンス" },
    keys: { description: "キーボード + マウス + コピー/貼り付け リファレンス" },
    plans: { description: "このセッションのアクティブおよびアーカイブ済みプランを新しい順に表示" },
    replay: {
      description:
        "アーカイブ済みプランを読み取り専用のタイムトラベルスナップショットとして読み込み（デフォルト: 最新）",
      argsHint: "[N]",
    },
    sessions: { description: "保存されたセッションを一覧（現在のセッションは ▸ で表示）" },
    title: { description: "会話からこのセッションの名前をモデルに変更させる" },
    qq: {
      description:
        "このセッションのQQチャンネルを接続、検査、または切断（初回接続時にApp ID / App Secretの設定をガイド）",
      argsHint: "[connect [appId appSecret [sandbox]]|status|disconnect]",
    },
    setup: { description: "終了して `reasonix setup` を実行するよう促します" },
    semantic: {
      description: "semantic_search の状態を表示 — 構築済み？Ollama導入済み？有効化方法は？",
    },
    clear: { description: "表示されているスクロールバックのみをクリア（ログ/コンテキストは保持）" },
    new: { description: "新しい会話を開始（コンテキストとスクロールバックをクリア）" },
    loop: {
      description: "<prompt> を <interval> ごとに自動再送信（何か入力 / Esc / /loop stop で停止）",
      argsHint: "<5s..6h> <prompt>  ·  stop  ·  (引数なし = 状態表示)",
    },
    exit: { description: "TUIを終了" },
    init: {
      description:
        "プロジェクトをスキャンしてベースラインの REASONIX.md を生成（モデルが作成; /apply でレビュー）。`force` で既存ファイルを上書き。",
      argsHint: "[force]",
    },
    apply: {
      description:
        "保留中の編集ブロックをディスクにコミット（引数なし → すべて; `1`, `1,3`, `1-4` → そのサブセット、残りは保留）",
      argsHint: "[N|N,M|N-M]",
    },
    discard: {
      description:
        "保留中の編集ブロックを書き込まずに破棄（引数なし → すべて; インデックス → そのサブセット）",
      argsHint: "[N|N,M|N-M]",
    },
    walk: {
      description:
        "保留中の編集を1ブロックずつ確認（git-add-p スタイル: ブロックごとに y/n、a で残りを適用、A でAUTOに切替）",
    },
    undo: { description: "最後に適用された編集バッチをロールバック" },
    history: {
      description: "このセッションの全編集バッチを一覧（/show用のID、取り消しマーカー付き）",
    },
    show: {
      description: "保存された編集差分を表示（idを省略すると最新の未取り消し分）",
      argsHint: "[id]",
    },
    commit: { description: "git add -A && git commit -m ...", argsHint: '"msg"' },
    checkpoint: {
      description:
        "セッションが触れた全ファイルのスナップショット（Cursorスタイルの内部ストア、gitではありません）。/checkpoint 単体で一覧表示。",
      argsHint: "[name|list|forget <id>]",
    },
    restore: {
      description: "指定されたチェックポイントにファイルをロールバック（/checkpoint list を参照）",
      argsHint: "<name|id>",
    },
    plan: {
      description:
        "読み取り専用プランモードの切替（submit_plan + 承認まで書き込みがブロックされます）",
      argsHint: "[on|off]",
    },
    mode: {
      description:
        "編集ゲート: review（キュー）· auto（適用+取り消し）· yolo（適用+シェル自動実行）。Shift+Tabで切り替え。",
      argsHint: "[review|auto|yolo]",
    },
    jobs: { description: "run_background で起動されたバックグラウンドジョブを一覧表示" },
    kill: {
      description: "IDでバックグラウンドジョブを停止（SIGTERM → 猶予後にSIGKILL）",
      argsHint: "<id>",
    },
    logs: {
      description: "バックグラウンドジョブの出力をtail表示（デフォルト: 最後の80行）",
      argsHint: "<id> [lines]",
    },
    btw: {
      description: "簡単な脇道の質問 — 白紙状態から回答し、会話コンテキストには追加されません",
      argsHint: "<question>",
    },
    "search-engine": {
      description:
        "Web検索バックエンドを切り替え — bing（デフォルト、プロキシなしで中国から利用可）、searxng（セルフホスト）、metaso（無料100回/日）、tavily（無料1000回/月）、perplexity（AIネイティブ）、exa（AIネイティブ）",
      argsHint: "<bing|searxng|metaso|tavily|perplexity|exa> [<key>]",
    },
  },
  wizard: {
    languageTitle: "言語を選択",
    languageSubtitle: "システムロケールから検出されました。後で /language で切り替え可能です。",
    welcomeTitle: "Reasonix へようこそ。",
    apiKeyPrompt: "DeepSeek APIキーを貼り付けて開始してください。",
    apiKeyGetOne: "取得先: https://platform.deepseek.com/api_keys",
    apiKeySavedLocally: "{path} にローカル保存されました",
    apiKeyInputLabel: "key › ",
    apiKeyInvalid:
      "キーが短すぎます — 完全なトークンを貼り付けてください（16文字以上、スペースなし）。",
    apiKeyChecking: "APIキーを確認中…",
    apiKeyRejected:
      "DeepSeekがこのAPIキーを拒否しました。有効なキーを貼り付けるか、Escでセットアップをキャンセルしてください。",
    apiKeyCheckFailed:
      "現在このAPIキーを確認できませんでした ({message})。ネットワークを確認するか、再試行してください。",
    apiKeyPreview: "プレビュー: {redacted}",
    themeTitle: "テーマを選択",
    themeSubtitle: "操作に応じてプレビューがライブ更新されます。後で /theme で変更可能です。",
    themeSampleHeading: "サンプル",
    themeFooter: "[↑↓] 移動 · [Enter] 確定 · [Esc] キャンセル",
    themeCaption: {
      dark: "クールなダークトーン（デフォルト）",
      light: "クリーンなライトモード",
      midnight: "Tokyo Night パレット",
      "deep-blue": "黒背景にディープブルー",
      "high-contrast": "アクセシビリティ",
    },
    reviewLabelTheme: "テーマ",
    mcpTitle: "Reasonix に接続するMCPサーバーを選択してください",
    mcpUserArgsHint: "({arg} を入力してください)",
    mcpFooterMulti:
      "[↑↓] 移動  ·  [Space] 切替  ·  [Enter] 確定  ·  [Esc] キャンセル  ·  空 = スキップ",
    mcpArgsTitle: "{name} の設定",
    mcpArgsDirMissing: "ディレクトリ {path} が存在しません。",
    mcpArgsDirCreateHint: "[Y/Enter] 作成 (mkdir -p) · [N/Esc] 別のパスを入力",
    mcpArgsDirCreateFailed: "{path} を作成できませんでした: {message}",
    mcpArgsRequiredParam: "必須パラメータ: ",
    mcpArgsEmpty: "{name} には値が必要です — 空文字列が入力されました。",
    mcpArgsNotADir: "{path} は存在しますがディレクトリではありません。",
    reviewTitle: "保存準備完了",
    reviewLabelApiKey: "APIキー",
    reviewLabelLanguage: "言語",
    reviewLabelMcp: "MCP",
    reviewMcpNone: "(なし)",
    reviewMcpServers: "{count} サーバー",
    reviewSavesTo: "保存先: {path}",
    reviewSaveError: "設定を保存できませんでした: {message}",
    reviewFooter: "[Enter] 保存 · [Esc] キャンセル",
    savedTitle: "▸ 保存しました。",
    savedShellHint:
      "モデルが実行したいシェルコマンドは毎回確認されます — プロンプトで `allow always` を選択すると、そのコマンドをこのプロジェクトで許可できます。設計上、全許可フラグはありません。",
    savedFooter: "[Enter] で終了",
    selectFooter: "[↑↓] 移動 · [Enter] 確定 · [Esc] キャンセル",
    stepCounter: "ステップ {step}/{total} · ",
    exitHint: "/exit で中断",
    apiKeyPlaceholder: "sk-...",
    themeSampleReasoning: "推論",
  },
  themePicker: {
    header: "テーマ",
    footer: "↑↓ 選択 · ⏎ 確定 · esc キャンセル",
    currentPref: "現在の設定",
    activeNow: "現在適用中",
    autoDesc: "REASONIX_THEME またはデフォルトを使用",
  },
  planFlow: {
    approveCardTitle: "プランを承認",
    approveCardMetaRight: "待機中",
    openQuestionsBanner:
      "▲ プランに未解決の質問またはリスクがあります — モデルが進む前に {refine} を選んで具体的な回答を書いてください。",
    openQuestionsHeader: "未解決の質問 / リスク",
    truncatedBodyMore: "… スクロールバックにさらに {n} 行あります",
    truncatedBodyMorePlural: "… スクロールバックにさらに {n} 行あります",
    picker: {
      accept: "承認",
      acceptHint: "このまま順番に実行",
      refine: "修正依頼",
      refineHint: "エージェントに追加指示を与え、新しいプランを作成",
      revise: "編集",
      reviseHint: "実行前にプランをインライン編集（スキップ/並べ替え）",
      reject: "拒否",
      rejectHint: "破棄し、エージェントがゼロから再試行",
    },
    refineFooter: "⏎ 送信  ·  esc でピッカーに戻る",
    refineQuestionsHeading: "以下に回答するか、希望する変更を記述してください:",
    modes: {
      approve: {
        title: "承認中 — 最後に指示はありますか？",
        hint: "プランが提起した質問に答えたり、制約を追加したり、そのままEnterで承認します。",
        blankHint: "（空でEnter = 追加指示なしで承認）",
      },
      refine: {
        title: "修正依頼中 — モデルに何を変更させますか？",
        hint: "問題点や不足点を説明するか、プランが提起した質問に答えてください。",
        blankHint: "（空でEnter = 未解決の質問はモデルが安全なデフォルトを選択）",
      },
      reject: {
        title: "拒否中 — モデルに理由を伝えてください（任意）",
        hint: "モデルが目標について何を誤解したか、代わりに何が欲しいかを伝えてください。",
        blankHint: "（空でEnter = 説明なしでキャンセル; モデルが希望を尋ねます）",
      },
      "checkpoint-revise": {
        title: "編集中 — 次のステップの前に何を変更しますか？",
        hint: "範囲変更、ステップのスキップ、代替アプローチ — モデルが残りのプランを調整します。",
        blankHint: "（空でEnter = 現在のプランを続行）",
      },
      "choice-custom": {
        title: "自由回答 — 適切な内容を入力してください",
        hint: "自由形式の返信。モデルはそれをそのまま読み取って続行します — 選択肢に合わせる必要はありません。",
        blankHint: "（空でEnter = モデルに実際の希望を尋ねる）",
      },
    },
    checkpoint: {
      title: "チェックポイント — ステップ完了",
      continue: "続行 — 次のステップを実行",
      continueHint: "モデルが次のステップで再開します。",
      finish: "完了 — サマリーしてクローズ",
      finishHint: "モデルが最終ステップを記録し、完了したプランをサマリーします。",
      revise: "修正 — 次のステップの前にフィードバック",
      reviseHint: "一時停止したまま、指示を入力; モデルが残りのプランを調整します。",
      stop: "停止 — ここでプランを終了",
      stopHint: "モデルがここまでの内容をサマリーして終了します。",
    },
    stepList: {
      counter: "{total} ステップ",
      counterSingular: "{total} ステップ",
      counterDone: "{done}/{total} 完了 ({pct}%) · {total} ステップ",
      counterDoneSingular: "{done}/{total} 完了 ({pct}%) · {total} ステップ",
    },
    noPlanSummary: "プラン本文はまだ送信されていません。",
    detailCollapsedHint: "Ctrl+P でプランの詳細を展開。",
    detailExpandedHint: "Ctrl+P で詳細を折りたたみ。",
    detailHeader: "プラン詳細",
    detailWindow: "{start}-{end} 行目 / 全 {total} 行を表示中",
    detailScrollHint: "PgUp/PgDn で詳細スクロール · Home/End でジャンプ",
    reviseTitle: "プランを編集",
    reviseSteps: "{count} ステップ",
    reviseFooter:
      "\u2191\u2193 フォーカス  \u00b7  space でスキップ切替  \u00b7  k/j 移動  \u00b7  \u23ce 確定  \u00b7  esc キャンセル",
    riskMed: " 中",
    riskHigh: " 高",
    completeMsg: "\u25b8 プラン完了 \u2014 全 {total} ステップ完了 \u00b7 アーカイブ済み",
  },
  app: {
    walkCancelledRemaining: "▸ ウォークをキャンセルしました — {count} ブロックがまだ保留中です。",
    walkCancelled: "▸ ウォークをキャンセルしました。",
    editModeYolo:
      "▸ 編集モード: YOLO — 編集とシェルコマンドが自動実行されます。/undo で編集のロールバックは可能です。注意して使用してください。",
    editModeAuto:
      "▸ 編集モード: AUTO — 編集が即時適用されます; 5秒以内に u を押すと元に戻せます（スペースでタイマー一時停止）。シェルコマンドは確認あり。",
    editModeReview:
      "▸ 編集モード: review — 編集は /apply（または y）/ /discard（または n）のキューに入ります",
    rejectedEdit: "▸ {path} への編集を拒否しました{context}",
    autoApprovingRest: "▸ このターンの残りの編集を自動承認します",
    flippedAutoSession: "▸ セッションの残りをAUTOモードに切り替えました（永続化）",
    flippedAutoWalk:
      "▸ AUTOモードに切り替え — 以降の編集は即時適用されます。ウォークを終了しました。",
    dashboardStopped: "▸ ダッシュボードを停止しました。",
    notedMemory: "▸ メモ ({scope}) — {verb} {path}",
    notedScopeProject: "プロジェクト",
    notedScopeGlobal: "グローバル",
    notedVerbCreated: "作成",
    notedVerbAppended: "追記",
    memoryWriteFailed: "# メモリ書き込みに失敗しました",
    verboseOn: "▸ 詳細モード オン — 推論とツール出力を全文表示",
    verboseOff: "▸ 詳細モード オフ — head/tail 省略を復元",
    commandFailed: "! コマンドが失敗しました",
    steerInjected: "▸ ステアリングをキューに入れました — 現在のステップの後に追加されます",
    steerCommandRejected: "▸ ビジー状態のターンを操作中はコマンドが無効です",
    btwUsage: "▸ /btw <question> — 会話コンテキストを汚さずに脇道の質問をします。",
    btwHeader: "≫ btw",
    btwFailed: "/btw が失敗しました",
    restoreCodeOnly: "▸ /restore はコードモード専用です",
    hookUserPromptSubmit: "UserPromptSubmit フック",
    hookStop: "Stop フック",
    atMentions: "▸ @メンション: {parts}",
    atUrl: "▸ @url: {parts}",
    atUrlFailed: "@url 展開に失敗しました",
    sessionTitleNoSession:
      "▸ 永続化されたセッションがアクティブでないため、名前変更する対象がありません。",
    sessionTitleNoContent: "▸ このセッションに名前を付けるのに十分な会話内容がまだありません。",
    sessionTitleNoTitle: "▸ モデルが使用可能なセッションタイトルを返しませんでした。",
    sessionTitleUpdated: '▸ セッションタイトルを更新しました: "{title}"',
    sessionTitleRenameFailed: '▸ タイトル "{title}" でセッションの名前を変更できませんでした。',
    sessionTitleRenamed: '▸ セッション名を "{name}" に変更しました — {title}',
    sessionTitleAutoRenamed: '▸ セッションを自動命名しました "{name}" — {title}',
    workspaceSwitched: "▸ ワークスペースを {root} に切り替えました",
    semanticRepointed: "▸ semantic_search の参照先を {root} に変更しました",
    semanticDisabledForRoot:
      "▸ semantic_search を無効化しました（{root} に互換インデックスがありません）",
    semanticRebootstrapFailed: "▸ semantic_search の再ブートストラップに失敗しました: {reason}",
    denied: "▸ 拒否: {cmd}{context}",
    alwaysAllowed: '▸ "{prefix}" を {dir} で常に許可',
    runningCommand: "▸ 実行中: {cmd}",
    startingBackground: "▸ 起動中（バックグラウンド）: {cmd}",
    checkpointSaved:
      "⛁ チェックポイント保存 · {id} · {count} ファイル · /restore {id} でこのステップをロールバック",
    continuingAfter: "▸ {label}{counter} の後に続行",
    planStoppedAt: "▸ {label}{counter} でプランを停止",
    revisingAfter: "▸ {label} の後に修正 — {feedback}",
    historyScrollHint: " ↑ 履歴を読込中 · End / PgDn で最下部に戻る · ↓ で1行進む",
    editHistoryTitle: "編集履歴（古い順）:",
    editHistoryNoCodeMode: "コードモードではありません",
    editHistoryNoEdits: "このセッションではまだ編集が記録されていません",
    editHistoryNoShowId:
      "使い方: /show [id] [path]   （idを省略すると最新; path はファイル別サマリーから）",
    editHistoryIdNotFound: "編集 #{id} はありません — /history で有効なIDを確認してください",
    editHistoryLookupFailed: "予期せぬエラー: 履歴の検索に失敗しました",
    editHistoryBatchNoFile:
      'バッチ #{id} に "{path}" は含まれていません — このバッチのファイル: {files}',
    editHistoryNoEdits2: "このセッションでは編集が記録されていません — /history は空です",
    editHistoryStatusApplied: "適用済み",
    editHistoryStatusPartial: "一部適用",
    editHistoryStatusUndone: "取り消し済み",
    editHistoryHelpShow:
      "/show <id>            \u2192 ファイル別サマリー    \u00b7    /show <id> <path>  \u2192 1ファイルの完全な差分",
    editHistoryHelpUndo:
      "/undo                 \u2192 最新の未取り消し   \u00b7    /undo <id> [path]  \u2192 特定のバッチまたはファイルを対象",
    editHistoryAlreadyReverted: "（既に復元済み — /history でバッチレベルの状態を確認）",
    editHistoryRevertFile: "/undo {id} {path}  \u2192 このファイルのみ復元",
    mcpFailed: "MCP {name} が失敗しました",
    mcpWarn: "MCP {name} 警告",
    unknownTheme: "不明なテーマ: {name}\n利用可能: {choices}",
    themeSaved: "テーマを保存しました: {name}\n次回起動時に有効: {active}",
    noPendingEdits:
      "保留中の編集はありません — 最後の /apply または /discard 以降、モデルは編集を提案していません。",
    noMatchedApply:
      "\u25b8 どの編集もそれらのインデックスに一致しませんでした — 何も適用されていません。引数なしの /apply ですべてコミットしてください。",
    noPendingDiscard: "破棄する保留中の編集はありません。",
    noMatchedDiscard:
      "\u25b8 どの編集もそれらのインデックスに一致しませんでした — 何も破棄されていません。",
    blocksStillPending:
      "\u25b8 {count} 件の編集ブロックがまだ保留中です — /apply または /discard でクリアしてください。",
    nothingWritten: "。ディスクには何も書き込まれませんでした。",
    discardedCount: "\u25b8 {count} 件の保留中編集ブロックを破棄しました",
    noEventsFor: 'セッション "{name}" のイベントはありません',
    lookedAtFile: "参照: {path}",
    sidecarHint:
      "（セッションは最初のターンでサイドカーを自動作成します — このセッションはまだ実行されていませんか？）",
  },
  hooks: {
    head: "フック {tag} `{cmd}` {decision}{truncTag}",
    headWithDetail: "フック {tag} `{cmd}` {decision}{truncTag}: {detail}",
    truncated: "（出力は256KBで切り詰められました）",
    decisionBlock: "ブロック",
    decisionWarn: "警告",
    decisionTimeout: "タイムアウト",
    decisionError: "エラー",
  },
  summary: {
    status: "収集内容をサマリー中…",
    hallucinatedFallback:
      "（モデルが散文サマリーの代わりに偽のツール呼び出しマークアップを出力しました — より狭い質問で /retry するか、/think でR1の推論を確認してください）",
    failedAfterReason:
      "{label} の後、フォールバックサマリー呼び出しが失敗しました: {message}。/clear を実行してより狭い質問で再試行するか、--max-tool-iters を増やしてください。",
  },
  loop: {
    budgetExhausted:
      "セッション予算を使い切りました — 消費 ${spent} ≥ 上限 ${cap}。/budget <usd> で上限を引き上げるか、/budget off でクリアするか、セッションを終了してください。",
    budget80Pct:
      "▲ 予算の80%を使用 — ${spent} / ${cap}。次の1〜2ターンで上限に達する可能性があります。",
    proArmed: "⇧ /pro 武装 — このターンは deepseek-v4-pro で実行（1回限り · ターン後に解除）",
    toolUploadStatus: "ツール結果をアップロードしました · 次の応答前にモデルが思考中…",
    turnStartFoldStatus: "ターン開始: コンテキストが上限に接近中、履歴を圧縮中…",
    turnStartFolded:
      "ターン開始: リクエスト ~{estimate}/{ctxMax} トークン ({pct}%) — {beforeMessages} メッセージ → {afterMessages} に圧縮。送信中。",
    harvestStatus: "推論からプラン状態を抽出中…",
    repeatToolCallWarning:
      "繰り返しツール呼び出しを検出 — モデルに問題を認識させ、別のアプローチで再試行させます。",
    stormStuck:
      "スタックしたリトライループを停止 — モデルが自己修正の促し後も同じツールを同一引数で呼び続けました。/retry を試すか、言い換えるか、根本的なブロッカーを解決してください。",
    stormSuppressed:
      "{count} 件の繰り返しツール呼び出しを抑制 — 同じ名前+引数が3回以上発行されました。",
    compactingHistoryStatus: "履歴を圧縮中{aggressiveTag}…",
    aggressiveTag: "（強制）",
    foldedHistory:
      "コンテキスト {before}/{ctxMax} ({pct}%) — {beforeMessages} メッセージ → {afterMessages} に折りたたみ（サマリー {summaryChars} 文字）。続行します。",
    aggressivelyFoldedHistory:
      "コンテキスト {before}/{ctxMax} ({pct}%) — {beforeMessages} メッセージ → {afterMessages} に強制折りたたみ（サマリー {summaryChars} 文字）。続行します。",
    forcingSummary:
      "コンテキスト {before}/{ctxMax} ({pct}%) — 収集内容からサマリーを強制生成中。/compact、/clear、または /new でリセットしてください。",
  },
  errors: {
    contextOverflow:
      "コンテキストオーバーフロー (DeepSeek 400): セッション履歴が {requested} で、モデルのプロンプト制限（V4: 1Mトークン; 旧来のchat/reasoner: 131k）を超えています。通常、単一のツール結果が大きすぎるのが原因です。Reasonixは新しいツール結果を8kトークンに制限し、セッション読み込み時に大きすぎる履歴を自動修復します — 再起動で解決することが多いです。それでもオーバーフローする場合は、/new で新規開始するか、/sessions を開いて [d] でこのセッションを削除してください。",
    contextOverflowTooMany: "トークンが多すぎます",
    auth401:
      "認証に失敗しました (DeepSeek 401): {inner}。APIキーが拒否されました。`reasonix setup` または `export DEEPSEEK_API_KEY=sk-...` で修正してください。https://platform.deepseek.com/api_keys から取得できます。",
    balance402:
      "残高不足 (DeepSeek 402): {inner}。https://platform.deepseek.com/top_up でチャージしてください — 残高がゼロでなくなるとパネルヘッダーに表示されます。",
    badparam422: "無効なパラメータ (DeepSeek 422): {inner}",
    badrequest400: "不正なリクエスト (DeepSeek 400): {inner}",
    concurrency429:
      "DeepSeekの同時実行制限に達しました (429): {inner}。アカウントの処理中リクエストが多すぎます（上限: v4-proは500、v4-flashは2500、アカウント全体のAPIキー合計）。通常、別のReasonixプロセスが同じキーを共有しているか、並列サブエージェントのファンアウトが超過したことを意味します。数秒待って再試行するか、並列度を下げるか、https://platform.deepseek.com でより高い上限をリクエストしてください。",
    deepseek5xxHead:
      "DeepSeekサービス利用不可 ({status}) — これはDeepSeek側の問題であり、Reasonixの問題ではありません。バックオフ付きで4回再試行済みです。",
    deepseek5xxReachable:
      " DeepSeekのメインAPIはヘルスチェックに応答しましたが、/chat/completions が失敗しています — 部分的な障害が発生しています。",
    deepseek5xxUnreachable:
      " DeepSeek APIにネットワークから到達できません — 広範なDS障害か、ローカルネットワークの問題の可能性があります。",
    deepseek5xxActionNetwork:
      " 対処: (1) ネットワークを確認, (2) 30秒待って再試行, (3) ステータスページ: https://status.deepseek.com",
    deepseek5xxActionRetry:
      " 対処: (1) 30秒待って再試行, (2) /model でモデル切替, (3) ステータスページ: https://status.deepseek.com",
    upstream5xxHead:
      "アップストリームサービス利用不可 ({status}) at {host} — 設定されたAPIエンドポイントがサーバーエラーを返しました。Reasonixのバグではありません。バックオフ付きで4回再試行済みです。",
    upstream5xxActionRetry:
      " 対処: (1) ローカル/プロキシモデルサーバーが起動しているか確認, (2) 待って再試行, (3) /model でモデル切替。",
    innerNoMessage: "(メッセージなし)",
    reasonAborted: "[ユーザーにより中断 (Esc) — ここまでの調査内容をサマリーします]",
    reasonContextGuard:
      "[コンテキスト残量が低下 — 次の呼び出しがオーバーフローする前にサマリーします]",
    reasonStuck:
      "[繰り返しツール呼び出しでスタック — 試みた内容と進行を妨げているものを説明します]",
    labelAborted: "ユーザーにより中断",
    labelContextGuard: "コンテキストガード発動（プロンプトがウィンドウの80%超）",
    labelStuck: "スタック（ストームブレーカーが繰り返しツール呼び出しを抑制）",
  },
  handlers: {
    basic: {
      newInfo:
        "▸ 新しい会話 — コンテキストから {count} 件のメッセージを削除しました。同じセッション、新規の状態です。",
      newInfoArchived:
        '▸ 新しい会話 — コンテキストから {count} 件のメッセージを削除しました。以前のトランスクリプトは "{archived}" としてアーカイブされました（Sessions で表示可能）。',
      newInfoSystemReloaded:
        " · REASONIX.md / プロジェクトメモリを再読み込みしました（次のターンでキャッシュミス1回）",
      helpTitle: "コマンド:",
      helpShellTitle: "シェルショートカット:",
      helpShell: "  !<cmd>                   サンドボックスルートで <cmd> を実行; 出力は",
      helpShellDetail:
        "                             会話に渡され、次のターンでモデルが参照できます。",
      helpShellConsent:
        "                             許可リストは不要 — ユーザー入力 = 明示的同意。",
      helpShellExample: "                             例: !git status   !ls src/   !npm test",
      helpShellGateTitle: "モデルが起動するシェルコマンド（毎回承認）:",
      helpShellGate: "  ↑↓ + ⏎                   各呼び出しで `allow once` / `allow always`",
      helpShellGateDetail:
        "                             / `deny` のプロンプトを表示。`allow always` で",
      helpShellGatePolicy:
        "                             そのコマンドプレフィックスをこのプロジェクトで許可。全許可フラグはありません。",
      helpMemoryTitle: "クイックメモリ:",
      helpMemoryPin:
        "  #<note>                  <note> を <project>/REASONIX.md に追記（コミット可能）。",
      helpMemoryPinEx: "                             例: #findByEmail は大文字小文字を区別しない",
      helpMemoryGlobal:
        "  #g <note>                <note> を ~/.reasonix/REASONIX.md に追記（グローバル、コミット不可）。",
      helpMemoryGlobalEx: "                             例: #g npm ではなく pnpm を使う",
      helpMemoryPinBoth:
        "                             どちらも今後の全セッションのプレフィックスにピン留めされます。/memory より高速。",
      helpMemoryEscape:
        "                             `\\#text` でリテラルの `#text` をモデルに送信。",
      helpFileTitle: "ファイル参照（コードモード）:",
      helpFile:
        "  @path/to/file            送信時にファイル内容を [Referenced files] にインライン展開。",
      helpFilePicker:
        "                             `@` を入力するとピッカーが開きます（↑↓ 移動、Tab/Enter 選択）。",
      helpUrlTitle: "URL参照:",
      helpUrl:
        "  @https://example.com     URLを取得し、HTMLを除去して [Referenced URLs] にインライン展開。",
      helpUrlCache:
        "                             同じURLはセッション内で1回のみ取得（インメモリキャッシュ）。",
      helpUrlPunct: "                             文末の句読点（./,/)）は自動的に除去されます。",
      helpSessionsTitle: "セッション（デフォルトで自動有効、名前は 'default'）:",
      helpSessionCustom: "  reasonix chat --session <name>   別の名前付きセッションを使用",
      helpSessionNone: "  reasonix chat --no-session       この実行の永続化を無効化",
      retryNone:
        "再試行するものがありません — このセッションのログにユーザーメッセージがありません。",
      retryInfo: '▸ 再試行中: "{preview}"',
      loopTuiOnly: "/loop は対話型TUIでのみ利用可能です（run/replay では使えません）。",
      loopStopped: "▸ ループを停止しました。",
      loopNoActive: "停止するアクティブなループはありません。",
      loopNoActiveHint:
        "アクティブなループはありません。`/loop <interval> <prompt>` で開始してください（例: /loop 30s npm test）。\n停止条件: /loop stop · Esc · /clear /new · 任意のユーザー入力プロンプト。",
      loopStarted:
        '▸ ループを開始しました — "{prompt}" を {duration} ごとに再送信します。何か入力するか /loop stop でキャンセル。',
      keysNeedsTui: "/keys は TUI コンテキストが必要です（postKeys wired）。",
      aboutHeader: "Reasonix v{version} — キャッシュファーストのDeepSeekコーディングエージェント",
      aboutWebsiteLabel: "ウェブサイト",
      aboutRepoLabel: "GitHub ",
      aboutLicenseLabel: "ライセンス",
      unknownCommand: "不明なコマンド: /{cmd} — {list} の間違いではありませんか？",
      unknownCommandShort: "不明なコマンド: /{cmd}  (/help を試してください)",
    },
    sessions: {
      titleUnavailable: "/title はアクティブな永続化TUIセッションでのみ利用可能です。",
      titleStarted: "▸ セッション名を生成中…",
      titleFailed: "▸ セッションタイトルが失敗しました: {reason}",
    },
    qq: {
      unavailable: "/qq はこのセッションでは利用できません。",
      connecting: "QQ: 接続中…",
      connectFailed: "QQ 接続に失敗しました: {reason}",
      disconnecting: "QQ: 切断中…",
      disconnectFailed: "QQ 切断に失敗しました: {reason}",
      usage: "使い方: /qq connect [appId appSecret [sandbox]] | /qq status | /qq disconnect",
      promptAppId:
        "QQ設定: QQ Open Platform の App ID を入力し、Enter を押してください。/cancel で中断。",
      promptAppSecret:
        "QQ設定: QQ Open Platform の App Secret を入力し、Enter を押してください。/cancel で中断。",
      setupWaitingAppId: "App ID の入力を待機中",
      setupWaitingAppSecret: "App Secret の入力を待機中",
      setupCancelled: "QQ設定をキャンセルしました。",
      credentialsRequired: "QQ App ID と App Secret が必要です。",
      connected: "QQ が {mode} モードで接続されました。今後の起動時に自動起動します。",
      alreadyConnected: "QQ は既に {mode} モードで接続されています。自動起動が有効です。",
      disconnected: "QQ を切断しました。自動起動が無効です。",
      status:
        "QQ: {connected}, 自動起動 {enabled}, 認証情報 {configured}, appId {appId}, {sandbox}, アクセス {access}, 現在のモード {mode}。",
      statusSetup: "QQ: セットアップ中 — {step}",
      stateConnected: "接続済み",
      stateDisconnected: "切断済み",
      stateEnabled: "有効",
      stateDisabled: "無効",
      stateConfigured: "設定済み",
      stateNotConfigured: "未設定",
      sandbox: "サンドボックス",
      production: "本番",
      none: "なし",
      modeChat: "チャット",
      modeCode: "コード",
      accessOwner: "所有者 {owner}",
      accessOwnerWithAllowlist: "所有者 {owner}, 許可リスト {count}",
      accessAllowlist: "許可リスト {count}",
      accessRuntime: "最初の送信者（ランタイムのみ, {owner}）",
      accessOpen: "オープン（制限なし）",
      lockAlreadyRunning:
        "QQチャンネルは既にプロセス {pid} で実行中です。別のQQチャンネルを開始する前にそのプロセスを停止してください。",
      unauthorizedMessage:
        "QQ が未認証の openid {openid} からのメッセージを無視しました。現在のアクセス: {access}。",
      runtimeBound:
        "QQ はこの実行を最初の送信者 {openid} に一時的にバインドしました。アクセスを永続化するには設定で `qq.ownerOpenId` を設定してください。",
      missingAppId: "QQ App ID が必要です。`/qq connect` を実行して設定してください。",
      missingAppSecret: "QQ App Secret が必要です。`/qq connect` を実行して設定してください。",
      authFailed: "QQ ボット認証に失敗しました — App ID と App Secret を確認してください。",
      readyTimeout:
        "QQ ボットが15秒以内に READY を受信しませんでした — App ID と App Secret を確認してください。",
    },
    admin: {
      doctorNeedsTui: "/doctor は TUI コンテキストが必要です（postDoctor wired）。",
      doctorRunning: "⚕ Doctor — ヘルスチェック実行中…",
      hooksReloadUnavailable:
        "/hooks reload はこのコンテキストでは利用できません（reload コールバックが未接続）。",
      hooksReloaded: "▸ フックを再読み込みしました · {count} 個アクティブ",
      hooksUsage:
        "使い方: /hooks            アクティブなフックを一覧\n       /hooks reload     settings.json ファイルを再読込",
      hooksNone: "フックが設定されていません。",
      hooksDropHint: "`hooks` キーを持つ settings.json を以下のいずれかに配置してください:",
      hooksProject: "  · {path}（プロジェクト）",
      hooksProjectFallback: "  · <project>/.reasonix/settings.json（プロジェクト）",
      hooksGlobal: "  · {path}（グローバル）",
      hooksEvents: "イベント: PreToolUse, PostToolUse, UserPromptSubmit, Stop",
      hooksExitCodes: "exit 0 = 通過 · exit 2 = ブロック (Pre*) · その他 = 警告",
      hooksLoaded: "▸ {count} 個のフックを読み込みました",
      hooksSources: "ソース: project={project} · global={global}",
      updateCurrent: "現在: reasonix {version}",
      updateLatestPending: "最新:  （未解決 — バックグラウンドチェック中またはオフライン）",
      updateRetryHint:
        "新しいレジストリ取得をトリガーしました — 数秒後に `/update` を再試行してください。",
      updateRetryHint2:
        "または別の端末で `reasonix update` を実行して同期的に強制取得してください。",
      updateLatest: "最新:  reasonix {version}",
      updateUpToDate: "最新バージョンです。何もする必要はありません。",
      updateNpxHint: "npx 経由で実行中です — 次回の `npx reasonix ...` 起動時に自動取得されます。",
      updateNpxForce: "より早く強制リフレッシュするには: `npm cache clean --force`。",
      updateUpgradeHint: "アップグレードするには、このセッションを終了して以下を実行してください:",
      updateUpgradeCmd1: "  reasonix update           （対話的、--dry-run でドライラン可能）",
      updateUpgradeCmd2: "  {command}   （直接）",
      updateInSessionDisabled:
        "セッション内インストールは意図的に無効化されています — インストールのスポーンにより",
      updateInSessionDisabled2:
        "このTUIのレンダリングが破損し、Windowsでは実行中バイナリがロックされる可能性があります。",
      statsNoData: "まだ使用データがありません。",
      statsEveryTurn: "ここでの毎ターン実行が1レコードを追加します — このセッションのターンは",
      statsWillAppear: "メッセージを送信するとダッシュボードに表示されます。",
    },
    edits: {
      undoCodeOnly:
        "/undo は `reasonix code` 内でのみ利用可能です — チャットモードでは編集を適用しません。",
      historyCodeOnly: "/history は `reasonix code` 内でのみ利用可能です。",
      showCodeOnly: "/show は `reasonix code` 内でのみ利用可能です。",
      applyCodeOnly:
        "/apply は `reasonix code` 内でのみ利用可能です（ここで適用するものはありません）。",
      discardCodeOnly: "/discard は `reasonix code` 内でのみ利用可能です。",
      planCodeOnly:
        "/plan は `reasonix code` 内でのみ利用可能です — チャットモードではツール書き込みを制御しません。",
      planOn:
        "▸ プランモード ON — 書き込みツールが制御されます; モデルは実行前に `submit_plan` を呼び出す必要があります。（プランモードがオフでも、大きなタスクではモデルが自主的に submit_plan を呼び出せます — このトグルはより強い明示的な制約です。）/plan off で終了。",
      planOff:
        "▸ プランモード OFF — 書き込みツールが再有効化されました。大きなタスクではモデルが自主的にプランを提案できます。",
      modeCodeOnly: "/mode は `reasonix code` 内でのみ利用可能です。",
      modeUsage: "使い方: /mode <review|auto|yolo>   （Shift+Tab でも切り替え可能）",
      modeYolo:
        "▸ 編集モード: YOLO — 編集とシェルコマンドがプロンプトなしで自動実行されます。/undo で編集のロールバックは可能です。注意して使用してください。",
      modeAuto:
        "▸ 編集モード: AUTO — 編集が即時適用されます; 5秒以内に u を押すか、後で /undo で元に戻せます。シェルコマンドは確認あり。",
      modeReview:
        "▸ 編集モード: review — 編集は /apply（または y）/ /discard（または n）のキューに入ります",
      commitCodeOnly:
        "/commit は `reasonix code` 内でのみ利用可能です（git リポジトリが必要です）。",
      commitUsage:
        '使い方: /commit "コミットメッセージ"  — {root} で `git add -A && git commit -m "…"` を実行します',
      walkCodeOnly: "/walk は `reasonix code` 内でのみ利用可能です。",
      checkpointCodeOnly:
        "/checkpoint は `reasonix code` 内でのみ利用可能です — チャットモードでは編集を適用しません。",
      checkpointNone:
        "まだチェックポイントはありません — `/checkpoint <name>` でセッションが触れた全ファイルのスナップショットを保存します。後で `/restore <name>` で復元できます。",
      checkpointHeader: "◈ チェックポイント · {count} 件保存",
      checkpointRestoreHint:
        "  /restore <name|id> · /checkpoint forget <id> · /checkpoint <name> で追加",
      checkpointForgetUsage: "使い方: /checkpoint forget <id|name>",
      checkpointNoMatch:
        '▸ "{name}" に一致するチェックポイントはありません — /checkpoint list を参照',
      checkpointDeleted: "▸ チェックポイント {id} ({name}) を削除しました",
      checkpointDeleteFailed: "▸ {id} の削除に失敗しました（既に存在しませんか？）",
      checkpointSaveUsage: "使い方: /checkpoint <name>   （または /checkpoint list で既存を表示）",
      checkpointSavedEmpty:
        '▸ チェックポイント "{name}" を保存しました ({id}) — しかし、まだファイルが触られていないため空のベースラインです。この後の編集は復元可能になります。',
      checkpointSaved:
        '▸ チェックポイント "{name}" を保存しました ({id}) — {files} ファイル, {size} KB。復元: /restore {name}',
      restoreCodeOnly: "/restore は `reasonix code` 内でのみ利用可能です。",
      restoreUsage: "使い方: /restore <name|id>   （/checkpoint list でIDを確認）",
      restoreNoMatch:
        '▸ "{target}" に一致するチェックポイントはありません — /checkpoint list を試してください',
      restoreInfo: '▸ "{name}" ({id}) を {when} から復元しました',
      restoreWrote: "  · {count} ファイルを書き戻しました",
      restoreRemoved:
        "  · {count} ファイルを削除しました（チェックポイント時点では存在しませんでした）",
      restoreSkipped: "  ✗ {count} ファイルをスキップしました:",
      cwdCodeOnly: "/cwd は `reasonix code` 内でのみ利用可能です。",
      cwdUsage:
        "使い方: /cwd <path>   （現在のルート: {current}）。ファイルシステム / シェル / メモリツールの参照先を <path> に変更します。",
      cwdUsageNoCurrent: "使い方: /cwd <path>   ワークスペースルートを <path> に変更します。",
    },
    model: {
      modelHint:
        "deepseek-v4-flash または deepseek-v4-pro を試してください — /models で最新リストを取得",
      modelUsage: "使い方: /model <id>   ({hint})",
      modelNotInCatalog:
        "model → {id}   （⚠ 取得済みカタログにありません: {list}。間違いの場合、次の呼び出しが400になります — /models で更新してください。）",
      modelSet: "model → {id}",
      effortStatus: "effort → {current}   （選択肢: {list}）",
      effortUsage: "使い方: /effort <{list}>   （high が安全なデフォルト; max はDeepSeek拡張）",
      effortUsageNoMax: "使い方: /effort <{list}>",
      effortSet: "effort → {effort}",
      budgetNoCap:
        "セッション予算が設定されていません — 停止するまで続行します。設定: /budget <usd>   （例: /budget 5）",
      budgetStatus: "budget: ${spent} / ${cap} ({pct}%) · /budget off で解除、/budget <usd> で変更",
      budgetOff: "budget → off（上限なし）",
      budgetUsage:
        '使い方: /budget <usd>   （"{arg}" が指定されました — 正の数値である必要があります。例: /budget 5 または /budget 12.50）',
      budgetExhausted:
        "▲ budget → ${cap} ですが既に ${spent} 消費しています。次のターンは拒否されます — 上限を上げるか、セッションを終了してください。",
      budgetSet:
        "budget → ${cap}  （現在まで: ${spent} · 80%で警告、100%で次のターンを拒否 · /budget off で解除）",
    },
    permissions: {
      mutateCodeOnly:
        "/permissions add / remove / clear は `reasonix code` 内でのみ利用可能です — プロジェクトスコープの許可リストを編集します（`~/.reasonix/config.json` projects[<root>].shellAllowed）。",
      addUsage:
        '使い方: /permissions add <prefix>   （複数トークン可: /permissions add "git push origin"）',
      addAlready: "▸ 既に許可済み: {prefix}",
      addBuiltin:
        "▸ `{prefix}` は既にビルトイン許可リストに含まれています — プロジェクト固有のエントリは不要です。（ビルトインエントリは常に有効です。）",
      addInfo:
        "▸ 追加しました: {prefix}\n  → 次回の `{prefix}` 呼び出しはこのプロジェクトでプロンプトなしで実行されます。",
      removeUsage:
        "使い方: /permissions remove <prefix-or-index>   （例: /permissions remove 3、または /permissions remove npm）",
      removeEmpty: "▸ 削除するプロジェクト許可リストエントリはありません。",
      removeIndexOob:
        "▸ インデックスが範囲外です: {idx}（プロジェクトリストには {count} エントリあります）",
      removeNothing: "▸ 削除するものがありません。",
      removeBuiltin:
        "▸ `{prefix}` はビルトイン許可リストに含まれています（読み取り専用）。ビルトインエントリは実行時に削除できません — バイナリに組み込まれています。",
      removeInfo: "▸ 削除しました: {prefix}",
      removeNotFound:
        "▸ そのようなプロジェクトエントリはありません: {prefix}   （/permissions list で保存内容を確認してください）",
      clearAlready: "▸ プロジェクト許可リストは既に空です。",
      clearConfirm:
        "{root} のプロジェクト許可リスト {count} エントリを削除しようとしています。'confirm' を付けて再実行すると続行します: /permissions clear confirm",
      clearedNone: "▸ プロジェクト許可リストは既に空でした — 変更はありません。",
      cleared: "▸ プロジェクト許可リスト {count} エントリをクリアしました。",
      usage:
        '使い方: /permissions [list]                   現在の状態を表示\n       /permissions add <prefix>            永続化（例: "npm run build"）\n       /permissions remove <prefix-or-N>    1エントリ削除\n       /permissions clear confirm           全プロジェクトエントリを消去',
      modeYolo:
        "▸ 編集モード: YOLO — すべてのシェルコマンドが自動実行され、許可リストはバイパスされます。/mode review でプロンプトを再有効化。",
      modeAuto:
        "▸ 編集モード: auto — 編集は自動適用、シェルは許可リスト（または未許可の場合はShellConfirmプロンプト）で制御。",
      modeReview:
        "▸ 編集モード: review — 編集と未許可シェルコマンドの両方が実行前に確認を求めます。",
      projectHeader: "プロジェクト許可リスト ({count}) — {root}",
      projectNone1: '  （なし — ShellConfirmプロンプトで "always allow" を選ぶか、',
      projectNone2: "   `/permissions add <prefix>` で直接追加してください。）",
      projectNoRoot:
        "プロジェクト許可リスト — （プロジェクトルートなし; チャットモードではビルトインエントリのみ表示）",
      builtinHeader: "ビルトイン許可リスト ({count}) — 読み取り専用、組み込み",
      subcommands:
        "サブコマンド: /permissions add <prefix> · /permissions remove <prefix-or-N> · /permissions clear confirm",
    },
    dashboard: {
      notAvailable:
        "/dashboard はこのコンテキストでは利用できません（startDashboard コールバックが未接続）。",
      stopNoCallback: "/dashboard stop: stop コールバックが未接続です。",
      notRunning: "▸ ダッシュボードは実行されていません。",
      stopping: "▸ ダッシュボードを停止中…",
      alreadyRunning: "▸ ダッシュボードは既に実行中です:",
      alreadyRunningHint: "任意のブラウザで開けます。`/dashboard stop` で停止します。",
      ready: "▸ ダッシュボード準備完了:",
      readyHint: "127.0.0.1 のみ · トークン認証。`/dashboard stop` でシャットダウン。",
      failed: "▸ ダッシュボードの起動に失敗しました: {reason}",
      starting: "▸ ダッシュボードサーバーを起動中…",
      copied: "▸ ダッシュボードURLをクリップボードにコピーしました: {url}",
      tokenResetting: "▸ ダッシュボードトークンを再生成中 — サーバーを再起動中…",
      tokenReset: "▸ ダッシュボードトークンを再生成しました。新しいURL:",
    },
    observability: {
      contextInfo: "context: ~{total} / {max} ({pct}%) · system {sys} · tools {tools} · log {log}",
      compactStarting: "▸ 古いターンをサマリーに折りたたみ中…",
      compactNoop:
        "▸ 折りたたむものがありません — ログが既に小さいか、最近のターンのみで予算を超えています。",
      compactDone:
        "▸ {before} メッセージ → {after} に折りたたみ（サマリー {chars} 文字）。続行します。",
      compactFailed: "▸ 折りたたみに失敗しました: {reason}",
      costNoTurn:
        "まだターンがありません — `/cost` は最新ターンのトークンと消費の内訳を表示します。",
      costNeedsTui: "/cost は TUI コンテキストが必要です（postUsage wired）。",
      costNoPricing:
        '▸ /cost: モデル "{model}" の価格テーブルがありません。telemetry/stats.ts に追加してください。',
      costEstimate:
        "▸ /cost 見積もり · {model} · {prompt} プロンプトトークン（sys {sys} + tools {tools} + log {log} + msg {msg}）",
      costWorstCase:
        "  最悪ケース（完全ミス）: {input} 入力 + ~{output} 出力（{avg} 平均）≈ {total}",
      costLikely:
        "  想定（{pct}% セッションキャッシュヒット）: {input} 入力 + ~{output} 出力 ≈ {total}",
      costLikelyCold:
        "  想定: キャッシュが溜まるまで最悪ケースと同じ（まだ完了したターンがありません）",
      statusModel: "  model   {model}",
      statusFlags: "  flags   stream={stream} · effort={effort}",
      statusCtx: "  ctx     {bar} {used}/{max} ({pct}%)",
      statusCtxNone: "  ctx     まだターンがありません",
      statusCost: "  cost    ${cost} · cache {bar} {pct}% · turns {turns}",
      statusCostCold: "  cost    ${cost} · turns {turns}（キャッシュ準備中）",
      statusBudget: "  budget  ${spent} / ${cap} ({pct}%){tag}",
      statusSession: '  session "{name}" · {count} メッセージ（再開 {resumed}）',
      statusSessionEphemeral: "  session（エフェメラル — 永続化なし）",
      statusWorkspace: "  workspace {path} · 起動時に固定（--dir <path> で再起動すると切り替え）",
      statusMcp: "  mcp     {servers} サーバー, {tools} ツール登録済み",
      statusEdits: "  edits   {count} 件保留（/apply でコミット、/discard で破棄）",
      statusPlan: "  plan    ON — 書き込み制御中（submit_plan + 承認）",
      statusLifecycle: "  lifecycle {mode}/{state} · {progress}{evidence}",
      lifecycleNoPlan: "プランなし",
      lifecycleEvidencePending: "エビデンス保留中",
      lifecycleRejected: "lifecycle: {tool} が {state} でブロックされました — 次: {next}",
      lifecycleEvidenceRejected: "lifecycle: ステップ {stepId} にエビデンスが必要 — 次: {next}",
      lifecycleRepeatedRejected:
        "lifecycle: {tool} の繰り返し拒否 — 同一引数で再試行しないでください",
      statusModeYolo:
        "  mode    YOLO — 編集+シェルがプロンプトなしで自動実行（/undo でロールバック · Shift+Tab で切替）",
      statusModeAuto: "  mode    AUTO — 編集が即時適用（5秒以内に u で取消 · Shift+Tab で切替）",
      statusModeReview: "  mode    review — 編集は /apply または y のキュー（Shift+Tab で切替）",
      statusDash: "  dash    {url}（ブラウザで開く · /dashboard stop）",
    },
    plans: {
      noSession:
        "セッションが接続されていません — `/plans` はセッションごとです。プロジェクトで `reasonix code` を実行するとセッションが取得できます。",
      activePlan: "▸ アクティブプラン{label} — {done}/{total} ステップ完了 · 最終更新 {when}",
      activeNone: "▸ アクティブプラン: （なし）",
      noArchives:
        "このセッションにはまだアーカイブ済みプランがありません — 全ステップ完了時に自動アーカイブされます",
      archivedHeader: "アーカイブ ({count}):",
      evidencePending:
        "  ! エビデンス保留中 — 現在のステップに検証/差分/チェックポイント/手動エビデンスが必要です",
      evidenceLine: "  エビデンス {stepId}: {summary}",
      archivedEvidenceLine: "    エビデンス: {summary}",
      replayNoSession:
        "セッションが接続されていません — `/replay` はセッションごとです。プロジェクトで `reasonix code` を実行するとセッションが取得できます。",
      replayNoArchives:
        "このセッションにはまだアーカイブ済みプランがありません — プランが完了すると `/replay` が使えるようになります（全ステップ完了時に自動アーカイブ）。",
      replayInvalidIndex:
        "無効なインデックス — `/replay` は 1..{max} を取ります（最新 = 1）。`/plans` で一覧を確認してください。",
      archivedRow: "  ✓ {when}  {total} ステップ · {completion}  {label}",
      completionComplete: "完了",
      stopAborted:
        "▸ プランが停止しました — モデルが中断されました; フォローアップを入力して続行するか、新しいタスクを開始してください。",
      doneUsage:
        "使い方: /plans done <stepId>  ·  /plans done all — モデルが mark_step_complete の呼び出しを忘れた場合の手動上書き",
      doneUnavailable: "/plans done はアクティブなセッション内でのみ利用可能です。",
      doneNoPlan: "アクティブなプランがありません — 完了とマークするものがありません。",
      doneNotInPlan:
        "ステップ `{id}` はアクティブプランにありません。/plans でステップIDを確認してください。",
      doneAlready: "ステップ `{id}` は既に完了とマークされています。",
      doneOk: "▸ ステップ `{id}` を完了とマークしました。",
      doneAllNoop: "すべてのステップが既に完了しています。",
      doneAllOk: "▸ {count} ステップを完了とマークしました。",
    },
    jobs: {
      codeOnly: "/jobs は `reasonix code` 内でのみ利用可能です。",
      killCodeOnly: "/kill は `reasonix code` 内でのみ利用可能です。",
      logsCodeOnly: "/logs は `reasonix code` 内でのみ利用可能です。",
      empty:
        "◈ jobs · 0 実行中 · 0 合計\n  （run_background がスポーンします — 開発サーバー、ウォッチャー、長時間スクリプト）",
      header: "◈ jobs · {running} 実行中 · {total} 合計",
      footer: "  /logs <id> tail · /kill <id> SIGTERM → SIGKILL",
      killUsage: "使い方: /kill <id>   （/jobs でIDを確認）",
      killNotFound: "job {id}: 見つかりません",
      killAlreadyExited: "job {id} は既に終了しています（{code}）",
      killStopping:
        "▸ job {id} を停止中（ツリーキル: SIGTERM → 2秒猶予後にSIGKILL; Windows: taskkill /T /F）",
      killStatus: "▸ job {id} {status}",
      killStillAlive: "SIGKILL後も生存中 (!) — バグとして報告してください",
      logsUsage: "使い方: /logs <id> [lines]   （デフォルト: 最後の80行）",
      logsNotFound: "job {id}: 見つかりません",
      logsStatus: "[job {id} · {status}]\n$ {command}",
      logsRunning: "実行中 · pid {pid}",
      logsExited: "終了 {code}",
      logsFailed: "失敗（{reason}）",
      logsStopped: "停止済み",
    },
    memory: {
      disabled:
        "メモリが無効です（REASONIX_MEMORY=off が環境変数に設定されています）。変数を解除して再有効化してください — その間、REASONIX.md や ~/.reasonix/memory の内容はピン留めされません。",
      noRoot:
        "このセッションには作業ディレクトリがありません — `/memory` は REASONIX.md を解決するためのルートが必要です。（テストハーネスで実行中ですか？）",
      listEmpty:
        "まだユーザーメモリがありません。モデルが `remember` を呼び出して保存するか、~/.reasonix/memory/global/ またはプロジェクトごとのサブディレクトリに手動でファイルを作成できます。",
      listHeader: "ユーザーメモリ ({count}):",
      listFooter: "本文表示: /memory show <name>   削除: /memory forget <name>",
      showUsage: "使い方: /memory show <name>  または  /memory show <scope>/<name>",
      showNotFound: "メモリが見つかりません: {target}",
      showFailed: "表示に失敗しました: {reason}",
      forgetUsage: "使い方: /memory forget <name>  または  /memory forget <scope>/<name>",
      forgetNotFound: "メモリが見つかりません: {target}",
      forgetInfo: "▸ {scope}/{name} を削除しました。次回の /new または起動時には表示されません。",
      forgetFailed: "{scope}/{name} を削除できませんでした（既に存在しませんか？）",
      forgetError: "削除に失敗しました: {reason}",
      clearUsage: "使い方: /memory clear <global|project> confirm",
      clearConfirm:
        "scope={scope} のすべてのメモリを削除しようとしています。'confirm' を付けて再実行すると続行します: /memory clear {scope} confirm",
      cleared: "▸ scope={scope} をクリアしました — {count} メモリファイルを削除しました。",
      noMemory: "{root} にピン留めされたメモリはありません。",
      layers: "3つのレイヤーが利用可能です:",
      layerProject: "  1. {file} — コミット可能なチームメモリ（リポジトリ内）。",
      layerGlobal: "  2. ~/.reasonix/memory/global/ — プロジェクト横断のプライベートメモリ。",
      layerProjectHash:
        "  3. ~/.reasonix/memory/<project-hash>/ — このプロジェクトのプライベートメモリ。",
      askModel: "モデルに `remember` を依頼するか、手動でファイルを編集してください。",
      changesNote:
        "変更は次回の /new または起動時に反映されます — プレフィックスキャッシュを保温するため、システムプロンプトはセッションごとに1回ハッシュ化されます。",
      subcommands:
        "サブコマンド: /memory list | /memory show <name> | /memory forget <name> | /memory clear <scope> confirm",
      changesNoteShort:
        "変更は次回の /new または起動時に反映されます。サブコマンド: /memory list | show | forget | clear",
    },
    mcp: {
      noServers:
        'MCPサーバーが接続されていません。`reasonix setup` を実行して選択するか、--mcp "<spec>" で起動してください。`reasonix mcp list` でカタログを表示します。注意: モデル起動のシェルコマンドは呼び出しごとに制御されます（allow once / allow always / deny）— 全許可フラグはありません。',
      toolsLabel: "  tools     {count}",
      resourcesHint: "`/resource` で閲覧+読取",
      promptsHint: "`/prompt` で閲覧+取得",
      awarenessOnly:
        "チャットモードは現在ツールのみ利用; リソースとプロンプトは参照用にここに表示されています。",
      catalogHint:
        "完全なカタログ: `reasonix mcp list` · 詳細診断: `reasonix mcp inspect <spec>`。",
      fallbackServers: "MCPサーバー ({count}):",
      fallbackTools: "登録済みツール ({count}):",
      fallbackChange: "このセットを変更するには、終了して `reasonix setup` を実行してください。",
      usageDisableEnable:
        "使い方: /mcp {action} <name>  ·  /mcp に表示される名前を選択（匿名サーバーは名前で切り替え不可）。",
      usageReconnect: "使い方: /mcp reconnect <name>  ·  /mcp に表示される名前を選択。",
      unknownServer: '不明なMCPサーバー "{name}"。既知: {list}。',
      noneList: "（なし）",
      reconnectNoTui: "/mcp reconnect は対話型TUIが必要です（postInfo が未接続）。",
      liveTab: "Live",
      marketplaceTab: "Marketplace",
      tabHint: "tab で切り替え",
    },
    init: {
      codeOnly:
        "/init はコードモードでのみ動作します（ファイルシステムツールが必要です）。\n初期化したいプロジェクトをルートとして `reasonix code [path]` でセッションを開始し、/init を実行してください。",
      exists: "▸ REASONIX.md は既に {path} に存在します",
      existsForce: "  /init force   ゼロから再生成（上書き）",
      existsEdit: "  または手動で編集してください — 単なるmarkdownです。現在のファイルは",
      existsPinned: "  起動のたびにシステムプロンプトにそのままピン留めされます。",
      info: "▸ /init — モデルがプロジェクトをスキャンして REASONIX.md を生成します。\n  結果は保留中の編集として届きます; /apply または /walk でレビューしてください。",
    },
    webSearchEngine: {
      currentEngine: "現在のWeb検索エンジン: {engine}",
      endpoint: "SearXNG エンドポイント: {url}",
      usageHeader: "使い方:",
      usageBing:
        "  /search-engine bing              Bingを使用（デフォルト、プロキシなしで中国から利用可）",
      usageSearxng: "  /search-engine searxng            デフォルトエンドポイントでSearXNGを使用",
      usageSearxngUrl: "  /search-engine searxng <url>      カスタムエンドポイントでSearXNGを使用",
      usageMetaso:
        "  /search-engine metaso              Metaso APIを使用（100回/日無料、独自APIキーで上限増）",
      usageTavily:
        "  /search-engine tavily              Tavily APIを使用（LLM向け、無料1000回/月 — TAVILY_API_KEY または tavilyApiKey を設定; https://tavily.com で取得）",
      usagePerplexity:
        "  /search-engine perplexity          Perplexity AIを使用（AIネイティブ回答+引用 — PERPLEXITY_API_KEY または perplexityApiKey を設定; https://perplexity.ai/settings/api で取得）",
      usageExa:
        "  /search-engine exa                 Exa APIを使用（AIネイティブ回答+引用、無料1000回/月 — EXA_API_KEY または exaApiKey を設定; https://exa.ai で登録）",
      alias: "エイリアス: /se",
      searxngInfo:
        "SearXNG はセルフホストのメタサーチエンジンです（https://github.com/searxng/searxng）。",
      searxngInstall: "インストール:  docker run -d -p 8080:8080 searxng/searxng",
      switched: 'Web検索エンジンを "{engine}" に切り替えました。{note}',
      switchedSearxngNote: " SearXNG が {endpoint} で実行されていることを確認してください。",
      switchedMetasoNote: " 1日100回のクォータがあります（独自APIキーでより高い上限に設定可能）。",
      switchedTavilyNote:
        " TAVILY_API_KEY または `tavilyApiKey` を設定してください; 無料1000回/月 https://tavily.com。",
      switchedPerplexityNote:
        " PERPLEXITY_API_KEY または `perplexityApiKey` を設定してください; https://perplexity.ai/settings/api で取得。",
      switchedExaNote:
        " EXA_API_KEY または `exaApiKey` を設定してください; https://exa.ai で登録。",
      keyNeeded:
        '"{engine}" のAPIキーが設定されていません。\n\n  1. {envVar} 環境変数を設定\n  2. またはインラインで提供:  /search-engine {engine} <your-key>\n  3. または "{engine}ApiKey" を ~/.reasonix/config.json に追加\n\nその後 /search-engine {engine} を再試行してください。',
      keySaved: " APIキーを設定に保存しました。",
      confirmed:
        'Web検索エンジンを "{engine}"{detail} に設定しました。次のアシスタントターンで反映されます。',
      confirmedDetail: " ({endpoint})",
    },
    skill: {
      listEmpty: "スキルが見つかりません。Reasonix は以下からスキルを読み取ります:",
      listProjectScope:
        "  · <project>/.reasonix/skills/<name>/SKILL.md  （または <name>.md） — プロジェクトスコープ",
      listGlobalScope:
        "  · ~/.reasonix/skills/<name>/SKILL.md  （または <name>.md） — グローバルスコープ",
      listProjectOnly: "  （プロジェクトスコープは `reasonix code` 内でのみアクティブ）",
      listFrontmatter:
        "各ファイルのフロントマターには少なくとも `name` と `description` が必要です。",
      listInvoke:
        "スキルは `/skill <name> [args]` で呼び出すか、モデルに `run_skill` の呼び出しを依頼してください。",
      listHeader: "ユーザースキル ({count}):",
      listFooter: "表示: /skill show <name>   実行: /skill <name> [args]   新規: /skill new <name>",
      listEmptyNewHint:
        "/skill new <name> で雛形を作成（プロジェクトスコープ）— リモートレジストリはまだありません; スキルは直接作成します。",
      showUsage: "使い方: /skill show <name>",
      showNotFound: "スキルが見つかりません: {name}",
      runNotFound: "スキルが見つかりません: {name}  （/skill list を試してください）",
      runInfo: "▸ スキル実行中: {name}{args}",
      newUsage: "使い方: /skill new <name> [--global]",
      newCreated:
        "▸ スキルを作成しました: {name}\n  {path}\n  編集後、`/skill {name}` で呼び出せます",
      newError: "▲ /skill new に失敗しました: {reason}",
      pathsHeader: "スキルパス（優先順）:",
      pathsPriority:
        "優先順: プロジェクト > 設定のカスタムパス順 > グローバル > ビルトイン。変更は次回 /new または新規セッションでシステムプロンプトに反映されます。",
      pathsUsage:
        "使い方: /skill paths [list]\n       /skill paths add <path>\n       /skill paths remove <path|N>",
      pathsAddUsage: "使い方: /skill paths add <path>",
      pathsRemoveUsage: "使い方: /skill paths remove <path|N>",
      pathsAdded: "▸ カスタムスキルパスを追加しました: {path}",
      pathsAlready: "▸ カスタムスキルパスは既に設定済みです: {path}",
      pathsRemoved: "▸ カスタムスキルパスを削除しました: {path}",
      pathsRemoveNotFound: "▸ 一致するカスタムスキルパスはありません: {target}",
      pathsRestartHint:
        "現在のセッションのシステムプロンプトは変更されていません; /new を実行するか新規セッションを開始してスキルインデックスを更新してください。",
    },
  },
  statusBar: {
    turn: "ターン",
    cache: "キャッシュ",
    spent: "消費",
    left: " 残",
    slow: "低速",
    disconnect: "切断",
    reconnecting: "再接続中\u2026",
    approvingIn: "承認待ち ",
    escToInterrupt: "秒 \u00b7 escで中断",
    recordingGlyph: "\u25CFREC",
    mb: " MB",
    evt: " evt",
    editsLabel: "編集:",
    mcpLoading: "MCP",
    ctx: "ctx",
    shortcutsHint: "Ctrl+P ショートカット",
  },
  editMode: {
    plan: "PLAN MODE",
    yolo: "YOLO",
    auto: "AUTO",
    review: "REVIEW",
    writesGated: "   書き込み制御中 \u00b7 /plan off で解除",
    editsShellAuto: "編集+シェル自動 \u00b7 /undo でロールバック",
    editsLandNow: "編集即時適用 \u00b7 u で取消",
    queuedApplyDiscard: "{count} 件キュー \u00b7 y 適用 \u00b7 n 破棄",
    editsQueued: "編集中 \u00b7 y 適用 \u00b7 n 破棄",
    shiftTabFlip: "   {mid} \u00b7 Shift+Tab で切替",
    queuedDots: "キュー中\u2026",
  },
  composer: {
    placeholder:
      "何でも聞いてください  \u00b7  スラッシュでコマンド  \u00b7  アットマークでファイル",
    waitingForResponse: "\u2026応答待機中\u2026",
    hintSend: "送信",
    hintNewline: "改行",
    hintClear: "クリア",
    hintScroll: "スクロール",
    hintHistory: "履歴",
    hintAbort: "中断",
    hintQuit: "終了",
    abortedHint:
      "ユーザーによりターン中断 \u00b7 もう一度escでクリア \u00b7 \u23ce でフォローアップ",
    editorNoRawMode:
      "外部エディタが利用できません \u2014 この端末ではstdinのraw-mode切替がサポートされていません",
    editorFailed: "外部エディタ:",
    editorMissing:
      "$EDITOR / $VISUAL / $GIT_EDITOR が設定されていません \u2014 いずれかをエクスポートして（例: `export EDITOR=nano`）再試行してください",
    editorExited: "エディタがコード {code} で終了しました",
    typeaheadStaged: "\u25b8 {count} 行をステージング \u00b7 esc で呼び戻し",
    steerPlaceholder: "現在のタスクを操作するために入力 — ビジー中はコマンド無効",
    steerHint: "送信 — ターン中に注入",
    stashNothing: "スタッシュするものはありません",
    stashSaved: "スタッシュしました",
    stashRecall: "呼び戻しました",
  },
  pathConfirm: {
    title: "サンドボックス外のパス",
    subtitleRead: "{tool} がプロジェクトサンドボックス外のファイルを読み取ろうとしています",
    subtitleWrite: "{tool} がプロジェクトサンドボックス外のファイルに書き込もうとしています",
    awaiting: "待機中",
    denyTitle: "拒否 \u2014 コンテキストを入力",
    optional: "任意",
    denyFooter:
      "コンテキストを入力  \u00b7  \u23ce 理由を添えて送信  \u00b7  esc スキップ（理由なしで拒否）",
    pickFooter:
      "\u2191\u2193 選択  \u00b7  \u23ce 確定  \u00b7  Tab コンテキスト追加  \u00b7  esc キャンセル",
    allowOnce: "一度だけ許可",
    allowOnceDesc: "このアクセスを許可; このセッションの残りでディレクトリを記憶",
    allowAlways: "常に許可",
    allowAlwaysDesc: "`{prefix}` をこのプロジェクトで記憶（~/.reasonix/config.json に永続化）",
    deny: "拒否",
    denyDesc: "Tab を押してモデルに理由を伝えるコンテキストを追加",
    pathLabel: "パス",
    sandboxLabel: "サンドボックス",
    allowPrefixLabel: "プレフィックス",
    promptTitleRead: "パスアクセス \u2014 読み取り",
    promptTitleWrite: "パスアクセス \u2014 書き込み",
    actionAllowRead: "読み取り許可",
    actionAllowWrite: "書き込み許可",
    actionAlwaysAllow: "常に許可 \u2014 {prefix}",
    actionDeny: "拒否",
  },
  shellConfirm: {
    title: "シェルコマンド",
    bgTitle: "バックグラウンドプロセス",
    subtitle: "モデルがシェルコマンドを実行しようとしています",
    bgSubtitle: "長時間実行プロセス \u2014 承認後も実行継続、/kill で停止",
    denyTitle: "拒否 \u2014 コンテキストを入力",
    optional: "任意",
    denyFooter:
      "コンテキストを入力  \u00b7  \u23ce 理由を添えて送信  \u00b7  esc スキップ（理由なしで拒否）",
    awaiting: "待機中",
    pickFooter:
      "\u2191\u2193 選択  \u00b7  \u23ce 確定  \u00b7  Tab コンテキスト追加  \u00b7  esc キャンセル",
    allowOnce: "一度だけ許可",
    allowOnceDesc: "このコマンドを実行、次回は再確認",
    allowAlways: "常に許可",
    allowAlwaysDesc: "`{prefix}` をこのプロジェクトで記憶",
    deny: "拒否",
    denyDesc: "Tab を押してモデルに理由を伝えるコンテキストを追加",
    cwdLabel: "cwd",
    timeoutLabel: "タイムアウト",
    waitLabel: "待機",
    previewMore: "… さらに {n} 行が非表示 — esc を押し、モデルに分割を依頼してください",
    previewMorePlural: "… さらに {n} 行が非表示 — esc を押し、モデルに分割を依頼してください",
    promptTitleRunCommand: "コマンド実行",
    promptTitleRunBackground: "バックグラウンドコマンド実行",
    actionRunOnce: "一度だけ実行",
    actionAlwaysAllow: "常に許可 \u2014 {prefix}",
    actionDeny: "拒否",
  },
  editConfirm: {
    footer:
      "[y/Enter] 適用  \u00b7  [n] 理由を添えて拒否  \u00b7  [a] 残りを適用  \u00b7  [A] AUTOに切替  \u00b7  [\u2191\u2193/Space] スクロール  \u00b7  [Esc] 中断",
    newTag: "新規",
    editTag: "編集",
    linesCount: "-{removed} +{added} 行",
    viewingRange: "{start}-{end}/{total} を表示中",
    denyFooter: "\u23ce 送信  \u00b7  esc スキップ（理由なしで拒否）",
    oldLabel: "  - 旧",
    newLabel: "  + 新",
    sideBySide: "   サイドバイサイド \u00b7 左が削除行、右が追加行 \u00b7 オフセットでペアリング",
    linesAbove: "  \u2191 上に {count} 行  (\u2191/k または PgUp)",
    linesAbovePlural: "  \u2191 上に {count} 行  (\u2191/k または PgUp)",
    linesBelow: "  \u2193 下に {count} 行  (\u2193/j または Space/PgDn)",
    linesBelowPlural: "  \u2193 下に {count} 行  (\u2193/j または Space/PgDn)",
  },
  editPicker: {
    title: "前のメッセージを編集",
    hint: "↑↓ 選択 · Enter で composer に読み込み · Esc でキャンセル",
    empty: "まだユーザーターンがありません — 編集するものがありません",
    dismiss: "Esc で閉じる",
    forked: "▸ ターン #{turn} でフォーク — バッファに元のテキストを保持",
  },
  sessionPicker: {
    header: " \u25c8 REASONIX \u00b7 セッションを選択 ",
    title: "セッションを選択 \u2014 {workspace}",
    messages: "{count} メッセージ",
    messagesPlural: "{count} メッセージ",
    turns: "{count} ターン",
    pickerHint:
      "\u2191\u2193 選択 \u00b7 / 検索 \u00b7 \u23ce 開く \u00b7 [n] 新規 \u00b7 [d] 削除 \u00b7 [r] 名前変更 \u00b7 esc 終了",
    empty: "  このワークスペースに保存済みセッションはまだありません \u2014 ",
    emptyNew: " を押して新規作成",
    renamePrompt: '  "{from}" の名前変更 \u2192 ',
    renameHint: "  \u23ce 名前変更を確定  \u00b7  esc キャンセル",
    searchPrompt: "  セッション検索: /",
    searchHint: "  入力でフィルタ  \u00b7  \u23ce 一致を開く  \u00b7  esc クリア",
    searchEmpty: "  検索に一致するセッションはありません",
    emptyHint: "  \u23ce 新規セッション  \u00b7  esc 終了",
    justNow: "たった今",
    minAgo: "{count} 分前",
    yesterday: "昨日",
    hoursAgo: "{count}時間前",
    daysAgo: "{count} 日前",
  },
  workspacePicker: {
    header: " ◈ REASONIX · ワークスペースを選択 ",
    title: "ワークスペースを選択 — {workspace}",
    sessions: "{count} セッション",
    sessionsPlural: "{count} セッション",
    current: "現在",
    pickerHint: "↑↓ 選択 · / 検索 · ⏎ 切替+セッション選択 · esc 終了 · /cwd <path> で追加",
    empty: "  既知のワークスペースはまだありません — /cwd <path> を実行して追加してください",
    searchPrompt: "  ワークスペース検索: /",
    searchHint: "  入力でフィルタ  ·  ⏎ 切替+セッション選択  ·  esc クリア",
    searchEmpty: "  検索に一致するワークスペースはありません",
  },
  modelPicker: {
    header: " \u25c8 REASONIX \u00b7 設定を選択 ",
    loading: "  \u00b7  カタログ読み込み中\u2026",
    catalogEmpty: "  \u00b7  カタログが空です \u2014 既知のフォールバックを使用",
    modelsAvailable: "  \u00b7  {count} モデル利用可能",
    effortHeader: "    EFFORT  \u00b7  reasoning_effort 上限",
    modelsHeader: "    MODELS  \u00b7  DeepSeek互換ID",
    effortDesc: {
      low: "最速 \u2014 最小限の推論",
      medium: "バランス",
      high: "デフォルト \u2014 vLLM / Azure で安全",
      max: "DeepSeek拡張; 標準OpenAI / vLLMでは拒否されます",
    },
    pickerFooter:
      "  \u2191\u2193 選択  \u00b7  \u23ce 確定  \u00b7  [r] 更新  \u00b7  esc キャンセル",
    currentLabel: "  \u00b7 現在",
  },
  slashSuggestions: {
    noMatch: "そのプレフィックスに一致するスラッシュコマンドはありません",
    backspaceHint: " \u2014 Backspace で編集、または /help で全リスト",
    commandCount: "{count} コマンド",
    commandCountPlural: "{count} コマンド",
    aboveLabel: "   \u2191 上に {count} 件",
    belowLabel: "   \u2193 下に {count} 件",
    advancedHint: "  + {count} 件の詳細  \u00b7  文字を入力して検索",
    footerHint: "  \u2191\u2193 移動 \u00b7 Tab / \u23ce 選択 \u00b7 esc キャンセル",
    groupChat: "チャット",
    groupSetup: "設定",
    groupInfo: "情報",
    groupSession: "セッション",
    groupExtend: "拡張",
    groupCode: "コード",
    groupJobs: "ジョブ",
    groupAdvanced: "詳細",
    groupDetailSetup: "モデル+コスト",
    groupDetailInfo: "現在の状態",
    groupDetailChat: "日常ターン操作",
    groupDetailExtend: "MCP、メモリ、スキル",
    groupDetailSession: "保存済みセッション",
    groupDetailCode: "編集+プラン（コードモード）",
    groupDetailJobs: "バックグラウンドプロセス（コードモード）",
    groupDetailAdvanced: "高度な設定",
  },
  atMentions: {
    loading: "読み込み中\u2026",
    entrySingular: "{count} エントリ",
    entryPlural: "{count} エントリ",
    searching: "検索中\u2026",
    scanned: "スキャン済み",
    match: "一致",
    matches: "一致",
    forFilter: '"{filter}" の結果',
    noMatch: '"{filter}" に一致するファイルはありません',
    emptyDir: "空のディレクトリ",
    scanning: "ツリーをスキャン中\u2026",
    footerBrowse:
      "\u2191\u2193 移動 \u00b7 Tab フォルダ展開 \u00b7 \u23ce 挿入 \u00b7 esc キャンセル",
    footerBrowseSearch:
      "\u2191\u2193 移動 \u00b7 Tab / \u23ce @path として挿入 \u00b7 esc キャンセル",
    footerInsert: "\u2191\u2193 移動 \u00b7 Tab / \u23ce @path として挿入 \u00b7 esc キャンセル",
  },
  statsPanel: {
    modePlan: "PLAN",
    modeYolo: "yolo",
    modeAuto: "auto",
    modeReview: "review",
    pro: "\u21e7 pro",
    budget: "  予算  ",
  },
  welcomeBanner: {
    workspace: "\u25b8 ワークスペース",
    relaunchHint: "  （--dir <path> で再起動すると切替）",
    dashboard: "\u25b8 Web",
  },
  ctxBreakdown: {
    title: "\u25a3 コンテキスト",
    compactHint: "  /compact で折りたたみ（50%で自動）\u00b7 /new でログ消去",
    topTools: "  コスト上位のツール結果（{count}）:",
    msg: "メッセージ",
    turnLabel: "ターン",
  },
  startup: {
    codeRooted:
      '\u25b8 reasonix code: ルート {rootDir}, セッション "{session}" \u00b7 {tools} ネイティブツール{semantic}',
    ephemeral: "（エフェメラル）",
    semanticOn: " \u00b7 semantic_search オン",
  },
  doctorErrors: {
    unreadable: "{path} 読み取り不可 \u2014 {message}",
    cannotList: "一覧表示できません \u2014 {message}",
    parseFailed: "settings.json をパースできませんでした \u2014 {message}",
    probeFailed: "プローブに失敗しました \u2014 {message}",
  },
  webErrors: {
    status:
      "web_search {status} \u2014 対処: 検索バックエンドがエラーを返しました。クエリを言い換えるか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    rateLimit429:
      "web_search 429 \u2014 対処: 10秒待ってから再試行するか、クエリを言い換えてください。検索バックエンドがこのクライアントをレート制限しています",
    forbidden403:
      "web_search 403 \u2014 対処: 検索バックエンドがこのクライアントをブロックしています。/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えるか、しばらく待って再試行してください",
    serverError5xx:
      "web_search {status} \u2014 対処: 検索URLをブラウザで開いてみてください。読み込める場合は一時的な問題で、30秒後に再試行すると改善する可能性があります",
    bingBlocked:
      "web_search: Bingのアンチボットページ \u2014 レート制限またはブロックされました \u2014 対処: 30秒待って再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    bingNoResults:
      "web_search: 0件の結果ですが、レスポンスが実際の空ページとは異なります（{chars}文字、先頭120: {preview}）\u2014 対処: より簡単な用語でクエリを言い換えるか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    invalidEndpoint:
      'web_search: 無効なSearXNGエンドポイント "{endpoint}" \u2014 対処: /search-endpoint http://host:port で有効なURLを設定してください',
    endpointMustBeHttp:
      "web_search: SearXNGエンドポイントはhttp(s)である必要がありますが、{protocol} が指定されました \u2014 対処: /search-endpoint http://host:port で有効なURLを設定してください",
    cannotReach:
      "web_search: {endpoint} のSearXNGサーバーに到達できません \u2014 対処: SearXNGをインストールして起動するか（https://github.com/searxng/searxng, 例: `docker run -d -p 8080:8080 searxng/searxng`）、/search-engine bing|searxng|metaso|tavily|perplexity|exa で別のエンジンに切り替えてください",
    searxngNoResults:
      "web_search: 0件の結果ですが、SearXNGのレスポンスが空の結果ページとは異なります（{chars}文字）\u2014 対処: より簡単な用語でクエリを言い換えるか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    metasoMissingKey:
      "web_search: MetasoにはAPIキーが必要です \u2014 METASO_API_KEY を設定するか、/search-engine metaso <key> で設定してください。https://metaso.cn/search-api/playground から取得できます",
    metasoDailyLimit:
      "web_search: Metasoの1日あたりの検索制限に達しました \u2014 METASO_API_KEY を設定するか、https://metaso.cn/search-api/playground からキーを取得してください",
    metasoUnauthorized:
      "web_search: Metaso APIキーが拒否されました \u2014 METASO_API_KEY を確認するか、https://metaso.cn/search-api/playground から取得してください",
    metasoRateLimit:
      "web_search: Metasoがレート制限されました \u2014 待って再試行するか、https://metaso.cn/search-api/playground から独自のAPIキーを取得してください",
    metasoServerError:
      "web_search: Metasoサーバーエラー ({status}) \u2014 後で再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    metasoParseError:
      "web_search: Metasoが解析不能なレスポンスを返しました (HTTP {status}) \u2014 後で再試行してください",
    metasoApiError:
      "web_search: Metaso APIエラー (コード {code}: {message}) \u2014 後で再試行してください",
    tavilyMissingKey:
      "web_search: TavilyバックエンドにはAPIキーが必要です \u2014 TAVILY_API_KEY 環境変数または ~/.reasonix/config.json の `tavilyApiKey` を設定してください。無料1000回/月の登録は https://tavily.com から",
    tavilyUnauthorized:
      "web_search: Tavily APIキーが拒否されました \u2014 TAVILY_API_KEY を確認するか、https://tavily.com から取得してください",
    tavilyRateLimit:
      "web_search: Tavilyがレート制限または月間クォータを超過しました \u2014 待つか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えるか、Tavilyプランをアップグレードしてください",
    tavilyServerError:
      "web_search: Tavilyサーバーエラー ({status}) \u2014 後で再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    tavilyParseError:
      "web_search: Tavilyが解析不能なレスポンスを返しました (HTTP {status}) \u2014 後で再試行してください",
    perplexityMissingKey:
      "web_search: PerplexityバックエンドにはAPIキーが必要です \u2014 PERPLEXITY_API_KEY 環境変数または ~/.reasonix/config.json の `perplexityApiKey` を設定してください。https://perplexity.ai/settings/api から取得できます",
    perplexityUnauthorized:
      "web_search: Perplexity APIキーが拒否されました \u2014 PERPLEXITY_API_KEY を確認するか、https://perplexity.ai/settings/api から取得してください",
    perplexityRateLimit:
      "web_search: Perplexityがレート制限されました \u2014 待って再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    perplexityServerError:
      "web_search: Perplexityサーバーエラー ({status}) \u2014 後で再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    perplexityParseError:
      "web_search: Perplexityが解析不能なレスポンスを返しました (HTTP {status}) \u2014 後で再試行してください",
    exaMissingKey:
      "web_search: ExaバックエンドにはAPIキーが必要です \u2014 EXA_API_KEY 環境変数または ~/.reasonix/config.json の `exaApiKey` を設定してください。無料1000回/月の登録は https://exa.ai から",
    exaUnauthorized:
      "web_search: Exa APIキーが拒否されました \u2014 EXA_API_KEY を確認するか、https://exa.ai から取得してください",
    exaRateLimit:
      "web_search: Exa APIがレート制限または月間クォータを超過しました \u2014 待つか、https://exa.ai/pricing でアップグレードしてください",
    exaServerError:
      "web_search: Exaサーバーエラー ({status}) \u2014 後で再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa でエンジンを切り替えてください",
    exaParseError:
      "web_search: Exaが解析不能なレスポンスを返しました (HTTP {status}) \u2014 後で再試行してください",
    braveMissingKey:
      "web_search: Brave SearchにはAPIキーが必要です \u2014 BRAVE_SEARCH_API_KEY（または BRAVE_API_KEY）環境変数、または ~/.reasonix/config.json の `braveApiKey` を設定してください。https://brave.com/search/api/ で月2000回まで無料登録できます",
    braveUnauthorized:
      "web_search: Brave Search APIキーが拒否されました \u2014 BRAVE_SEARCH_API_KEY を確認するか、https://brave.com/search/api/ から取得してください",
    braveRateLimit:
      "web_search: Brave Search APIがレート制限または月間クォータを超過しました \u2014 待つか、https://brave.com/search/api/ でアップグレードしてください",
    braveServerError:
      "web_search: Brave Searchサーバーエラー ({status}) \u2014 後で再試行するか、/search-engine bing|searxng|metaso|tavily|perplexity|exa|brave でエンジンを切り替えてください",
    braveParseError:
      "web_search: Brave Searchが解析不能なレスポンスを返しました (HTTP {status}) \u2014 後で再試行してください",
    fetchStatus:
      "web_fetch {status} ({url}) \u2014 対処: ブラウザでURLが解決できるか確認してください。ステータスはホストがエラーページを返したことを示しています",
    fetchRateLimit429:
      "web_fetch 429 ({url}) \u2014 対処: 10秒待ってから再試行してください。ホストがこのクライアントをレート制限しています",
    fetchForbidden403:
      "web_fetch 403 ({url}) \u2014 対処: ホストがこのクライアントをブロックしています。ページがログインを必要とするか、ボットをブロックしている可能性があります \u2014 代わりにweb_searchのスニペットを使用してください",
    fetchServerError5xx:
      "web_fetch {status} ({url}) \u2014 対処: ブラウザでURLを開いてみてください。読み込める場合は一時的な問題で、30秒後に再試行すると改善する可能性があります",
    fetchTimeout:
      "web_fetch: {url} が {ms}ms でタイムアウトしました \u2014 対処: より短いURLか小さなコンテンツを試してください。遅いCDNの可能性があります。もう一度再試行してください",
    fetchTooLarge:
      "web_fetch拒否: content-length {len} バイトが {cap} バイトの上限を超過 ({url}) \u2014 対処: より小さいコンテンツの別のURLを試してください。このページは大きすぎて取得できません",
    fetchBodyTooLarge:
      "web_fetch拒否: レスポンスボディが {cap} バイトの上限を超過（{seen} バイト確認）\u2014 対処: より小さいコンテンツの別のURLを試してください。このページはサイズ上限を超えてストリーミングされました",
    fetchInvalidUrl:
      "web_fetch: URLは http:// または https:// で始まる必要があります \u2014 対処: 絶対http(s) URLを渡してください（URLが不正か、未対応のスキームを使用しています）",
  },
  choiceConfirm: {
    customLabel: "自由に回答を入力",
    customDesc:
      "上記のいずれも当てはまらない \u2014 自由形式で返信してください。モデルはそれをそのまま読み取ります。",
    cancelLabel: "キャンセル \u2014 質問を破棄",
    cancelDesc: "モデルが停止し、代わりに何が欲しいかを尋ねます。",
  },
  cardTitles: {
    usage: "使用量",
    context: "コンテキスト",
    search: "検索",
    subagent: "サブエージェント",
    reply: "返信",
    reasoning: "推論",
    reasoningAborted: "推論（中断）",
    reasoningEllipsis: "推論\u2026",
    error: "エラー",
    doctor: "診断",
    you: "あなた",
    task: "タスク",
  },
  cardLabels: {
    prompt: "prompt",
    reason: "reason",
    output: "output",
    cache: "cache",
    session: "session",
    balance: "残高",
    turn: "ターン",
    system: "システム",
    tools: "ツール",
    log: "ログ",
    input: "入力",
    topTools: "上位ツール",
    logMsgs: "ログメッセージ",
    hitSingular: "{count} ヒット \u00b7 {files} ファイル",
    hitsPlural: "{count} ヒット \u00b7 {files} ファイル",
    moreHitSingular: "\u22ee +{count} 件ヒット",
    moreHitsPlural: "\u22ee +{count} 件ヒット",
    earlierLine: "\u22ee {count} 行非表示（Ctrl+Rで全出力）",
    earlierLines: "\u22ee {count} 行非表示（Ctrl+Rで全出力）",
    hiddenLine: "\u22ee {count} 行非表示",
    hiddenLines: "\u22ee {count} 行非表示",
    earlierStackLine: "\u22ee {count} 行のスタックトレース非表示",
    earlierStackLines: "\u22ee {count} 行のスタックトレース非表示",
    agent: "エージェント \u00b7 {name}",
    response: "応答",
    writing: "書き込み中\u2026",
    tok: "トークン",
    pilcrow: "\u00b6",
    aborted: "中断",
    truncatedByEsc: "[escで切り詰め]",
    rejected: "拒否",
    exit: "終了 {code}",
    bytesIn: "{bytes} 受信",
    elapsedSec: "{secs}秒",
    stackTrace: "スタックトレース",
    retries: "リトライ",
    reasoningLabel: "推論 \u00b7 {count} \u00b6",
    runningLabel: "実行中",
    workingLabel: "処理中",
    defaultFooter: "\u2191\u2193 選択  \u00b7  \u23ce 確定  \u00b7  esc キャンセル",
    applyAction: "[a] 適用",
    skipAction: "[s] スキップ",
    rejectAction: "[r] 拒否",
    levelOk: "OK",
    levelWarn: "警告",
    levelFail: "失敗",
    checksLabel: "チェック",
    passed: "合格",
    warnTag: "警告",
    failTag: "失敗",
    stepLabel: "ステップ",
    done: "完了",
    inProgress: "\u2190 進行中",
    upcoming: "予定",
    resumed: "再開 \u00b7 ",
    archive: "\u23ea アーカイブ \u00b7 ",
    more: "\u22ee +{count} 件",
    categoryUser: "ユーザー",
    categoryFeedback: "フィードバック",
    categoryProject: "プロジェクト",
    categoryReference: "リファレンス",
  },
  mcpHealth: {
    noData: "検査データなし",
    healthy: "正常 \u00b7 {ms}ms",
    slow: "低速 \u00b7 {ms}ms",
    verySlow: "非常に低速 \u00b7 {ms}ms",
    slowToast: "\u26a0 MCP `{name}` が低速 \u00b7 直近 {sampleSize} 回の呼び出しで p95 {seconds}秒",
    emptyHint:
      "\u2139 MCPサーバーが設定されていません \u2014 対処: `reasonix setup` で再選択するか、`reasonix mcp install filesystem` \u00b7 シェルコマンドは毎回確認（allow once / allow always / deny）、全許可フラグなし",
  },
  denyContextInput: {
    description:
      "拒否した理由をエージェントに伝えてください。次の試行ではあなたの理由が追加コンテキストとして参照されます。",
  },
  cardStream: {
    scrollAbove: " \u2191 {scroll} / {max} 行上",
    scrollAbovePlural: " \u2191 {scroll} / {max} 行上",
    scrollMore: " \u2014 さらに {remaining} 件",
    scrollPgUp: " \u00b7 PgUp / ホイール",
    scrollCopy: " \u00b7 /copy でコピーモード",
  },
  slashArgPicker: {
    noMatch: '"{partial}" に一致するものはありません',
    keepTyping: " \u2014 続けて入力するか、Backspace で編集",
    above: "   \u2191 上に {hidden} 件",
    below: "   \u2193 下に {hidden} 件",
    footer: "  \u2191\u2193 移動 \u00b7 Tab / \u23ce 選択 \u00b7 esc キャンセル",
  },
  mcpMarketplace: {
    title: "MCP マーケットプレイス",
    filter: "フィルタ: ",
    filterPlaceholder: "（入力でフィルタ）",
    matchSingular: "{n} 件一致",
    matchPlural: "{n} 件一致",
    loading: "読み込み中\u2026",
    noEntries: "エントリなし",
    opening: "レジストリを開いています\u2026",
    cached: "\u00b7 キャッシュ済み",
    exhausted: "\u00b7 全件取得済み",
    loadingMore: "さらに読み込み中\u2026",
    allLoaded: "全ページ読み込み済み",
    fetchingDetail: "smithery詳細を取得中\u2026",
    noInstallInfo:
      "{name} のインストール情報がありません - `npx -y @smithery/cli install {name}` を試してください",
    alreadyInstalled: "インストール済み: {spec}",
    installed: "インストール済み \u2192 {spec}",
    uninstalled: "{name} をアンインストールしました",
    installFailed: "インストール失敗: {message}",
    notInstalled: "未インストール: {name}",
    bridged: "\u2713 {name} をインストール - ブリッジ済み",
    bridgeFailed: "\u25b2 {name} をインストール - ブリッジ失敗: {reason}",
    bridgeReloadFailed:
      "\u2713 {name} をインストール - `reasonix code` を再起動してブリッジ（再読込失敗: {message}）",
    restartBridge: "\u2713 {name} をインストール - `reasonix code` を再起動してブリッジ",
    needsEnv: "  \u00b7  要環境変数: {env}",
    badgeOfficial: "[公式]",
    badgeSmithery: "[smt]",
    badgeLocal: "[ローカル]",
    footerHint:
      "フィルタ入力 \u00b7 \u2191\u2193 選択 \u00b7 \u23ce インストール/切替 \u00b7 PgDn さらに読込 \u00b7 esc 閉じる",
    specLine: "仕様: {runtime} {id} \u00b7 {transport}",
    smitheryDetail: "（smitheryリスティング \u2014 Enterでインストール詳細を取得）",
    statusError: "エラー: {message}",
  },
  mcpBrowser: {
    title: "\u25c8 MCP ブラウザ",
    empty:
      "MCPサーバーが接続されていません。`reasonix setup` を実行して選択するか、--mcp で起動してください。",
    serverCount: "{count} サーバー",
    footer: "\u2191\u2193 選択 \u00b7 [r] 再接続 \u00b7 [d] 無効化 \u00b7 esc 終了",
  },
  mcpBrowse: {
    noResources:
      "接続されたMCPサーバーにリソースがありません（またはサーバーが接続されていません）。`/mcp` で現在の設定を表示します。",
    readOne: "読み取り: `/resource <uri>` \u2014 またはピッカーでTabを使用。",
    noPrompts:
      "接続されたMCPサーバーにプロンプトがありません（またはサーバーが接続されていません）。`/mcp` で現在の設定を表示します。",
    fetchOne:
      "取得: `/prompt <name>` \u2014 引数はまだサポートされていません。必須引数があるプロンプトはサーバーからエラーが返ります。",
    noServerForResource: 'リソース "{name}" を公開しているサーバーはありません',
    resourceHint: "引数なしの `/resource` で利用可能なものを一覧表示します。",
    readFailed: "readResource に失敗しました",
    noServerForPrompt: 'プロンプト "{name}" を公開しているサーバーはありません',
    promptHint: "引数なしの `/prompt` で利用可能なものを一覧表示します。",
    fetchFailed: "getPrompt に失敗しました",
  },
  mcpLifecycle: {
    handshake: "ハンドシェイク中\u2026",
    connected: "接続済み",
    failed: "失敗",
    disabled: "無効",
    reconnect: "再接続中\u2026",
    initDetail: "初期化 \u2192 tools/list \u2192 resources/list",
    reconnectDetail: "切断中 \u00b7 再ハンドシェイク \u00b7 ツール一覧取得",
    disabledDetail: "/mcp disable {name} 経由",
    failedSetupHint:
      "→ `reasonix setup` を実行してこのエントリを削除するか、根本的な問題（npmパッケージ不足、ネットワークなど）を修正してください。",
    failedSetupConfigHint:
      "→ `reasonix setup` を実行して、保存された設定から壊れたエントリを削除してください。",
    abortedHint:
      "MCP起動が中断されました — {count} サーバーがスキップされました。根本的な問題を修正した後、/mcp で再試行してください。",
    toolsReady: "ツール準備完了",
    warnLabel: "警告",
  },
  checkpointPicker: {
    title: "チェックポイントを復元 \u2014 {workspace}",
    header: " \u25c8 REASONIX \u00b7 チェックポイントを選択 ",
    empty:
      "  このワークスペースにはまだチェックポイントがありません - /checkpoint で作成してください",
    more: "     \u2026 さらに {hidden} 件",
    footer: "  \u2191\u2193 選択  \u00b7  \u23ce 復元  \u00b7  [d] 削除  \u00b7  esc 終了",
    footerEmpty: "  esc 終了",
  },
  planReviseConfirm: {
    title: "プラン修正案",
    metaRight: "\u2212{removed}  +{added}  \u00b7  {kept} 保持",
    updatedSummary: "更新されたサマリー: {summary}",
    acceptLabel: "修正を承認 - 新しいステップリストを適用",
    acceptHint:
      "残りのプランを提案されたステップで置き換えます。完了済みステップは変更されません。",
    rejectLabel: "拒否 - 元のプランを維持",
    rejectHint: "提案を破棄します。モデルは元の残りステップで続行します。",
  },
  diffApp: {
    title: "reasonix diff",
    turnLabel: "ターン {turn} ({current}/{total})",
    turnsAligned: "{count} ターンが一致",
    paneEmpty: "（この側にはこのターンのレコードがありません）",
    kindMatch: "\u2713 一致",
    kindDiverge: "\u2605 相違",
    kindOnlyInA: "\u2190 Aのみ",
    kindOnlyInB: "\u2192 Bのみ",
  },
  recordView: {
    userPrefix: "あなた \u203a ",
    assistant: "アシスタント",
    toolPrefix: "tool<",
    argsLabel: "  引数: ",
    resultArrow: "  \u2192 ",
    error: "エラー ",
    cache: "  \u00b7 キャッシュ ",
    toolCallOnly: "（ツール呼び出し応答のみ）",
    truncateExtra: "（+{extra} 文字）",
  },
  replayApp: {
    emptyTranscript: "空のトランスクリプト",
    turnProgress: "ターン {current}/{total}",
    noRecords: "レコードなし",
    untracked: "（未追跡）",
    churned: "（チャーン \u00d7{count}）",
  },
  builtinSkills: {
    explore:
      "コードベースを分離サブエージェントで調査 — 広範囲の読み取り専用調査で1つの要約された回答を返します。最適: 「〜を使用している場所をすべて見つけて」「Xはプロジェクト全体でどのように動作するか」「Yのコードを調査して」。",
    research:
      "Web検索とコード読み取りを組み合わせて分離サブエージェントで質問を調査します。最適: 「X機能はライブラリYでサポートされているか」「Zの標準的な方法は」「私たちの実装を仕様と比較して」。",
    review:
      "保留中の変更（デフォルトでは現在のブランチ差分）を分離サブエージェントでレビュー — 正確性、セキュリティ、不足テスト、隠れた動作変更を指摘。判定と問題ごとのファイル:行を報告。読み取り専用。親が対応を決定します。",
    securityReview:
      "現在のブランチ差分のセキュリティ重視レビューを分離サブエージェントで実行 — インジェクション/認証/シークレット/デシリアライゼーション/パストラバーサル/暗号の問題を重要度タグ付きで指摘。読み取り専用。認証、入力解析、ファイルIO、外部リクエストに関わる変更をリリースする際に使用します。",
    test: "プロジェクトのテストスイートを実行し、失敗を診断してSEARCH/REPLACE修正を提案、グリーンになるまで再実行（同じ失敗に対して2回の修正試行後に停止）。インライン実行 — 親ループで実行されるため編集ブロックを確認して /apply できます。npm/pnpm/yarn/pytest/go/cargo を検出します。",
  },
  shortcutsHelp: {
    title: "ショートカット",
    groupInput: "入力",
    groupNavigation: "ナビゲーション",
    groupSession: "セッション",
    groupSystem: "システム",
    descEnter: "メッセージ送信",
    descShiftEnter: "改行",
    descCtrlEnter: "改行",
    descCtrlJ: "改行",
    descCtrlU: "入力をクリア",
    descCtrlW: "単語を削除",
    descCtrlP: "ショートカット表示/非表示",
    descCtrlX: "エディタで開く",
    descArrows: "入力履歴",
    descPgUpDown: "ページスクロール",
    descCtrlL: "画面クリア",
    descCtrlB: "サイドバー切替",
    descNewSession: "新規セッション",
    descListSessions: "セッション一覧",
    descSwitchModel: "モデル切替",
    descSwitchEffort: "推論努力切替",
    descSwitchTheme: "テーマ切替",
    descCtrlC: "終了",
    descEsc: "停止 / キャンセル",
    descCtrlR: "詳細モード切替",
    descCtrlO: "返信を展開（ストリーミング時のみ）",
    descHelp: "全コマンド表示",
    descShiftTab: "編集モード切替",
    descAltS: "入力のスタッシュ / 呼び戻し",
  },
  mcpCli: {
    bundledCatalog: "バンドルMCPサーバー（オフラインカタログ）:",
    justFetched: "取得直後",
    cachedAge: "キャッシュ済み, {age}",
    moreAvailable: "さらに利用可能",
    allLoaded: "全件読み込み済み",
    morePagesAvailable:
      "\u25b8 さらにページがあります \u2014 `reasonix mcp list --pages <n>` または --all",
    installHint: "インストール:  reasonix mcp install <name>",
    usageSearch: "使い方: reasonix mcp search <query>",
    usageInstall: "使い方: reasonix mcp install <name>",
    noMatchesFor: '{count} 件の読み込み済みエントリ中に "{q}" の一致はありません（{source}）',
    matchCount: '{source} レジストリで "{q}" に {count} 件一致（{loaded} エントリをスキャン）:',
    moreLoaded:
      "\u2026 さらに {count} 件読み込み済み \u2014 `reasonix mcp search <query>` でフィルタしてください",
    moreMatches: "\u2026 さらに {count} 件一致",
    installed: "インストール済み: {spec}",
    noServerFound:
      '{source} レジストリの {pages} ページを走査しましたが、"{target}" という名前のMCPサーバーは見つかりませんでした。',
    noServerTryMore: "試す: reasonix mcp install {target} --max-pages 100",
    noInstallMeta:
      '"{name}" のインストールメタデータを導出できませんでした \u2014 `npx -y @smithery/cli install {name}` を直接試してください。',
    buildSpecFailed: "{name} のインストール仕様を構築できません: {message}",
    alreadyInstalled: "インストール済み: {spec}",
  },
};
