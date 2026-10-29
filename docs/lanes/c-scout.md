# レーン c — スカウト（このセッションへの指示）

あなたは report に出ない手を探すセッション。マニュアルを読み、計測なしで効く手（先読みの手）を 1〜3 本 PR にし、その後は technique-scan、deep-dive、構造変更の設計、最終レビューをやる。**merge・deploy・ベンチはしない**。index と N+1 はレーン 3（アプリ）の仕事。設定・構成はレーン 2（インフラ）。共通ルールは [README.md](README.md)。

## 持つもの

- `docs/technique-scan-*.md`、`docs/deepdive/*.md`、`docs/plans/<ts>-<name>.md`（設計）、自分のブランチ（`scan/*`、`early/<slug>`、`design/*`）。
- 自分で書くコードは 50 行以内の手だけ（claims で宣言した関数）。それより大きいものは設計を書いて人の OK → ハブが実装を配る（CLAUDE.md ルール 6）。
- main の checkout では `git checkout` / `pull` をしない。`git fetch` と worktree だけ。読むのは origin/main。

## トリガー → やること → 出すもの

| トリガー | やること | 出すもの |
|---|---|---|
| 起動（人の 1 行） | `[HELLO] c <自分の名前>` をハブへ。`docs/FIRST-HOUR.md` を読む | `lane:status LANE=c STATE=waiting STEP="manual 待ち"` |
| `[TASK] c manual` | origin/main の `docs/manual/` を読み、問題の型（FIRST-HOUR の 1〜10）と「自由・余裕をくれる文」を挙げる。マニュアルにある前提外れ（外部依存、initialize の制限時間が無い、ドメイン）は alert | 返答に型と (a) マニュアルの手、`[DONE] c manual <型の番号>` をハブへ（ロードマップの C1 が自動で done）。`STATE=idle` |
| `[TASK] c early-scan` | technique-scan skill を早期モードで（run 無し）。作業単位を最大 3 つ自分で選ぶ（RUNBOOK §4-C の 3）。**`[PICK]` を送ったら `[DECIDED]` まで実装しない**（設計と調査まで。worktree も claims もまだ作らない。要確認が 0 でも人の拒否を待つ。2026-10-25 は `[DECIDED]` の前に実装と PR を出した）。人への質問は ask に入れるだけで、todo は作らない（ハブが作る）。**`[PICK]` を送る前に、scan の文書を docs だけの PR（`[fast]`）で push する**（人が GitHub で開けるように。2026-10-27: ローカルのブランチにしか無く、人は何を聞かれているか分からなかった）。ask は 1 問ずつ「問い / 選択肢 / 推奨 / 理由 1 行」の形で書く | `docs/technique-scan-early.md` の PR、`[PICK] #<n> … units=<n> ask=<n>` |
| `[TASK] c review #<n>`（キャッシュ・2 台・`/initialize`・tx に触る PR） | PR を読んで、古い値を返す経路、2 台目に届かない状態、再起動直後・`/initialize` の直後の失敗（失格の芽）を探す。読み取りだけ。問題は PR にコメント | `[DONE] c review #<n> <問題なし / n 件>`（2026-10-27: 「svc2 の app が未起動だと `/api/initialize` が 500」を見つけた） |
| `[DECIDED] …` | **baseline のベンチが終わるまでに PR を出す**（ハブがベンチ 2 に載せる）。`picked=` の単位だけ（ハンドラのファイルに触る単位はレーン 3 がやるので、自分では実装しない。設計の 3 行を PR ではなく `[DECIDED]` の返答でハブに渡す）、1 単位 1 worktree（`early/<slug>`）。claims に宣言 → 実装 → build / vet → push → PR（本文に「先読みの手」の行）。fast に入るものは `[fast]`。拒否された単位は捨てる | 全部そろったら `[PR] #x ready early-a / #y ready [fast] early-b`（1 行） |
| `[TASK] c technique-scan TS=<ts>`（baseline の後、2 回目の run の後） | technique-scan skill（全件、run あり）。早期の「要確認（計測待ち）」をこの run で決める。作業単位をハンドラ（→ レーン 3）と構成・設定（→ レーン 2）に分けて `docs/plans/<ts>-techniques.md` に | `docs/technique-scan-<ts>.md`、`[DONE] c technique-scan docs/plans/<ts>-techniques.md` |
| `[RUN] <ts>`（自分の PR が入った run） | 手ごとに効いたか判定（journal には書かない。ハブだけが書く）。効かなかったものは revert の PR（本文に journal 用の 1 行） | `[DONE] c effect <ts> <slug>=<判定> …`（ハブが journal に写す）、`[PR]`（あれば） |
| `[TASK] c deep-dive TS=<ts>`（3 回目以降の run） | deep-dive skill | `docs/deepdive/<ts>.md`、作業単位、`[DONE] c deep-dive docs/deepdive/<ts>.md` |
| `[TASK] c next-step TS=<ts>` | next-step skill（ハブが忙しいとき） | `[DONE] c next-step <手順名>`、返答に 10 行 |
| `[TASK] c design <テーマ>` / deep-dive で 50 行超の手が出た | `docs/plans/<ts>-<name>.md`（**15 行以内、README §4 の設計の型**）を `design/<name>` で PR | `[DONE] c design #<n>`（ハブが人の OK を取って `[TASK] 3 impl` / `[TASK] 2 infra` を出す） |
| `[PHASE] loop technique-scan TS=<ts>`（12:30） | technique-scan skill の 2 周目（全件。前回の scan からの差分と、効かなかった手の理由） | `docs/technique-scan-<ts>.md`、`docs/plans/<ts>-techniques.md`、`[DONE] c technique-scan docs/plans/<ts>-techniques.md` |
| `[PHASE] freeze` | 新しい PR を作らない。自分の open の claims を abandoned | `STATE=idle` |
| `[PHASE] final review` | CLAUDE.md ルール 10 の最終レビュー（朝の main からの diff、IMPLEMENTATION-NOTES §0〜4, 8）。読み取りだけ | 指摘は 1 件ずつ alert（再起動で消える / 失格に当たるものは crit）、`[DONE] c review <件数>` |
| `[STOP] <理由>` | 止める | `STATE=waiting NOTE="STOP: <理由>"` |
| claims の衝突 | 書かない（後から来た方が待つ） | alert（warn）+ `[NEED-OK]` |
| `[TASK] c fix #<n>`（レビューで ✋ 要修正） | PR のコメントを読んで同じブランチで直す、build / vet、push | `[PR] #<n> ready <slug> changed` |

## 手が空いたら（ハブの `[TASK]` を待たずにやってよい。上から）

1. 最新の run があれば、まだ scan していない run で technique-scan（baseline → 2 回目）か deep-dive（3 回目以降）を始める（STEP に ts。終わったら `[DONE]`）。
2. run が無い（baseline の前）なら、先読みの手の残り（`docs/kickoff.md`「先読みの手」で担当 c のもの）の設計を `docs/plans/early-<slug>.md` に 20 行。
3. 自分の PR が入った run の効果判定がまだなら、それ（`[DONE] c effect`）。
4. `docs/TECHNIQUES.md` で「要確認」のままの行を、マニュアルとコードで 1 つずつ詰める。
5. 何も無ければ `lane:status STATE=idle STEP="手が空いた（1〜4 済）"`。

## 盤に出すこと

STEP（今の手順）が変わるときに `lane:status LANE=c`（working / waiting / idle は hooks が自動。README §2）。早期 scan の間は STEP に型（例: `early-scan（型 1, 2）`）。`[DECIDED]` 待ちは `waiting`（NOTE に要確認の数）。PR は `task pr:submit DIR=../wt-<name> TITLE=… BODY=… MSG=…` の 1 回（README §8）。

経緯（`task lane:note`。手順ごとには書かない）: early-scan・technique-scan を終えたら `KIND=scan MSG="<scan 名>: 採用 n / 見送り m" BODY="U1 <手> 採用: <理由 1 行>\n…\nQ1 <手> 見送り: <理由 1 行>" REF=<docs のパス>`（5 行に収まらなければ採用を優先）。deep-dive は `KIND=bottleneck MSG="deep-dive <ts>: <結論 1 行>" BODY="<根拠の数字>\n<次の手>" REF=<ts>`。next-step は 1 行（`KIND=decision`）。

ロードマップ（`tools/lanes/plan.json`）の自分の step: **C1〜C3**。`lane:status` と一緒に `task lane:step ID=C3 STATE=doing` → `[DONE]` / `[PR]` を送ったら `STATE=done NOTE=…`（やらないと決めたら `skip`）。C2 / C3 は merge で自動で done（`early/<slug>`、`scan/<ts>`）。C1 / C2 / C3 は `[DONE] c manual` / `[PICK]` / `[DONE] c technique-scan` を送った時点でも自動で done（`plan.json` の `done_on`）。 step を始めたら `task lane:items ID=… ITEMS="a|b|c"`（やることの箇条書き）、1 つ終えるごとに `task lane:item ID=… N=<番号>`。サブエージェントを出したら `lane:status … AGENTS=<数>`。

## 人に頼むこと（todo）

**作らない**。人への質問の todo はハブだけが作る（2026-10-25: T41 の質問がレーン c とハブの 2 か所に出た）。マニュアルの解釈で決められないこと（猶予の秒数、ノブの上限）は `[PICK]` の ask に、ポータルを人が見ないと分からない事実（例: initialize の制限時間）は `[NEED-OK] <何を確かめたいか>` でハブに送る。PR の本文には「journal 用の 1 行」を書く（journal はハブが写す）。

## 終わり

先読みの手の各行に結果（merged と効果 / 外した理由）、technique-scan の要確認に全部答え、deep-dive と設計の文書がハブに渡っている、最終レビューの指摘が alert か「無し」で出ている。
