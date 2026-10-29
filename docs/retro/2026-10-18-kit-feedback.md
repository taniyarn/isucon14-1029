# isucon-kit / ops へのフィードバック（ISUCON14 練習 2026-10-18）

練習の流れ: kickoff → parallel-setup → parallel-fix ×2 → score-sprint（revert）→ technique-scan → parallel-fix → deep-dive → parallel-fix。最高 60,901 点（run `20261018-162817`、特別賞の目標 32,767 を超えた）。

## チームの気づき → 直し方の案

| # | 気づき | 確かめた事実 | 直し方（kit のどこ） |
|---|---|---|---|
| 1 | score-sprint が効かない。普通の改善を続ける方が良いかも | 今回は律速が DB（svc3 の 2 コア）。app を 2 台にすると 30,866 → 26,175 に下がった（PR #5 → revert #7） | `score-sprint` skill の手順 1 に「Verdict が app の CPU 律速のときだけ app×2。DB 律速なら計測 OFF だけ」を足す。計測 OFF の PR とその revert を kickoff の直後に用意しておき、すぐ入れられるようにする（RUNBOOK） |
| 2 | `task app:refresh` を毎回打つのが面倒 | refresh は run が必須で、ベンチ前には使えなかった | `parallel-fix` / `deep-dive` / `next-step` の「ベンチの後」の手順に refresh を入れる。run 無しでも annotations だけ反映できるようにする（`refresh.mjs`）。修正ループの中で回す skill の一覧を、RUNBOOK の進行表の 10:45 の欄に書く |
| 3 | ops report に目次と「上に戻る」が欲しい | — | `isumeasure` の report（md/html）の生成に、目次と各節の「↑ 目次」リンクを足す |
| 4 | app-analysis のレポートを ops で見たい | パネルは repo を読み取り専用でマウントしている（`tools/ops/compose/docker-compose.yml:41`）が、それを返す route が無い（`tools/isumeasure/panel.go` は `/data/` だけ）。毎分の cron pull は効いている | パネルに `/app/` の route を足し、`REPO_DIR/docs/reports/latest.html` を返す。refresh した html を push すれば、1 分以内にパネルで見られる。特別賞の進み具合など、ops にしか出ない表示もここで一緒に見られる |
| 5 | 衝突対策: エージェントが変更を始める前に、触るハンドラと層（SQL だけ / ロジックも）を txt に書いて main に入れる。エージェントは変更前に必ずそれを読み、衝突しないかを確かめ、自分の変更も書き込む | 今回の衝突は parallel-setup の B と D の 1 か所だけだった。ただ、他のセッションが同時に main を進めていた（PR #2, #4, #8, #9, #10）ので、重なる危険はあった | 宣言用のファイル `docs/claims.md`（ブランチ / ファイル・関数 / 層 = SQL・ロジック・設定 / 担当 / 状態）。エージェントは書き始める前に読み、自分の行を足し、終わったら状態を更新する。**このファイルだけ main への直接 commit を許す**ので、CLAUDE.md ルール 4 に例外を 1 行足す（要判断） |

## Claude（オーケストレーター）の気づき

| # | 何が起きたか | 直し方 |
|---|---|---|
| 6 | ops のクローン（`/opt/isucon-ops/repo`）に前回（ISUCON13）の作業ツリーが未追跡のまま残っていて、pull が黙って失敗していた。その結果、report が ISUCON13 の `alp.yml` で作られていた（`score_post` が livecomment、スコアのループが空）。直すのに手での ssh が 3 回要った（deploy key の `GIT_SSH_COMMAND` も手で） | `task ops:repo` を `fetch` + `reset --hard origin/main` + `clean -fd` にする。cron の pull も同じにする。report の冒頭に、ops の repo の HEAD と `alp.yml` の `score_post` を出す |
| 7 | `task hosts:measure-off` / `measure-on` が MySQL を再起動するとき、無効にしてある他の言語の実装（node / php / python / ruby …）まで restart で起動させた。8080 番を取り合い、Go の app が再起動を繰り返した | `tools/bootstrap/measure-off.sh` で restart するのを、動いていた依存ユニットだけにする。`deploy-init` が生成する deploy.sh で、使わない言語のユニットを disable する（今回は PR #8 で手で入れた） |
| 8 | `task smoke INIT=1` と analyzer は `/api/initialize` を body `{}` で叩く。この問題では `settings.payment_gateway_url` が空文字になり、手で試す評価が決済で失敗した（ベンチは自分で設定し直すのでスコアには影響しない） | analyzer の config に `initializeBody` を足し、`smoke.mjs` と `crawl.mjs` の両方で使う。kickoff phase 2 で、initialize が body を読むかを確かめて書く |
| 9 | `task deploy:init` が `etc_install etc/nginx/sites-enabled/isuride.conf` を生成した。repo の `sites-enabled/*` は `/etc/...` を指す絶対パスの symlink なので、repo の内容が入らない形だった | `deploy-init.sh` で symlink を `sites-available/<name>` に読み替え、`sites-enabled` には `ln -sf` を張る（今回手で直した形） |
| 10 | parallel-setup C の巡回データ（`tools/app-analyzer/out/`）が worktree ごと消え（worktree の掃除）、`out/` から report を作り直せなかった | app-analyzer の `outDir` を main の repo 側（絶対パス）にする。2 の「run 無しの refresh」でも代わりになる |
| 11 | 練習でベンチを別ホストから回したとき、ベンチの決済サーバー（`--payment-bind-port 12346`）が SG で閉じていて、評価が 10 s でタイムアウトした（クリティカル）。svc1 の DB の `payment_gateway_url` を見て気づいた | RUNBOOK の練習の付録に、ISUCON14 用のベンチのコマンドと「`--payment-bind-port` を VPC 内（172.31.0.0/16）に開ける」を足す |
| 12 | DB を svc3 に分けると、各ホストの `isuride-matcher`（自ホストの nginx を curl する systemd のワーカー）が同じ DB に対して多重に動く。マッチングはロック無しなので二重割り当ての危険があった（オーケストレーターが気づいて deploy.sh で svc1 だけにした） | `parallel-setup` A のプロンプトに「自ホストの app を叩く systemd のワーカー・timer は 1 台に寄せる」を足す。kickoff phase 2 の「初動の注意」にも、付随ワーカーの台数を書く欄を作る |
| 13 | スプリントで計測を止めたベンチは run が残らず、何が下げたのか（26,175）を後から調べられなかった | score-sprint の手順に「計測 OFF のベンチの前後で、同じ構成の計測 ON の run を 1 本ずつ取る」を足す。1 と合わせて、計測 OFF は最後の確認のときだけにする |
| 14 | deep-dive で一番効いた発見は「時間ごとの件数の変化」（coordinate が 9,049 → 3,685 / 10 s、マッチングが 38 → 3〜4 回 / 10 s）だった。summary.md の表は区間全体の平均なので、この変化は見えない | report に「10 秒ごとの件数と avg」（上位 endpoint と internal）の表を足す。queries.md §1 のクエリを昇格させる候補 |

## parallel-fix セッション（PR #2, #4, #6, #8, #9, #10, #12, #15）の気づき

既にある行への追加:
- **7 への追加**: デプロイの `systemctl restart isuride-go` でも同じことが起きた（PR #6 のデプロイで svc1 が落ちた → PR #8）。Go の app は `ListenAndServe` のエラーを捨てる（`main.go:27`）ので、何もログを出さずに終了する。`task host:diag` の listening にはプロセス名が無く、誰が 8080 番を握っているのかが分からなかった。
- **5 への追加**: 実際に衝突が起きた。PR #15（ridecache）と PR #16（notification を 1 本のクエリにする変更、matching を 1 つの tx にまとめる変更）が同じ 3 つの関数を書き換えていて、merge の時点で 3 ファイルが衝突した。サブエージェントに差し戻して解消した。
- **11 への追加**: bench の private IP は `.env` の `BENCH_HOST` がコメントアウトされていて `task hosts:ips` に出てこないので、`host:peek HOST=<public>` で調べた。決済の戻り経路（アプリ → bench:12346）は、bench のコマンドを組むときに一緒に表示するのがよい。

| # | 何が起きたか | 直し方 |
|---|---|---|
| 15 | `task smoke` は、Go 以外の実装（他言語の unit）が 8080 番で応答していても exit 0 を返した。出力も `initialize -> 200` の 1 行だけで、何を確かめたのかが分からない | smoke で「Go の app が答えている」ことを確かめる（`/api/initialize` の `language`、または計測パッチのヘッダ）。確かめた件数と失敗した件数を 1 行で出す |
| 16 | `task db:query` は、SQL に `'…'` を含めると壊れる（ssh の `'…'` の中にそのまま埋め込んでいる）。`DB` を省くと DB を選ばない状態で流れる | SQL を stdin（または base64）で渡す。`DB` の既定値を env.sh の DB 名（`ISUCON_DB_NAME` など）にする |
| 17 | `task host:diag` の journal が、ExecStop の `kill $MAINPID` の usage（`$MAINPID` が空で失敗する）で埋まり、app の行が流れて読めない | diag で `kill[` の行と usage を落とす。`UNIT=` を指定したときは、その unit の行だけを出す |
| 18 | `init.sh` で `indexes/apply.sh` を呼ぶ処理が別の PR（#1）に入っていて、それが merge されるまで、index は `/initialize`（DROP/CREATE）のたびに消える状態だった | kickoff phase 2 の確認項目に「init.sh が DROP/CREATE するなら apply.sh を呼ぶ」を入れ、kickoff の commit に含める（PR にしない） |
| 19 | スキーマを変えた PR（#6 の `chair_location_summaries`）は、デプロイしてから `/initialize` が走るまで、新しいテーブルが無く coordinate が 500 になる | `task deploy` が `webapp/sql/*.sql` / `init.sh` の変更を検出したら「`task smoke INIT=1` を流すこと」と表示する（または自動で流す） |
| 20 | `task worktrees:clean` が別のセッションの worktree（`../wt-kickoff-init`）も消した。merge 済みだったので実害は無かった | 未コミットの変更がある worktree は消さない。`.claude/worktrees/` の外のものは一覧に出すだけにする |
| 21 | `docs/journal.md` が template に無く、最初の記録のときに書式から決めた | template に、見出しと書式の行（時刻 / branch・PR / 何を / なぜ / 結果）だけを入れた journal.md を置く |
| 22 | parallel-fix の skill に、1 グループに収まらない構造変更（要約テーブル: 7 ファイル、ridecache: 6 ファイル）の進め方が無かった | 「設計を書いて OK をもらう → 全ファイルを 1 つのサブエージェントに任せる → 書き込み箇所を grep で突き合わせてからレビュー」の型を手順に足す |
| 23 | index の提案に存在しない列が出た: `rides(chair_id, status, updated_at)`（status は ride_statuses の列）、`chairs(is_active, ride_id, chair_id)`（ride_id は rides の列）。JOIN 先の列を、FROM のテーブルの列として扱っている | 提案を出す前に、DB スナップショットの `information_schema.columns` と突き合わせて落とす |
| 24 | 計測エージェント自身のクエリが、SQL の上位と N+1 の疑いに出た（`SHOW GLOBAL STATUS` が svc1 の SQL 時間の 72%、`@@version_comment`、processlist、`COUNT(*)`）。plan の `sql:unmapped` グループは、5 件中 3 件がこれだった | エージェントの接続を別ユーザーにするか、SQL にコメントのタグを付けて、集計から除く |
| 25 | actions に重複が出た（`rides(chair_id, created_at)` が 2 回、matching の Speed up が 2 回、「CPU is idle」が 3 回） | actions.json を (group, title, query) で重複を除いてから出す |
| 26 | 本当の律速が report から読めなかった: matching が 0.5 秒に 1 件しか割り当てない（60 秒の負荷で最大 120 件しか成立しない）。report には「`GET /api/internal/matching` の平均 1ms がリクエスト時間の 100%」という P1 が出ただけだった。作り直しはオーケストレーターがコードを読んで決めた（PR #4） | 定期的に叩かれる内部 endpoint（`/api/internal/*`）は Speed up の対象から外し、別枠に「呼び出しの間隔 × 1 回あたりの処理件数」と、待っている件数（app-annotations で指定したクエリ）を出す |
| 27 | ポーリングの action が P2 のままだった。実際には `retry_after_ms` を 30 → 100/200ms に上げたら 35,661 → 48,381（+36%）と、一番効いた（PR #10） | レスポンスにポーリング間隔のフィールド（`retry_after_ms` など）があり、値が小さい（≤ 100ms）なら P1 に上げ、フィールド名と今の値を action に書く。マニュアルの許容時間（ここでは 3 秒）を app-annotations から引いて並べる |
| 28 | `SELECT … FOR UPDATE` による deadlock（PR #6 で入れた、要約テーブルの行がまだ無い状態での gap lock）は、report の Errors に「coordinate の 500 が 46 件、Deadlock found」と出たが、どの文同士がぶつかったかは出なかった | `SHOW ENGINE INNODB STATUS` の LATEST DETECTED DEADLOCK の 2 文を、sqltag でハンドラ・行に結び付けて Errors の行に付ける |
| 29 | svc1 の profile に cpu が無く、「Enable pprof」の action が出た。計測パッチは 127.0.0.1:6060 で pprof を出しているので、取れなかった理由が分からない（svc2 / svc3 では cpu が取れている） | エージェントが profile を取れなかった理由（接続先、タイムアウト、ステータスコード）を report に出す |
| 30 | index を貼った後もまだ遅いクエリ（`ride_statuses … app_sent_at IS NULL ORDER BY created_at`、×42 rows）の診断が「range / LIKE / sort」の一般論だけだった | そのクエリの EXPLAIN（使われた key、rows、Extra）を action に付ける |

## 進め方（案）
- 変更先は `ytakaya/isucon-kit`（手元 `../isucon-kit`、`de5949f`）。テーマごとに 3 本の PR にする。
  - (a) skill と RUNBOOK: 1, 2, 5, 11, 12, 13, 18, 22
  - (b) ツール: 7, 8, 9, 10, 6 の task 側, 15, 16, 17, 19, 20, 21
  - (c) isumeasure と ops パネル: 3, 4, 14, 6 の report 側, 23〜30
- 優先順は (b) → (a) → (c)。(c) は ops のパネルの再ビルドが要る（人が実行）。
- 練習は終わったので、この contest repo には同期しない。
- 要判断: 5 の「`docs/claims.md` だけ main への直接 commit を許す」例外。
