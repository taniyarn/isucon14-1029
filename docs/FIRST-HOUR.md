# 最初の 1 時間に打てる手（マニュアルと参考実装だけで決まるもの）

計測を待たずに、**マニュアルの一文と参考実装のコードだけ**で「効く」と言える手を、問題の型ごとに並べたもの。kickoff phase 2 の「先読みの手」、technique-scan の早期モード、RUNBOOK のレーン c が使う。数字と実例は全部 `docs/TECHNIQUES.md`（と各 T# の原本 `docs/techniques/sources/`）から引いている。ここに無い数字を足さない。

なぜ要るか: 2026-10-18 の練習（ISUCON14）で一番大きく伸びた 2 手は、どちらも計測無しで読めた。

| 手 | 読めた所 | スコア | 出典 |
|---|---|---|---|
| マッチングの作り直し（T34） | マニュアル「内部の matching は自由に変更してよい」、コードの `ORDER BY RAND()` と 0.5 秒に 1 件 | 8,188 → 30,866 | `docs/journal.md` 00:55 |
| `retry_after_ms` 30 → 100/200 ms（T17） | マニュアル「クライアントは `retry_after_ms` 待ってから次を送る」 | 35,661 → 48,381 | `docs/journal.md` 01:20 |

どちらも report には P1 として出なかった（matching は平均 1 ms、ポーリングは P2）。最初の parallel-fix（00:42）から特別賞の目標（32,767、01:17）まで 35 分かかったうち、この 2 手は 2 回目以降のベンチまで待たずに入れられた。

## 使い方

1. 下の「型」から、この問題に当てはまるものを選ぶ（1 問で 2〜4 型が普通）。マニュアルの構成図と `etc/systemd/system/*.service` を見れば決まる。
2. 各型の「マニュアルの文言」で `docs/manual/*.md` を grep し、「コードの兆候」で `webapp/` を grep する。両方当たった手だけを「先読みの手」に入れる。
3. 「猶予」列が「触る」の手は、マニュアルの秒数を書けない限りレーン c では入れない（technique-scan の早期モードで要確認にする）。

```
grep -nE '自由に|変更してよい|変更しても|待って|retry|Retry-After|429|503|以内|猶予|遅れ|スコアに含まれ|スコア外|不要|campaign|available_days|initialize' docs/manual/*.md
grep -rnE 'ORDER BY RAND|time\.Sleep|NewTicker|time\.Tick|retry_after|RetryAfter|LIMIT 1|exec\.Command|flock|Flock|http\.(Post|Get|NewRequest)|BeginTx|Beginx|FOR UPDATE' webapp/go --include=*.go
```

## 型ごとの手

列: **手**（T#）/ **マニュアルの文言**（あれば可）/ **コードの兆候** / **実例**（TECHNIQUES.md から）/ **猶予**（整合性の猶予に触るか）/ **fast**（CLAUDE.md ルール 4 の fast lane に入るか。○ = 入る、× = 通常の PR）

### 1. ポーリング・通知（14 通知、10f Web Push）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| ポーリング間隔を延ばす（T17） | 「クライアントは `retry_after_ms` 待つ」「通知は N 秒以内」 | 応答に `retry_after_ms: 30` のような小さい定数 | 14 都営三田線 30 → 100 → 300 → 1000 で 4,981 → 39,027、takonomura 09471f0d（30 → 500/1000） | 触る（通知の N 秒の内に収める） | ○（定数だけ。値はマニュアルの N 秒の内） |
| 待たせた分をまとめて返す（T36） | 「全ての遷移を順序通り少なくとも 1 回」 | 1 回の応答で未送信の遷移を 1 件だけ返す | 14 都営三田線 30,936 → 39,027、takonomura a00586c4 | 触る（順序違反はクリティカル） | × |
| SSE / ロングポーリング（T18） | 通知だけタイムアウトが長い、SSE の書式がある | — | 14 mazrean 感想戦 26,710 → 46,286。講評「優勝に必須ではなかった」、多くのチームが時間を溶かした | 触る | ×（最初の 1 時間ではやらない） |

### 2. マッチング・割り当て・ジョブのワーカー（14 matcher、10f ジョブ受け取り）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| 割り当てを作り直す（T34） | 「内部 endpoint / マッチングは自由に変更してよい」、評価にマッチ時間や距離の閾値 | `ORDER BY RAND()`、1 回に 1 件、`LIMIT 1` の待ち行 | 14 matsuu 6,025 → 21,023、都営三田線 11,068 → 21,372、takonomura 88eae785 | 触らない（割り当てる処理の中だけ） | × |
| 固定間隔・直列の区間を詰める（T35） | 同上 | `time.Sleep`、ticker、systemd の timer が curl で叩く間隔（14 matcher 500 ms） | 10f 50 ms sleep のポーリングを push に（kazeburo 3be8029f） | 触らない | ○（間隔の定数だけ、マニュアルが自由と言うとき） |
| 空き判定を列に持つ（T7） | — | 空き判定が `NOT IN (… GROUP BY … HAVING …)` | 14 is_free / is_matchable（takonomura 541c9cd1） | 触らない | × |
| ワーカーを 1 台に寄せる（T26 のリスク） | — | 各ホストで同じワーカーの unit が動く | 14 講評「排他制御」 | — | ○（etc/ の unit と deploy.sh だけなら） |

### 3. 外部決済・外部 API（14 決済、9q 配送・決済、9f 決済キャンセル）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| 冪等キーで再送だけに、確認 GET を省く（T14） | 「`Idempotency-Key`」、リトライの規定 | 失敗時に確認 GET → 件数比較、tx の中で外部 HTTP | 14 fujiwara ad8b39ec（2 並列 + URL キャッシュ）、iXi 3,850 → 7,157（複合） | 触らない（二重課金・金額誤りはクリティカル） | × |
| 不要な外部呼び出しを消す（T14） | 外部 API が 1 回 N 秒固定、講評で「実は必要がない」 | 一覧の 1 件ごとに外部 `/status` | 9q nil 1cd1a0eb → 17,260、netmarkjp 16,000 → 26,000 | 触る（状態の出どころが変わる） | × |
| 並列に呼ぶ・接続を使い回す（T14） | — | 独立な外部呼び出しが直列、`http.Client` の既定のまま | 9q nil ce3e0853 / にがり 22,000 → 35,000、`MaxIdleConnsPerHost` 1000〜4096（にがり → 4,500） | 触らない | △（Transport の数値だけなら ○） |
| 切断で処理を壊さない（T39） | — | 外部呼び出しや tx に `r.Context()` | 9q nil cb3cee95 | 触らない | × |
| 後でまとめて送る（T40） | 「終了後 N 秒待ってから整合性チェック」、bulk の API | キャンセルを 1 件ずつ同期 | 9f 白金動物園 cancelbot 9972b64c | 触る（N 秒の内） | × |

### 4. DNS・付随ミドルウェア（13 PowerDNS）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| 付随サービスを別ホストへ（T27） | 構成図に DNS 等の別プロセス | `pdns.conf` の `launch=gmysql`、app が `exec.Command("pdnsutil")` | 13 くすサポ 72,957 → 128,660（ベンチの向き先を PowerDNS のホストに）。13 練習: `zone-metadata-cache-ttl=60` で `domainmetadata` 72k 回が消えた、`pdnsutil` の exec を INSERT にして register 517 → 58 ms | 触らない | ○（`etc/powerdns` の数値だけ） |
| 最小実装で置き換える（T41） | 「A レコードのみ」「再帰しない」「スコア外」 | — | 13 NaruseJun「DNS on App」76,951 → 132,390 | 触らない（再起動後に名前一覧を DB から作り直す） | ×（1〜2 時間。最初の 1 時間では設計だけ） |
| 付随 DB の index（T20） | — | `records(name)` 等に index が無い | 13 織時屋: 2 台目に isudns の index を当て忘れ | 触らない | ×（PowerDNS のクエリは app の外で ORDER BY を確かめられない → 通常の PR） |

### 5. 画像・アイコン・静的ファイル（13 icon、11q icon、11f ZIP、9q 画像）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| BLOB をファイルにして nginx で返す（T19） | — | スキーマに `LONGBLOB`、GET で毎回 SELECT | 13 釜中の鯖 9c08a103da +54,047、11q matsuu 6fd8706e 146,710 → 208,386 | 触らない（再起動後と initialize でファイルを作り直す） | × |
| ハッシュを列に（T7） | 「icon_hash」のような検証子 | 読むたびに SHA256 | 13 釜中の鯖 +19,325、NaruseJun 0b6f3f2394 | 触る（13 は 2 秒） | × |
| 条件付き GET に 304（T15） | 「304 で応答してもよい」 | 検証子がある | 13 MONOS 16:13 → 63,000。**初回 GET に 304 で失格**（11f） | 触る | × |
| Cache-Control（T16） | 「キャッシュでリクエストが発生しない場合も応答があったとみなす」 | nginx に `expires` が無い | 11q Mirrativ 44608585（max-age=3600） | 触る | ○（マニュアルに文言があるときだけ。`etc/nginx/`） |

### 6. ランキング・集計・統計（13 統計、11q trend、11f GPA、10f dashboard、12q ranking）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| 全件ループを GROUP BY 1 本に（T21） | — | 全ユーザー × クエリのループ | 13 釜中の鯖 8c062f6cbf +143,119 | 触らない | × |
| 全員に同じ応答を周期生成・singleflight（T3） | 「最大 N 秒古い情報を返せる」 | パラメータも認証も無いハンドラ | 11q matsuu 37,019 → 44,034、都営三田線 143,372 → 161,966、10f がんもどき 11,852 → 14,162 → 16,718 | 触る | × |
| 終わったものは確定時に 1 回だけ（T4） | 「終了後に確定」 | `if finished` が無い重い集計 | 12q 第7西東京市 28c16690 2,598 → 3,602 | 触る（12q は 3 秒） | × |

### 7. マスタデータ・認証（14 chair_models、12f master、9q categories、9f 時刻表）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| 増えないテーブルをメモリに（T1） | マスタの更新が 1 回来る年がある（12f） | `INSERT INTO <table>` が init 以外に無いテーブル | 9f ARINE 15,066 → 25,841（白金動物園 d15c54b6）、9q categories 43 行（nil 8f6d41ee） | 触らない（起動時と initialize で再ロード） | × |
| access_token → 行をキャッシュ（T2） | — | 認証ミドルウェアが毎回 `SELECT … WHERE access_token = ?` | 14 都営三田線 30,936 → 32,003（okashoi 16ec1faa）、11q fujiwara 2028bb36 48,346 → 73,570 | 触らない（2 台目の無効化に注意） | × |

### 8. テナント DB・ファイルロック・ID 生成（12q SQLite、12f）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| flock を外して 1 tx に（T13） | — | `syscall.Flock` | 12q matsuu 488645a6 34,464 → 43,629、第7西東京市 52c7a816 11,346 → 23,121 | 触らない（tx は残す） | × |
| 履歴を「最新 1 行」に畳む（T8） | — | 読み手が最新行だけの追記テーブル | 12q 第7西東京市 16e60eae 45,857 → 66,960 | 触らない | × |
| ID をアプリで作る（T12） | 「文字種を変えない範囲で自由」 | `REPLACE INTO id_generator` | 12q matsuu cdf251ee 2,561 → 3,831、12f NaruseJun f0a73553 8,315 → 24,498 | 触らない | × |

### 9. 追記の多い POST・時系列（11q condition、14 coordinate）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| まとめて INSERT（T9 / T10） | 「反映の遅れを許容」「N 秒の猶予」 | ループ内の INSERT | 11q NaruseJun d375e46e 107k → 158k、Classi 136k → 222k、fujiwara bc9cd176 39,704 → 48,346 | T9 は触る / T10 は触らない | × |
| 捨ててよい割合（T11） | 「欠損を許容」 | `dropProbability` | 11q fujiwara 1a5e79e9 73,570 → 374,366。**先に下げると DB が飽和**（matsuu は戻した） | 触る | ×（書き込み経路を直した後） |
| LIMIT と範囲（T22） | — | 全件取って先頭だけ使う | 11q fujiwara 06190135 ~17.9k → 30k | 触らない | × |

### 10. ベンチの強さ・クライアントの行動を選べる年（9q campaign、9f available_days、10f TeamCapacity、10q bot）

| 手 | マニュアルの文言 | コードの兆候 | 実例 | 猶予 | fast |
|---|---|---|---|---|---|
| initialize の応答でノブを上げる（T37） | initialize 節に数値パラメータ | initialize が `campaign` 等を返す | 9q nil campaign 3 で 53,460、9f ARINE 20 日 → 15,066、10f がんもどき 16,718 → 29,822 | 触らない | ○（返す数値だけ。上げすぎはタイムアウト減点 → 1 段ずつ） |
| bot に 503（T17） | 「bot には 503 を返してよい、減点なし」 | — | 10q FCCPC 726 → 920 | 触らない | ○（`etc/nginx/` の `if`） |

### どの型でも（parallel-setup D と lane 2 がやる。レーン c ではやらない）

TECHNIQUES §8 の小技（interpolateParams、pool、debug off、nginx keepalive）と、WHERE 列の index（T20。14 講評「index だけで 4,000 点」、13「3,000 → 10,000」）。index は baseline の report が既存 index を除いて提案するので、lane 2 の parallel-fix に任せる。
