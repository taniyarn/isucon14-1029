---
name: retrospective
description: コンテスト終了後（または練習後）に、ops に溜まった run の履歴（スコア、コミット、診断、メモ）と git log から振り返り文書を書く手順。「何をしたらスコアがどう動いたか」「時間をどこに使ったか」「次回のチェックリスト」を根拠付きでまとめる。
---

# retrospective

## 入力（`task retro` が揃える。約 1 コマンド）

```
task retro                                   # ops から runs.json と各 run の meta.json / summary.md を取得し、手元で retro.md / retro.html を生成
task retro REPO_DIR=~/repos/<contest-repo>   # run 間の commit 一覧と git-log.txt も付ける（.env の REPO_DIR でも可）
task retro ISSUES=1                          # gh issue list も同梱する（カレントの repo に gh が通っている時）
```

`measurements/<contest>-retro/` に次が揃う:

| ファイル | 中身 |
|---|---|
| `retro.md` | **最初に読む**。run の時系列（時刻、スコア、Δ、デプロイ commit、前回からの commit 一覧、診断の一言、req、5xx、メモ）、主要エンドポイントの平均レイテンシの推移、ベンチ出力、issue 一覧、診断の履歴 |
| `retro.html` | 同内容のグラフ版（人が眺める用） |
| `git-log.txt` | コンテスト repo の `git log --stat`（時刻付き）。誰がいつ何を変えたか |
| `measurements/<ts>/summary.md` | run ごとの自動診断（当時の verdict と next actions）。必要な run だけ読む |
| `measurements/<ts>/note.json` | run のスコア・メモ・ベンチ出力 |
| `issues.json` | GitHub issue（任意） |

## 手順（Claude がやる）

1. `retro.md` を読む。最高点、最終点、fail した run、スコアが大きく動いた run（±20% 以上）を拾う。
2. 動いた run について、「前回からの commit」と当時の `measurements/<ts>/summary.md` の next actions を突き合わせ、**何をした結果か**を 1 行で書く。コミットメッセージが曖昧なら `git-log.txt` の stat（触ったファイル）で補う。
3. 時間配分を出す。開始〜最初のベンチ（セットアップ）、最初のベンチ〜最初の改善コミット（把握）、改善の区間、終盤（再起動確認・片付け）。run の時刻と commit の時刻から機械的に出せる。
4. 以下の形式で `docs/retro/<contest>.md` を書く（repo 直下の `docs/retro/`）。推測と事実を分ける。

```
# <contest> 振り返り（<date>）

## 結果
最終スコア / 最高スコア / 順位（分かれば） / fail の有無と理由

## スコアの推移（事実）
時刻 → スコア の表（retro.md の Timeline を圧縮。5〜15 行）

## 効いた手・効かなかった手（事実 + 解釈）
- hh:mm  <変更>（commit）: <Δスコア>。<なぜ効いたか / 効かなかったか>
  ※ 診断（verdict）がどう変わったかを添える。例: "db-cpu → app-cpu に遷移"

## 時間の使い方
セットアップ / 把握 / 改善 / 終盤 の所要時間と、手が止まっていた区間（run の間隔が 40 分以上空いた所）

## 問題だったこと
計測・運用・チーム連携で詰まったこと（ベンチが走らなかった、デプロイ事故、ロールバックなど）。run の 5xx と note から拾う

## 来年に向けて（チェックリスト化できるものだけ）
- 準備で先にやっておく
- 当日の最初の 30 分で確認する
- 捨てる作業
```

5. 文章は 80 行以内。数字は表に。「もっと早く index を貼れた」のような反省ではなく、「次回は最初の 30 分で `summary.md` の P1 index を貼る」のように行動にする。

## 予算

- 読むのは `retro.md`（数 KB）と、スコアが動いた run の `measurements/<ts>/summary.md` だけ。全 run の summary を読まない。
- `git-log.txt` は grep で引く（`grep -n "^commit\|^Date\|^    " git-log.txt`）。
