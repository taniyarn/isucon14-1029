# 構成の案（run 20261029-133401、レーン2）

元の数字（summary.md Verdict）: svc1はCPU 99%使用（mysqld 168%、isuride 14%、nginx 5%、上限は2コアで200%）。svc2とsvc3はどちらも約4%で空いている。MySQLは1272 q/s、threads_runningの平均は28.9で、DBのCPUで頭打ちになっている。

## 案1（推奨）: DBだけsvc3へ（#4 + #8 + #9）

- 何が変わるか: アプリのMySQLをsvc3に移す。matcherはsvc1だけで動かす。アプリとnginxはsvc1のまま。
- 空くCPU: svc1からmysqldの約168%が抜ける。DBはsvc3の2コア（約200%、今は使用約4%）をまるごと使える。
- 注意: ベンチもポータルも変わらない（svc1に向けたまま）。再起動ではsvc3が後から上がってもアプリはretryで戻る（#8でRequiresをWantsにした）。行数はPR済み（#4は約40行、#8は4行、#9は2行）。DEPLOY_FIRSTは空のままでよい。

## 案2: DBをsvc3へ + appを2台（svc1とsvc2、nginxはsvc1）

- 何が変わるか: 案1に加えて、nginxのupstreamにsvc2:8080を足す。`/api/internal/`と`/api/initialize`はsvc1に固定する（docs/plans/infra-app2.md）。
- 空くCPU: app（isuride）は今14%しか使っていないので、2台に分けても今は何も増えない。index・N+1を直してappが100%を超えてから効く。
- 注意: ベンチは変わらない。プロセス内キャッシュ（レーン3が入れるもの）は2台と両立しない。再起動ではsvc2のunitとSG（svc1→svc2:8080）が増える。約30行。

## 案3: 構成を変えない（index・N+1だけ）

- 何が変わるか: DBはsvc1のローカルのまま。レーン3のindex群だけを入れる。
- 空くCPU: 0（svc1の200%をDBとappが取り合ったまま）。代わりにDBまでの往復（同じホストなので約0.1 ms未満）が増えない。
- 注意: ベンチも再起動試験も今のまま。PRは0行。indexでmysqldが大きく下がった後に、もう一度考える案。

## 付録: 比べる表

| 案 | svc1のCPU | DBのCPU上限 | 増える遅延 | ベンチ/ポータル | 再起動の注意 | 行数 |
|---|---|---|---|---|---|---|
| 1 DB→svc3 | 約168%空く | 200%（svc3） | DBへの往復1回ごとに数百µs（同じVPC内） | 変わらない | svc3が遅れて上がる（retryで戻る） | PR済み |
| 2 DB→svc3 + app ×2 | 案1 + app分 | 200% | 案1と同じ + svc2経由の分 | 変わらない | svc2のunitとSG | 約30 |
| 3 そのまま | 0 | 200%（appと共有） | 0 | 変わらない | 今のまま | 0 |

推奨は案1。DBが律速（168%/200%）なので、まずDBにコアをまるごと渡す。案2は、appのCPUが律速になったら（Verdictが「app CPU-bound」になったら）出す。
