# 3 人の役割（AI が実装する前提）

実装の手数は AI が出す。人の 3 枠は **運用（A）・検証と守り（B）・読みと判断（C）** に寄せる。根拠は `docs/TECHNIQUES.md` §7（失格・FAIL の実例は実装の遅さではなく、再起動・キャッシュ・目視・計測の切り忘れ・不整合）と、上位チームが「計測して指示する 1 人」を固定していたこと。

AI への指示は A のセッション（4 枚のレーン）だけが出す。B と C も Claude を使ってよいが、**読み取り専用**（deep-dive、PR の説明、マニュアルの検索）に限り、repo を触るときは worktree（CLAUDE.md ルール 4）。**A が 1 人で Claude セッションを 4 枚、10:00 から終わりまで回す**（レーン 1 = ハブ: kickoff・merge・deploy・判断、レーン 2 = ビルド: サーバー準備・parallel-setup・parallel-fix・tuning、レーン c = スカウト: 先読みの手・technique-scan・deep-dive・設計。[docs/lanes/](lanes/README.md)）。時刻表ではなく出来事で動き、状況は手元の盤に出る（4 枚目のターミナルのタブで `task lane:watch`。盤はこの PC だけのもので、B / C の PC からは見えない）。A が見るのは**`lane:watch` のタブとレーン 1 の返答**だけ: 盤の todo（人にしかできない操作）を片付け、alert をレーン 1 と相談し、レーン 1 に OK / 答えを言う。B と C は盤を一緒に見て、`[PICK]` の要確認と PR の一括レビューを A の横で手伝う。チームの変形として、C（または B）がレーン c を自分のセッションで持ってもよい（RUNBOOK §4-C の注）。そのとき書いてよいのは、レーン c が `docs/claims.md` に宣言したファイルだけ。

---

## 開催前にやること（人ごと）

| | A オーケストレーター | B 検証・守り | C 読みと判断 |
|---|---|---|---|
| 環境 | `docs/PREP.md` 全部（.env、deploy key、ops ホスト、`task ops:deploy`）。コンテスト repo をテンプレートから作る。Claude のメインセッションの設定（`CLAUDE.local.md`、model）。`.claude/settings.json` の読み取り専用の許可（PREP の 7） | repo を clone、`gh auth`、管制盤のログイン。手元のブラウザで `/etc/hosts` を書ける状態 | repo を clone。Claude を読み取り専用で使える状態（worktree の作り方を一度やる） |
| 練習で通すこと | 4 枚のタブを起動の 1 行（RUNBOOK 冒頭）で立ち上げ、盤の todo だけを見て 2 回目のベンチまで進める（2 回以上）。メッセージが表の名前にだけ届くこと、盤の状態が手順ごとに変わること、頼みごとが全部 todo で来ることを確かめる。PR の表 → OK → レーン 1 の merge → deploy → smoke → ベンチの todo の一連を手が覚えている | §8 のチェックリストを練習で **自分の手で** 通す: `task hosts:measure-off` → `db:snapshot` → `reboot-test` → ベンチ → `db:snapshot` の差分確認。ブラウザ目視の手順（主要画面 5 つ）を書いておく | 過去 2 回分（13・14）のマニュアルを読み、`docs/TECHNIQUES.md` の付録 A と照らして「猶予秒数・禁止事項・スコア式・ノブ」を 30 分で抜き出す練習。technique-scan の出力を 1 回読んで「要確認」に答える。早期 technique-scan の出力（`docs/technique-scan-early.md`）を 1 回読み、10:20 の要確認に答える練習 |
| 作っておくもの | `.env`、`alp.yml` の作り方（`task groups`）、管制盤の `score_post`。トラブル時の手順（RUNBOOK §2 のよくある NG、`task ops:reset`） | **PR レビューの 4 観点のチェックシート**（下）。再起動試験の手順カード（コマンドと所要時間）。目視の画面リスト | **マニュアル読みの型**（下）。`docs/techniques/sources/*.md` から自分の年に似た問題を 2 つ選んで読んでおく（13 と 14 は必須）。`docs/FIRST-HOUR.md` の型を頭に入れる |
| 練習の最後に | retro を回し、HANDOFF の未検証を減らす | 練習で出た FAIL / 失格要因を §7 の表に足す | 練習の technique-scan の判定が外れた項目を TECHNIQUES に反映 |

### B のレビュー 4 観点（PR ごとに全部見る）

1. **猶予**: この変更で古い値を返す可能性がある endpoint はどれか。マニュアルの猶予秒数の中か（C に聞く）。猶予の無い年は「即時反映」。
2. **再起動**: メモリだけに持つ状態は無いか。あるなら DB から再構築する経路と `/initialize` の reset があるか。ファイルは initialize で再生成されるか。
3. **2 台目**: app を 2 台にしたとき壊れるもの（プロセス内キャッシュの無効化、ファイルの置き場、ワーカーの二重起動）。PR 本文の「two-app risk」を読む。
4. **初期化と index**: スキーマ変更は `webapp/sql/` に入っているか、index は `indexes/*.sql` にあるか、`/initialize` の所要は上限内か。

加えて `task diffcheck BRANCH=`（応答の JSON 差分）と `task smoke` の結果を見てから OK を言う。

### C のマニュアル読みの型（kickoff phase 1 と並行して自分でも読む）

1. スコア式: 何が点か、倍率、減点、クリティカル。→ `alp.yml` の `score_post` を決める。
2. 猶予: 「秒」「以内」「許容」「古い」で grep。endpoint ごとに秒数を表にする。無ければ「即時反映」と書く。
3. 許可と禁止: ステータスコード、Cache-Control / 304、外部 API、bot、ノブ（campaign / available_days / capacity の類）。→ TECHNIQUES の T15〜T18、T37 に当たるか。
4. 追試: 再起動、データ保持、目視、% ルール。→ B に渡す。
5. 付随サービス: DNS、決済、ワーカー、外部 API。→ A の parallel-setup の指示に入れる。

---

## 当日（時刻は 10:00 開始基準）

| 時刻 | A | B | C |
|---|---|---|---|
| 10:00 | 4 枚のタブを起動（RUNBOOK 冒頭の 1 行）。以後、盤の todo を上から（`.env` の HOSTS、`! task hosts:setup`、baseline ベンチ） | マニュアルを読む（追試・禁止事項の節から） | マニュアルを読む（スコア式・猶予・**自由や余裕をくれる文**の節から）。Claude に kickoff phase 1 を実行させる（A のセッションで A が指示。C はその出力を自分の読みと照合） |
| 10:05 | 盤の todo（続き）。alert が出たらレーン 1 と相談 | 参考実装の `/initialize` と schema を読む（何が初期化されるか） | 猶予の表と禁止事項の表を `docs/kickoff.md` に書く（AI の下書きを直す） |
| 10:10 | 盤の todo（続き） | 主要画面 5 つをブラウザで開き、目視リストを確定 | `alp.yml` の `score_post` と `bench_patterns` を確かめる（kickoff phase 2 が書く） |
| 10:15 | baseline ベンチの todo → collect → Runs に score と bench log → ts をレーン 1 に貼る | ベンチ中に `task host:diag HOST=1` で状態を記録（後で比較） | summary.md の Verdict と Bench output を読んで仮説 3 つ |
| 10:20 | レーン 1 が出すレーン c の手（最大 3）と要確認を見て、拒否と答えをレーン 1 に（3 分。来たときに） | — | **要確認に答える**（猶予の秒数、ノブ）。猶予に触る手は拒否を提案 |
| 10:25 | （レーン 1 が `[TASK] 3 fix` と `[TASK] c technique-scan` を出す。setup は `[DONE] servers` で始まっている。A は何も打たない）許可待ちのタブがあれば許可 | PR が出るまでに §8-3 の再起動試験の段取りを確認 | app-map（レーン 3）の出力を読み、書き込み経路と増えないテーブルをメモ |
| 10:30〜10:40 | レーン 1 が並べた PR の表を B と一括レビュー → レーン 1 に OK（merge はレーン 1 が全部やる）→ ベンチ 2 回目の todo | **PR レビュー（4 観点）**。3 レーン分を 1 回で | レーン c の PR の猶予の観点を B に言う。ベンチ 2 回目の summary.md でレーン c の手が効いたかを判定し、Where the time goes とスコア経路を突き合わせ |
| 10:50 | 作戦会議（15 分）。レーン 1 の run の判定と次の `[TASK]` を聞く | 守りの観点から「やらない方がよい手」を言う | 仮説と担当の提案、§6a の構成判断の材料 |
| 11:00〜 | ループ: ベンチの todo → ts を貼る → PR の表に OK → ベンチの todo。1 周 30〜40 分。指示はレーン 1 が `[TASK]` で配る | 毎 PR のレビュー。diffcheck の差分を読む。14:00 に **自主再起動試験**（`task reboot-test`） | Bench output と Score timeline を見て伸びが止まった理由を言う。report の冒頭の「Special prize target」が 75% を越えたら A に score-sprint を提案する（RUNBOOK §6e）。deep-dive が要るなら自分のセッションで（読み取り専用） |
| 13:00 | （12:30 にレーン 1 が `[PHASE]` でレーン c に technique-scan を回させる）要確認の todo に答える | 「○」の手のリスク欄を §7 と照合 | 「要確認」に答える（猶予・ステータス・ノブ）。○ の優先順を A に返す |
| 12:30〜14:00 | §6a の構成判断を C と決めて指示 | app 2 台化なら §6b のチェックリスト | 構成判断の根拠を journal に |
| 15:00 | （レーン 1 が `[PHASE]` でレーン 2 に tuning。● を 1 つずつ 1 ベンチ、15:45 で打ち切り）ベンチの todo | ノブの PR を見る（耐久性を削る設定が無いか） | 残す/戻すの判定（スコア −3% 以内で兆候が消えたか）を journal に |
| 16:00 | 凍結。未 merge ブランチを捨てる判断 | §8-1 | 最終レビューの観点を Claude に渡す（§8-4） |
| 16:30 | §8 の todo（レーン 1 が出す）を B の進行で実行 | **§8 を主導**（measure-off → snapshot → reboot-test → ベンチ → snapshot → 目視） | 最終スコアと構成を記録、Discord に共有 |
| 終了後 | `task retro` | 失格要因の振り返り | retrospective を指示、TECHNIQUES に反映 |

### 終わった判定（人ごと）

- A: 全ての merge が PR 経由で、Score timeline に全 run の score と note がある。最終ベンチ後にサーバーを触っていない。
- B: 全 PR に 4 観点のレビュー記録がある。14:00 と 16:30 の再起動試験が両方 pass。目視リストに全部チェック。
- C: kickoff.md に猶予の表・禁止事項・スコア式がある。「先読みの手」の各行に結果（merged と効果 / 外した理由）がある。technique-scan の「要確認」に全部答えが付いている。journal に構成判断の根拠がある。

### やらないこと

- 人が handler を実装で分担すること（AI が並列に書く。人は上の表の仕事）。
- B・C が A のセッションや main の checkout を触ること。
- レーン 1 がメッセージ（`[PR]` など）だけを根拠に merge すること。OK は人がレーン 1 のタブに直接言う。
- レーン 2 / 3 / c が merge すること（merge はレーン 1 だけ）。
- 人に文章だけで頼むこと（頼みごとは盤の todo）。
- 16:30 まで守りを見ないこと（B は 14:00 に一度回す）。
