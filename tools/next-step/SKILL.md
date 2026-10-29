---
name: next-step
description: ベンチの後に「次にどの手順（parallel-fix / technique-scan / deep-dive / 構成変更 / revert / 再計測 / 計測を直す / 失格回避 / app-analysis）を回すか」を 1 つ選ぶ。report の Next step 節（ルール判定）の上位が拮抗したとき、または人が迷ったときに使う。
---

# next-step

## いつ使うか

- report（`summary.md`）の **Next step** 節に「The top two are close: ask Claude」と出たとき。
- 人が「次どうする？」と迷ったとき。

人が言うこと: `next-step skill で。対象 run は <ts>`

**回すのはレーン 1（ハブ）**。run の ts を受けたとき（人が貼る / `[RUN]` / `task run:pull`）に、report の Next step が拮抗していれば回す。ハブが忙しいときは `[TASK] c next-step TS=<ts>` でレーン c に回させ、結論は `[DONE] c next-step <手順名>` で受ける。始めと終わりに `task lane:status`。結論はレーン 1 が `[TASK]` / `[PHASE]` にして配る（`docs/lanes/1-hub.md`）。

## 最初に（1 秒）

app-analysis の report をこの run で更新しておく（チームが「今解決すべきこと」タブを見ながら判断を聞けるように）:

```
task app:refresh TS=<ts>      # app-analysis の report に今回の run を反映（巡回しない、1 秒）
git add docs/reports/latest.html docs/reports/latest.md && git commit -m "app report <ts>" && git push
```
push すると、1 分以内に ops パネルの `/app/` で見られる（毎分の cron pull）。

## 読むもの（これだけ）

| ファイル | 見るところ |
|---|---|
| `measurements/<ts>/summary.md`（`task run:pull TS=`） | Next step 節（候補と理由）、Verdict、Flows（Score loops と Drop-off）、Next actions の P1/P2 の件数 |
| panel の Runs（スコアの推移、メモ）。読めなければ人に直近 3 run のスコアを聞く | スコアが伸びているか、横ばいか、落ちたか |
| `docs/journal.md` | 直近に入れた変更と、その結果 |
| `docs/technique-scan-*.md`、`docs/deepdive/*.md`、`docs/plans/*.md` | もう回した手順と、残っている作業単位 |
| 現在時刻 | ハブの時刻の区切り（`docs/lanes/1-hub.md`: 12:30 technique-scan、15:00 tuning、16:00 凍結、16:30 失格回避） |

コードやログは読まない（それは選んだ手順の中でやる）。

## 判断のしかた

1. **止める理由が先**: 計測が壊れている（code index 0、no alp.yml）、5xx が 1% 以上、スコアが前回より 10% 以上落ちた、16:30 を過ぎた。これらがあれば、それを選ぶ。
   - 特別賞の目標（`alp.yml` の `prize_target`）の 75% 以上でまだ誰も取っていなさそうなら、`score-sprint`（計測 OFF で 1 ベンチ。app ×2 は app の CPU 律速のときだけ）を最優先の候補にする。取られたら（リーダーボード）以後は候補から外す。
2. **残っている作業と時間を比べる**: P1/P2 が残っていて前回の変更が効いた → parallel-fix を続ける。P1 が無い、またはスコアが 3 run 横ばい → technique-scan（まだなら）か構成変更。
3. **CPU の形を見る**: app host が 90% 以上で他の host が空いている → 構成変更（T26 / T27 / T41）。CPU が余っていて遅い → deep-dive。
4. **同じ手順を 2 回続けて空振りしていないか**: parallel-fix を 2 周してスコアが動かないなら、別の手順に切り替える。

## 出力（10 行以内）

```
次: <手順名>（<skill 名>）
最初のコマンド: <コマンド 1 行>
理由: <数字つきで 1〜2 行。report のどの行か>
次点: <手順名> — <選ばなかった理由 1 行>
終わったら: <どの数字が動けば成功か>（例: svc1 の CPU が 90% → 60% 以下、score/POST が上がる）
```

決めるのは人。選んだ手順の実行（PR、デプロイ）はしない。止める理由（手順 1）に当たったら、出力の前に `task lane:alert`（スコア −10% / 5xx / 計測の故障は crit）。
