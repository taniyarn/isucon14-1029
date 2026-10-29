# etc/だけで済むノブ（15:00のtuning用、レーン2）

元の数字: report 20261029-133401のTuning節とFleet（DBはsvc1にあった時点）。前提: #4でDBはsvc3、#9でthread_cache_sizeは256、#1でnginxのkeepaliveとGOGC=200は入っている。1ノブ1ベンチで試し、スコアが落ちず兆候が消えたものだけ残す。

## もう入っているもの（試さない）

| ノブ | 値 | どこで |
|---|---|---|
| innodb_flush_log_at_trx_commit / sync_binlog / skip-log-bin | 2 / 0 / on | zz-perf.cnf（svc3、#4） |
| max_connections | 1024 | zz-perf.cnf |
| thread_cache_size | 256 | zz-perf.cnf（#9） |
| nginxのupstream keepalive、worker_connections | 64 / 4096 | #1 |
| Go GOGC、pool | 200 / 64 | #1 |

## 候補（上から順に。根拠の欄が次のreportで確かめる行）

| # | ホスト | ノブ | 変える値 | 根拠（次のreportで見る行） | 判定 | ファイル |
|---|---|---|---|---|---|---|
| 1 | svc3 | `innodb_buffer_pool_size` | 128M → 1536M | DBホストはRAM 3.7GB。svc3のappは使わないのでその分を回せる。今のデータは小さい（chair_locationsが約2.3万行）ので、hit rateが99%未満のときだけ効く | 要確認（Tuning節にbuffer poolの行が出たら） | zz-perf.cnf（コメント行を有効にする。2Gではなく1536Mにする） |
| 2 | svc3 | `skip-name-resolve` | on | #4の後、svc1・svc2から毎回TCPで接続してくる（10.7 threads created/s）。名前の逆引きを接続ごとにしなくなる。grantは`'isucon'@'%'`なので影響しない | ○ | zz-perf.cnf |
| 3 | svc3 | `table_open_cache`の頭打ち | `open_files_limit`の確認 | 4.6 tables opened/s（4000でも足りない）。mysqldのLimitNOFILEが小さいと上げても効かない | ○（host:diagでopen_files_limitを見てから） | `etc/systemd/system/mysql.service.d/limits.conf`（LimitNOFILE=65535） |
| 4 | svc1/svc2 | app pool `SetMaxOpenConns` | 64 → 32 | threads_runningの平均28.9が2コアを超えていて、DBが列を作っている。indexが入った後もDBのCPUが張りついていたら下げる | ○（index群の後） | main.go（etc/ではない。レーン2のconfig行） |
| 5 | svc1 | sysctl `net.ipv4.ip_local_port_range` / `tcp_tw_reuse` / `somaxconn` | 1024 65535 / 1 / 4096 | TIME_WAITのピーク15825（keepaliveが入る前の値）。#1の後に下がっていれば不要 | ○（次のreportのTIME_WAITを見て） | `etc/sysctl.d/99-isucon.conf`（新規）+ deploy.shの63行目のコメントを外す |
| 6 | svc3 | `sort_buffer_size` | 2M | sort merge passes 1.2/s。index（ORDER BY付き）で消えるはずなので最後 | ○（indexの後に残っていたら） | zz-perf.cnf |
| 7 | svc3 | `performance_schema` | off | 数%のCPUとメモリが浮く。計測のagentが使っていないか先に確かめる | 要確認 | zz-perf.cnf |

## やらないもの

- `innodb_doublewrite=0`、`innodb_flush_method=O_DIRECT_NO_FSYNC`など耐久性を下げるもの: IMPLEMENTATION-NOTES §5で禁止されている。
- slow logを止める（zz-isucon.cnf）: 計測が止まる。最後に`[sprint]`の#2（計測OFF）でまとめて止める。deploy.shの62行目は有効にしない（deployのたびにslow logが戻り、DBも再起動する）。
- nginxのキャッシュヘッダ、gzip_static: マニュアルが許していない（IMPLEMENTATION-NOTES §7）。
