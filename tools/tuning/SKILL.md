---
name: tuning
description: 終盤（15:00〜15:45）に、report の「Tuning」節が ● を付けたノブ（MySQL / Go / nginx / カーネル）を 1 つずつ 1 ベンチで当てて、効いたものだけ残す。探索はしない。
---

# tuning

## いつ使うか

**15:00〜15:45 の 1 回だけ**（対象は Tuning 節の「いつ = 最後（15:00 tuning）」の ●。「すぐ（fast lane）」の ● は run の直後にハブがレーン 2 へ `[fast]` PR として渡しているので、ここでは入っているかを確かめるだけ）。条件は 3 つ: 機能変更の PR が全部 merge 済み、直前の run の `summary.md` に「Tuning」節の ● がある、16:00 の凍結まで 1 時間ある。A が Claude に:

```
tuning skill で。対象 run は <ts>
```

16:00 以降は触らない（追試の 75% ルール。TECHNIQUES §7）。● が無ければ何もしない（「free-wins の既定値のまま」と返して終わる）。

## 入力

- `measurements/<ts>/summary.md` の「Tuning」節（`task run:pull TS=<ts>`）。各行に ノブ / 現在値 / 兆候 / 推奨 / ●○。
- `etc/mysql/mysql.conf.d/zz-perf.cnf`、`etc/nginx/`、`etc/systemd/system/<unit>.d/restart.conf`（deploy.sh が `etc_install` で配る）。
- 直前 2 run のスコアと p99（管制盤の Score timeline）。ブレの基準にする。

## 手順

1. ● の行を **効果の大きい順** に最大 3 つ選ぶ。順序は固定: (1) buffer pool / max_connections / app pool（DB 待ちを消す）、(2) GOGC（GC が 10% 超のときだけ）、(3) nginx keepalive / worker_connections / rlimit、(4) カーネル（somaxconn、port range）。○ は触らない。
2. 1 つ目を **1 commit** で入れる。置き場所は必ず `etc/` か unit drop-in か `main.go` の接続まわり（deploy.sh で配れる形。手で sudo しない）。
   - MySQL: `zz-perf.cnf` に追記。buffer pool と redo log の変更は mysql の再起動が要る → `task deploy` の DB ホスト先行で入る。再起動中は app が落ちないこと（restart.conf）。
   - Go: `restart.conf` の `[Service]` に `Environment=GOGC=400` のように。pool は `main.go` の `SetMaxOpenConns`。
   - nginx: `etc/nginx/nginx.conf` の `worker_*`、site の `upstream { keepalive 64; }` + `proxy_http_version 1.1` + `proxy_set_header Connection ""`。
   - カーネル: `etc/sysctl.d/99-isucon.conf`（deploy.sh に `sysctl --system` が無ければ 1 行足す）。
3. PR を作ってリンクを貼り、**B の OK** を待つ（他の PR と同じ）。merge → `task deploy` → `task smoke` → ベンチ。
4. 判定: 直前 run と比べて **スコア −3% 以内かつ対象の兆候が消えた** なら残す。スコアが 3% 超下がった、FAIL、兆候が消えないなら `git revert` して deploy。ブレが ±5% あるので「+1% 上がった」を根拠にしない。
5. 2 つ目、3 つ目も同じ。15:45 を過ぎたら残りは捨てる。最後に `docs/journal.md` に「入れたノブ / 戻したノブ / 理由」を 3 行。

## やらないこと

- 探索（値を変えて何度も回す）。1 ノブ 1 回。
- `innodb_flush_log_at_trx_commit=0`、redo を tmpfs へ、`innodb_doublewrite=0` のような耐久性を削る設定。再起動試験でデータが消える側の博打（TECHNIQUES T24）。
- JSON ライブラリ、GOMAXPROCS、`performance_schema=OFF`、http2 の on/off を 15:00 以降に変えること（効果が小さく、挙動が変わる）。
- `summary.md` 以外の生ログを読むこと。

## 出力

PR 本文に「ノブ / 現在値 → 新値 / 根拠（Tuning 節の行）/ 戻し方（revert の SHA）」。終わったら Discord 用に 5 行（残したノブ、戻したノブ、スコアの推移）。
