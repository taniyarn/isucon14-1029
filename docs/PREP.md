# 前日までに済ませること（PREP）

当日は触らない準備。上から順に。各ステップの「確認」が通ったら次へ。当日の手順は [`RUNBOOK.md`](RUNBOOK.md)。

| 担当 / レーン |
|---|
| 上から順に、各ステップの「確認」が通ったら次へ |

1. **コンテスト用 repo を作って clone する**
   ```
   gh repo create <org>/<contest> --template ytakaya/isucon-kit --private --clone && cd <contest>
   cp .env.example .env
   ```
   `.env` に書く: `SSH_KEY`（ops と競技ホストに入れる秘密鍵）、`OPS_HOST`（ops の IP）、`CONTEST=<contest>`、`GIT_REMOTE=git@github.com:<org>/<contest>.git`、`LANGS=go`。`HOSTS` だけが当日の値（ポータルの 3 台の IP）で、これを書いた瞬間から全タスクが `HOST=1|2|3` で動く。`HOST_LOGIN`（本番は `isucon`、練習 AMI は `ubuntu`）と `HOST_USER=isucon` も今のうちに。
   確認: `task --list` が出る。

2. **deploy key を作って登録する**（GitHub の deploy key は 1 鍵 = 1 repo なので毎回新しく作る）
   ```
   ssh-keygen -t ed25519 -N '' -f tools/bootstrap/deploy_key -C <contest>
   cat tools/bootstrap/deploy_key.pub      # → repo の Settings → Deploy keys に Allow write access で登録
   ```
   この鍵をサーバー（bootstrap が `isucon` ユーザーに入れる）と ops（`task ops:repo`）が共用する。
   確認: `ssh -i tools/bootstrap/deploy_key -T git@github.com` が "successfully authenticated" を返す。

3. **ops ホストを用意して、この repo の道具を配る**
   - 初めてなら `tools/ops/README.md` の手順で EC2 を 1 台（SG は 443 だけ）、`setup-ops-host.sh`、compose の `.env`（PANEL_DOMAIN、Basic 認証）。既にあるならそのまま。作り直したときは `tools/ops/README.md` §ops ホストを作り直したとき（compose の `.env` と鍵は移らない）。
   - サイズ: 練習は t3.small（2 GB）で足りる。**本番は t3.medium（2 vCPU / 4 GB、$0.05/h）** を推奨。gp3 30 GB。report は子プロセスで `REPORT_MEM_MB`（既定 1024）まで、panel コンテナは `PANEL_MEM`（既定 1536m）まで。4 GB なら compose の `.env` に `PANEL_MEM=2g REPORT_MEM_MB=1536`。`setup-ops-host.sh` が 2 GB の swap を作る（保険。常用すると遅いので `task ops:diag` で swap 使用量を見る）。前日に `task ops:diag` で free / disk / 再起動回数が全部きれいなことを確認する。
   ```
   task ops:deploy        # この repo の tools/ops + tools/isumeasure を ops に rsync して compose を再ビルド（kit を更新したら必ず）
   task ops:env           # .env の CONTEST を ops の isucon.env に書く（HOSTS が空でも可）。panel と tunnel を作り直す
   ```
   確認: `https://<PANEL_DOMAIN>/` が開き、ヘッダに `contest: <contest>` と出る。Fleet は空で良い。Basic 認証のパスワードは本番前に変える（`caddy hash-password`）。

4. **手元の道具**: `task`, `rsync`, `jq`, `gh auth status`, `duckdb`, `node`（`cd tools/app-analyzer && npm run setup`）。
   確認: `task build` が通る（`tools/isumeasure/dist/` にバイナリ）。

5. **ssh 鍵**: ポータルに GitHub アカウントを紐付けると、そのアカウントの SSH keys がサーバーに配られる（例年）。**ops の公開鍵（`task ops:key`）をメンバー 1 人の GitHub アカウントの SSH keys に登録しておく**と、ops → サーバーの ssh も配布だけで通る（deploy key ではなくアカウントの SSH key）。
   練習（AMI 起動）のときは代わりに `SSH_KEY` の鍵で入れるので、`task host:authorize-ops` が ops 鍵を入れる。

6. **Basic 認証のパスワードをチームに共有**（当日のチャットではなく前日に）。

7. **Claude の許可設定**（当日は A が 1 人で Claude セッションを 4 枚回す。人は盤とレーン 1 しか見ていないので、レーン 2 / 3 / c が許可の確認で止まると気づきにくい。許可済みのコマンドも、パイプや `;` で繋ぐと許可に当たらない（`docs/lanes/README.md` §8）。セッションは許可の要るコマンドの直前に盤を「許可待ち」にする: `docs/lanes/README.md` §2）
   - **盤のコマンド `task lane:status|alert|resolve|todo|done|board|phase` は許可に足す**（手元の repo の git common dir の下に書くだけで、サーバーも ops も変えない。止まると盤が更新されず、この仕組み全体が働かない）。`.claude/settings.json` に `"Bash(task lane:*)"`。
   - repo の `.claude/settings.json` に **読み取り専用** のコマンドだけ許可してある（`task env:show` / `hosts:ips` / `hosts:status` / `host:diag` / `host:peek` / `db:query`（SELECT 等しか通らない）/ `run:pull` / `actions:plan`、`gh pr view|list|diff|checks`、`gh issue view|list`、`git status|log|diff|show|fetch`、`go build|vet`、`gofmt -l`）。
   - 練習の後、練習の repo で Claude に `/fewer-permission-prompts` を 1 回。よく止まったコマンドの候補が出る。**読み取り専用のものだけ**を kit の `.claude/settings.json` に足す。足さないもの: `ssh`、`rm`、`systemctl`、`mysql`（書き込み）、`git push`（特に main）、`gh pr merge`、`task deploy` / `task smoke INIT=1` / `task diffcheck`（サーバーやデータを変える）。これらは毎回人が許可する。
   確認: 練習で 3 セッションを並べたとき、許可待ちで止まったのが書き込み系だけだった。

8. **（試験運用）クラウドの自動レビュー**: repo に Claude の GitHub App を入れ、claude.ai/code/routines に `pull_request.opened` で起動するタスクを作る。filter・プロンプト・確かめ方は [`cloud-review.md`](cloud-review.md) §1。
   確認: 小さな PR を開いて数分以内に `cloud-review:` のコメントが付く。

---
