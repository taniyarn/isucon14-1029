# ops host（共有サーバー）

管制盤・計測結果の回収・レポート置き場。競技サーバーへは outbound の ssh だけで繋ぎ、競技サーバー側の設定は変えない。チームからは 443 + Basic 認証でアクセスする。

```
[PC] --https(443, basic auth)--> [Caddy] --> [panel :8080] --http--> [tunnel 1910N] --ssh -L--> [svcN 127.0.0.1:19100 isumeasure agent]
```

## 初回セットアップ（ops ホストで、1 回）

```
sudo bash /tmp/ops/setup-ops-host.sh          # docker, autossh, /opt/isucon-ops, 鍵ペア生成（公開鍵が表示される）
```

表示された公開鍵を、競技サーバーの `isucon` ユーザーの `authorized_keys` に入れる（本番は事前に GitHub に登録しておけばポータルが配る）。

## 起動

```
sudo mkdir -p /opt/isucon-ops/src && sudo chown -R ubuntu /opt/isucon-ops
# 手元から: scp -r tools/ops tools/isumeasure ubuntu@<ops>:/opt/isucon-ops/src/
cd /opt/isucon-ops/src/ops/compose
cp .env.example .env         # PANEL_DOMAIN, BASIC_USER, BASIC_HASH を編集
docker run --rm caddy:2 caddy hash-password --plaintext 'パスワード'   # → BASIC_HASH='<hash>'（シングルクォート必須、下の注意）
cp /opt/isucon-ops/isucon.env.example /opt/isucon-ops/isucon.env      # SVC1.., SSH_USER, CONTEST を編集
docker compose up -d --build
docker compose logs -f tunnel   # svcN への接続が張れているか
```

ブラウザで `https://<PANEL_DOMAIN>/` を開く。

**BASIC_HASH はシングルクォートで囲む**（`BASIC_HASH='$2a$14$...'`）。囲まないと compose が `$...` を変数展開してハッシュが壊れ、正しいパスワードでも 401 になる（caddy のログに `bcrypt algorithm version '3' requested is newer than current version '2'`）。`.env` を直したら `docker compose up -d --force-recreate caddy`（`restart` では env が読み直されない）。

## ops ホストを作り直したとき

compose の `.env`（Basic 認証）と鍵ペアはホストにしか無いので、新しいホストでは作り直しになる。`task ops:deploy` は `.env` を転送しないので、無いと `!! .env missing on the ops host` で止まる。

1. 手元の `.env` の `OPS_HOST`（と `PANEL_URL` を書いていればそれも）を新しい IP に変える。SG は 443 と自分の 22。
2. `setup-ops-host.sh` を転送して実行（docker, autossh, swap, `/opt/isucon-ops`, 鍵ペア）:
   ```
   scp -i <SSH_KEY> tools/ops/setup-ops-host.sh ubuntu@<OPS_HOST>:/tmp/
   task ops:ssh
   sudo bash /tmp/setup-ops-host.sh
   ```
3. `task ops:deploy`（`.env` 無しで止まるが、ソースの rsync はここで済む）。
4. ops ホストで compose の `.env` を作る（`PANEL_DOMAIN` は新しい IP の `<a-b-c-d>.sslip.io`）:
   ```
   cd /opt/isucon-ops/src/ops/compose && cp .env.example .env
   H=$(docker run --rm caddy:2 caddy hash-password --plaintext '<パスワード>')
   sed -i "/^BASIC_HASH=/d" .env && printf "BASIC_HASH='%s'\n" "$H" >> .env
   vi .env   # PANEL_DOMAIN, BASIC_USER（4 GB なら PANEL_MEM=2g REPORT_MEM_MB=1536）
   ```
5. 手元から:
   ```
   task ops:deploy      # compose を build して起動
   task ops:env         # isucon.env（CONTEST, SVC1..）を書いて tunnel/panel を作り直す
   task ops:repo        # repo の clone と毎分 pull の cron
   task host:authorize-ops HOST=1   # 鍵ペアが新しくなったので、競技サーバー全台に（HOST=2, 3 も）
   task ops:diag
   task ops:logs SERVICE=caddy FOLLOW=0   # 401 が続くなら上の BASIC_HASH の注意
   ```
   古いホストのデータ（`/opt/isucon-ops/data/`）は移らない。要るなら作り直す前に rsync で退避する。

## パネルの URL

| URL | 中身 |
|---|---|
| `/` | Fleet / Score timeline / Runs |
| `/data/<CONTEST>/measurements/<ts>/report.html` | run ごとの report（Runs の行から開く） |
| `/app/` | app-analysis のレポート（repo の `docs/reports/latest.html`）。compose が読み取り専用でマウントしている `REPO_DIR`（`/opt/isucon-ops/repo`）から返すので、refresh した html を main に push すれば、毎分の pull の後に見える。`..` / dotfile / repo の外を指す symlink は 404。**merge 前**は `/app/?ref=<branch>` で clone の `origin/<branch>` の同じファイルを返す（`git cat-file`、読むだけ。ページ上部に「branch … (not merged)」の帯）。panel 上部の「App analysis」カード（`/api/app`）が main の巡回時刻・commit と、main より新しい report を持つブランチを出す |

## コンテストの切り替え

`/opt/isucon-ops/isucon.env` の `CONTEST=` と `SVC1..` を書き換えて `docker compose up -d --force-recreate panel tunnel`（`restart` だと tunnel の env_file が読み直されない）。手元からは `task ops:env`。データは `/opt/isucon-ops/data/<CONTEST>/` に分かれる。

## データを消す（同じコンテスト名で練習をやり直すとき）

```
task ops:reset CONFIRM=<CONTEST>     # /opt/isucon-ops/data/<CONTEST> を削除して panel を再起動
```

別のコンテストに移るだけなら不要（`CONTEST` を変えれば別ディレクトリになる）。ホスト側に残っている未回収の run は `task hosts:status` で見え、collect すれば回収後に消える。

## 生ログを消す（ディスクが足りないとき。レポートは残す）

long_query_time=0 の DB ホストがあると 1 run の生データが 200 MB を超える（ops のディスクは 1 GB 弱しか空かない）。panel の Runs で消したい run にチェック → 「ログを削除（レポートは残す）」。確認ダイアログに解放される合計サイズが出る。各 run の raw 列が消せるサイズ。手元からは:

```
task run:prune TS=20261020-160320,20261020-161752   # panel の POST /api/prune と同じ
```

- 残るもの: run 直下のファイル全部（summary.md, report.md, report.html, report.json, actions.json, meta.json, collect.json, note.json, bench.log, flows.md/html）と、ホスト別の run.json, *.svg, *-flame.html, 小さいテキスト（explain.txt, db-catalog.txt, innodb-status*.txt, rows-*.txt, nginx-conf.txt, svc-env.txt）
- 消えるもの: ホスト別の *.log / *.log.gz（access, slow, app, error log）, *.jsonl, *.pb.gz（pprof）, processlist.txt, goroutine.txt と export/（CSV）。各ホストの agent に残っている同じ run のコピーも消す（/api/delete と同じやり方）
- 消した run は一覧で「ログ削除済み」。re-report / re-collect はできない（空のレポートで上書きしないため）。`task run:pull` はレポートだけを持ってきて警告を出し、`task deepdive` は止まる
- 自動では消さない。ただし従来どおり、collect のたびに新しい方から `RAW_KEEP`（既定 20）個より古い run の生ログは同じ残すものリストで消える

agent 0.3.1 からは slow log の 256 バイトを超える文字列リテラル（画像の blob など）を `'…(N bytes)'` に縮めて送る（ISUCON13 の run で 210 MB gz → 16 MB gz）。古い agent が送った run も report 側で同じように縮めながら読む。

## メモリ（report が落ちるとき）

report は panel とは別プロセスで走り、`REPORT_MEM_MB`（compose の `.env`、既定 1024）を超えると「report failed: memory over …」で止まる。panel 本体は落ちない。
2026-10-13 の練習では panel 内で report を作っていて、44 MB gz の slow log（DB 1 台に集約、long_query_time=0）で exit 137 → 再起動ループになった。今は slow log を読みながら集計するので、同じ run で 75 MB 程度。
保険は 3 段: (1) report の子プロセス上限 `REPORT_MEM_MB`、(2) panel コンテナの `mem_limit`（`PANEL_MEM`、既定 1536m。超えると cgroup 内で一番大きいプロセス = report の子が殺され、Caddy と tunnel は無傷）、(3) ホストの 2 GB swap（`setup-ops-host.sh`）。本番は t3.medium（4 GB）にして `PANEL_MEM=2g REPORT_MEM_MB=1536`。状態は `task ops:diag`（free / disk / コンテナの RSS と再起動回数 / oom-kill / report-error.txt の一覧）。
それでも落ちるなら `task run:pull TS=` で手元に持ってきて `isumeasure report` を実行する。

## 止める

```
docker compose down
```
