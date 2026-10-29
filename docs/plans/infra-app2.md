# app ×2の下準備（レーン2、PRにする前のメモ）

前提: PR #1（free wins、`upstream app`）とPR #4（DB → svc3、matcherはsvc1だけ）がmergeされていること。DBはsvc3、appはsvc1とsvc2、nginxはsvc1だけで受ける（ベンチはsvc1に向く）。

## 変えるもの（etc/とdeploy.shだけ、30行くらい）

1. `etc/nginx/sites-available/isuride.conf`の`upstream app`に`server 172.31.6.124:8080;`（svc2）を足す。重みは最初1:1にして、Fleetを見て決める。
2. `location /api/internal/`は`proxy_pass http://127.0.0.1:8080;`のまま（svc1に固定）。matcherはsvc1から`/api/internal/matching`を叩くので、マッチングはsvc1のappだけが行う。
3. `POST /api/initialize`はsvc1に固定する（`location = /api/initialize { proxy_pass http://127.0.0.1:8080; }`）。init.shはenv.db.shを読むのでどちらのホストで動いてもsvc3に入るが、言語タグと所要時間を1か所で見るため。
4. svc2のnginxはそのまま（ベンチは来ない）。svc2の`isuride-go`は`:8080`で全IFにlistenしている（`ListenAndServe(":8080")`）。svc1 → svc2の8080にSGで届くかを、`task host:diag`で先に確かめる。
5. svc3のappは`systemctl disable --now isuride-go`（DB専用にする）。deploy.shの3節を、`HOST`がsvc3なら飛ばすように変える。drop-inのRequires=mysqlは、svc1とsvc2ではローカルのmysqlを指したままでよい。

## 2台にすると壊れるもの（今のコードで）

- プロセス内の状態: `var db`以外のグローバルは無い（`webapp/go/*.go`をgrepした結果）。今は問題ない。レーン3がキャッシュ（ride status、chair locationなど）を入れたら、2台構成と両立しない。そのPRで「svc1だけ」か「DBから再構築」を決める。
- 通知（SSEやpolling）: 状態をDBから読んでいる限り、どちらのホストでも同じ結果になる。
- payment_mock: ホストごとのメモリで動いている。payment URLは`/initialize`の時にDBのsettingsに入るので、どちらのappも同じURLに送る（mergeの前に、`/initialize`のbodyを見て確かめる）。

## 再起動試験

- svc2のappは、DB（svc3）が後から上がってもping retryとRestart=alwaysで戻る。
- svc1のnginxは、upstreamのsvc2が落ちていてもsvc1側に流す（`max_fails`は既定値のまま）。
