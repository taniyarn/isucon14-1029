# isucon-kit

ISUCON 用のチームテンプレート。大会（練習）ごとに **「Use this template」で新しい repo を作って使う**。
作った repo は、競技サーバーの `/home/isucon`（アプリ `webapp/`、OS 設定 `etc/`）と、この道具一式（`tools/`、`Taskfile.yml`、`docs/`、`CLAUDE.md`）を 1 つに持つ。
bootstrap が初回 push 時にサーバー側の内容とテンプレートを merge するので、手元・サーバー・ops が同じ repo を見る。

**新しい Claude セッションは最初に [`docs/HANDOFF.md`](docs/HANDOFF.md) を読ませる**（「docs/HANDOFF.md を読んでから始めて」）。**当日の 4 セッション（ハブ / インフラ / アプリ / スカウト）の動き方は [`docs/lanes/README.md`](docs/lanes/README.md)**（手元の盤 = `task lane:watch`・alert・todo・メッセージ・起動の 1 行。盤は PC ごとで ops パネルには出ない）、**手順の詳細は [`docs/RUNBOOK.md`](docs/RUNBOOK.md)**（ssh でログインするところから最終ベンチ・振り返りまで）。

## 中身

| パス | 役割 |
|---|---|
| `docs/HANDOFF.md` | 新セッションへの引き継ぎ（構成、分担、検証方法、踏んだ落とし穴、検証済み/未検証）。最初に読む |
| `docs/lanes/` | 当日の 4 セッションのルール（README = 共通: 原則・盤・alert・todo・メッセージ・merge の順・コマンドの打ち方、1-hub / 2-infra / 3-app / c-scout = レーンごとのトリガー → やること → 手が空いたら） |
| `docs/RUNBOOK.md` | 当日の手順（前日準備 → 3 台セットアップ → baseline → 改善ループ → 最終チェック → 振り返り） |
| `docs/strategy-2026.html` | 作戦資料（分担、計測の考え方、お題予想、タイムライン）。ブラウザで開く |
| `docs/IMPLEMENTATION-NOTES.md` | ISUCON 固有の実装注意（再起動試験とデータ保持、`/initialize`、レスポンス互換、整合性の猶予、app 2 台、SQL、Go、nginx、失格要件）。実装前に人も AI も読む |
| `CLAUDE.local.md.example` | 個人設定（返答言語など）。`cp` して `CLAUDE.local.md` に。gitignore 済み |
| `CLAUDE.md` | AI セッションの規則（main には PR 経由のみ、人の OK 後に merge → deploy）（デプロイはするがベンチはしない、サーバーを直接触らない、ISUCON の前提）。当日冒頭を埋める |
| `.claude/skills/` | `kickoff`（当日マニュアルとアプリマニュアルを最初に読み、アクセス方法・構成・ログイン・スコア・禁止事項を `docs/kickoff.md` にまとめて設定を作る）、`app-analysis`（画面→API→ハンドラ対応を 40 分で作る）、`parallel-setup`（baseline 直後の DB 分離・計測パッチ・app-map を 3 サブエージェントで並列）、`parallel-fix`（溜まった actions をグループ分けしてサブエージェントで並列実装）、`deep-dive`（DuckDB で生データ分析）、`retrospective`（振り返り文書） |
| `deploy.sh` | 各サーバーで走るデプロイ手順（build → 設定反映 → restart）。当日 3 行埋める |
| `Taskfile.yml` | 手元からの運用: `hosts:setup`、`deploy`（DB ホスト先）、`smoke`（initialize・ログイン・主要 GET）、`diffcheck`（ブランチと main の JSON 比較）、`host:diag`（読み取り専用診断）、`env:show`、`hosts:measure-off`、`db:snapshot`（行数の控え）、`reboot-test`（再起動試験の予行）、`retro`、`deepdive`、`ops:*` |
| `tools/bootstrap/` | 競技ホストの初期化（git 化 + テンプレート merge、設定取り込み、alp/slp、ltsv ログ、スローログ）、`measure-off.sh` |
| `tools/isumeasure/` | 計測 agent（ホスト常駐、ベンチを自動検出して記録）、report（診断と次の一手）、管制盤（ops）、export、retro |
| `tools/ops/` | 共有 ops ホスト（Caddy 443 + Basic 認証、管制盤、競技ホストへの ssh トンネル） |
| `tools/app-analyzer/` | Playwright でアプリを巡回し、画面・API・遷移を 1 枚の HTML にする |
| `tools/sqltag/` | SQL にエンドポイント名のコメントを付ける 3 行パッチ（スローログをエンドポイント別に集計できる） |

## 大会ごとの使い方（要約）

1. テンプレートから repo を作る → clone → `cp .env.example .env` を書く（`GIT_REMOTE`、`CONTEST`、鍵）
2. deploy key を 1 つ登録（`tools/bootstrap/deploy_key.pub`、write。サーバーと ops が共用）
3. 当日: `.env` に `HOSTS` → `task hosts:setup` → ベンチ → 管制盤で collect/report → `task deploy BRANCH=…` のループ
4. 最後: `task hosts:measure-off` → `task hosts:reboot` → ベンチ → `task retro`

## テンプレート側のメンテ

道具の改修はこの repo（テンプレート）で行い、大会中は大会 repo のみ触る（大会中にテンプレートを変えない）。
大会で得た改善は、終了後にテンプレートへ cherry-pick する。`tools/app-analyzer/isuride.config.json` は ISUCON14 を題材にした例。`docs/reports/latest.*` は「まだ巡回していません」の置き場所（当日 app-analysis が上書きする。テンプレートに実際の巡回結果を commit しない）。
