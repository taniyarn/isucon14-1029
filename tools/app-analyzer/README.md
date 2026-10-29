# isucon-app-analyzer

ISUCON の対象アプリを Playwright で巡回し、「どの画面が」「どの API を」「どのくらいの頻度で」呼ぶかを記録して、人間向けの HTML と AI 向けの Markdown に出すツール。
Claude がこれと参考実装のコードから `app-map.md`（画面 → API → ハンドラ → テーブル）を書く。手順は `SKILL.md`。

ISUCON14（ISURIDE）の実機で動作確認済み。サンプル出力は `examples/isuride/app-map.md`。

## セットアップ（一度だけ。Node 20 以上）

```
npm run setup        # npm ci + Chromium（playwright の固定バージョンに合わせて取得）
```

## 当日の手順（目標 10 分）

```
task analyzer:init                          # repo 直下で: kickoff.md の ```analyzer ブロック + webapp/ + HOSTS の 2 台目から config を作って doctor（FORCE=1 で上書き）
npm run init -- https://<競技ホスト>          # 雛形 analyzer.config.json を生成（手で書くとき）
#   → マニュアルとフロントのコードを見て personas（Cookie / seeds / authCheck）を埋める
npm run routes -- https://<host> --ip <svc2 ip>   # SPA のバンドルからルート一覧（ネスト込み）→ seeds の材料
npm run doctor -- analyzer.config.json        # hosts 解決・https 疎通・各ペルソナの認証・flow / OpenAPI の存在・ブラウザ起動を確認
npm run analyze -- analyzer.config.json       # doctor → crawl → report → ブラウザで開く（実測 35 秒）
```

- `analyze` は最後に **publish** する: 設定の `publishDir`（既定 `../../docs/reports`）に `latest.html` と `latest.md`（AI 用）を上書きする。最新だけを残し、過去分は `out/`（main のチェックアウト側。`outDir` 参照）にのみ残る。`docs/index.html` → `reports/latest.html` のリンクは固定なので、競技当日に回せば資料がそのまま置き換わる。`--no-publish` で抑止。
- `doctor` が NG を出したらそこを直してから `analyze`。`--skip-doctor` で飛ばせる。
- `authCheck` を各ペルソナに書くと、トークンが有効かを API で検証する（書かない場合は seed がログイン画面に飛ばされないかで判定）。
- 個別に動かす: `npm run crawl -- <config>` / `npm run report`（`-- --no-inline` で画像を埋め込まない）。
- 動作確認済みの設定例: `isuride.config.json`（ISUCON14）。
- **ベンチのたびに巡回なしで更新**: `task app:refresh TS=<ts>`（= `node refresh.mjs analyzer.config.json --run ../../measurements/<ts>`、約 1 秒）。公開済みの `latest.html` に埋め込まれたモデル（前回の巡回の結果）を取り出し、ベンチの情報（ops の `report.json` と `actions.json`、前の run、`gh issue/pr list`、`docs/app-annotations.json`、今のコードの「ホストに縛られる副作用」と「endpoint × テーブル」）を重ねて書き戻す。`out/` が無いマシンや worktree からでも動く。**run 無しでも動く**: `task app:refresh`（TS 無し、または `TS=none`。= `--run none` / `--run` 省略）は `docs/app-annotations.json` と、コードから読む「ホストに縛られる副作用」「endpoint × テーブル」だけを読み直す。ページに載っている前回の run はそのまま残す（その run が `measurements/` にあれば annotations と合わせて計算し直す）。最初のベンチの前や、annotations を書き足した後に使う。`npm run report -- --run <dir>` でも同じものが重なる。

## 設定（analyzer.config.json）

| キー | 意味 |
|---|---|
| `name` / `publishDir` | レポートの名前（latest.txt に記録）と、latest.html / latest.md を置くディレクトリ（設定ファイルからの相対パス） |
| `outDir` | 巡回結果（`<outDir>/<ts>/` と `<outDir>/latest`）の置き場所（既定 `out`）。相対パスは **main のチェックアウト側**の、設定ファイルと同じ位置から解決する（`git rev-parse --git-common-dir` の親。worktree から巡回しても `task worktrees:clean` で消えない）。絶対パスならそのまま使う。crawl / report / analyze / smoke が同じ場所を読む（`outdir.mjs`） |
| `baseURL` | 対象の URL。自己署名 / 期限切れ証明書でも可（`ignoreHTTPSErrors`） |
| `dwellMs` | 各画面での滞在時間。ポーリングと SSE を観測するために 4〜5 秒 |
| `concurrency` | ペルソナ内で同時に開く画面数（既定 3）。ペルソナ同士は常に並列 |
| `settleMs` | 画面表示後に最初の XHR を待つ時間（既定 800ms）。ポーリングがある画面は networkidle にならないので短くしてある |
| `maxPages` | ペルソナごとの巡回上限 |
| `apiPrefixes` | API とみなすパスの接頭辞（既定 `/api/`） |
| `openapiPath` / `openapiPrefix` | OpenAPI があれば指定。未観測の操作を一覧にする |
| `codeDir` | 参考実装（Go）のディレクトリ。ルート定義とハンドラを静的に読んで、エンドポイントごとに「ハンドラ名 / file:line / 触るテーブル / SQL 数 / ループ内 SQL（N+1 疑い）/ 行ロック / 外部 HTTP 呼び出し」を付ける |
| `schemaPath` | DB スキーマ（`CREATE TABLE` の SQL）。省略時は `codeDir/../sql/*schema*.sql` を探す。各 SQL 文の WHERE / ORDER BY 列に index があるかを突き合わせる |
| `frontendDir` | フロントのソース（例 `frontend/app`）。ルートファイルの meta（title / description）を拾って画面の概要に使う。Remix のフラットルートと Next.js の pages / app に対応 |
| `actorNames` | API パスの先頭セグメント → アクター名（例 `{"app": "user"}`）。サマリーや行列のグループ分けに使う |
| `stateKeys` | レスポンス JSON から状態値を拾うキー（既定 `status`, `state`, `phase`）。Storyboard の状態タイムラインと遷移図に使う |
| `ignoreUrlPatterns` / `idPatterns` | 無視するパスと、`:id` に正規化する ID の正規表現 |
| `initialize` | 巡回前に 1 回 POST する初期化 API（例 `/api/initialize`）。ベンチを一度も受けていないホストは DB が空で初期ユーザーのログインが 401 になる。**ベンチ対象ホストに向けない**（データが消える）。分析は svc2 のような空きホストに向ける |
| `initializeBody` | initialize に POST する JSON の body（既定 `{}`）。`crawl.mjs` と `smoke.mjs`（`task smoke INIT=1` / `task diffcheck`）の両方が使う。initialize が body を読む問題では必ず書く（ISUCON14 は `{"payment_server": "http://<bench>:12346"}`。`{}` だと `payment_gateway_url` が空になり、手で試す決済が失敗する） |
| `expectLanguage` | `smoke.mjs --initialize`（`task smoke INIT=1`）で、initialize のレスポンスの `language` がこれと一致するかを確かめる（既定: `.env` の `LANGS` の先頭、無ければ `go`。`""` で確かめない。`--lang <x>` で上書き）。他の言語のユニットが 8080 番で答えていると失敗する。smoke の最後の行は `checked N, failed M`（失敗が 1 つでもあれば exit 1） |
| `apiTimeoutMs` / `maxInflightPerEndpoint` / `flowTimeoutMs` / `totalBudgetMs` | 既定 15000 / 2 / 120000 / 240000。flow の `api()` の fetch タイムアウト、同一エンドポイントの同時実行上限（超過は abort して "throttled" と記録。ページのポーリング連打で DB プールを埋めない）、flow 全体の締め切り、ペルソナごとの巡回予算。遅い API は「待つ」のではなく「記録して先へ」 |
| `sameOriginHosts` | 同じアプリとして扱うホスト名パターン（`resolve` のキーは自動で含む）。テナント／ユーザーのサブドメイン越しの API を同じ行に集計する |
| `resolve` | ホスト名パターン → IP（例 `{"*.u.isucon.local": "18.183.49.79"}`）。Chromium の host-resolver-rules なので `/etc/hosts` 不要。doctor も同じルールで疎通確認する |
| `annotations` | 画面と endpoint の役割、マニュアルのルール（既定 `<repo>/docs/app-annotations.json`。app-analysis skill の 4c で Claude が書く。形は `examples/isupipe/app-annotations.json`） |
| `opsReportBase` | ops の report への URL の前半（例 `https://<panel>/data/<contest>/measurements`）。「今解決すべきこと」の各行から ops の report の該当行へ飛ぶ。無ければリンクなし |
| `repoRoot` | contest repo のルート（既定: 設定ファイルのある場所の git ルート）。issue / PR、annotations、`webapp/sql/indexes/` をここから読む |
| `personas[]` | ロールごとの設定。`login`（巡回前に叩く API: `{path, method, body, headers}`。Set-Cookie がコンテキストに残る）、`cookies`、`headers`、`viewport`、`seeds`、`authCheck`、`flows`、`vars` |

ペルソナの `flows` は `export default async (page, { persona, cfg, step }) => {}` を持つ ESM ファイル。
ページ内で `fetch()` を実行すればネットワークログに残る（`flows/isuride-ride-lifecycle.mjs` 参照）。`step("ラベル")` を呼ぶとレポートに区切りが入る。

## 出力

- `report.md`: 画面一覧、Storyboard（flow の各ステップのスクショ + その間の API 呼び出し）、Navigation graph（画面間リンクの Mermaid 図 + 画面内ボタン / フォーム一覧）、画面ごとのリクエスト（繰り返しは 3 件に畳む）、エンドポイント集計（回数・呼び元・status・中央値 ms・リクエスト / レスポンス例）、ポーリング検出、JS バンドル走査（API 文字列、setInterval / setTimeout、EventSource / WebSocket）、OpenAPI との差分
- `report.html`: 単一ファイルの対話アプリ（画像は base64 で埋め込み、外部依存なし。そのまま Discord に貼れる）。**ヘッダー右上で日本語 / English を切り替え**（ブラウザに記憶。初期値はブラウザの言語）。UI 文言と所見・注記はどちらの言語でも読める。report.md は AI 用に英語固定。タブ構成:
  - Overview: 「ベンチはブラウザを使わない」注意書き → **サービス要約（アクター × 画面 × API × リソース）** → **状態遷移図（状態 → それを起こした API とアクター）** → **Top findings（ポーリング比率と retry_after 系フィールド、無負荷でも遅い API、UI から呼ばれない internal API、ループ内 SQL、書き込みホットパス、外部 HTTP 呼び出し、エラーの分類）** → 数値タイル → アクター別にグループ化した画面 × エンドポイントのヒートマップ
  - Map: スクショ付きノードの遷移図。クリックで右パネルに API 表と時系列。到達不能な画面は右側の島
  - Storyboard: 遷移図 + 状態タイムライン + フィルムストリップ（状態が変わったフレームを強調、主役 API を太字、ポーリングは折りたたみ）
  - Endpoints: アクター別グループ、プリセット絞り込み（polling / slow / write / loop / errors / external）、各行の下に **一行の概要**、**コードから読み取った高速化に関わる事実**（ループ内 SQL、トランザクション、行ロック、外部 HTTP、リトライ、sleep、アプリ側ソート、ORDER BY RAND()、ウィンドウ関数、WHERE なし SELECT、SELECT *、サブクエリ、WHERE / ORDER BY 列の index 有無、最新 1 行取得）、行数 / 分岐 / ループ数、**ハンドラが実行する SQL 文の一覧**（ループ内と index 欠落を強調）、呼び元画面、レスポンス JSON の構造要約
  - Map / Screens: 各画面に **概要**（フロントの meta description、見出し、入力項目、ボタン、呼ぶ API の役割から自動生成）
  - Screens: カードと画面ごとのリクエスト表（レスポンス構造列つき）
  - Code scan / OpenAPI: 未観測 API に「なぜ重要か」の注記（internal → バッチ候補、initialize、登録系）とハンドラ対応
  - 4xx/5xx は「アプリ起因」と「クロール起因（Cookie なし・再実行の重複・中断）」に分けて表示。赤く出るのはアプリ起因だけ
  - `--no-inline` を付けると画像を埋め込まず相対パス参照にする
- `data.json`: 生データ

## 制約

- ベンチマーカーはブラウザを使わない。ここで分かるのは画面と API の対応であって、ベンチの行動ではない（そちらはアクセスログ分析で見る）
- 巡回は滞在型なので、ボタン操作の先にある API は `flows` か Claude Code の Playwright MCP で補う
- 書き込みを伴う flow はアプリのデータを変える。ベンチ前に initialize で戻る

## 当日の TODO（設定に必要な情報）

1. hosts と証明書（https で開けるか）
2. ログイン方式と各ロールのトークン（初期データ or DB）
3. seed URL（フロントの README / ルート定義）
4. OpenAPI の有無
5. 書き込み導線の flow を 1 本（去年なら配車 1 件）
