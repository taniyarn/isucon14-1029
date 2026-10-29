---
name: app-analysis
description: ISUCON 競技開始直後に、対象アプリの画面・API・ハンドラ・テーブルの対応（app-map）を 40 分以内に作る手順。Playwright で収集し、Claude が考察を書く。
---

# app-analysis

## いつ使うか

競技開始直後、`kickoff` skill の後（マニュアルとサーバーの中身が手元にある状態）。担当 C が Claude に「app-analysis skill で app-map を作って」と言う。設定作り・巡回・app-map 執筆まで Claude がやり、C は目視で直す。
目的は `docs/app-map.md` を作り、全員と AI が同じ理解で動き出すこと。

## 手順

### 1. 設定ファイルを作る（Claude、5 分）

**`analyzer.config.json` の持ち主は kickoff phase 2（ハブ）。app-analysis は読むだけ**（2026-10-25: kickoff phase 2 とレーン 3 の両方が新規作成して衝突し、app-map の PR が rebase で 18 分遅れた）。

- origin/main に config があれば（`git show origin/main:tools/app-analyzer/analyzer.config.json`）`doctor` だけ回して 2 へ。
- まだ無ければ、自分では新規作成せず、3（コード側の材料）と 4 の code-only の app-map を先に書き、config が main に来たら巡回する。
- 直す必要があれば（seed の id、ペルソナ）、app-map とは**別の commit** にして、ハブに `[NEED-OK] analyzer config <何を直したか 1 行>` を送る（app-map の PR に混ぜない）。
- 1 枚で回すとき（レーンが無い）だけ、ここで下の項目を自分で作る。人に聞くのは資格情報が本当に無い時だけ。

- `baseURL`: nginx の `server_name`（`etc/nginx/sites-enabled/`）とマニュアルから。**`/etc/hosts` は不要**: `resolve: {"*.example.local": "<svc1 IP>"}` でブラウザ内で名前解決する。自己署名 TLS は無視される。
- ログイン: `personas[].login = {path: "/api/login", method: "POST", body: {...}}` で巡回前に API を叩く（Cookie はそのコンテキストに残る）。Cookie/Bearer を直接持つなら `cookies` / `headers`。資格情報は `webapp/sql/initial_users.sql` 等の初期データから。bcrypt のコストが低ければ `test` / `password` / `isucon` を Go 数行で照合して当てる。当たらなければ `login` に登録 API（`/api/register`）を使って新規ユーザーを作る。
- `authCheck`: ログイン必須の GET（`/api/user/me` 等）が 200 になること。
- `seeds`: SPA のバンドル（`curl -sk --resolve <host>:443:<ip> https://<host>/` → `/assets/index-*.js` → `grep -o 'path:"[^"]*"'`）か、フロントのルート定義から。役割ごとにペルソナを分ける（guest / user / owner / admin など）。
- `codeDir`（`../../webapp/go`）、`schemaPath`（スキーマ SQL）、`openapiPath`（あれば）、`frontendDir`（ソースがあれば）。
- 書き込みが要る導線（予約、投稿、状態遷移）は `flows/*.mjs` に書く（例: `flows/isuride-ride-lifecycle.mjs`）。`await step("ラベル")` を状態が変わるたびに呼ぶ。

`npm run doctor -- analyzer.config.json` が全部 OK になるまで直す。

### 1b. 向き先は svc2、初期化してから（1 分）

巡回はベンチ対象（svc1）に向けない。同じ初期状態の **svc2** に `resolve` を向ければ、svc1/svc3 の作業やベンチと干渉せず、flow の書き込みも気にしなくてよい。ベンチを受けていないホストは DB が空なので `"initialize": "/api/initialize"` を設定に入れる（巡回前に 1 回 POST、約 15 秒）。
ルート一覧は `npm run routes -- https://<host> --ip <svc2 ip>`（バンドルのネストしたルートも出る）。flow の中で API を呼ぶときは第 2 引数の `api(method, path, body)` を使う（タイムアウトと再試行付き。生の `fetch` を await すると遅い API で止まる）。

### 2. 収集（5〜10 分）

```
task app:crawl > /tmp/analyze.log 2>&1 &   # バックグラウンドで。ステップごとに経過秒が出る。前後で全 agent の検知を止める / 戻す
tail -f /tmp/analyze.log                    # 1 分以上止まっていたら flow の該当ステップを疑う（flowTimeoutMs で自動で打ち切られる）
```

`task app:crawl` は `npm run analyze` の前後で `detect-pause.sh on|off` を打つ（巡回の `/api/initialize` で panel に偽の run ができない）。`.env` の無い worktree で `task` が動かないときは `task detect:pause MIN=10` を人かハブに頼み、`npm run analyze -- analyzer.config.json` を直接回す。

目安: 静的巡回 15〜20 秒 + initialize 15 秒 + flow 1 本 20〜30 秒。

`out/<ts>/report.md`（AI 用）と `report.html`（人間用）ができる（`out/` は main のチェックアウト側の `tools/app-analyzer/out/`。worktree から回しても worktree の掃除で消えない。README の `outDir`）。HTML はブラウザで開いて全員に共有する。

### 3. コード側の材料を集める（並行、2 分）

```
grep -n 'HandleFunc\|\.Get(\|\.Post(\|Route' webapp/go/main.go            # ルート表
grep -n 'CREATE TABLE\|KEY\|INDEX' webapp/sql/*schema*.sql                 # スキーマと index
grep -n '^func \|SELECT\|INSERT\|UPDATE\|DELETE\|FOR UPDATE' webapp/go/*.go # ハンドラごとの SQL
```

### 3b. report の読み方（初見の人向け）

Overview を上から順に読む。**Service summary**（誰が・どの画面で・どの API を使うか）→ **State machine**（状態と、それを進める API とアクター）→ **Top findings**（ポーリング、遅い API、internal API、ループ内 SQL、書き込みホットパス、エラー分類）。この 3 つで仮説の材料は揃う。
Endpoints タブは「handler → tables」と「response schema」の行を見る。どの API が何を join して返しているかが読める。

### 4. app-map.md を書く（Claude、10 分）

以下のプロンプトで Claude に書かせ、担当 C が目視で直す。

```
report.md、ルート表、スキーマ、ハンドラの SQL、アプリマニュアル、当日マニュアルを読んで docs/app-map.md を書いて。
構成は次の 7 節。表は Markdown。仮説には必ず「仮説」と書く。全体で 150 行以内。**性能の所見（N+1、遅い API、index 不足）は書かない** — それは bench の report / summary.md の仕事で、最初の改善で変わる。app-map はアプリの挙動とデータの地図に徹する。

1. 画面一覧（ペルソナ / 画面 / 役割 / 呼ぶ API / 備考）
2. エンドポイント表（method path / ハンドラ / 読む・書くテーブル / 呼び元画面 / 所見は「挙動」のみ: 書き込み、外部呼び出し、exec、ホスト依存）
3. データモデル: ER（report の Data model タブの関係を文章と表で。テーブル / 行数（ベンチ後の `task db:snapshot` 値があれば）/ 主キー / 参照 / append-only か）と、各テーブルが「現在の状態」をどう表すか、遷移を進める API
4. スコアとユーザー増加に効く導線（マニュアルのスコア式・整合性の猶予・タイムアウトと結びつける）
5. 待ちが発生する箇所（ポーリング間隔、retry、外部 API、ロック、常駐プロセス、ホスト依存の副作用）
6. 未確認・ログで確かめること
7. 参照（スクショと report のパス）

前提: ベンチマーカーはブラウザを使わず API を直接叩く。report に出る頻度は「画面がどう呼ぶか」であって「ベンチがどう呼ぶか」ではない。
```

### 4b. 画面遷移の読み方（report の 3 つの節を順に見る）

1. **Navigation graph**: ルート間のリンク関係（Mermaid）。「どの画面からどこへ行けるか」の骨格。リダイレクト（点線）は認証ガード。
2. **In-page actions**: 画面内のボタンとフォーム。リンクではないので巡回では辿られない。**ここが flow を書く候補リスト**になる（「配車を依頼」「登録」「評価」など）。
3. **Storyboard**: flow の各ステップのスクショと、その間に発生した API 呼び出し。1 ルート内の状態（モーダル、進行中表示、到着画面）はここでしか見えない。
   「ステップ N → N+1 の間に呼ばれた API」が、その画面遷移を引き起こす API。これを app-map の「状態遷移を進める API」欄に転記する。

ルート数が少なく見えるアプリ（SPA）は、画面の実体が「データの状態」で決まっていることが多い。フロントのコードで状態名ごとのコンポーネント（例: ISURIDE の `routes/client._index/driving-state/*`）やステータス enum を探し、状態一覧を app-map の 3 節に書く。

### 4c. annotations を書く（Claude、5 分）— report の「今解決すべきこと」「画面マップ」「ルール」タブの材料

`docs/app-annotations.json` を書いて commit する（形は `tools/app-analyzer/examples/isupipe/app-annotations.json`）。

- `screens`: 画面のパス（巡回の path と同じ）→ `{ "purpose": "1 行の役割" }`。点に関係する画面は役割にそう書く。
- `endpoints`: `"METHOD /path"`（パラメータは全部 `:id`）→ `{ "purpose": "1 行の役割" }`。少なくとも点を生む POST、ポーリング、書き込み、外部副作用のある endpoint。
- `roles`（最初のベンチの後）: report の Flows の役割のキー（「POST /api/livestream/:livestream_id/livecomment + POST …」）→ 名前（「視聴者」）。ベンチの流れと画面マップのシナリオで使う。無ければ endpoint の役割から自動で名前を作る。
- `rules`: マニュアルの「守ること」を 1 行ずつ。`{ "text", "source": "day.md の見出し", "endpoints": [...], "tables": [...] }`。整合性の猶予（秒数）、304 の条件、変更禁止（bcrypt コストなど）、initialize の制限、DNS。修正担当とレビューのチェックリストになる。

書いたら `task app:refresh`（TS 無し、または `TS=none`。run が要らないので、ベンチ前でも annotations とホストの割り当てだけ反映される）→ `docs/reports/latest.*` を commit して push（ops パネルの `/app/` に 1 分以内に出る）。

### 4d. ベンチのたびに更新する（1 秒、巡回しない）

```
task app:refresh TS=<ts>      # run を取ってきて docs/reports/latest.html と latest.md の "Now" 節を描き直す
git add docs/reports/latest.html docs/reports/latest.md && git commit -m "app report <ts>" && git push
```

- push すると ops パネルの `/app/` に 1 分以内に出る（パネルは ops の repo クローンの `docs/reports/latest.html` を返す。毎分の cron pull で更新）。チームはパネルで見る。
- 毎回打たなくてよいように、ベンチの後に回す skill（parallel-fix の手順 5、deep-dive の手順 1、next-step の最初）に組み込んである。annotations だけ直したときは `task app:refresh`（TS 無し）。

- 開くのは **「今解決すべきこと」タブ**: 未解決のものだけを、ベンチの時間の割合 × 点との関係（スコアの POST ×3、そのループ ×2）の順に最大 7 件。各行に「直し方」（**parallel-fix で直る**: 作業単位つき / **設計から**: 担当を決める / **要調査**: deep-dive）と「やること」（日本語 1 行）、守るルール、SQL を発行したコードの行。上に report の Next step（1 件 1 行）。
- 「解決済み」は計測で決める: 前の run の action が今回の report から消えた、または重かった endpoint が軽くなった。最下部に畳む。
- ほかのタブ: 概要（構成と状態遷移だけ。初日に一度）、画面マップ（赤枠 = その画面が呼ぶ API のベンチの割合、★ = スコアの POST。シナリオ = ベンチの役割を選ぶと、その役割が使う画面に順番が付く）、ベンチの流れ（役割ごとのスコアのループと一番遅いステップ、離脱とその理由、前回に無かったものに「新」）、エンドポイント（コードの全ルート。method / アクターで絞る。行を開くと「壊してはいけない仕様」と計測した SQL）、データモデル（テーブルの表と endpoint × テーブル。今のコードから読む。ER はテーブル名をクリック）、ルール、画面（1 行 1 画面。開くとスクショとリクエスト）。
- Claude は `docs/reports/latest.md` の先頭の **Now** 節（`<!-- bench:start -->` から）を読めば、今の優先順位が分かる。

### 5. 共有（2 分）

- `docs/app-map.md` を repo にコミット。Discord に report.html と app-map の要点（ホットスポット上位 3 つ）を貼る。
- レーンで回すときは `setup/app-map` のブランチで PR にし、タイトルを `[fast] app-map` にして `[PR] #<n> ready [fast] app-map` をハブへ。**docs だけの PR は fast lane**（CLAUDE.md ルール 4 の d）なので、ハブは人の OK を待たずに `hub:ship` で merge できる（deploy・smoke は自動で飛ぶ。`ops:repo` だけ）。`docs/journal.md` には書かない（PR 本文に journal 用の 1 行）。
- merge の前でも、ops パネルの **`/app/?ref=<branch>`**（例 `/app/?ref=setup/app-map`）でそのブランチの `docs/reports/latest.html` が見られる。人に見てほしければそのリンクを返答に書く。merge の後は panel の App analysis のタブ（`/app/`）。
- baseline ベンチの後からは `task app:refresh TS=<ts>` で report に数字が入る（app-map.md に手で書き足さなくてよい）。

## 注意

- 収集中にアプリへ書き込みが発生する（flows）。ベンチ前に `POST /initialize` で戻る範囲なら問題ない。
- 巡回は滞在型なので、ボタン操作の先にある API は `flows` か Playwright MCP（Claude Code）で補う。flow では `await step("ラベル")` を状態が変わるたびに呼ぶと、その時点のスクショが Storyboard に並ぶ。
- 画面から観測できなかった API は report の「OpenAPI not observed」「Referenced in code but not observed」を見て、ルート表と突き合わせる。
