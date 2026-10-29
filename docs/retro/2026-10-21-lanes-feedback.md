# レーン運用へのフィードバック（ISUCON13 練習 2026-10-21、1 人で 3 セッション）

練習の流れ（JST）: 00:33 レーン 1 起動 → 00:40 マニュアル URL → 00:45 `[DONE] servers` → 01:03 baseline（3,113 点。1 回目は run 終了前の collect で report 不可、回し直し）→ 01:12 setup+fix の PR → 01:17 deploy / smoke → 2 回目のベンチ（run 20261020-161752）で切り上げ。

## 事実（盤と会話のログから）

| 区間 | かかった時間 | 中で何を待っていたか |
|---|---|---|
| `[DONE] servers` → baseline の todo | 約 16 分 | 練習専用の DNS の答え（`env.sh`）の PR → それを配る `deploy.sh` が kickoff phase 2（バックグラウンド 6 分）待ち → サブエージェントの main への push が自動モードで拒否 → 人が `! git push` → deploy → smoke |
| baseline → 2 回目のベンチ | 約 15 分（collect のやり直し 5 分を含む） | parallel-setup（DB 分離・計測・free wins・app-map）が **baseline の ts を待ってから**始まった。setup と fix を 1 つのレーンで回した |
| レーン c | 00:50〜01:17 のほとんど idle | 早期 scan の PR 2 本の後、`[RUN]` 待ちの指示しか無かった |
| レーン 2 | 00:46〜01:06 idle | `[DONE] servers` の後は baseline の ts 待ち |
| 単純なコマンド | 1 回で数十秒〜人の往復 | ハブが `task run:pull … \| tail; ls; find …` のように**パイプや `;` で繋いだ**ため、`.claude/settings.json` の許可（`Bash(task run:pull:*)`）に当たらず自動モードの判定に回って拒否された。ops への生の ssh も拒否。サブエージェントの `git push origin main` も拒否。その度に人に `!` で打ってもらった |

## 気づき → 直したこと

| # | 気づき（人） | 原因 | 直したこと |
|---|---|---|---|
| 1 | 初回ベンチまでが遅い | baseline の前に deploy を要る形にしていた（練習の DNS の答え + `deploy.sh` が kickoff phase 2 の成果物） | **baseline は deploy しない初期状態で、`[DONE] servers` の直後に todo**。`task deploy:init` はレーン 2（インフラ）が hosts:setup の直後に回す（kickoff phase 2 から外した）。練習専用の前提（DNS の答え、SG）はレーン 2 が `[DONE] servers` の前に済ませる |
| 2 | 1 回目と 2 回目の間が空きすぎ | parallel-setup が baseline の ts を待っていた | DB 分離・計測パッチ・free wins は **`[DONE] servers` で着手**（PR は作るが merge は baseline の後）。baseline の ts で来るのは parallel-fix だけ |
| 3 | 単純なコマンドが遅い。エージェントから打つのが原因か | 打つこと自体ではなく、許可のパターンに当たらない形（パイプ、`;`、生の ssh）で打って判定に回ったこと。人に回すともっと遅い | ルール「許可済みの `task …` は**単体で**打つ（パイプ・`;`・`&&` で繋がない。出力の絞り込みは次の呼び出しで）」を README §8 と各レーンに。許可を追加（`ops:diag`、`ops:logs`、`run:report`、`smoke`、`groups`、`bench:log`、`worktrees:clean`、`gh pr create`）。merge → deploy → smoke → ベンチの todo を 1 コマンドにした `task hub:ship PRS="…"`（ハブが 1 ターンで回す） |
| 4 | 「〜と入力して」のコピペが面倒 | todo の本文にコマンドが混ざっていた | todo に `CMD=`（貼る 1 行だけ）。`task lane:copy ID=t5` でクリップボードへ。`task lane:watch` は数字キーでコピー、`task lane:web` はブラウザの盤に 📋 ボタン |
| 5 | エージェントが暇している時間が長い | 各レーンの仕事が「ベンチの結果待ち」に縛られていた | 各レーンに「手が空いたら」の列（ベンチを待たずにできる仕事の一覧）。ハブは毎ターン、idle のレーンにそこから `[TASK]` を出す |
| 6 | app-analysis や DB 分離の PR はベンチを待つ必要が無いのでは | 「baseline は初期構成で測る」を「準備も baseline の後」と取り違えた設計 | 準備は並行、**merge だけ** baseline の後 |
| 7 | DB 分離の PR と parallel-fix を同じレーンにしたのはなぜ | レーン 2 を「report の行を PR にする係」と一まとめにしていた | レーン 2 = インフラ（サーバー、DB 分離、計測、nginx / MySQL、tuning、app ×2）、レーン 3 = アプリ（app-map、parallel-fix、構造変更の実装）に分けた |
| 8 | オーケストレーターは修正しないのか。それならオーケストレーター + 3 レーンか | ハブは人との会話・merge・deploy に専念する方が速い（練習でも merge の判断と deploy の待ちで手が埋まった） | **ハブ + 3 レーン（インフラ / アプリ / スカウト）の 4 枚**。前回の「4 枚目を作らない」はハブがコードも書く前提だったので撤回 |
| 9 | 2 回目のベンチの間、レーン c が暇だった | technique-scan を 12:30 の `[PHASE]` まで出さない設計 | baseline の ts で **technique-scan（run あり）**、3 回目以降の run で **deep-dive**。12:30 の `[PHASE]` は 2 周目の scan |
| 10 | `task lane:watch` が見にくい（「レーン1・レーン2・レーンc」の名前、流れていく、warn の消し方が分からない） | 定期的に print するだけだった | 流れない全画面の表示（色・絵文字、レーンの日本語名、alert ごとに「直ったら `task lane:resolve ID=…`（誰が）」）と、ブラウザで見る `task lane:web`（カード、📋 コピー、✅ 完了、解消のボタン） |

## まだ直していないもの（別の PR / 別のエージェント）

- ops の report が 17.5 s（run 20261020-160320）→ 108.6 s（20261020-161752、DB を svc3 に分けて計測パッチを入れた後）。調査は kit の report 担当に依頼済み（PR #13 と関係するかも）。
- panel の collect を run の終了前（agent の idle 10 s の前）に押すと、ホストのデータが消え run.json が無く、panel が leader を「破棄」扱いにして shadow も消す。再 collect できない。→ 「running の間は collect を押せない」か「再 collect できる」にする。
- mysql の qps で始まった run は MinReq の破棄の対象外なので、`task smoke INIT=1` の initialize だけで run が残る。
