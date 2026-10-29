---
name: kickoff
description: 競技開始直後、当日マニュアルとアプリケーションマニュアルを Claude が最初に読み、インフラ構成・禁止事項・アクセス方法・ログイン・スコア式を把握して、チームが使う設定（.env の補足、analyzer.config.json、CLAUDE.md の冒頭、docs/kickoff.md）を作る手順。人は sudo が要る操作だけ指示を受けて実行する。
---

# kickoff

## 2 つのフェーズ

| phase | いつ | 入力 | 出力 |
|---|---|---|---|
| **1** | 10:00、サーバーに入る前 | マニュアル 2 本の **URL**（Claude が `docs/manual/day.md`、`app.md` に保存して commit） | `docs/kickoff.md` の下書き（アクセス、ログイン、スコア、猶予、禁止事項、初期化 API、言語ディレクトリ、去年との差分、**末尾の ```analyzer ブロック**（§1c））、`.env` の提案（`TRIGGER` `LANGS` `HOST_LOGIN`）、人がやること |
| **2** | `task hosts:setup` → `git pull` の後 | 上に加えて `etc/`、`env.sh`、`webapp/`、`docs/server-info-*.txt` | **ハブが最初に** `task analyzer:init`（`tools/app-analyzer/analyzer.config.json`、doctor 済み。§2）。続けてサブエージェントが `docs/kickoff.md` 完成（**前提の確認**、構成要素、データの置き場所、初動の注意、**先読みの手**）、前提外れの alert、`CLAUDE.md` の冒頭、`alp.yml`、人がやること |

実行するのはレーン 1（ハブ。`docs/lanes/1-hub.md`）。phase 1 は起動の 1 行（マニュアルの URL 付き）で、phase 2 はレーン 2 の `[DONE] servers` → `git pull` →（baseline ベンチの todo は hosts:setup が 0 NG なら盤が作り済み。無いときだけハブが作る。どちらも `task runs:wait AFTER=<panel の一番新しい ts>` を背景で）→ **ハブ自身が `task analyzer:init` と `task app:size OUT=docs/app-size.md`（1 分以内。doctor が通れば config と app-size.md を main に commit + push、例外 a）** と、**同時に人への todo「アプリの規模を見る」**（`task lane:todo LANE=1 MSG="アプリの規模を見る（ターミナルで実行し、出力の先頭を読む）" CMD="task app:size" WHY="app-map の前に行数・ルート数・テーブルと初期行数・外部依存を自分の目で知る" AFTER="読んだら ✅"`。ハブの返信は流れるので、人が自分で打って見る。2026-10-28 の振り返り） → 残りを**バックグラウンドのサブエージェントで**始める（2026-10-28: config を phase 2 の最後（8 分のサブエージェント）で作り、URL → app-map 完成が 23 分）（baseline は phase 2 を待たない。deploy しない初期状態で測る）。**phase 2 のサブエージェントは main の checkout で作業しない**: `git worktree add ../wt-kickoff2 origin/main` で作業し、commit を `git push origin HEAD:main`（衝突したら `git pull --rebase origin main` してから）で main に入れ、終わったら worktree を消す。ハブは push の後に main の checkout で `git pull`（2026-10-27 夜: phase 2 が main の checkout を書き換えている最中に `hub:ship` が流れ、「未 commit の変更あり」で 2.5 分止まった）。`task deploy:init` と `deploy.sh` は phase 2 ではなくレーン 2（インフラ）が `[DONE] servers` の前に作る。各 phase の始めと終わりに `task lane:status LANE=1 STATE=working|idle STEP="kickoff phase <n>"`。**ロードマップの step も**: phase 1 の始めに `task lane:step ID=H1 STATE=doing`、phase 1 の commit を push したら `task lane:step ID=H1 STATE=done`。phase 2 も同じく `ID=H2`（サブエージェントで回すときはサブエージェントが打つ）。H3〜H5 は `actions:plan` / `hub:ship` が自動で付ける（2026-10-25 は打たず、盤のハブの step が全部 ready のままだった）。人に頼むことは全部 `task lane:todo`（§3）、前提外れは `task lane:alert`（§1a）。

人の操作は「Claude にマニュアルの URL を 2 つ渡す」「phase を指定して一言」だけ。保存は Claude がやる。
- phase 1:
  ```
  kickoff skill の phase 1 を実行して。マニュアルはこの 2 つ:
  当日マニュアル: <URL>
  アプリマニュアル: <URL>
  ```
- phase 2: **「kickoff skill の phase 2 を実行して（先に task analyzer:init、残りは repo を読んで kickoff.md を完成、CLAUDE.md の冒頭、alp.yml）」**

## Claude がやること（phase 1 は表の 1〜3 行目と「スコア」「禁止・必須」「去年との差分」と §1c の analyzer ブロックだけ。phase 2 で残りを埋める。各項目は根拠のファイル名を添える）

### 0. マニュアルを repo に取り込む（phase 1 の最初）

URL を受け取ったら `docs/manual/day.md`（当日マニュアル）と `docs/manual/app.md`（アプリケーションマニュアル）に保存して commit する。元の URL は各ファイルの 1 行目に `<!-- source: URL -->` で残す。

- GitHub の `blob` URL は raw に直して取る: `https://github.com/<o>/<r>/blob/<ref>/<path>` → `https://raw.githubusercontent.com/<o>/<r>/<ref>/<path>`（`curl -fsSL`）。
- gist も同様に raw URL。HTML しか無いページは本文をテキスト化して保存（WebFetch で読んでから書き出す）。
- ポータルのようにログインが要る URL は取れないので、**人にコピーしてもらう**（「ポータルの本文を貼ってください」と頼み、貼られた本文をそのまま保存）。
- 保存したら `git add docs/manual && git commit -m "manuals" && git push`。**phase 1 と 2 の成果物（`docs/manual/`、`docs/kickoff.md`、`CLAUDE.md` の冒頭、`deploy.sh`、`alp.yml`、`analyzer.config.json`、`webapp/sql/init.sh` 末尾の `indexes/apply.sh` の 1 行）は main に直接 commit してよい**（CLAUDE.md ルール 4 の例外 a: 10:00〜10:25 はまだ誰のブランチも無く、人がその場で見ている。例外 b は `docs/claims.md` だけの commit、c は `docs/journal.md`（ハブだけ）、d は練習専用の `practice/` の変更（レーン 2、練習のときだけ））。それ以外のコードは例外にならない。

### 1. 事実を集めて `docs/kickoff.md` に書く（60 行以内、表中心）

phase 2 では「言語ディレクトリ」の行に **`docs/app-size.md` へのリンクと 1 行の要約**（例: `Go 2,484 行 / ルート 18 / テーブル 11`、`task app:size` の出力から）を書く。行数・テーブルの数字を kickoff.md に写さない（app-size.md が正）。

| 節 | 何を書くか | どこを見るか |
|---|---|---|
| アクセス | ブラウザで開く URL、必要な名前解決（hosts 行 or 本ツールの `resolve`）、TLS（自己署名か）、ベンチが使うホスト名 | 当日マニュアルの hosts/DNS 節、`etc/nginx/sites-enabled/*`（`server_name`, `ssl_certificate`, `root`, `proxy_pass`）。**マニュアルのドメインと実機のドメインを必ず突き合わせる**（例: ISUCON13 は本番 `.dev`、AMI `.local`）。判定は機械的に: nginx の `server_name`、コードの Cookie Domain、`pdnsutil`/ゾーン名の 3 つが一致する方が正。人がやることに「自己署名証明書なので Chrome では `thisisunsafe` と打つか、シークレットウィンドウで開く」を入れる（about:blank と区別が付かず迷う） |
| 構成要素 | app ユニット、DB、nginx、それ以外（DNS、外部 API モック、ワーカー、メディアサーバー）。**各要素が何にデータを置くか**、**設定ファイルを読む別プロセス**（例: app が exec する `pdnsutil` が `/etc/powerdns/pdns.conf` を読む → 権限を変えると壊れる）（MySQL / SQLite ファイル / メモリ）。DNS やモックが MySQL や SQLite を使っていれば負荷の行き先が変わる。**付随要素ごとに「自前実装の候補か」を一言**: マニュアルが仕様を狭く書いている（「A レコードのみ」「再帰しない」「スコア外」など）要素は、チューニングより最小実装での置き換えが速いことがある（TECHNIQUES T41。13 の DNS） | `etc/systemd/system/*.service`、`env.sh`、`docs/server-info-*.txt` の `detected:` / `NOTE:` 行（SQLite ファイル、pdns の `launch=`、パッケージ）、`etc/powerdns/` 等、マニュアルの構成図。app-analysis のレポート冒頭「System」にも同じ内容が自動で出る |
| ログイン | 認証方式（Cookie/Bearer/フォーム）、ログイン API、初期ユーザーと**パスワード**、登録 API | ルーター（`main.go` の `e.POST("/api/login")` 等）、`webapp/sql/initial_users.sql`。bcrypt のコストが低ければ `test` / `password` / `isucon` を照合して当てる（Go 数行）。当たらなければ登録 API で新規ユーザーを作る方針にする |
| 画面 | フロントのルート一覧（SPA のバンドルから） | `curl -sk --resolve <host>:443:<ip> https://<host>/` → `/assets/index-*.js` を取り `grep -o 'path:"[^"]*"'` |
| スコア | スコア式、fail 条件、整合性の猶予（「N 秒以内に反映」など）、タイムアウト、**特別賞の目標スコア**（「最初に N 点に到達した 1 チーム」。例年 15,000〜80,000 点、達成は昼前後） | アプリマニュアル、当日マニュアルの「特別賞」節 |
| 禁止・必須 | 変更禁止ファイル、SG の制約、再起動後の要件、終了後の操作禁止、初期化コマンド | 当日マニュアル |
| 去年との差分 | 前回の構成と違う点（DB の種類、DNS の有無、外部サービス、言語ディレクトリ、ポート） | `docs/strategy-2026.html` の想定、`CLAUDE.md`、server-info |
| レビューで見ること | クラウドの自動レビュー（`docs/cloud-review.md`）とレーン c のレビューが読む、**この大会固有の観点を 5 行以内**: 整合性の猶予（「N 秒以内」の API と秒数。書いていないものは即時）、応答で特に比べられるもの（並び順、件数、画像の同一性など）、触ってはいけないファイル・設定、失格・FAIL の条件で code が関わるもの、外部サービスと呼び出しの約束（タイムアウト、冪等キー）。見出しは `## レビューで見ること` のまま変えない（プロンプトが名前で探す） | アプリマニュアル、当日マニュアルの「整合性」「失格」「禁止」節 |
| 初動の注意 | 計測で壊しやすい所（例: DB 再起動で DNS も落ちる、`init.sh` が DNS ゾーンも初期化する、journald に全リクエストが出ている）。**アプリが DNS を返す年は「DNS の答えの IP」を必ず確かめる**（`task host:diag NAME=<app のホスト名>` の dns 節。`env.sh` の `*_SUBDOMAIN_ADDRESS` 類が `127.0.0.1` のままだと、別ホストからのベンチが「サーバーリストに含まれていません」で 0 点）。**`init.sh` / `init.sql` が TRUNCATE か DROP/CREATE か**（DROP なら index が消えるので `init.sh` の末尾で `bash indexes/apply.sh` を呼ぶ。phase 2 のチェック項目、下の §2）。**`/initialize` が body を読むか**（読むなら何を。下の §2 の `initializeBody`）。**付随ワーカー**（自ホストの app / nginx を叩く systemd のワーカー・timer。例: ISUCON14 の `isuride-matcher` は自ホストの nginx を curl する）の名前と、今それが何台で動いているか。DB を共有したら 1 台に寄せる（parallel-setup A が deploy.sh で他のホストを disable する）。ロック無しの処理を全台で回すと二重処理になる | `init.sh`、ユニットの `Requires=`、`main.go` の Logger/Debug、`env.sh`、`etc/systemd/system/*.service` / `*.timer`（`ExecStart` に curl / localhost） |

### 1a. 前提の確認（phase 2 の最初、3 分。外れたら alert）

kit は「MySQL / MariaDB、`webapp/go`、systemd のユニット、前に nginx、3 台、マニュアルどおりのドメインとポート、initialize の制限時間が書いてある、外部依存なし」を前提にしている。今年もそうかを **表にして `docs/kickoff.md` の節「前提の確認」に書く**。根拠は必ずファイルと行（`docs/server-info-*.txt` の `detected:`、`etc/systemd/system/*.service` の `ExecStart`、`etc/nginx/`、`webapp/` の `ls`、マニュアルの見出し）。

```
## 前提の確認
| 前提 | 期待 | 見つけたもの | 根拠 | 判定 |
|---|---|---|---|---|
| DB | MySQL / MariaDB | PostgreSQL 16 | server-info-svc1.txt `detected: postgresql` | ✗ |
| 言語ディレクトリ | webapp/go（go.mod） | webapp/go | `ls webapp` | ○ |
| app の起動 | systemd のユニット | isuride-go.service | etc/systemd/system/isuride-go.service | ○ |
| 前段 | nginx | nginx 1.24 | etc/nginx/sites-enabled/isuride.conf | ○ |
| 台数 | 3 | 3 | day.md「競技環境」 | ○ |
| ドメイン / ポート | マニュアルどおり | `server_name` 一致、443 | nginx conf / day.md | ○ |
| initialize の制限時間 | 書いてある | 書いていない | app.md「初期化」 | ✗ |
| 外部依存 | 無し | 決済サーバー（ベンチ側） | app.md「決済」 | ✗ |
```

✗ の行ごとに `task lane:alert LANE=1 LEVEL=<crit|warn> MSG="<見つけたもの>（<根拠>）" IMPACT="<動かない kit の部品>"` を出す。IMPACT は `docs/lanes/README.md` §3 の表から写す（例: PostgreSQL → slow log / indexes/apply.sh / db:query / sqltag の driver / zz-perf.cnf / parallel-setup A）。次の手順（baseline、parallel-setup）が使えなくなるものは crit で、`[STOP]` が要るかを人に聞く todo も作る。○ だけなら節に「全部 ○」と書いて進む。レーン 2 が `task host:peek` で同じ alert を先に出していたら（`task lane:board`）重ねて出さず、根拠を足すだけ。

### 1b. 先読みの手（phase 2、5 分。レーン c に渡す）

計測を待たずに **マニュアルとコードだけで効くと言える手** を `docs/kickoff.md` の節「先読みの手」に表で書く。2026-10-18 の練習（ISUCON14）で一番伸びた 2 手（マッチングの作り直し 8,188 → 30,866、`retry_after_ms` 30 → 100/200 ms で 35,661 → 48,381）はどちらもこれで読めたのに、report には P1 で出ず、入れたのは 3〜4 回目のベンチの後だった。型ごとの候補と実例は `docs/FIRST-HOUR.md`。

(a) マニュアルが **自由や余裕をくれている文**（`docs/FIRST-HOUR.md` の grep をそのまま使う）:

| 文言の型 | 例 | つながる手 |
|---|---|---|
| 「自由に変更してよい」 | 14 内部の matching | T34 / T35 |
| 「クライアントは〜を待つ / retry する」 | 14 `retry_after_ms`、12q 429 + Retry-After | T17 / T36 |
| 「N 秒以内に反映」「N 秒古くてよい」 | 14 座標 3 秒、10f dashboard 1 秒 | T5 / T3 / T9（猶予に触る → 秒数を書く） |
| 「〜は不要」「スコアに含まれない」「〜のみ」 | 13 DNS（A レコードのみ、スコア外） | T41 / T27 |
| initialize のパラメータ | 9q `campaign`、9f `available_days` | T37 |
| 「bot に 503 を返してよい」「429 を返してよい」 | 10q bot | T17 |

(b) 参考実装の **見れば分かる律速**（`grep` で当てる。全ハンドラは読まない）: `ORDER BY RAND()`、固定間隔のワーカー・ポーリング（`time.Sleep`、ticker、systemd timer の curl 間隔）、1 回に 1 件しか処理しないワーカー、外部 HTTP を tx の中で呼ぶ、極端に短いポーリング間隔の定数（`retry_after_ms: 30`）、目に見えるループの中の SELECT（N+1）、WHERE に出る列に index が無い（`webapp/sql/*.sql` の CREATE TABLE と突き合わせ）。

書く形（最大 8 行。期待効果の大きい順）:

```
## 先読みの手（マニュアル + コード。計測前）
| # | 手 | 根拠（マニュアルの見出し / file:line） | 期待効果 | リスク（猶予・クリティカル・再起動） | T# | 担当 |
|---|---|---|---|---|---|---|
| 1 |（例）matching を「待ち全件 × 空き全件の最近傍」に | app.md「マッチング」（自由に変更可）/ internal_handlers.go:<行> `ORDER BY RAND() LIMIT 1` | 1 回 1 件 → 全件。マッチ待ちの不満が減る（推測） | matcher が複数ホストで二重に動く | T34 | レーン c |
```

- 期待効果は「何が何倍になるか」を言葉で。数字はコードから言えるもの（間隔、1 回の件数）だけ。それ以外は「推測」と書く。
- 猶予に触る手（T3 / T5 / T9 / T17 の間隔）は、マニュアルの秒数をリスク欄に書く。書けなければ担当を「要確認」にする。
- index と N+1 は書いてよいが、担当は「レーン 3」（baseline の report が提案し、parallel-fix がやる）。担当「レーン c」にするのは report に出にくい手（ワーカー、間隔、外部 API、initialize のノブ、付随ミドルウェア）。
- 書いたら kickoff の commit に含める（main に直接。例外 a の範囲の `docs/kickoff.md`）。レーン c（`docs/lanes/c-scout.md`）はこの節と technique-scan の早期モードから始める。

### 1c. analyzer ブロック（phase 1 の最後、2 分。`task analyzer:init` が読む）

`docs/kickoff.md` の末尾に、マニュアルから機械的に取れる巡回の入力を JSON で書く（読めないと init は警告を出して guest だけの config になる）。値ごとの根拠は `source` にマニュアルの見出し。

````
```analyzer
{
  "domain": "isuride.xiv.isucon.net",
  "initializeBody": { "payment_server": "http://localhost:12345" },
  "source": "app.md「初期データ」「決済マイクロサービスのモック」",
  "personas": [
    { "name": "user", "group": "app", "token": "<sessionToken>", "seedPrefix": "client", "viewport": { "width": 390, "height": 844 }, "source": "app.md 利用者" },
    { "name": "owner", "token": "<sessionToken>", "source": "app.md オーナー" },
    { "name": "alice", "login": { "path": "/api/login", "body": { "username": "alice", "password": "pass" } }, "source": "app.md ログイン" }
  ]
}
```
````

- `domain`（必須）: ベンチ / ブラウザが使うホスト名。`scheme` は既定 `https`、サブドメインを使う年は `"subdomains": true`。
- `personas`: `token`（Cookie の値。Cookie 名はコードの `Cookie("x")` / `Name: "x"` から init が引く。`group` = `/api/<group>/` の group、省略時は name と同じ）か `login`（API ログイン）。名前が分からない Cookie は `"cookie": "<name>"`。`seedPrefix` は SPA のパスの先頭（例 `client` → `/client/...` がこの persona）。guest は自動で足す。
- `initializeBody`: initialize が body を読むときだけ（§2）。phase 1 で分からなければ省略し、phase 2 で足して `task analyzer:init FORCE=1`。

### 2. 設定を作る（phase 2）

- `tools/app-analyzer/analyzer.config.json`（**持ち主は kickoff / ハブ**。app-analysis（レーン 3）は読むだけで新規作成しない。2026-10-25 は両方が新規作成して衝突し、app-map の PR が rebase で遅れた）: **phase 2 の最初にハブが `task analyzer:init`** で作る（§1c のブロック + `webapp/` のコード + `HOSTS` の 2 台目から機械的に。既存は `FORCE=1` の時だけ上書き）。doctor が NG のときだけ下の項目を手で直す: `baseURL`、`resolve`（ホスト名→**svc2** の IP。ベンチ対象の svc1 には向けない。これで `/etc/hosts` 不要）、`initialize`（`/api/initialize`。ベンチ未経験のホストは DB が空）、`initializeBody`（initialize が body を読むときだけ。下の項目）、`personas`（guest + ログイン済み。`login: {path, body}` で API ログイン）、`seeds`（`npm run routes -- <baseURL> --ip <svc2 ip>` の出力から。`:id` は実在する id に）、`codeDir` / `schemaPath`、`authCheck`。`task analyzer:init` の最後に doctor が走る。全部 OK にする（失敗は自分で直す。人に聞くのは資格情報が本当に無い時だけ）。OK になったら config だけを main に commit + push し、サブエージェントを待たずにレーン 3 が巡回に進める。
- `alp.yml`（repo 直下）: `task groups`（= `isumeasure groups -code webapp/go -out alp.yml`）で生成して commit。ルーターから endpoint のグループ（alp 互換の `matching_groups` + 表示名）を作る。これが無いと ops の report は `/api/user/<name>/icon` をユーザーごとに別行にして、最重量 endpoint が上位 10 件から消える（2026-10-13 の練習）。固定パスがパラメータ付きの兄弟より上にあるか目で確認。ルートが増えたら作り直す。末尾の `bench_patterns:` にはマニュアルや bench の文言から「負荷が上がった / ユーザーが増えない / 不満の内訳」のような行の正規表現と意味を書く（report の Bench output が色を付ける）。`score_post:` にマニュアルのスコア式で点を生む POST（表示名のまま。例 `POST /api/livestream/:livestream_id/livecomment`）を書く。管制盤の Score timeline に score ÷ その件数の列が出る。`prize_target:` に特別賞の目標スコア（数字だけ。例 `prize_target: 50000`）を書く。report の冒頭に「目標の何 % か」が出て、75% を越えると Next step が `score-sprint` skill（計測 OFF で 1 ベンチ。app ×2 は app の CPU 律速のときだけ）を勧める。
- `CLAUDE.md` の「This contest」節（アプリ名と unit、付随コンポーネント、サーバーの役割、スコア式 3 行）。書いたら **`task agents:md`** で `AGENTS.md`（Codex のレーンが読む写し）を作り直し、同じ commit に入れる。
- **`init.sh` が DROP/CREATE するなら（`init.sql` の `DROP TABLE` / `CREATE TABLE`、スキーマの再投入）、`webapp/sql/init.sh` の末尾に `bash "$(dirname "$0")/indexes/apply.sh"` を足す**。kickoff の commit に含めて main に直接入れる（別の PR にしない。2026-10-18 の練習では別の PR に入っていて、merge されるまで index が `/initialize` のたびに消えていた）。`task deploy:init` が「NOTE: add 'bash $(dirname $0)/indexes/apply.sh'…」と出したら必ずこれ。
- **`/initialize` が body を読むか**: initialize ハンドラが `c.Bind` / `json.NewDecoder(r.Body)` などで body を読むなら、そのフィールドと、ベンチが送る値（マニュアル）を kickoff.md に書き、analyzer config の `initializeBody`（JSON オブジェクト、既定 `{}`）に入れる。`task smoke INIT=1` と巡回はこの body で initialize を叩く。空のままだと設定が空文字で上書きされることがある（ISUCON14: `payment_server` が空 → `settings.payment_gateway_url` が空になり、手で試す評価が決済で失敗した）。
- `deploy.sh` は**レーン 2（インフラ）**が `[DONE] servers` の前に `task deploy:init` で作っている（`docs/lanes/2-infra.md`）。phase 2 では、生成された `deploy.sh` が systemd ユニットと合っているかを読むだけ（ズレていれば alert と `[TASK] 2`）。app が Docker（`docker compose up`）で動いているなら、ネイティブ起動への切り替え手順を kickoff.md に書く。（1 枚で回すときは従来どおり自分で `task deploy:init`）
- `.env` に入れる値は**提案だけ**する（phase 1: `TRIGGER` の初期化パス、`LANGS`、`HOST_LOGIN`）。`.env` は人が編集する。現在値は `task env:show`（秘密は伏せて表示）で読める。

### 3. 人にやってもらうことを todo にする（phase 1 で 2〜4 件、phase 2 で 0〜2 件）

sudo や外部操作が要るものだけ。**1 件ずつ `task lane:todo` にし、同じ内容を返答にも書く**（文章で頼むだけにしない。形は `docs/lanes/README.md` §4: MSG = そのまま貼れるコマンドか画面操作、WHY、AFTER）。**それ以外は自分で終わらせる。**

phase 1 で作る todo の例:

| MSG | WHY | AFTER |
|---|---|---|
| `.env` に `TRIGGER=<初期化のパス>` `LANGS=<言語>` `HOST_LOGIN=<ユーザー>`（提案値） | Claude は .env を書けない。`hosts:setup` が使う | レーン 2 のタブに `done` |
| ポータルで「<確かめること>」（例: initialize の制限時間、ベンチの向き先の選び方、特別賞の目標） | マニュアルに無い / 曖昧 | 答えをレーン 1 のタブに |
| 自己署名証明書: Chrome では `thisisunsafe` と打つ | about:blank と区別が付かず迷う | 何もしない |
| （練習）手元 PC の社内プロキシ（Netskope など）を切る | 手元から svc1 への `curl` / `dig` が TLS 中継で Empty reply になる（2026-10-27） | ✅ を押すだけ |

phase 2 で作る todo の例（IP は `task hosts:ips` で分かる）: `sudo sh -c 'echo "<svc1 の public IP> <ホスト名>" >> /etc/hosts'`（WHY: ブラウザで目視する。巡回は `resolve` で要らない / AFTER: 何もしない）、SG を開ける（外部依存の戻り経路。AWS コンソールの手順を MSG に）。

### 3b. phase の終わりに

- phase 1: マニュアルと kickoff.md の下書きを push → `task lane:step ID=H1 STATE=done`。
- phase 2: analyzer config は最初に push 済み（レーン 3 の巡回はそれで始まる）。サブエージェントが kickoff.md・CLAUDE.md の冒頭（+ `task agents:md` の `AGENTS.md`）・`alp.yml` を push → `task lane:step ID=H2 STATE=done NOTE="<commit>"`。

### 4. 次のステップを 1 行で

「`app-analysis` skill に進める（config は `doctor` 済み。レーン 3 がやる）」か「ブラウザ巡回は無理なので code-only で app-map を書く」のどちらか。ハブは baseline の ts が来たら `[TASK] 3 fix TS=<ts>` と `[TASK] c technique-scan TS=<ts>` を送る（`[TASK] 2 setup` / `[TASK] 3 app-map` は `[DONE] servers` で送り済み）（`docs/lanes/1-hub.md`）。

## 出力の置き場

- `docs/kickoff.md`（commit する。全員と全 AI セッションが読む）
- `tools/app-analyzer/analyzer.config.json`（commit する。資格情報は初期データのものなので可）
- `CLAUDE.md`（更新）

## 予算

マニュアル 2 本と上記ファイルだけ読む。`webapp/go/*.go` はルーター（`main.go`）とログイン・登録ハンドラのみ。先読みの手（§1b）は `docs/FIRST-HOUR.md` の grep で当たった行の前後だけ読む。全ハンドラの読解は `app-analysis` の仕事。
