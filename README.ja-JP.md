<p align="center">
  <img src="docs/logo.svg" alt="Reasonix" width="640"/>
</p>

<p align="center">
  <a href="./README.md">English</a>
  &nbsp;·&nbsp;
  <a href="./README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <strong>日本語</strong>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/">公式サイト</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/configuration.html">ガイド</a>
  &nbsp;·&nbsp;
  <a href="./docs/ARCHITECTURE.md">アーキテクチャ</a>
  &nbsp;·&nbsp;
  <a href="./benchmarks/">ベンチマーク</a>
  &nbsp;·&nbsp;
  <strong><a href="https://discord.gg/XF78rEME2D">Discord</a></strong>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/v/reasonix.svg?style=flat-square&color=cb3837&labelColor=161b22&logo=npm&logoColor=white" alt="npm version"/></a>
  <a href="https://github.com/esengine/reasonix/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/esengine/reasonix/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/npm/l/reasonix.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/dm/reasonix.svg?style=flat-square&color=3fb950&labelColor=161b22&label=downloads" alt="downloads"/></a>
  <a href="./package.json"><img src="https://img.shields.io/node/v/reasonix.svg?style=flat-square&color=5fa04e&labelColor=161b22&logo=nodedotjs&logoColor=white" alt="node"/></a>
  <a href="https://github.com/esengine/reasonix/stargazers"><img src="https://img.shields.io/github/stars/esengine/reasonix.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
  <a href="https://atomgit.com/esengine/DeepSeek-Reasonix"><img src="https://atomgit.com/esengine/DeepSeek-Reasonix/star/badge.svg" alt="AtomGit stars"/></a>
  <a href="https://github.com/esengine/reasonix/graphs/contributors"><img src="https://img.shields.io/github/contributors/esengine/reasonix.svg?style=flat-square&color=bc8cff&labelColor=161b22&logo=github&logoColor=white" alt="contributors"/></a>
  <a href="https://github.com/esengine/reasonix/discussions"><img src="https://img.shields.io/github/discussions/esengine/reasonix.svg?style=flat-square&color=58a6ff&labelColor=161b22&logo=github&logoColor=white" alt="Discussions"/></a>
  <a href="https://discord.gg/XF78rEME2D"><img src="https://img.shields.io/badge/discord-join-5865F2.svg?style=flat-square&labelColor=161b22&logo=discord&logoColor=white" alt="Discord"/></a>
</p>

<p align="center">
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/9e931d80-2050-4b10-902e-44970cc133ad.svg" alt="oosmetrics — Top 2 in Agents by velocity"/></a>
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/556d94b3-61b7-486b-baf2-888b9327deab.svg" alt="oosmetrics — Top 3 in LLMs by velocity"/></a>
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/0f457d4c-efca-4d15-ad2b-139691ff342c.svg" alt="oosmetrics — Top 3 in CLI by velocity"/></a>
</p>

<br/>

<h3 align="center">ターミナルで動く DeepSeek ネイティブの AI コーディングエージェント。</h3>
<p align="center">プレフィックスキャッシュの安定性を中心に設計されているため、長時間のセッションでもトークンコストを低く保ち、動かしっぱなしにできます。</p>

<br/>

<p align="center">
  <img src="docs/assets/hero-terminal.svg" alt="Reasonix code mode — assistant proposes a SEARCH/REPLACE edit; nothing on disk until /apply" width="860"/>
</p>

<br/>

> [!TIP]
> **キャッシュの安定性はオンにする「機能」ではなく、ループそのものが前提として設計された不変条件です。** これこそが Reasonix が DeepSeek 専用である理由であり、すべてのレイヤーがバイト単位で安定したプレフィックスキャッシュの仕組みに合わせて調整されています。

> [!NOTE]
> **実ユーザーの1日（2026-05-01）:** 入力トークン 4億3,500万、**キャッシュヒット率 99.82%**、同じワークロードを `v4-flash` でキャッシュなしに実行した場合の約 \$61 に対して、わずか約 \$12 で済んでいます — 詳しくは[ケーススタディ](./benchmarks/real-world-cache/README.md)を参照。キャッシュ可能なバイト列を提供するのは DeepSeek ですが、それを長時間のセッションにわたってキャッシュ可能に保つのが [Pillar 1](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) の4つの仕組みです。

> [!IMPORTANT]
> **Community · 加入社区** — セットアップのヘルプ（`#help` / `#求助`）、ワークフローのショーケース、機能アイデア、コントリビューター限定の PR 調整用チャンネルを備えたバイリンガル Discord。サーバー内で GitHub を認証すると、自動的に **Contributor** ロールが付与されます。→ **<https://discord.gg/XF78rEME2D>**

<br/>

## インストール

Node ≥ 22 が必要です。macOS · Linux · Windows（PowerShell · Git Bash · Windows Terminal）で動作します。

`reasonix` コマンドを `PATH` 上で使えるようにしたい場合は、Reasonix をグローバルにインストールします:

~~~bash
npm install -g reasonix
reasonix code my-project   # 初回実行時に DeepSeek API キーを貼り付け。以降は保存される
~~~

または、グローバルインストールせずに一度だけ実行することもできます:

~~~bash
cd my-project
npx reasonix code          # デフォルトでは常に最新のパッケージを使用
~~~

[DeepSeek API キーを取得 →](https://platform.deepseek.com/api_keys) · フラグは `reasonix code --help` を参照。

Reasonix を毎日使うなら、グローバルインストールが最もシンプルな方法です。とりあえず試してみたいだけなら `npx` を使ってください。

**キー入力を減らしたい？** より短い `dsnix` エイリアスが同じ CLI を指します:

~~~bash
npm install -g dsnix       # PATH 上に `dsnix` を公開。reasonix に依存
npx dsnix@latest code      # より短いコマンドで一度だけ実行
~~~

`npm install -g reasonix` でグローバルインストールすると `dsnix` のシムも PATH 上に配置されるため、2つは相互に置き換え可能です。

サブコマンドなしの `reasonix`（裸の `reasonix`）はカレントディレクトリで `code` を起動します — `reasonix` と入力するのは `reasonix code` と等価です。

| コマンド | 用途 |
|---|---|
| `reasonix` / `reasonix code [dir]` | コーディングエージェント。**ここから始めてください。** |
| `reasonix chat` | プレーンなチャット — ファイルシステムやシェルのツールなし。 |
| `reasonix run "task"` | 一度だけ実行し、stdout にストリーミング。パイプ向き。 |
| `reasonix doctor` | ヘルスチェック: Node、API キー、MCP の配線。 |
| `reasonix update` | Reasonix 自体をアップグレード。 |

その他のサブコマンド（`replay` · `diff` · `events` · `stats` · `index` · `mcp` · `prune-sessions`）は `reasonix --help` と [CLI リファレンス](https://esengine.github.io/DeepSeek-Reasonix/#cli)にあります。

### QQ チャンネル

QQ は既存の `chat`、`code`、またはデスクトップのセッションをリモートチャンネルとして拡張できます。これは独立したランタイムモードではなく、現在のセッションフローの一部です。

- CLI: セッションを開始し、`/qq connect` を実行
- デスクトップ: `Settings -> General -> QQ Channel` を開く

接続すると、QQ のメッセージを現在のセッションに取り込み、アシスタントの返信を QQ に転送し、その後のやり取りをリモートで継続できます。

詳しいセットアップ、デスクトップのクイックスタート、トラブルシューティングは [QQ チャンネルのセットアップ](./docs/qq-connect.md)を参照してください。

### デスクトップクライアント（プレリリース）

同じループの GUI が欲しいユーザー向けのネイティブ Tauri クライアント。マルチタブ対応で、右パネルにはこのセッションでエージェントが読み取り・編集したファイルが表示され、下部には同じコスト / キャッシュ / トークンのメーターがリアルタイムで表示されます。同じ DeepSeek API キー、同じ `~/.reasonix` 設定を使い、デスクトップ版は独自の Node ランタイムを同梱するため、別途 `npm install` する必要はありません。

各プラットフォームのインストーラーは [GitHub Releases](https://github.com/esengine/DeepSeek-Reasonix/releases) からダウンロードできます。デスクトップ版は **プレリリース** として提供されます: ループとプロトコルは CLI と同じですが、UI はまだ磨き込み中で、インストーラーはまだコード署名されていません。

- **macOS** — 初回起動時に Gatekeeper に引っかかります。一度きりの対処: `xattr -dr com.apple.quarantine /Applications/Reasonix.app`（または右クリック → 開く → 確認）。
- **Windows** — SmartScreen が「不明な発行元」と警告します。**詳細情報 → 実行** をクリックしてください。
- **Linux** — `.deb` と `.AppImage` はそのまま起動でき、追加の手順は不要です。

CLI が引き続き正規のインターフェースです。CLI に入った機能はすべてデスクトップのコンポーザーからも利用できます。

<details>
<summary><strong>別のフォルダで作業する · chat と code の違い · スキルを書く</strong></summary>

**別のフォルダで作業する。** Reasonix はファイルシステムツールを起動ディレクトリにスコープします。対象を変えるには `--dir` を渡してください。セッション途中での切り替えは設計上サポートされていません（メモリのパスが古いルートと絡まってしまうため）— 一度終了して再起動してください。

~~~bash
npx reasonix code --dir /path/to/project
~~~

**`chat` と `code` の選び方。** `code` がデフォルトであり、ファイルシステム / シェルツールと SEARCH/REPLACE レビューを備えた唯一のモードです。`chat` はより軽量でツールをオフにしたシェルです — MCP は接続しつつディスクアクセスのない思考のパートナーが欲しいときに使ってください。

| 得られるもの | `code` | `chat` |
|---|---|---|
| ファイルシステムツール + `edit_file` | ✓ | — |
| SEARCH/REPLACE → `/apply` レビュー | ✓ | — |
| シェルツール（ゲート付き） | ✓ | — |
| プランモード · `/todo` · `/skill new` · `/mcp add` | ✓ | — |
| メモリ（`remember` / `recall_memory`） | プロジェクト + グローバル | グローバルのみ |
| 設定からの MCP サーバー · ウェブ検索 · `ask_choice` | ✓ | ✓ |
| コーディング用システムプロンプト | ✓ | 汎用 |
| セッションのスコープ | ディレクトリごと | 共有デフォルト |

**最初のスキルを書く。** リモートレジストリはありません — 直接書きます。ファイル（`description:` フロントマター + 本文）を編集し、`/skill list` を実行します。本文をインライン展開する代わりに分離したサブエージェントループを生成するには `runAs: subagent` を追加します。

~~~bash
/skill new my-skill              # <project>/.reasonix/skills/my-skill.md
/skill new my-skill --global     # プロジェクト横断で使うなら ~/.reasonix/skills
~~~

**Claude 形式のスキルも読み込まれます。** `<project>/.claude/skills/<name>/SKILL.md` と `~/.claude/skills/` が Reasonix のネイティブパスと並んで読み込まれるため、Claude 形式のスキルを出力するツールはそのまま動きます。例 — 上流アダプターなしで OpenSpec ワークフローを投入する:

~~~bash
npx openspec init --tools claude    # .claude/skills/openspec-*/SKILL.md を書き出す
/skill openspec-propose <task>      # その後 Reasonix から呼び出す
~~~

</details>

<br/>

## 設定

`~/.reasonix/config.json` の1つの JSON ファイルと、`<project>/.reasonix/` 配下のプロジェクトごとのオーバーライドで設定します。すべてのキー、すべてのスラッシュコマンド、スキル / メモリ / フックのオンディスク構造を網羅した完全なバイリンガルリファレンスはこちら:

> 📘 **[設定ガイド](https://esengine.github.io/DeepSeek-Reasonix/configuration.html)** · [中文](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh)

| トピック | 概要 |
|---|---|
| [MCP サーバー](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#mcp) | stdio · SSE · Streamable HTTP。1つの仕様形式が `config.json` と `--mcp` の両方で動作。 |
| [スキル](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#skills) | モデルが呼び出せる Markdown のプレイブック。`inline` または `subagent` モード。 |
| [メモリ](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#memory) | プレフィックスにピン留めされるユーザー専用の知識。`user` / `feedback` / `project` / `reference` の型。 |
| [フック](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#hooks) | ライフサイクルイベント時のシェルコマンド。`PreToolUse`（ゲート） · `PostToolUse` · `UserPromptSubmit` · `Stop`。 |
| [権限](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#permissions) | ワークスペースごとのシェル許可リスト。完全な前方一致。 |
| [ウェブ検索](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#search) | デフォルトは Mojeek。`/search-engine` でセルフホストの SearXNG や Metaso に切り替え可能。 |
| [セマンティックインデックス](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#index) | `reasonix index` — ローカルの Ollama または任意の OpenAI 互換の埋め込みエンドポイント。 |

<br/>

## Reasonix の何が違うのか

ループは3つの柱を中心に構成されています。それぞれが、汎用的なエージェントフレームワークには見えてすらいない問題を解決します — それらは別のキャッシュの仕組み向けに設計されているからです。

<sub align="center">

完全なアーキテクチャ解説はこちらから → [Pillar 1 — キャッシュファーストのループ](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) · [Pillar 2 — ツール呼び出しの修復](./docs/ARCHITECTURE.md#pillar-2--tool-call-repair) · [Pillar 3 — コスト制御](./docs/ARCHITECTURE.md#pillar-3--cost-control-v06)

</sub>

<br/>

## 機能

<p align="center">
  <img src="docs/assets/feature-grid.svg" alt="Reasonix capabilities — cell-diff renderer, MCP, plan mode, permissions, dashboard, persistent sessions, hooks/skills/memory, semantic search, auto-checkpoints, /effort knob, transcript replay, event log" width="880"/>
</p>

<br/>

## 比較

|                                   | Reasonix         | Claude Code       | Cursor              | Aider              |
|-----------------------------------|------------------|-------------------|---------------------|--------------------|
| バックエンド                       | DeepSeek         | Anthropic         | OpenAI / Anthropic  | 任意（OpenRouter） |
| ライセンス                         | **MIT**          | クローズド        | クローズド          | Apache 2           |
| コストプロファイル                 | **タスクあたり低コスト** | プレミアム  | サブスク + 従量     | まちまち           |
| DeepSeek プレフィックスキャッシュ  | **設計込み**     | 該当なし          | 該当なし            | 偶発的             |
| 組み込みウェブダッシュボード       | あり             | —                 | 該当なし（IDE）     | —                  |
| 設定可能なウェブ検索エンジン       | `/search-engine` | —             | —                   | —                  |
| ワークスペースごとの永続セッション | あり             | 一部              | 該当なし            | —                  |
| プランモード · MCP · フック · スキル | あり           | あり              | あり                | 一部               |
| ウェブ検索（Mojeek + SearXNG + Metaso） | あり        | あり              | あり                | あり               |
| オープンなコミュニティ開発         | あり             | —                 | —                   | あり               |

リアルタイムのキャッシュヒット率、コスト、手法については [`benchmarks/`](./benchmarks/) を参照してください — 数値はモデルの料金とともに変動するため、README ではなくハーネスとともに置いています。

<br/>

## ドキュメント

- [**アーキテクチャ**](./docs/ARCHITECTURE.md) — 3つの柱: キャッシュファーストのループ、ツール呼び出しの修復、コスト制御
- [**CLI リファレンス**](./docs/CLI-REFERENCE.md) — すべてのシェルサブコマンド、すべてのスラッシュコマンド、すべてのキーバインド
- [**QQ チャンネルのセットアップ**](./docs/qq-connect.md) — CLI の初回接続フロー、デスクトップのエントリ、QQ Open Platform の認証情報
- [**ベンチマーク**](./benchmarks/) — τ-bench-lite ハーネス、トランスクリプト、コスト算出手法
- [**公式サイト**](https://esengine.github.io/DeepSeek-Reasonix/) — はじめに、ダッシュボードのモックアップ、TUI のモックアップ
- [**コントリビューション**](./CONTRIBUTING.md) — コメントポリシー、エラーハンドリングのルール、自作よりライブラリを優先
- [**行動規範**](./CODE_OF_CONDUCT.md) · [**セキュリティポリシー**](./SECURITY.md)

<br/>

## コミュニティ

> [!NOTE]
> Reasonix はオープンソースであり、コミュニティによって開発されています。このファイル末尾の謝辞ウォールにあるすべてのアバターは、実際にマージされた PR です。

スコープが切られた入門用チケット — それぞれ背景、コードの参照ポイント、受け入れ基準、ヒント付き — は [`good first issue`](https://github.com/esengine/reasonix/labels/good%20first%20issue) ラベルの下にあります。オープンなものなら何でも選んでください。

**オープンな Discussions — 意見募集中:**

- [#20 · CLI / TUI デザイン](https://github.com/esengine/reasonix/discussions/20) — 何が壊れていて、何が足りず、何を変えますか？
- [#21 · ダッシュボードデザイン](https://github.com/esengine/reasonix/discussions/21) — [提案中のモックアップ](https://esengine.github.io/DeepSeek-Reasonix/design/agent-dashboard.html)への反応をどうぞ
- [#22 · 今後の機能ウィッシュリスト](https://github.com/esengine/reasonix/discussions/22) — 次に Reasonix に組み込みたいものは？

**すでに Reasonix を使っていて、他の人に広める手伝いをしてもいい？** ブログ記事、記事、スクリーンショット、講演、動画を [**Show and tell**](https://github.com/esengine/reasonix/discussions/categories/show-and-tell) に公開してください。このプロジェクトにはマーケティング予算がありません — 新しいユーザーは口コミで見つけてくれます。継続的に応援してくれる人は、以下のバッジを獲得でき、付与されるとコントリビューターウォールの横に表示されます:

<p align="center">
  <a href="https://github.com/esengine/reasonix/discussions/categories/show-and-tell">
    <img src="https://img.shields.io/badge/REASONIX-📣%20ADVOCATE-c4b5fd?style=for-the-badge&labelColor=0d1117" alt="Reasonix Advocate badge — earned by sustained advocates"/>
  </a>
</p>

**最初の PR の前に**: [`CONTRIBUTING.md`](./CONTRIBUTING.md) を読んでください — 短く厳格なルール（コメント、エラー、自作よりライブラリを優先）。`tests/comment-policy.test.ts` がコメントのルールを強制します。`npm run verify` がプッシュ前のゲートです。参加することで [行動規範](./CODE_OF_CONDUCT.md) に同意したものとみなされます。セキュリティの問題は → [SECURITY.md](./SECURITY.md)。

<br/>

## 非目標

> [!IMPORTANT]
> Reasonix には明確な方針があります。あえて *やらない* ことがいくつかあります — あなたの作業に合ったツールを選べるよう、ここに挙げておきます。

- **マルチプロバイダーの柔軟性。** あえて DeepSeek 専用にしています。1つのバックエンドに密結合していることは制限ではなく機能です。
- **IDE 統合。** ターミナルファースト。差分は `git diff` に、ファイルツリーは `ls` にあります。ダッシュボードはあくまで補助であり、Cursor の代替ではありません。
- **最難関リーダーボードの推論。** Claude Opus はいまだに一部のベンチマークで勝っています。DeepSeek はコーディングで競争力がありますが、もしあなたの作業が「この認証バグを直す」ではなく「この博士課程レベルの証明を解く」ものなら、Claude から始めてください。
- **エアギャップ / 完全無料。** Reasonix には有料の DeepSeek API キーが必要です。エアギャップ環境やゼロコストでの実行には Aider + Ollama または [Continue](https://continue.dev) を参照してください。

<br/>

## スター履歴

<a href="https://www.star-history.com/?repos=esengine%2FDeepSeek-Reasonix&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=esengine/DeepSeek-Reasonix&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=esengine/DeepSeek-Reasonix&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=esengine/DeepSeek-Reasonix&type=date&legend=top-left" />
 </picture>
</a>

<br/>

## サポート

Reasonix が役に立って、お礼を言いたいと思ったらどうぞ。これはあくまでコーヒー一杯であって契約ではありません — 寄付で機能の優先順位が買えるわけでも、issue のトリアージのされ方が変わるわけでもありません。

- **International** — PayPal: [paypal.me/yuhuahui](https://paypal.me/yuhuahui)
- **国内** — 微信支付（扫码）

<p align="center">
  <img src=".github/sponsor/wechat-pay.jpg" alt="WeChat Pay QR code" width="240"/>
</p>

<br/>

## 謝辞

Reasonix を最も形づくってくれた人々の小さなリストです — コミット数とコード量の両方で測っています。**重要度の順位付けはなく、アルファベット順に記載しています。** 完全なコントリビューターグラフは
[GitHub](https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors) にあります。

- [**ctharvey**](https://github.com/ctharvey)
- [**dimasd-angga**](https://github.com/dimasd-angga) (Dimas D. Angga)
- [**Evan-Pycraft**](https://github.com/Evan-Pycraft)
- [**ForeverYoungPp**](https://github.com/ForeverYoungPp)
- [**GTC2080**](https://github.com/GTC2080) (TaoMu)
- [**kabaka9527**](https://github.com/kabaka9527)
- [**lisniuse**](https://github.com/lisniuse) (Richie)
- [**wade19990814-hue**](https://github.com/wade19990814-hue)
- [**wviana**](https://github.com/wviana) (Wesley Viana)

また、プロジェクトのロゴをデザインしてくれた [**Bernardxu123**](https://github.com/Bernardxu123)（[`docs/brand/`](./docs/brand/) を参照）と、小紅書（XiaoHongShu）でプロジェクトを宣伝してくれた [AIGC Link](https://xhslink.com/m/80ngts127cA) にも別途感謝します。

<p align="center">
  <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=esengine/DeepSeek-Reasonix&max=100&columns=12" alt="Contributors to esengine/DeepSeek-Reasonix" width="860"/>
  </a>
</p>

<br/>

---

<p align="center">
  <sub>MIT — <a href="./LICENSE">LICENSE</a> を参照</sub>
  <br/>
  <sub><a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors">esengine/DeepSeek-Reasonix</a> のコミュニティによって構築</sub>
</p>
