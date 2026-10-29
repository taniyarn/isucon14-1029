---
name: handoff
description: 1 人で 4 セッションを回していた A がチームに合流するとき（特別賞を越え、スコアが伸び悩むか構成が固まったとき）に、ハブ（レーン 1）がそれまでの変更・効果・分かったこと・残りを 1 枚の docs/handoff.md にまとめ、チームが 10 分で状況をつかめるようにする手順。lanes README §11 の合流の目安に当たり、人が「合流する」を選んだときに使う。
---

# handoff

## いつ使うか

- lanes README §11 の「合流の目安」に当たり、盤の todo で人が「合流する」を選んだとき。
- 人が「handoff skill で」と言ったとき（練習の終わりにチームへ共有するときも）。

## 入力（読むものだけ。新しく計測しない）

| 何を | どこ |
|---|---|
| 変更と結果 | `docs/journal.md`（ハブが書いた行。PR、何を、なぜ、結果） |
| run の score と Verdict | `task runs`（全 run。誤検知の run = score なしは除く）、最新の `measurements/<ts>/summary.md`（無ければ `task run:pull TS=<ts>`） |
| 効果判定 | レーンの `[DONE] <lane> effect …`（journal の結果欄に写したもの） |
| 構成 | `deploy.sh` の DB ホスト節、`env.db.*.sh`、`etc/nginx/sites-enabled/*` の upstream、kickoff.md の構成要素 |
| open のもの | `gh pr list --state open`、`docs/claims.md` の open、盤の open の alert（`task lane:board`） |
| 次の手の候補 | 最新の `docs/plans/<ts>.md`、`docs/plans/deepdive-*.md`、`docs/plans/<ts>-techniques.md` |

## 出力: `docs/handoff.md`（80 行以内、表中心、日本語）

```
# handoff <日付 hh:mm>（A → チーム）

## 今
- スコア: baseline <n> → 最新 <n>（run <ts>）/ 最高 <n>（run <ts>）。特別賞: <目標> を <hh:mm> に越えた / 未達
- 構成: svc1 = …、svc2 = …、svc3 = …（DB、付随サービス、app の台数、nginx の振り分け）
- 律速（最新の Verdict）: …

## 効いた変更（大きい順）
| PR | 何を | 根拠 | 前 → 後（score） | 効いた指標 | 状態 |

## 効かなかった / 戻した変更
| PR | 何を | 結果 | なぜ（分かっている範囲。推測は推測と書く） |

## 分かったこと
- 律速の推移（例: DB → app CPU → 待ち → ベンチ機）
- ベンチの形（固定並列か、負荷が上がる条件）
- 失格の芽と対処（再起動・initialize・2 台・DNS の答え）

## 残っているもの
| 種類 | 何 | 持ち主 | 次にやること |
（open の PR、open の claims、open の alert、未実施の再起動試験）

## 次の手の候補
| 手 | 根拠（report の行 / scan / deep-dive） | 見込み | 触るもの | issue |
```

## 手順

1. `task lane:status LANE=1 STATE=working STEP="handoff"`。
2. 上の入力を読み、表を埋める。**効果の数字は run の score と report の指標だけ**（推測は「推測」と書く）。複数の PR を 1 本のベンチで入れた run は「#a #b の合計」と書き、1 本ずつの効果を作らない。
3. 「次の手の候補」は issue にする: `task actions:issues TS=<最新の ts>`（`GH_PROJECT` があれば Project にも入る）。handoff にしか無い候補は `gh issue create --title … --body …` で 1 本ずつ（本文は issue テンプレート「改善タスク」の欄: 根拠・何をするか・担当・期待効果・触るもの・確かめ方）、続けて `task project:add URL=<issue> LANE=… PRIORITY=… SCORE=…`（Score の決め方は RUNBOOK §6f）。表の issue 列に番号。
4. ブランチ `docs/handoff-<hhmm>` で commit → push → `gh pr create`（タイトル `[fast] docs: handoff <hh:mm>`）→ docs だけなので `task hub:ship PRS="<n>"`（deploy なし）。
5. 返答に PR のリンクと、`docs/handoff.md` の「今」の 3 行。人がチーム（Discord）に共有する。
6. `task lane:status LANE=1 STATE=idle STEP="handoff 済み（PR #n）"`。以後は RUNBOOK §6f（GitHub Project）。

## やらないこと

- 新しい計測やベンチを頼む（今ある run だけで書く）。
- journal と違う数字を書く（journal が間違っていたら journal を直す commit を先に）。
- 人ごとの評価を書く（変更と事実だけ）。
