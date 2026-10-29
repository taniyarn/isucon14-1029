# practice-log の記録（schema_version 1）

1 練習 = 1 ファイル `practice-log/<date>-<slug>.json`（date は `YYYY-MM-DD`、同じ日に 2 回なら `2026-10-27a` / `b`）。作るのは `task practice:record`（`tools/lanes/practice.py`、数字は `lane:timeline` と同じ関数）。**出せない値は null**（0 と書かない、推測で埋めない）。時刻は手元のローカル時刻。

起点 = **URL 受信**（盤の t3 を送った時刻。無ければ roadmap H1 が doing になった時刻。盤が無い練習は `url_at=` で人が渡す）。終点 = 盤の最後のイベント。費用・ターン・idle・人待ちは起点からの分だけ数える。**特別賞の時刻** = 目標以上の score を最初に知った時刻（`run_scored`。盤が無ければ run の開始時刻）。

| 節 | フィールド | 定義 |
|---|---|---|
| meta | date, slug, contest_repo, problem, prize_target | 練習の識別。prize_target は contest repo の `alp.yml` |
| | kit_commit / kit_head | contest repo の `merge template` の commit / 記録した時の kit の HEAD（後から埋めた記録は null） |
| | models, model_config | レーンごとのモデルと fast（`tools/lanes/sessions.json`、`models=` で上書き）、その短い表記（散布図の色） |
| | bench_machine, hosts | ベンチ機（自由記述）、`.env` の HOSTS の台数 |
| | notes.changed / hypothesis / observations | 人が書く 3 行: この練習の前に kit で変えたこと / 何が動くと思ったか / 見えたこと |
| north_star | | URL → 特別賞の分（越えなければ null）。**一番大事な数** |
| speed | url_to_baseline_ready_min | URL → 最初のベンチの帯が ready（盤が無ければ最初の run の開始） |
| | benches_to_prize | 特別賞を越えたベンチが何本目か（盤の ready の数。盤が無ければ score の付いた run の数） |
| | loop_median_min | ベンチの ready → 次の ready の中央値（1 周） |
| | ship_median_s / ship_max_s / deploy_median_s | `hub:ship` 全体 / `task deploy` の秒 |
| outcome | score_at_60min_ratio, best_ratio, best_score | URL から 60 分以内の最高 / 全体の最高 ÷ 目標、最高点 |
| | runs, fails | score の付いた run の数 / score 0 の run と journal の「ベンチ…失敗」の行 |
| | new_5xx_alerts, reverts | 5xx・500・deadlock を含む alert / journal で revert を含む行（「revert なし」は除く） |
| | reboot_test | pass / fail / not_run（人が `REBOOT=` で渡す。既定 not_run） |
| agents.<1\|2\|3\|c> | model, cost_usd | OTel の api_request の cost（`task otel:up` していなければ null） |
| | turns, turn_median_s, busy_min | Claude Code のターン（UserPromptSubmit → Stop）の数・中央値・合計 |
| | idle_min_before_prize, idle_breakdown | 特別賞までに止まっていた分と、その内訳 human / other_lane / nothing / subagent / unmarked（盤の打ち忘れと盤の記録なし） |
| | merged_prs, alerts_raised, permission_wait_s | そのレーンの PR で merge されたもの / 出した alert / 許可待ちの秒 |
| agents_total | cost_usd, cost_to_prize_usd | 全レーンの費用 / 特別賞の時刻までの費用 |
| | merged_prs, merged_unattributed, merged_per_usd | merge された PR、レーンが分からなかった数、PR ÷ 費用 |
| human | todos, response_median_s.<kind> | 人への todo の数、種類ごとの応答の中央値（秒） |
| | total_wait_min, wait_before_prize_min | 人への todo が 1 つ以上開いていた時間（重なりは 1 回）: 起点 → 終点 / 起点 → 特別賞 |
| | decisions | 閉じた todo のうち質問（CHOICES）/ PICKS / PR レビュー |
| safety | crit_alerts | crit の alert |
| | review_catches | `[DONE] c review #n ng …`（または `<n> 件` で n > 0）の数。レーン c のレビューが無ければ null |
| | dq_risks_found | 失格につながる穴を見つけた数（人が `dq_risks=` で渡す） |
| pr_lanes | | merge された PR → レーン（下の規則、分からなければ `?`） |

**merge された PR**: 盤の `ship.end` の merged。盤が無ければ journal で結果に `merge` がある行の `#n`。

**PR のレーン**（上から順に、最初に決まったもの）:
1. `[PR] #n ready …` を送ったセッションのレーン（cc-events の SendMessage。`/ #m ready` も同じレーン。括弧の中の `#n` は数えない）
2. journal のその PR の行の「何を」に `lane X` / `レーン X`
3. journal の branch の claims の owner（`lane2` / `laneC` …）。`/` と `-` は同じに扱う。claims の branch がそれで終わるもの（`ops-pdns` → `fix/<ts>/ops-pdns`）。`integ/<x>` は `<x>/*` か `fix/<x>/*` の行の owner が 1 つに揃うとき

ハブが自分で作った PR（kickoff の docs など）は 1〜3 のどれにも当たらず `?` になる。

フィールドを足すときは schema_version を上げずに足してよい（古い記録は null 扱い）。意味を変えるときは上げる。
