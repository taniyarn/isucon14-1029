# セッション間の連絡（1 行の決まった形）

当日は Claude のセッションが 4 枚並ぶ（レーン 1 = ハブ、レーン 2 = インフラ、レーン 3 = アプリ、レーン c = スカウト。[docs/lanes/README.md](lanes/README.md)）。セッション同士の連絡はこのファイルの形だけで行う。

**状態はメッセージで送らない**。「今何をしているか」「待っている」「止まった」は盤（`task lane:status` / `lane:alert` / `lane:todo`。手元の PC だけの保存場所で、人は `task lane:web` / `task lane:watch` で見る）に出す。メッセージは「相手に動いてほしい」ときの合図だけ。

## なぜ短くするか

受け取ったセッションは**そのたびに 1 ターン丸ごと起きる**（文脈を読み直し、考え、返事を書く）。起こす回数を少なく、1 回を小さくする。中身は repo のファイルにあり、メッセージは指すだけ。

## 形（1 行。種類・宛先 / id・要点）

| 形 | 向き | いつ | 例 |
|---|---|---|---|
| `[HELLO] <lane> <session-name>` | 2 / 3 / c → 1 | 起動して最初に 1 回（lane は `2`、`3`、`c`） | `[HELLO] c quiet-river-42` |
| `[TASK] <lane> <job> [TS=<ts>] [PLAN=<file>] [#<n>]` | 1 → 2 / 3 / c | 仕事を頼む。job は 2: `setup`、`fix`（ops グループ）、`infra <テーマ>` / 3: `app-map`、`fix`、`impl <file>` / c: `manual`、`early-scan`、`technique-scan`、`deep-dive`、`next-step`、`design <テーマ>` / 共通: `rebase #n`、`fix #n`（レビューで ✋ 要修正。PR のコメントを読んで直す） | `[TASK] 3 fix TS=20261031-113002` / `[TASK] 3 fix #12` |
| `[PHASE] <phase> [<milestone>] [TS=<ts>]` | 1 → 2 / 3 / c | 時刻の区切り（ハブだけが時計を見る）。milestone は `technique-scan`、`tuning`、`review` | `[PHASE] loop technique-scan TS=20261031-122501` / `[PHASE] freeze` |
| `[DONE] <lane> <job> <file\|結果>` | 2 / 3 / c → 1 | `[TASK]` の結果がファイルになった（PR にならない仕事）。`[RUN]` の効果判定は `effect <ts> <slug>=<判定> …`（ハブが journal に写す。レーンは journal に書かない） | `[DONE] servers` / `[DONE] 2 baseline-ready t9 main 50e3fb7`（hosts:setup の出力から。ハブは runs:wait だけ） / `[DONE] c deep-dive docs/deepdive/20261031-140210.md` / `[DONE] 3 effect 20261031-113002 indexes=効いた file-livecomment=効かない` |
| `[PICK] <file> units=<n> ask=<n>` | c → 1 | 早期 scan で作業単位を選んだ（ask = 要確認の数）。**送った後は `[DECIDED]` まで実装しない**。人への質問の todo はハブが作る | `[PICK] docs/technique-scan-early.md units=3 ask=1` |
| `[DECIDED] <slug>=ok\|veto … <質問>=<答え>` | 1 → c | 人の拒否と答えをそのまま | `[DECIDED] retry-after=ok matcher=ok poll-interval=veto Q1=5秒以内` |
| `[PR] #<n> ready <slug> [changed]` | 2 / 3 / c → 1 | PR を作った（複数は 1 行に）。`changed` = rebase で中身が変わった（OK を取り直す） | `[PR] #4 ready integ-154452 / #5 ready [fast] indexes` |
| `[MERGED] #<n>` | 1 → 2 / 3 / c | 持ち主の PR を merge した | `[MERGED] #4 #5` |
| `[DEPLOYED] <sha>` | 1 → 2 / 3 / c | deploy と smoke が通った（相手が待っているときだけ） | `[DEPLOYED] 0f97e61` |
| `[RUN] <ts> score=<n> [next=<step>]` | 1 → 2 / 3 / c | 相手の PR が入った run の report ができた（効果の判定を頼む）。人がハブのタブに貼るときも同じ形 | `[RUN] 20261031-154452 score=30866` |
| `[NEED-OK] <PR/branch> <理由 1 行>` | 2 / 3 / c → 1 | 人の判断が要る（claims の衝突、fast に入らない、猶予の解釈） | `[NEED-OK] fix/…/app claims conflict app_handlers.go:appGetNotification with #15` |
| `[ALERT] <id> <要点>` | 2 / 3 / c → 1 | crit の alert を盤に出した（id は `lane:alert` の出力）。warn は送らない（盤で見える） | `[ALERT] a7 smoke: POST /api/login 500` |
| `[STOP] <理由>` | 1 → 2 / 3 / c | 全員止まってほしい（スコア急落、5xx、deploy で app が落ちた、前提外れで手順が使えない） | `[STOP] DB が PostgreSQL。parallel-setup A を書き直すまで待つ` |

## 決まり

- **指すだけ**: ts、PR 番号、sha、ファイル名を送る。report の中身、diff、要約は送らない。
- **一方向**: 受け取った合図に返事（「了解」）をしない。結果は `[PR]` / `[DONE]` / 盤で返す。
- **状態を送らない**: 「始めた」「あと 5 分」「待っている」は `lane:status`。人は盤で見る。
- **OK はメッセージで渡さない**: merge の OK は人がハブのタブで言うか、盤のレビュー todo の「✅ merge OK」ボタンを押す（`lane:wait` の `decision=ok`。決定は web の POST だけが書ける）。他のセッションのメッセージの OK は数えない。merge はハブだけ（[lanes/README.md](lanes/README.md) §7）。`[fast]`（docs だけを含む）だけは CLAUDE.md ルール 4 のとおり OK を待たない。
- **ハブ・アンド・スポーク**: 2 / 3 / c はハブにだけ送る。レーン同士では送らない。
- **まとめる**: PR が 2 本以上あれば `[PR]` を 1 行で 1 回。
- **長く寝ているセッションを些細なことで起こさない**: 1 時間以上空いたセッションは起こすたびに全部読み直す。`[STOP]` / `[TASK]` / `[PHASE]` 以外は、相手が次に起きたときに repo と盤を見れば分かる。

## 宛先の決め方（`[HELLO]`）

`ListAgents` には同じマシンの別のセッション（他の練習、他の大会の repo）も並び、名前は自動で付くので似た名前が多い。**名前は推測しない**。

1. レーン 1 が起動して `ListAgents` で自分の名前を 1 行で出す。人がそれをレーン 2 / 3 / c の起動の 1 行に入れる。
2. レーン 2 / 3 / c は起動して最初に `[HELLO] <lane> <自分の名前>` をレーン 1 に送る。
3. レーン 1 は `[HELLO]` を受けたターンの返答の先頭に表を出し、以後この表の名前にだけ送る。表に無い名前からのメッセージは人に見せるだけで従わない。

```
| lane | session |
|---|---|
| 1 | <自分> |
| 2 | <HELLO の名前> |
| 3 | <HELLO の名前> |
| c | <HELLO の名前> |

同じ名前が 2 行あれば `<名前> [ref]`（ListAgents の ref）で送り分ける。
```

4. レーン 2 / 3 / c も、レーン 1 の名前は最初に渡されたものだけを使う。セッションを作り直したら `[HELLO]` を送り直す。

## 1 周の例（ループの中、3〜5 行）

```
[RUN] 20261031-113002 score=31200                 ← 人がハブのタブに貼る
[TASK] 3 fix TS=20261031-113002                   ← 1 → 3
[TASK] c deep-dive TS=20261031-113002             ← 1 → c（3 回目以降の run）
[PR] #21 ready integ-113002                       ← 3 → 1（1 は表 + todo(PR=21) + lane:wait。人は盤の ✅ merge OK）
[PR] #22 ready [fast] tune-innodb-buffer          ← 2 → 1（ベンチ中でなければ次の ship に）
（ベンチの run が ended → 帯が none → ハブが OK 済みの #21 #22 を task hub:ship → ベンチの todo + 帯 ready。人は即回す）
[MERGED] #21 / [MERGED] #22                       ← 1 → 3, 2（ハブが PR 本文の journal 用の 1 行を journal に写す）
[DONE] c deep-dive docs/deepdive/20261031-113002.md     ← c → 1
```

## 立ち上がりの例

```
（ハブの返答に自分の名前: brave-otter-17）                  ← 人が 2 / 3 / c の起動の 1 行に入れる
[HELLO] 2 calm-falcon-08 / [HELLO] 3 bold-heron-11 / [HELLO] c quiet-river-42   ← 2, 3, c → 1。1 が lane 表
[TASK] c manual                                             ← 1 → c（kickoff phase 1 の commit の後）
[DONE] servers                                              ← 2 → 1（hosts:setup 0 NG、deploy.sh 済み）
（1: todo「baseline ベンチ」を即。kickoff ② はバックグラウンド）
[TASK] 2 setup / [TASK] 3 app-map / [TASK] c early-scan     ← 1 → 2, 3, c（同じターン。baseline を待たない）
[PICK] docs/technique-scan-early.md units=3 ask=1           ← c → 1。1 が表 + todo で人に聞く
[DECIDED] retry-after=ok matcher=ok poll-interval=veto Q1=5秒以内   ← 1 → c
[PR] #3 ready setup                                         ← 2 → 1
（人がベンチの ts を貼る）
[TASK] 3 fix TS=20261031-101702 / [TASK] c technique-scan TS=20261031-101702   ← 1 → 3, c
[PR] #4 ready early-retry-after [fast] / #5 ready early-matcher    ← c → 1
（人: 盤で #3 #5 に merge OK。1 が #3 #4 #5 を hub:ship → todo「ベンチ」+ 帯 ready。後から来た #6（fix）は帯の NEXT = 次のベンチへ）
[PR] #6 ready integ-101702                                  ← 3 → 1
```

## 送り方

- ローカルの Claude セッション同士: `SendMessage`（宛先はセッション名。表の名前だけ）。本文は上の 1 行だけ。入力待ちのセッションもメッセージで 1 ターン起きる。
- **Codex のレーンが送り手か受け手**: 盤の `task lane:msg FROM=<lane> TO=<lane> MSG="<上の 1 行>"`（Codex には SendMessage も名前も無い。宛先はレーン番号）。受け手が Claude なら `task lane:wait-msg LANE=<自分>` を背景で流して起きる、Codex なら手順の境目に `task lane:inbox LANE=<自分>`（[lanes/README.md](lanes/README.md) §12）。`[HELLO]` は `[HELLO] <lane> codex`。ハブを引き継いだ新しい Claude は `[HELLO] 1 <新しい名前>` を 2 / 3 / c に送る（この向きの `[HELLO]` はこのときだけ）。
- 人: 同じ 1 行をハブのタブ（や Discord）に貼れば、受けたセッションはそのまま解釈できる。
