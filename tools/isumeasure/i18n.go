package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

// i18n.go: the 日本語 view of report.html (report.md / summary.md stay English for machines and skills).
// The page is generated in English; a small script swaps text nodes to Japanese with the catalog below:
//   - jaExact: the whole text node (whitespace collapsed, trimmed) → Japanese. Keys prefixed "toc:" apply only inside
//     the table of contents, "grp:" only to group headings (so a TOC entry can be shorter than the section heading).
//   - jaPatterns: sentence templates with values; applied in order to every text node (not anchored, so one long
//     node can get several sentences replaced). {n} = a number, {h} = a host / word, {s} = any text (shortest),
//     {*} = the rest of the node. The Japanese side uses {1}, {2} … for the captures in order.
// Not translated on purpose: SQL, endpoints, file:line, numbers, pprof output, raw log lines, skill / task names.
// Style: short plain sentences; です/ます not needed in tables. Section titles are short; their explanation is the
// one-line description under the title (toc.go htmlSecMeta), never in parentheses in the title.

var jaExact = map[string]string{
	// group titles (grp: = group headings in the body and the TOC) and their descriptions
	"grp:Verdict": "結論",
	"verdict, next step, errors, difference from the previous run": "判定、次の手順、エラー、前回との差",
	"grp:What to fix":                 "どこを直すか",
	"heavy work and suggested fixes":  "重い処理と改善の提案",
	"grp:Tuning and data":             "チューニングとデータ",
	"settings, data growth, trends":   "設定値、データの増え方、推移",
	"grp:Per host":                    "ホスト別の詳細",
	"timeline, endpoints, SQL, pprof": "Timeline、endpoint、SQL、pprof",

	// section titles of report.html (short; same text in the TOC) and their one-line descriptions
	"Summary":                          "概要",
	"Verdict":                          "律速の判定",
	"bottleneck":                       "律速",
	"fix first":                        "先に直す",
	"load window":                      "負荷区間",
	"seconds since the run start":      "ベンチ開始からの秒",
	"CPU by process (100% = one core)": "プロセスごとの CPU（100% = 1 コア）",
	"Bottleneck = the narrowest point that stops the score from growing: a box that uses all its CPU, row-lock waits, disk writes — or, when none of these, waiting (slow API responses, the bench's concurrency).": "律速 = スコアの伸びを止めている、いちばん狭い所。CPU を使い切っている箱、行ロック待ち、ディスク書き込み、どれでもなければ「待ち」（遅い API・ベンチの並列度）。",
	"Making something that is not the bottleneck faster does not raise the score. Look at the bottleneck box / resource first and fix the work that uses it.":                                                       "律速でない所を速くしてもスコアは上がらない。まず律速の箱・資源を見て、そこを使っている処理から直す。",
	"The chart is the CPU each process used (100% = one core). A process that sticks to the red dashed line (cores × 100%) is the bottleneck.":                                                                      "グラフは各プロセスが使った CPU（1 コア = 100%）。赤い破線（コア数 × 100%）に張り付いているプロセスがあれば、それが律速。",
	"Next step":                            "次の手順",
	"Errors":                               "エラー",
	"5xx and their causes":                 "5xx と原因",
	"Bench output":                         "ベンチの出力",
	"vs previous run":                      "前回との比較",
	"Where the time goes":                  "重い処理",
	"endpoints and SQL that take the time": "時間を使っている endpoint と SQL",
	"Next actions":                         "改善の提案",
	"Bench flows: app-analysis →":          "ベンチの流れは app-analysis で →",
	"App analysis: screens → API → handler → tables →": "App analysis: 画面 → API → ハンドラ → テーブル →",
	"Tuning":                  "Tuning",
	"settings likely to help": "効きそうな設定値",
	"Data growth":             "データの増え方",
	"rows per table":          "テーブルごとの行数",
	"Requests per 10 s":       "件数の推移",
	"requests and avg response time per 10 s for the main endpoints": "主な endpoint の 10 秒ごとのリクエスト数と平均応答時間",
	"requests per 10 s":                "10 秒ごとのリクエスト数",
	"avg ms per 10 s":                  "10 秒ごとの平均応答時間 ms",
	"show all":                         "すべて表示",
	"hide all":                         "すべて隠す",
	"Unmatched paths":                  "未分類のパス",
	"candidates for alp.yml":           "alp.yml に足す候補",
	"Requests and Resources":           "リクエストとリソース",
	"CPU, requests and MySQL per 10 s": "10 秒ごとの CPU・リクエスト・MySQL",
	"Endpoints":                        "endpoint 一覧",
	"Slow queries":                     "Slow Query",
	"Running queries":                  "実行中の SQL",
	"processlist sampled every 5 s":    "processlist を 5 秒ごとに取得",
	"Score loops":                      "得点ループ",
	"Transitions":                      "遷移",
	"Drop-off":                         "離脱",
	"(all hosts · 10s buckets)":        "（全 host · 10 秒ごと）",
	"(10s buckets)":                    "（10 秒ごと）",

	// section explanations (muted spans / divs)
	"· which procedure to run next (fixed rules over this report, the run history and the repo)": "· 次に回す手順（このレポート・run 履歴・repo に対する固定ルール）",
	"(5xx by endpoint · app log error lines, de-duplicated)":                                     "（endpoint ごとの 5xx · app log のエラー行、重複まとめ）",
	"(call interval × work per call; excluded from \"Speed up\")":                                "（呼び出し間隔 × 1 回の仕事量。「Speed up」からは除外）",
	"(ranked, auto-suggested — verify against the manual before acting)":                         "（優先度順・自動の提案。手を付ける前にマニュアルと照合）",
	"(knobs with evidence · fixed rules, no search · one per bench at 15:00)":                    "（根拠のある設定値 · 固定ルールで探索なし · 15:00 に 1 ベンチ 1 つ）",
	"· rows right after /initialize → after the bench":                                           "· /initialize 直後 → ベンチ後の行数",
	"(alp.yml candidates)":                               "（alp.yml に足す候補）",
	"(processlist snapshots every 5s)":                   "（5 秒ごとの processlist）",
	"(bench.log, de-duplicated)":                         "（bench.log、重複まとめ）",
	"to the next; shorten the slowest step to earn more": "を出してから次に出すまでの段。一番遅い段を縮めると得点が増える",
	"· share = of all steps that left \"from\" · gap = think time after the previous response":                                                "· share = 「from」から出た遷移のうちの割合 · gap = 前のレスポンス後の待ち時間",
	"⚠ = the count fell by ≥ 40 % from the first to the last full bucket: something stopped feeding this endpoint (a job, a queue, an error)": "⚠ = 最初と最後の bucket で件数が 40 % 以上減った。この endpoint への流れがどこかで止まっている（ジョブ、キュー、エラー）",
	"more requests at the same share = progress; fewer requests with a higher p99 = regression":                                               "同じ割合でリクエストが増えた = 前進。リクエストが減って p99 が上がった = 後退",
	"(knobs with evidence · fixed rules, no search · now = fast lane right away · later = one per bench at 15:00)":                            "（根拠のある設定値 · 固定ルールで探索なし · すぐ = fast lane で次のベンチへ · 最後 = 15:00 に 1 ベンチ 1 つ）",
	"now (fast lane)":      "すぐ（fast lane）",
	"later (15:00 tuning)": "最後（15:00 tuning）",
	"when":                 "いつ",
	"now ●: a limit — the hub hands it to lane 2 as a [fast] etc/ PR for the next bench · later: interacts or smaller than the noise — one per bench at 15:00":                                                                             "すぐ ●: 上限系。ハブがレーン 2 に [fast] の etc/ PR として渡し、次のベンチに載せる · 最後: 影響し合う / ぶれより小さい。15:00 に 1 ベンチ 1 つ",
	"● act: one change → one bench → keep only if the score holds and the signal drops · ○ worth a look":                                                                                                                                   "● 実施: 1 つ変えて 1 ベンチ。スコアが落ちず兆候が消えたときだけ残す · ○ 見る価値あり",
	"paths that no alp.yml rule and no route matched (only id-looking segments were collapsed), grouped by the segment that varies":                                                                                                        "alp.yml にも router にも当たらなかった path（id らしい部分だけまとめた）。変わる部分ごとにまとめた",
	"which tables the bench writes to (per table, the host whose DB changed — a DNS DB may live on the app host), and the baseline for the data-retention check (rows must never go down after a reboot)":                                  "ベンチが書き込むテーブル（テーブルごとに、DB が変わった host。DNS の DB は app の host にあることがある）。データ保持チェックの基準（再起動後に行数が減ってはいけない）",
	"a job that runs every N ms and handles K items per call caps the score at K/N items per second however fast each call is":                                                                                                             "N ms ごとに動き 1 回 K 件を処理するジョブは、1 回をどれだけ速くしても毎秒 K/N 件が上限になる",
	"two of the same statement (INSERT into a gap, FOR UPDATE on a non-unique key) = gap locks: lock by primary key, make the row exist first (INSERT … ON DUPLICATE KEY UPDATE), or drop FOR UPDATE where a unique key already guards it": "同じ文どうし（gap への INSERT、一意でない key への FOR UPDATE）= gap lock。主キーでロックする、先に行を作る（INSERT … ON DUPLICATE KEY UPDATE）、一意キーで守れているなら FOR UPDATE を外す",
	"Deadlocks (LATEST DETECTED DEADLOCK of SHOW ENGINE INNODB STATUS — MySQL keeps only the newest)":                                                                                                                                      "デッドロック（SHOW ENGINE INNODB STATUS の LATEST DETECTED DEADLOCK。MySQL は最新の 1 件だけ残す）",
	"nothing stood out; look at the detail sections": "目立つものなし。詳細の節を見る",
	"= worth a look ·":                    "= 見る価値あり ·",
	"amber":                               "黄",
	"red":                                 "赤",
	"Bench shape":                         "ベンチの形",
	"score_post:":                         "score_post:",
	"The top two are close: ask Claude (": "上位 2 つが拮抗。Claude に（",
	") to pick one with the run history and the journal.": "）と頼み、run 履歴と journal から 1 つ選ぶ。",

	// table column headers
	"host": "host", "requests": "リクエスト数", "trigger": "きっかけ", "end": "終了", "services": "サービス",
	"next step": "次の手順", "skill / command": "skill / コマンド", "why": "理由",
	"of requests": "リクエスト比", "cause": "原因", "count": "件数", "message": "メッセージ",
	"at": "時刻", "statement": "文", "issued by": "発行元", "waiting for": "待っているもの", "rolled back": "ロールバック",
	"calls": "呼び出し数", "median gap ms": "間隔 中央値 ms", "p90 gap ms": "間隔 p90 ms", "avg ms": "平均 ms",
	"work per call (sqltag)": "1 回の仕事量 (sqltag)", "first → last": "最初 → 最後",
	"Δ count": "Δ 件数", "Δ avg": "Δ 平均", "area": "分野", "knob": "設定", "current": "現在値",
	"signal": "兆候", "suggestion": "提案", "act": "実施", "family": "系統", "distinct paths": "path の種類",
	"examples": "例", "add to alp.yml (matching_groups)": "alp.yml に追加 (matching_groups)",
	"table": "テーブル", "after init": "init 直後", "after bench": "ベンチ後",
	"share": "割合", "SQL time / top queries": "SQL 時間 / 上位クエリ", "query": "クエリ",
	"per req":     "1 リクエストあたり",
	"per request": "1 リクエストあたり", "heaviest SQL": "一番重い SQL", "handler:": "ハンドラ:", "before:": "前:", "after:": "後:",
	"no SQL tagged with this endpoint (no measurement patch, or it runs no SQL)": "この endpoint の印が付いた SQL は無い（計測パッチが無いか、SQL を発行しない）",
	"no matching line in the app log":                                            "app log に該当する行が無い",
	"steps around it in the bench's sessions":                                    "ベンチのセッションでの前後の step",
	"· no app requests and no SQL in this run (bench machine or idle host)":      "· この run ではアプリのリクエストも SQL も無い（ベンチ機か空きホスト）",
	"the top 5 endpoints of each host and the heaviest SQL each one issues · click a row for every statement, the code that issued it, its 5xx and the steps around it (the full tables are under \"Per host\")": "ホストごとに時間を使っている endpoint の上位 5 件と、それが発行する一番重い SQL。全部の表は「ホスト別の詳細」にあり、行をクリックすると SQL・発行したコード・5xx・前後の step が開く", "examined/sent": "読んだ行/返した行", "called from": "呼び出し元", "code": "コード",
	"loop": "ループ", "times": "回数", "cycle median s": "1 周 中央値 s", "p90 s": "p90 s",
	"step response times ms": "各段の応答時間 ms", "from": "遷移元", "to": "遷移先", "median gap s": "間隔 中央値 s",
	"last step": "最後の段", "status": "状態", "reason": "理由", "clients": "クライアント数",
	"last ms (median)": "最後の応答 ms（中央値）", "endpoint median ms": "endpoint 中央値 ms", "meaning": "意味",
	"method path": "method path", "sum s": "合計 s", "max": "最大", "bytes avg": "平均 bytes",
	"req/s over time": "req/s の推移", "max ms": "最大 ms", "rows examined / sent": "読んだ行 / 返した行",
	"lock ms": "ロック ms", "seen": "観測回数", "max time s": "最長 s", "states": "状態", "sessions": "セッション数",
	"p90 gap s": "間隔 p90 s",
}

var jaPatterns = [][2]string{
	// Where the time goes (summary) and the endpoint row expansion
	{"of request time · {n}× · avg", "（リクエスト時間）· {1} 回 · 平均"},
	{"of SQL time · {n}× · per request", "（SQL 時間）· {1} 回 · 1 リクエストあたり"},
	{"· called from {*}", "· 呼び出し元 {1}"},
	{"SQL it issued (sqltag, slow log of {s}): {n}s = {n}% of its request time", "発行した SQL（sqltag、{1} の slow log）: {2} 秒 = リクエスト時間の {3}%"},
	{"SQL it issued (sqltag): {n}s = {n}% of its request time", "発行した SQL（sqltag）: {1} 秒 = リクエスト時間の {2}%"},
	{"5xx: {n} of {n}", "5xx: {2} 件中 {1} 件"},
	{"{n}× gap <{n} ms · parallel {n}%", "{1} 回・間隔 {2} ms 未満・並行 {3}%"},
	{"{n}× gap <{n} ms", "{1} 回・間隔 {2} ms 未満"},
	{"{n}× gap {n} ms · parallel {n}%", "{1} 回・間隔 {2} ms・並行 {3}%"},
	{"{n}× gap {n} ms", "{1} 回・間隔 {2} ms"},
	{"{n}× gap {n} s · parallel {n}%", "{1} 回・間隔 {2} 秒・並行 {3}%"},
	{"{n}× gap {n} s", "{1} 回・間隔 {2} 秒"},
	{"steps around it in the bench's sessions (gap = median time from the previous response to the next request; ~0 ms = the client sends the next request right away)", "ベンチのセッションでの前後の step（間隔 = 前の応答から次のリクエストまでの中央値。ほぼ 0 ms = クライアントは応答を受けてすぐ次を送っている）"},
	// headings with values
	{"{h} · Timeline", "{1} · Timeline"},
	{"{h} · Endpoints", "{1} · endpoint 一覧"},
	{"{h} · Bench behaviour", "{1} · ベンチの動き（Bench behaviour）"},
	{"{h} · Running queries", "{1} · 実行中の SQL"},
	{"{h} · Slow queries", "{1} · Slow Query"},
	{"vs previous run {h}", "前回との比較（vs previous run）{1}"},
	{"({n} requests, sorted by total time)", "（{1} リクエスト、合計時間順）"},
	{"({n} statements, {n}s total, sorted by total time)", "（{1} 文、合計 {2}s、合計時間順）"},
	{"({n} sessions seen in cookies; static assets excluded)", "（cookie で見えたセッション {1}、静的ファイルは除外）"},
	{"({h} · load window · count / avg ms per 10-second bucket; whole-run averages hide a mid-run collapse)", "（{1} · 負荷区間 · 10 秒ごとの件数 / 平均 ms。全体平均では途中の失速が見えない）"},
	{"({h} · {n} → {n} req/s)", "（{1} · {2} → {3} req/s）"},
	{"Polling (same endpoint repeated within a session)", "ポーリング（同じセッションで同じ endpoint を繰り返す）"},
	{"Transitions (previous → next request within a session; gap = think time after the previous response)", "遷移（同じセッションでの前 → 次のリクエスト。gap = 前のレスポンス後の待ち時間）"},
	{"· {h} · 推測 — {n} clients by session cookie; runs of the same endpoint (icon lists, polling) collapsed into one step · Sankey:", "· {1} · 推測 — セッション cookie で {2} クライアント。同じ endpoint の連続（アイコン一覧、ポーリング）は 1 段にまとめた · Sankey:"},
	{"· one client's steps from a", "· 1 クライアントが"},
	{"· {n} of {n} clients · a client whose last request ended > 10 s before the run's last request (normal ends included).", "· {1} / {2} クライアント · 最後のリクエストが run の最後より 10 秒以上前に終わったクライアント（正常終了も含む）。"},
	{"reason: error = last response 4xx/5xx; slow = last response > 2× the endpoint's median and ≥ 200 ms (the bench may have given up); end = most clients that reach this step stop there (the scenario's natural end); unknown = none of these", "理由: error = 最後の応答が 4xx/5xx。slow = 最後の応答が endpoint の中央値の 2 倍超かつ 200 ms 以上（ベンチが諦めたかも）。end = この段に来たクライアントの多くがここで終わる（シナリオの自然な終わり）。unknown = どれでもない"},

	// header legend
	{"= fix first (endpoint avg ≥ {*}", "= 先に直す（endpoint 平均 ≥ {1}"},

	// verdict titles
	{"no system samples in the load window", "負荷区間にシステムのサンプルがない"},
	{"idle host (not judged): no app requests and no DB work here (CPU {n}% used)", "空きのホスト（判定しない）: app のリクエストも DB の仕事も無い（CPU {1}% 使用）"},
	{"est. first (5xx)", "見込み: 最優先（5xx）"},
	{"= {n}% of SQL × DB busy {n}", "= SQL 時間の {1}% × DB の使用率 {2}"},
	{"= {n}% of app CPU × app busy {n}", "= app の CPU の {1}% × app の使用率 {2}"},
	{"= {n}% of request time", "= リクエスト時間の {1}%"},
	{"est. {n} =", "見込み {1} ="},
	{"bottleneck ×{n}", "律速 ×{1}"},
	{"score path ×{n}", "スコア経路 ×{1}"},
	{"= rule P{n}", "= ルール P{1}"},
	{"CPU hot spot: {s} ({n}% of the app's CPU)", "CPU を食っている関数: {1}（app の CPU の {2}%）"},
	{"self {n}%, cum {n}% of the CPU profile. Called from: {s}. Do the work less often (compute once on write and store it, cache it, skip it when the result is not used) or with a cheaper call.", "CPU プロファイルの self {1}%、cum {2}%。呼び出し元: {3}。回数を減らす（書き込み時に 1 回計算して保存、キャッシュ、結果を使わないなら呼ばない）か、軽い呼び方にする。"},
	{"Row-lock contention on {h} ({n} waits/s, {n} ms/s) is the likely cause when the 5xx are deadlocks / lock wait timeouts: shorten the transactions, move FOR UPDATE off hot rows.", "5xx がデッドロック / ロック待ちタイムアウトなら、{1} の行ロック競合（{2} 回/s、{3} ms/s）が原因の見込み: トランザクションを短く、よく使う行への FOR UPDATE を外す。"},
	{"bench host (not judged): bench uses {s}, no app requests here", "ベンチ機（判定しない）: bench {1}。ここに app のリクエストは無い"},
	{"fix first: {n}% of requests are 5xx — read the Errors section before anything else", "リクエストの {1}% が 5xx。何より先に Errors 節を見る"},
	{"DB host CPU-bound: mysqld {s} — fix the heaviest SQL (indexes, N+1) first", "DB ホストの CPU 律速: mysqld {1}。一番重い SQL（index、N+1）から直す"},
	{"DB CPU-bound: mysqld {s} (app {n}%) — fix the heaviest SQL (indexes, N+1) first", "DB の CPU 律速: mysqld {1}（app {2}%）。一番重い SQL（index、N+1）から直す"},
	{"app CPU-bound: {h} {s} (mysqld {n}%) — profile the app (pprof), reduce per-request work", "app の CPU 律速: {1} {2}（mysqld {3}%）。pprof で調べ、1 リクエストの仕事を減らす"},
	{"DB host: mysqld {s} — ", "DB ホスト: mysqld {1} — "},
	{"close call: also near CPU-bound (idle {n}%, {s})", "境目: CPU 律速にも近い（idle {1}%、{2}）"},
	{"close call: also row-lock waits {n}/s ({n} ms/s)", "境目: 行ロック待ちも {1}/s（{2} ms/s）"},
	{"{n}% of {n}%", "{1}% / 上限 {2}%"},
	{"— ceiling {n}% ({n} cores, estimated)", "— 上限 {1}%（{2} コア、推定）"},
	{"— ceiling {n}% ({n} cores)", "— 上限 {1}%（{2} コア）"},
	{"ceiling {n}%", "上限 {1}%"},
	{"{h} avg {n}%", "{1} 平均 {2}%"},
	{"CPU saturated ({n}% used) with app and DB both busy — start with the top SQL, then profile", "CPU が飽和（{1}% 使用）、app も DB も忙しい。上位の SQL から、次に pprof"},
	{"IO-bound: iowait {n}% — fsync/binlog/slow log/disk writes; check innodb_flush settings and what writes", "IO 律速: iowait {1}%。fsync / binlog / slow log / ディスク書き込み。innodb_flush の設定と何が書いているかを見る"},
	{"lock-bound: {n} row-lock waits/s ({n} ms/s) with idle CPU — shorten transactions, avoid FOR UPDATE hot rows", "ロック律速: 行ロック待ち {1} 回/s（{2} ms/s）で CPU は空き。トランザクションを短く、よく使う行への FOR UPDATE を避ける"},
	{"memory pressure: {n}% used — check buffer pool / app memory / swap", "メモリ不足: {1}% 使用。buffer pool / app のメモリ / swap を見る"},
	{"CPU is not the limit (idle {n}%): throughput is bounded by waiting — latency of the slowest endpoints, polling intervals, external calls, or the bench's concurrency", "CPU は律速ではない（idle {1}%）。待ちで頭打ち: 遅い endpoint の応答時間、ポーリング間隔、外部呼び出し、ベンチの並列度"},
	// verdict reasons
	{"load window {n}s: avg {n} req/s (peak {n}), median p99 {n}ms, 5xx {n}%", "負荷区間 {1}s: 平均 {2} req/s（最大 {3}）、p99 中央値 {4}ms、5xx {5}%"},
	{"CPU user {n}% + sys {n}% (idle {n}%, iowait {n}%)", "CPU user {1}% + sys {2}%（idle {3}%、iowait {4}%）"},
	{"CPU by process (% of one core):", "プロセスごとの CPU（1 コア = 100%）:"},
	{"MySQL {n} q/s, threads_running avg {n}, row-lock waits {n}/s ({n} ms/s)", "MySQL {1} q/s、threads_running 平均 {2}、行ロック待ち {3}/s（{4} ms/s）"},
	{"(推測): rising load — in-flight requests on {h} went {n} → {n} (median {n}, p10–p90 {n}–{n}, {n} s). Throughput follows how many clients the bench adds; look for the moment p99 or errors stop the growth.", "（推測）: 負荷が増えていく型。{1} の処理中リクエストは {2} → {3}（中央値 {4}、p10–p90 {5}–{6}、{7} s）。スループットはベンチが足すクライアント数に従う。p99 やエラーで伸びが止まる時点を探す。"},
	{"(推測): fixed concurrency — ≈ {n} requests in flight on {h} through the load window (p10–p90 {n}–{n}, start {n} → end {n}, {n} s). The bench waits for each response before sending the next, so shorter responses turn directly into more requests and score.", "（推測）: 並列数が一定の型。{2} で処理中リクエストは常に ≈ {1}（p10–p90 {3}–{4}、開始 {5} → 終了 {6}、{7} s）。ベンチは応答を待ってから次を送るので、応答が短いほどそのままリクエスト数とスコアが増える。"},
	{"× {n} (alp.yml; the Score timeline shows score ÷ this)", "× {1}（alp.yml。Score timeline はスコア ÷ この数を表示）"},
	{"Special prize target {n} (alp.yml prize_target): score of this run unknown", "特別賞の目標 {1}（alp.yml prize_target）: この run のスコアは不明"},
	{"⚠ Score not recorded for this run: the rules that compare runs (revert after a drop, technique-scan when flat) cannot fire. Type it in the panel's Runs row, or attach the bench output:", "⚠ この run のスコアが記録されていない。run を比べるルール（下がったら revert、横ばいなら technique-scan）が動かない。panel の Runs の行に入力するか、ベンチ出力を添付する:"},
	{"(its result line fills the score).", "（結果の行からスコアが入る）。"},
	{"{n} host(s)", "{1} host"},

	// next step reasons (joined with "; ")
	{"code index is empty: SQL → code and router grouping are off", "code index が空: SQL → コードの対応と router でのまとめが効いていない"},
	{"no alp.yml and {n} endpoint kinds on one host: the endpoint tables under-count", "alp.yml がなく 1 host で endpoint が {1} 種類: endpoint の表が少なく数えている"},
	{"{n} endpoint kinds on one host: add the Unmatched paths rules to alp.yml", "1 host で endpoint が {1} 種類: Unmatched paths のルールを alp.yml に足す"},
	{"{h}: {n}% of requests are 5xx (report: Errors section)", "{1}: リクエストの {2}% が 5xx（レポートの Errors 節）"},
	{"score {n} → {n} ({n}%) and the code changed ({h} → {h})", "スコア {1} → {2}（{3}%）、コードも変わった（{4} → {5}）"},
	{"score {n} → {n} with the same commit {h}: bench noise or state left from the previous run", "スコア {1} → {2}、commit {3} は同じ: ベンチのぶれか、前回の状態が残っている"},
	{"run started {s} JST (after 16:30): stop improving, rehearse the re-test", "run の開始が {1} JST（16:30 以降）: 改善はやめ、再試験のリハーサルをする"},
	{"score {n} = {n}% of the prize target {n} (alp.yml prize_target): one bench with app ×2 + DB ×1 and logging/measurement off may cross it", "スコア {1} は特別賞の目標 {3} の {2}%（alp.yml prize_target）: app ×2 + DB ×1、ログと計測 OFF の 1 ベンチで越えられるかも"},
	{"{h}: PowerDNS is {n}% of the host's SQL time, pdns_server {n}% of a core", "{1}: PowerDNS が host の SQL 時間の {2}%、pdns_server が 1 コアの {3}%"},
	{"CPU used: busy {s} / idle {*}", "CPU 使用率: 忙しい {1} / 空き {2}"},
	{"{n} P1 + {n} P2 actions in this report", "このレポートに P1 が {1} 件 + P2 が {2} 件"},
	{"the last change helped ({n} → {n})", "前回の変更は効いた（{1} → {2}）"},
	{"no P1 action left: index / N+1 fixes are running dry", "P1 が残っていない: index / N+1 の直しが尽きてきた"},
	{"score flat over the last 3 runs ({n} → {n})", "直近 3 回でスコアが横ばい（{1} → {2}）"},
	{"after 12:30 and no docs/technique-scan-*.md yet", "12:30 を過ぎたのに docs/technique-scan-*.md がまだない"},
	{"already run once (docs/technique-scan-*.md): re-run only for new evidence", "1 回実施済み（docs/technique-scan-*.md）: 新しい根拠があるときだけ再実施"},
	{"{h} verdict is \"{h}\": the endpoint tables do not show what it waits for", "{1} の判定は \"{2}\": 何を待っているかは endpoint の表からは見えない"},
	{"the bench raises load over the run: find when and why it stops growing", "ベンチは run の間に負荷を上げる: いつ、なぜ伸びが止まるかを探す"},
	{"docs/app-map.md is missing", "docs/app-map.md がない"},
	{"top endpoints not in docs/app-map.md:", "docs/app-map.md にない上位の endpoint:"},

	// next actions: titles
	{"Fix 5xx errors ({n}% of requests)", "5xx エラーを直す（リクエストの {1}%）"},
	{"Heavy aggregate query: {n}% of SQL time, avg {n}ms", "重い集計クエリ: SQL 時間の {1}%、平均 {2}ms"},
	{"Query on {h} is still slow with its index ({n}% of SQL time)", "{1} へのクエリは index があっても遅い（SQL 時間の {2}%）"},
	{"Add an index on {*}", "index を追加: {1}"},
	{"N+1 suspect: {n} executions per request", "N+1 の疑い: 1 リクエストで {1} 回実行"},
	{"Speed up {s} (avg {n}ms, {n}% of request time)", "{1} を速くする（平均 {2}ms、リクエスト時間の {3}%）"},
	{"Polling: {s} at {n} req/s (median gap {n}ms)", "ポーリング: {1} が {2} req/s（間隔 中央値 {3}ms）"},
	{"Enable pprof in the app (no CPU profile was captured)", "app で pprof を有効にする（CPU プロファイルが取れていない）"},
	{"Serve static files from nginx ({n} of {n} went through the app)", "静的ファイルを nginx で返す（{2} 件中 {1} 件が app を通った）"},
	{"Reduce row-lock contention", "行ロックの競合を減らす"},
	{"Cut disk writes", "ディスク書き込みを減らす"},
	{"CPU is idle: find what the bench waits for", "CPU が空いている: ベンチが何を待っているかを探す"},
	{"Periodic job {s}: every {n} ms, ≈ {n} writes per call", "定期ジョブ {1}: {2} ms ごと、1 回 ≈ {3} 件書き込み"},
	{"Periodic job {s}: every {n} ms, work unknown per call", "定期ジョブ {1}: {2} ms ごと、1 回の仕事量は不明"},
	{"{s} runs an external command per request", "{1} はリクエストごとに外部コマンドを実行する"},
	{"(also {*})", "（{1} でも同じ）"},
	// next actions: bodies
	{"Errors cost score and often hide a real bug (timeouts, connection limits, nil rows). Endpoints:", "エラーはスコアを減らし、本当のバグ（タイムアウト、接続数の上限、nil の行）を隠していることが多い。endpoint:"},
	{"{n} calls. Subquery/JOIN/window queries rarely get fast with an index alone: precompute the result (maintain a column or table on write), cache it within the allowed staleness, or move the computation into the app. Query:", "{1} 回。サブクエリ / JOIN / window のクエリは index だけでは速くなりにくい。結果を前もって作る（書き込み時に列やテーブルを更新）、許される遅れの範囲でキャッシュする、または計算を app に移す。クエリ:"},
	{"{n} calls, examines ×{n} more rows than it returns (avg {n}ms) although an index on ({s}) exists: a range/LIKE/function on the indexed column, low selectivity, or a sort the index does not cover. Rewrite the query, add the ORDER BY column to the index, or precompute. Query:", "{1} 回。({4}) の index があるのに返す行の ×{2} を読む（平均 {3}ms）。index の列への範囲 / LIKE / 関数、絞り込みが弱い、または index で賄えない並べ替え。クエリを書き直す、ORDER BY の列を index に足す、または前もって作る。クエリ:"},
	{"{n}% of SQL time, {n} calls, examines ×{n} more rows than it returns (avg {n}ms). Query:", "SQL 時間の {1}%、{2} 回、返す行の ×{3} を読む（平均 {4}ms）。クエリ:"},
	{"{n} calls for {n} API requests, {n}% of SQL time. Batch it (IN (...)), JOIN it, or cache it. Query:", "API リクエスト {2} 件に対して {1} 回、SQL 時間の {3}%。まとめる（IN (...)）、JOIN する、またはキャッシュする。クエリ:"},
	{"{n} calls, p99 {n}ms. Look at the SQL it runs (tables in the path name are a hint) and whether it can be precomputed or cached within the consistency window the manual allows.", "{1} 回、p99 {2}ms。実行している SQL を見る（path の名前のテーブルが手がかり）。前もって作れるか、マニュアルが許す整合性の範囲でキャッシュできるかを見る。"},
	{"The client re-polls as soon as it may. If the API has a server-directed interval field, raise it; otherwise make the polled endpoint cheap (cache within the allowed staleness) or switch to SSE/long-polling if the manual allows; check how the interval affects scoring.", "クライアントは許される限りすぐ再ポーリングする。API にサーバが間隔を指示する項目があれば上げる。なければポーリング先を軽くする（許される遅れの範囲でキャッシュ）か、マニュアルが許すなら SSE / long-polling にする。間隔がスコアにどう効くかを確認する。"},
	{"The response sets `{s}` = {n} ({s}): the client waits that long before the next poll.", "レスポンスが `{1}` = {2} を返す（{3}）: クライアントは次のポーリングまでその時間待つ。"},
	{"Allowed staleness (docs/app-annotations.json):", "許される遅れ（docs/app-annotations.json）:"},
	{"Add net/http/pprof (and fgprof) on :6060; the agent captures CPU/fgprof automatically during the next bench.", ":6060 に net/http/pprof（と fgprof）を足す。次のベンチで agent が CPU / fgprof を自動で取る。"},
	{"Add location blocks with root/alias + expires so the app never sees asset requests.", "root/alias + expires の location を足し、静的ファイルのリクエストが app に来ないようにする。"},
	{"Check the processlist section for statements stuck in 'Waiting for lock', shorten transactions, move FOR UPDATE/SHARE off hot rows, and avoid serial updates on the same row.", "processlist の節で 'Waiting for lock' で止まっている文を見る。トランザクションを短くし、よく使う行から FOR UPDATE/SHARE を外し、同じ行への連続更新を避ける。"},
	{"Disable the slow log and binlog for the final run, set innodb_flush_log_at_trx_commit=2 (zz-perf.cnf), batch the highest-frequency INSERTs, and check that logs are not on a tiny volume.", "最終ベンチでは slow log と binlog を止め、innodb_flush_log_at_trx_commit=2（zz-perf.cnf）にし、一番多い INSERT をまとめ、ログが小さいディスクにないかを見る。"},
	{"Look at p99 per endpoint, polling intervals, external HTTP calls (payment etc.), DB locks, and DB connection pool limits (SetMaxOpenConns). Raising concurrency in the app/DB may also help.", "endpoint ごとの p99、ポーリング間隔、外部 HTTP 呼び出し（payment など）、DB のロック、DB の接続プールの上限（SetMaxOpenConns）を見る。app / DB の並列度を上げるのも効くことがある。"},
	{"{n} calls, median gap {n} ms (p90 {n} ms), {n} ms per call (p99 {n}). Work per call:", "{1} 回、間隔 中央値 {2} ms（p90 {3} ms）、1 回 {4} ms（p99 {5}）。1 回の仕事量:"},
	{"If what it produces (matches, settlements) is what the bench waits for, process more items per call (the whole queue, not one) or run it more often — not faster per call.", "これが作るもの（マッチング、精算）をベンチが待っているなら、1 回で多く処理する（1 件ではなくキュー全体）か、もっと頻繁に動かす。1 回を速くするのではない。"},
	{"per second at this interval.", "/秒（この間隔で）。"},
	{"{n} calls, avg {n}ms. The handler (or a helper) calls exec.Command: replace the command with a direct call/INSERT to what it does, or batch it, and remember a 2nd app host would run it too.", "{1} 回、平均 {2}ms。handler（か helper）が exec.Command を呼ぶ。コマンドを、それがやること自体（直接の呼び出し / INSERT）に置き換えるか、まとめる。2 台目の app でも実行される点に注意。"},
	{"where:", "場所:"},

	// where the time goes / errors
	{"endpoints by total time · SQL column from sqltag comments (slow log of {h})", "endpoint を合計時間順に · SQL 列は sqltag のコメントから（{1} の slow log）"},
	{"endpoints by total time · SQL column from sqltag comments", "endpoint を合計時間順に · SQL 列は sqltag のコメントから"},
	{"{n}s SQL = {n}% of endpoint time", "SQL {1}s = endpoint の時間の {2}%"},
	{"SQL by total time · per request = executions per API request (N+1 signal) · called from = sqltag comment, or static code match · code = the line that issued it (sqltag caller, exact) or the matching SQL literal ·", "SQL を合計時間順に · 1 リクエストあたり = API リクエスト 1 件あたりの実行回数（N+1 の兆候）· 呼び出し元 = sqltag のコメントかコードの静的な照合 · コード = 発行した行（sqltag の呼び出し元、正確）か一致した SQL リテラル ·"},
	{"{h} app.log: {n} error lines → {n} kinds (numbers masked as N)", "{1} app.log: エラー {2} 行 → {3} 種類（数字は N に置換）"},
	{"⚠ one endpoint is split into many rows ({n} endpoint kinds; e.g. {s} seen as {n} different paths). The endpoint tables and \"Where the time goes\" under-count it. Add the rule below to alp.yml and re-report.", "⚠ 1 つの endpoint が多くの行に分かれている（endpoint {1} 種類。例: {2} が {3} 通りの path）。endpoint の表と「Where the time goes」が少なく数える。下のルールを alp.yml に足して re-report する。"},
}

var i18nTplRe = regexp.MustCompile(`\{[nhs*]\}|\{\d+\}`)

// jaCatalogJSON: the catalog compiled for the page script: exact map + [regex, replacement, needle] triples.
func jaCatalogJSON() string {
	type pat struct {
		Re     string `json:"r"`
		Rep    string `json:"t"`
		Needle string `json:"n"`
	}
	var pats []pat
	for _, p := range jaPatterns {
		var re strings.Builder
		needle := ""
		last := 0
		for _, loc := range i18nTplRe.FindAllStringIndex(p[0], -1) {
			lit := p[0][last:loc[0]]
			if len(lit) > len(needle) {
				needle = lit
			}
			re.WriteString(regexp.QuoteMeta(lit))
			switch p[0][loc[0]:loc[1]] {
			case "{n}":
				re.WriteString(`(-?\d[\d.,]*)`)
			case "{h}":
				re.WriteString(`([\w.:/-]+)`)
			case "{s}":
				re.WriteString(`(.+?)`)
			case "{*}":
				re.WriteString(`(.*)`)
			}
			last = loc[1]
		}
		lit := p[0][last:]
		if len(lit) > len(needle) {
			needle = lit
		}
		re.WriteString(regexp.QuoteMeta(lit))
		rep := regexp.MustCompile(`\{(\d+)\}`).ReplaceAllString(strings.ReplaceAll(p[1], "$", "$$"), "$$$1")
		pats = append(pats, pat{re.String(), rep, strings.TrimSpace(needle)})
	}
	b, _ := json.Marshal(map[string]any{"exact": jaExact, "pat": pats})
	return string(b)
}

// langToggleHTML: the 日本語 / English buttons (top of the header).
const langToggleHTML = `<div class="lang"><button type="button" data-setlang="ja">日本語</button><button type="button" data-setlang="en">English</button></div>`

// langJS: swaps text nodes between the generated English and the catalog's Japanese; remembers the choice.
const langJS = `
(function(){
  var C = I18N, pats = C.pat.map(function(p){ return {re: new RegExp(p.r, 'g'), t: p.t, n: p.n}; });
  var SKIP = {PRE:1, CODE:1, SCRIPT:1, STYLE:1, TEXTAREA:1};
  function tr(s, ctx){
    var k = s.replace(/\s+/g, ' ').trim();
    if (!k || !/[A-Za-z]/.test(k)) return null;
    if (ctx && C.exact[ctx + k] !== undefined) return C.exact[ctx + k];
    if (C.exact[k] !== undefined) return C.exact[k];
    var out = s, hit = false;
    for (var i = 0; i < pats.length; i++) {
      var p = pats[i];
      if (p.n && out.indexOf(p.n) < 0) continue;
      p.re.lastIndex = 0;
      if (p.re.test(out)) { p.re.lastIndex = 0; out = out.replace(p.re, p.t); hit = true; }
    }
    if (hit && ctx === 'toc:') out = out.replace(/（[^）]*）/g, '');
    return hit ? out : null;
  }
  var nodes = null;
  function collect(){
    nodes = [];
    var w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, {acceptNode: function(n){
      for (var e = n.parentNode; e && e !== document.body; e = e.parentNode) {
        if (SKIP[e.nodeName] || (e.nodeName.toLowerCase() === 'svg' && !(e.classList && e.classList.contains('i18n'))) || (e.classList && e.classList.contains('lang'))) return NodeFilter.FILTER_REJECT;
      }
      return NodeFilter.FILTER_ACCEPT;
    }});
    for (var n; (n = w.nextNode());) {
      var el = n.parentNode, ctx = '';
      if (el.closest && el.closest('.grp, .hostgrp, a.gt')) ctx = 'grp:'; else if (el.closest && el.closest('nav.toc')) ctx = 'toc:';
      var ja = tr(n.nodeValue, ctx);
      if (ja !== null) {
        var lead = n.nodeValue.match(/^\s*/)[0], tail = n.nodeValue.match(/\s*$/)[0];
        if (ja.trim() === ja) ja = lead + ja + tail;
        nodes.push([n, n.nodeValue, ja]);
      }
    }
  }
  function set(lang){
    if (lang !== 'en') lang = 'ja';
    if (!nodes) collect();
    for (var i = 0; i < nodes.length; i++) nodes[i][0].nodeValue = nodes[i][lang === 'ja' ? 2 : 1];
    document.documentElement.lang = lang;
    document.querySelectorAll('[data-setlang]').forEach(function(b){ b.classList.toggle('on', b.getAttribute('data-setlang') === lang); });
    try { localStorage.setItem('isumeasure.lang', lang); } catch (e) {}
  }
  document.querySelectorAll('[data-setlang]').forEach(function(b){ b.addEventListener('click', function(){ set(b.getAttribute('data-setlang')); }); });
  var saved = null; try { saved = localStorage.getItem('isumeasure.lang'); } catch (e) {}
  set(saved || 'ja');
})();
`
