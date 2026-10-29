---
name: score-sprint
description: 特別賞（最初に決められたスコアに到達した 1 チーム）が手の届く所に来たとき、ログと計測を止めた 1 ベンチで目標スコアを越えにいく手順。app ×2 + DB ×1 にするのは Verdict が svc1 の app の CPU 律速のときだけ（DB 律速なら計測 OFF だけ）。report の Next step に「特別賞を取りに行く」が出たとき、または C が声をかけたときに使う。
---

# score-sprint

## いつ使うか

- report の冒頭に `Special prize target N: this run X = Y%` が出ていて、Next step に「特別賞を取りに行く」が出たとき（今のスコアが目標の 75% 以上、まだ届いていない）。
- 目標スコアは kickoff phase 1 がマニュアルの「特別賞」節から読み、`alp.yml` の `prize_target:` に書いてある。無ければ C に聞いて書く。
- 例年の目標と達成時刻: 15,000〜80,000 点、11:54〜13:05（ISUCON 9〜14）。昼前後が勝負になる。

人が言うこと: `score-sprint skill で。対象 run は <ts>`

## 考え方

1 ベンチだけ「速い構成」にして目標を越え、越えたら（越えなくても）元の計測できる状態に戻す。

- **app ×2**: svc1 の nginx から svc1 と svc2 の app に振り分ける。DB は svc3（parallel-setup で分離済み）。**app の CPU が律速のときだけ**効く。DB が律速のときに app を増やすと DB への同時接続が増えて下がる（2026-10-18 の練習: 律速は svc3 の 2 コアの DB。app ×2 で 30,866 → 26,175、revert）。
- **計測 OFF**: レーン 2 が parallel-setup（サブエージェント E）で最初から用意してある `[sprint]` の PR 2 本を使う。`infra/measure-off` は `deploy.sh` の 2d ブロック（`MEASURE_OFF=1`）で、deploy のたびに各ホストで `tools/bootstrap/measure-off.sh off` を走らせる = `task hosts:measure-off` と同じ中身（isumeasure-agent の停止と disable、slow log の設定 `zz-isucon.cnf` を退避して DB を再起動、nginx の `access_log` を off、動いている isu* の unit に `MEASURE=0` の drop-in（sqltag、pprof、uid ヘッダが止まる）と再起動）。戻しの `infra/measure-on` は同じブロックを `MEASURE_OFF=0`（`measure-off.sh on`）にする 1 行。PR にしておく理由: `task hosts:measure-off` だけだと、次の `task deploy` が `etc/nginx/nginx.conf` を入れ直して access log が戻る。このベンチは ops の report が作られないので、スコアはポータルで見る。

2 台にして壊れるのは「プロセス内の状態」と「ホストに縛られる処理」だけ。手順 1 で必ず列挙する。

## 手順（Claude がやる）

### 0. やるかどうかを決める（ゲート、2 分）

対象 run の `summary.md` の **Verdict** と Fleet のホスト別 CPU で決める。

| Verdict（律速） | やること |
|---|---|
| svc1 の app の CPU（app の CPU 90% 以上、DB ホストに余裕） | app ×2 + 計測 OFF（手順 1 から） |
| DB（svc3 の mysqld の CPU、ロック、slow query） | **計測 OFF だけ**（手順 1〜2 は飛ばし、手順 3 の measure-off から）。または sprint をやめて普通の改善を続ける |
| 待ち（wait-bound、CPU が余っている。ポーリング間隔、マッチング間隔、プール上限） | sprint をやめて改善を続ける（計測 OFF では待ちは縮まない）。目標まで近ければ計測 OFF だけ |

判断を 1 行で人に見せ、OK をもらってから進む。迷ったら計測 OFF だけにする（構成を変えないので、戻すのも速い）。

### 1. 前提を確かめる（5 分、読むだけ。app ×2 のときだけ）

次を表にして人に見せる。1 つでも「不明」があれば、その endpoint は svc1 に固定する側に倒す。

| 確かめること | 見る場所 |
|---|---|
| DB が svc3 に分離済みで、svc2 の app も svc3 を向くか | `deploy.sh` の `DB_HOST` / `DB_PRIVATE_IP`、`env.db.<svc2 の hostname>.sh`（無ければ作る: svc1 用と同じ内容）。`task hosts:ips` で hostname と private IP |
| プロセス内の状態（キャッシュ、mutex で守った採番、常駐ワーカー） | `git grep -nE 'sync\.Map|cmap\.|Cache\.(Store|Load|Set)|\.Lock\(\)|go func' webapp/go`、`docs/app-map.md` の「状態」 |
| ホストに縛られる処理（外部コマンド、ローカルファイル） | `git grep -nE 'exec\.Command|os\.(WriteFile|Create|OpenFile)' webapp/go` |
| initialize がプロセス内の状態を作り直すか | initialize ハンドラ。キャッシュがあるなら、svc2 にも作り直しを届ける必要がある（全台に配る）か、その endpoint を固定する |
| app が `0.0.0.0` で listen しているか | `main.go` の listen |

判断:
- キャッシュがあり、その無効化を全台に届けられない → **その読み書きをする endpoint を全部 svc1 に固定**するか、スプリントをやめる（整合性チェックで落ちて 0 点になるより、今の構成で 1 回回す方がよい）。
- `/api/initialize` は常に svc1 に固定する。

### 2. ブランチ `sprint/2app` を作る（app ×2 のときだけ）

1 つの worktree・ブランチで（CLAUDE.md ルール 4）:
- `etc/nginx/`: upstream を 2 台に。雛形は `tools/bootstrap/etc/nginx-upstream-2app.conf.example`。
  ```nginx
  upstream app {
      server 127.0.0.1:8080;
      server <svc2 の private IP>:8080;
      keepalive 64;
  }
  # site の server {} の中: 既存の location /api は proxy_pass http://app; のまま（keepalive の設定も）
  # 手順 1 で固定すると決めた endpoint を svc1 だけに:
  location = /api/initialize { proxy_http_version 1.1; proxy_set_header Connection ""; proxy_set_header Host $host; proxy_pass http://127.0.0.1:8080; }
  ```
  `location =` は完全一致。パスにパラメータがある endpoint は `location ~ ^/api/user/[^/]+/icon$` のように正規表現で。
- `deploy.sh`: nginx をインストールする行が有効か確かめる（`etc_install etc/nginx/... && sudo nginx -t && sudo systemctl reload nginx`）。
- svc2 の DB の接続先（`env.db.<svc2 の hostname>.sh`）。
- `cd webapp/go && go build ./... && go vet ./...`、`bash -n deploy.sh`。
- PR を作り、本文に手順 1 の表と「固定した endpoint」を書く。**リンクを貼って止まる**。

### 3. 人の OK の後

**計測 OFF のベンチの前後で、同じ構成の計測 ON の run を 1 本ずつ取る**。計測 OFF のベンチは run が残らないので、スコアが下がったとき（練習の 26,175）に何が下げたのかを後から調べられない。前後の run があれば「構成の変化」と「計測 OFF の効果」を分けて読める。

```
gh pr merge --merge                           # app ×2 のときだけ
task deploy BRANCH=main
task smoke IP=<svc1 の public IP> INIT=1     # 入口（nginx → 2 台）
task smoke IP=<svc2 の public IP>            # svc2 の app 単体が DB につながっているか（app ×2 のときだけ）
```
1. 「ready for bench（計測 ON、sprint の前の run）」と言って止まる。人がベンチ → collect → report。この run の ts をメモする。
2. **計測 OFF の PR（`[sprint]` の `infra/measure-off`）を merge → deploy**（ハブ: 人の OK の後 `task hub:ship PRS="<measure-off の #>"`。deploy が各ホストで計測を止め、smoke も通す）。PR が衝突していれば `[TASK] 2 rebase #<n>`、PR が無ければ `task hosts:measure-off`（このときはベンチが終わるまで deploy しない。deploy で nginx の access log が戻る）。
3. 「ready for bench（計測 OFF のスプリント）」と言って止まる。ベンチは人が回す。

### 4. ベンチの後

1. スコアをポータルで確かめ、panel の Runs（このベンチは計測が無いので run は作られない。メモに残す）と `docs/journal.md` に書く。リーダーボードで「最初に到達したか」を確かめる。
2. **戻しの PR（`[sprint]` の `infra/measure-on`）をすぐ merge → deploy**（`task hub:ship PRS="<measure-on の #>"`。revert ではなく `MEASURE_OFF=0` の 1 行なので、deploy が `measure-off.sh on` で計測を戻す）。`task hosts:measure-off` で止めた場合は `task hosts:measure-on`。
3. **同じ構成のまま計測 ON でもう 1 本ベンチ**（sprint の後の run）。前の run と比べて、構成が効いたのか（前後の run が sprint 前より上か）、計測 OFF だけの差かを `docs/journal.md` に 1 行で書く。
4. 2 台構成を残すかは人が決める（前後の run で判断する）。
   - スコアが上がり、エラーが無い → 残す（以後の改善も 2 台前提。プロセス内キャッシュを足すときは無効化を全台に届ける）。
   - 下がった・エラーが出た → `git revert -m 1 <merge commit>` → deploy。

## やらないこと

- 計測 OFF のまま改善を続ける（何が効いたか分からなくなる）。スプリントは 1〜2 ベンチで終える。計測 OFF は特別賞の 1 本と最終確認のときだけ。
- DB 律速・待ち律速のときの app ×2（手順 0。練習で 15% 下がった）。
- 計測 ON の前後の run を取らずに計測 OFF のベンチを回す。
- 16:00 の凍結後のスプリント（最終スコアと再起動試験の手順が優先）。
- 猶予の無い読み込みをキャッシュして 2 台に載せる（整合性チェックで落ちる）。
