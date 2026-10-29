# practice-log — 練習ごとの記録と推移

kit やエージェントの変更で、1 人 4 セッションの周回が速く・安くなったかを練習をまたいで見る。1 練習 = 1 JSON（定義は `SCHEMA.md`）、全部をまとめた `report.html`。

**練習が終わったら（contest repo で）**:
1. `task practice:record SLUG=isucon14 NOTES_CHANGED="この練習の前に kit で変えたこと" NOTES_HYPOTHESIS="何が動くと思ったか" NOTES_OBS="見えたこと" REBOOT=not_run OUT=<kit の checkout>`（`OUT` の代わりに `KIT_DIR` を env に置いてもよい。どちらも無ければ JSON を表示するので手で置く）
2. kit の checkout で `task practice:report` → `practice-log/report.html` をブラウザで開く
3. 記録と report を kit のブランチに commit して PR（`docs/retro/` の振り返りと同じ PR でよい）

**report の読み方**: (a) URL → 特別賞の分が北極星。点の上の文が「その練習の前に変えたこと」。(b) その内訳と、同じ区間で人を待っていた分。(c) 左下ほど安くて速い（色 = モデルの組み合わせ）。(d) レーンごとの費用・特別賞までの idle・merge された PR。(e) 全部の数字（— = null）。

**kit の PR の本文には「狙う指標」を書く**（例: `狙う指標: speed.url_to_baseline_ready_min ↓、agents.2.idle_min_before_prize ↓`）。次の練習の記録で動いたかを確かめ、動かなければ戻すか次の手を考える。題材も目標も練習ごとに違うので、1 回の差は目安。
