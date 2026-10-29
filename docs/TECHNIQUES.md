# ISUCON で効くテクニック集（ISUCON 9〜14 の公式解説・上位チームの commit・参加記から）

中盤（13:00 頃）に `technique-scan` skill がこの全件を「この問題で使えるか」○ / × / 要確認 で判定する元データ。各テクニックは次の 5 点で書く。出典 URL 付きの原本は `docs/techniques/sources/isucon{9..14}.md`（大会別）にある。

- **効く条件**: アプリ側の兆候。これが当てはまるかを見る
- **当日の確かめ方**: kit の道具で何を見るか（`summary.md` の Data growth 表 = ベンチ中に増えないテーブル、endpoint 表、SQL 表、`docs/app-map.md` の書き込み経路、マニュアルの該当箇所）
- **リスク**: ベンチの整合性チェック、再起動試験（追試）、外部通信
- **実例**: 大会 / チーム / commit か記事（効果の数字があれば）。詳細は原本
- **派生**: 似た手。私の推測には「推測」と付ける

凡例: 14 = ISUCON14 ISURIDE（配車）、13 = ISUPIPE（配信 + DNS）、12q = ISUPORTS（SQLite テナント）、12f = ISUCONQUEST（ガチャ）、11q = ISUCONDITION（IoT 時系列）、11f = ISUCHOLAR（履修）、10q = ISUUMO（不動産検索）、10f = XSUCON、9q = ISUCARI（フリマ + 外部 API）、9f = ISUTRAIN。

---

## 0. レギュレーションの共通項（9〜14 でほぼ変わらない）

| 項目 | 内容 | 当日見る場所 |
|---|---|---|
| 変えてよい | サーバーの役割、スキーマと index、DB ミドルウェア、**キャッシュとジョブキューによる遅延書き込み**、他言語での再実装 | レギュレーション「変更してよいもの」 |
| 変えてはいけない | URI（サーバー生成の ID は文字種を変えない範囲で可）、レスポンスの構造、JS/CSS/メディアの内容 | 同「変更してはいけないもの」 |
| 再起動後 | 全アプリが正常動作、**ベンチ中に書かれたデータが再起動後も読める**、メンテコマンド互換 | 同「再起動後」。IMPLEMENTATION-NOTES §0 |
| 追試 | 全台再起動 → ベンチ再実行。FAIL か最終スコアの 75%（13・14）/ 85%（12）以下で失格。上位はデータ保持とブラウザ目視も | 当日マニュアル「追試」 |
| 外部資源 | 用意された 3〜5 台以外で処理するのは禁止。監視・開発・AI ツールは可。**同じ VPC に計測用インスタンスを残すと envcheck で失格**（14） | 当日マニュアル「外部リソース」 |
| /initialize | 20〜60 秒の上限。スキーマやデータ構造を変えたら initialize で全部再現する。`{"language"/"lang": ...}` を返す | 当日マニュアル「初期化」 |
| 整合性の猶予 | 回ごとに **秒数つき** で書かれる（12q: finish→billing 3 秒、11q: condition↔graph 1 秒、13: icon_hash 2 秒、14: 座標 3 秒）。**書かれていない年は「即時反映」が前提**（12f） | アプリケーションマニュアル「反映の猶予」 |
| Conditional GET | 11 以降、ベンチはブラウザ相当で `If-None-Match` / `Cache-Control` を扱う。**条件付きでない GET に 304 を返すと失格**（11f、13） | 同「Conditional GET」 |
| ステータス | 基本は参考実装と同じ。例外は明記される（12q: tenants/add の 429 + Retry-After、14: `retry_after_ms`） | 同 |

---

## 1. 読みを減らす（キャッシュ）

### T1. 増えないテーブルを丸ごとメモリに持つ
- 何を: `/initialize` 後に行数が変わらないテーブル（マスタ、タグ、カテゴリ、モデル一覧）を起動時と initialize 時に map に載せ、以後 DB を見ない。
- 効く条件: **Data growth 表で Δ = 0** かつ app-map に書き込み経路が無い（`grep "INSERT INTO <table>"` が空）。fill*Response の N+1 の中でそのテーブルを引いている。
- 当日の確かめ方: `summary.md` の Data growth 表 → Δ=0 のテーブルを列挙 → それぞれ `webapp/` を grep で書き込み無しを確認 → SQL 表の count 上位にその SELECT があるか。
- リスク: /initialize で再ロードを忘れる（13 NaruseJun は initialize と main の両方で `resetTagCache`）。マスタが **ベンチ中に 1 回だけ更新される** 年がある（12f: `PUT /admin/master` が開始 20 秒後に来る → version をキーにする）。
- 実例: 13 tags（NaruseJun b584ed2c34、ultra_fast_gopher b6e8e455da）。12f master data を master_version キーで（NaruseJun 11a556d1、講評「マスターテーブルのデータは、全てキャッシュに入れる想定」）。14 chair_models 55 行（matsuu edea175f は TRIGGER で speed を chairs に複製）。9q categories 43 行（nil 8f6d41ee、講評「ソースコードに直接書いたチームが多かった」）、configs（外部 API の URL）もメモリへ。9f station/fare/seat/train_master を Ruby の Marshal でコードに埋め込み（白金動物園 d15c54b6、ARINE 15,066 → 25,841）。ただし **RAM 1 GB の年は 280 万行の timetable は載らない**（講評「全ては乗り切らない」、sambaiz は OOM）。
- 派生: マスタに version があるなら「version 一致なら使う、違えば singleflight で再ロード」（12f NaruseJun 2de5b6e2）。複数 app ではリクエストごとの version 1 行 SELECT だけ残す。

### T2. 作成後に変わらない行をキーごとにキャッシュ（users、livestreams、courses、announcements、access_token）
- 何を: INSERT 後に UPDATE/DELETE されない行を id/name → 構造体で sync.Map に lazy fill。書き込み経路（register、icon 更新など）でだけ無効化。
- 効く条件: app-map でそのテーブルの UPDATE/DELETE が 0〜1 箇所。認証ミドルウェアが毎回 `SELECT ... WHERE access_token = ?` をしている。
- 当日の確かめ方: SQL 表 count 上位の `SELECT * FROM users WHERE id = ?` 系。app-map の「書き込み経路」で UPDATE の有無。
- リスク: **app 2 台で無効化が届かない**（13 NaruseJun は `/api/icon` と `/api/user/*/icon` を同じホストに nginx で寄せた、tmokmss は svc3 のキャッシュが initialize で消えず終了 10 分前に発覚）。13 都営三田線は livestream を tx の外でキャッシュして tags 無しの瞬間を観測され FAIL（227,377 → 0）。
- 実例: 13 UserModel/livestream（NaruseJun defefdc026、43e6ba6eaf。12:00 → 76,951）、織時屋 theme/users（13:11 → 48,494 → 63,968）。14 access_token → entity（okashoi 16ec1faa、都営三田線 30,936 → 32,003）。11f course/class/teacher name（fujiwara 78dc39ee）。11q user 存在チェック（fujiwara 2028bb36、48,346 → 73,570）。9q users を userMap に write-through（にがり 13:20 → 4,200 → 7,400。3 台 app にしたとき無効化できず一度諦め、非ログイン系を 1 台に戻して復活）。
- 派生: 無効化が面倒なら「複数 app のときは書き込み系 endpoint を 1 台に寄せる」（T26）。

### T3. 全員に同じ応答を返すエンドポイントは周期生成 or singleflight
- 何を: パラメータも認証も無い（or 誰が呼んでも同じ）応答を、ticker で 0.5〜1 秒ごとに作って全員に返す。または singleflight で同時リクエストを 1 回の計算に束ねる。
- 効く条件: ハンドラに `c.Param` / セッション参照が無い。endpoint 表で count が多く、応答がグローバル（trend、GPA 統計、全体ランキング）。マニュアルに「古い情報を返しても検知されない限り可」がある。
- 当日の確かめ方: endpoint 表の count 上位 → ハンドラの引数を読む。マニュアルの「反映の猶予」。
- リスク: ユーザー増加のループが trend の変化で回る年（11q）は、古すぎると増加が止まる。fd/接続数上限に当たる（11q 講評）。バックグラウンドの更新が失敗してログで disk full（11q テディーベア）。
- 実例: 11q trend（shallowverse 300c8343 1 秒 cron、NaruseJun 1e24895d 0.5 秒、matsuu Varnish ttl 0.8s 37,019 → 44,034、都営三田線 singleflight 143,372 → 161,966）。11f GPA（fujiwara f316f93a singleflight ~1s、講評「科目の終了と終了の間は使い回せる」）。13 user ranking singleflight（NaruseJun dbb481aa42）。10f audience dashboard（マニュアル「最大 1 秒古い情報を返せる」）を 1 秒 TTL + singleflight + `Cache-Control: public, max-age=1` + ETag で（takonomura af6860a2、がんもどき 0.9 秒キャッシュ → 11,852 → singleflight → 14,162、講評は Varnish `ttl=0.5s`）。**構造体ではなくシリアライズ済みのバイト列（protobuf/JSON）をキャッシュ**すると更に効く（がんもどき 14,162 → 16,718、takonomura 864eb22f）。
- 派生: 「変化したときだけ再計算」（11f GPA は科目終了時のみ変わる → `PUT /status` で無効化）。

### T4. 終わったものは永久にキャッシュ・確定時に 1 回だけ計算
- 何を: 状態が「終了」になった後は変わらない値（終了した大会の請求額やランキング、過去日のグラフ、完了したライド）を、終了イベントで 1 回計算して保存し、以後は読むだけ。未終了なら即 return。
- 効く条件: マニュアルや実装コメントに「終了後に確定」がある（12q `// 大会が終了している場合のみ請求金額が確定するので計算する`）。
- 当日の確かめ方: 重いハンドラに `if finished` 分岐が無いか。app-map で「終了」を書く endpoint は 1 つか。
- リスク: 確定値は **MySQL に保存**（再起動後に読める必要。12q いすもな は billing 500 で追試失格）。猶予 3 秒以内に反映。
- 実例: 12q billing（第7西東京市 28c16690 2,598 → 3,602、織時屋 0d5ba8b4/863c85f5、公式解説「5 倍」）。12q 終了大会の ranking 永久キャッシュ（Zatsu-X-Club e7f15cec）。14 `rides.evaluation IS NOT NULL` で完了判定（takonomura ccb33604）。
- 派生: 「ベンチ中に終了しないもの」は initialize で全部計算しておく（推測: 初期データの集計列）。

### T5. マニュアルの猶予秒数ぶんだけ TTL キャッシュ
- 何を: 「反映まで N 秒の猶予」と書かれた応答を N 秒弱の TTL でキャッシュする（書き込みで Forget も併用）。
- 効く条件: アプリケーションマニュアルの「猶予」節に秒数がある。その endpoint が count 上位。
- 当日の確かめ方: マニュアルを grep（猶予 / 以内 / 許容）。endpoint 表の count。
- リスク: 猶予が無い年に TTL を置くと整合性で FAIL（12f は「即時反映」）。12q player の TTL 3 秒は織時屋で「ほぼ効果なし」(hit 30%) — 大きい律速を先に。
- 実例: 12q player 3 秒（織時屋 21dabb65、釜中の鯖 失格フラグ 3 秒）。14 owner/chairs 3 秒（ミレニアム）。11q condition/graph は 1 秒以内に整合、その他は遅延許容。
- 派生: TTL ではなく「最後の書き込み時刻 + N 秒まで古い値を返す」(推測: 書き込みが疎なら hit が高い)。

### T6. 否定キャッシュ（存在しない・BAN されていない）
- 何を: 毎リクエストの `SELECT * FROM user_bans WHERE user_id=?` のような「ほぼ常に空」の結果を map で持ち、書き込み endpoint で消す。
- 効く条件: ミドルウェアで毎回引いている。書き込み経路が 1 つ。
- 当日の確かめ方: SQL 表で count が最大級なのに rows_sent ≈ 0 の SELECT。
- リスク: 複数 app（12f NaruseJun は 1 台構成のときだけ有効化 `ISUCON_ICHIDAI`）。
- 実例: 12f ban（NaruseJun 8bd330bc 16:15 → 348,000、釜中の鯖 52c5df32）。
- 派生: 「存在チェックだけの COUNT(*)」も同じ（11q `SELECT COUNT(*) FROM user`）。

### T7. 計算済みカラム・カウンタカラム（書き込み時に 1 回計算）
- 何を: 読むたびに計算している値（画像の SHA256、CSV 文字列から数えた level、走行距離の累積、投げ銭合計、提出数、空き状態）を、書き込み時に列へ入れる。生成列（STORED GENERATED）か TRIGGER かアプリで。
- 効く条件: 値が行データの純関数、または書き込み経路が全部アプリ内にある。読みが書きより桁違いに多い。
- 当日の確かめ方: pprof で `sha256` / 文字列処理が上位。SQL 表で LAG/ウィンドウ関数や `ORDER BY ... LIMIT 1` の履歴読み。app-map の書き込み経路数。
- リスク: 初期データへのバックフィルを **initialize で**（14 takonomura は初期データを再生成 517c4669。42 秒/30 秒の上限）。削除経路でカウンタを戻す（13 白金動物園 moderation で `total_tips - ?`）。UPSERT の評価順（14 iceman: `ON DUPLICATE KEY UPDATE` で新座標が先に入り距離がずれた）。**計算を SQL 側に置かない**（13 練習: `SHA2(image,256)` を SELECT で計算したら DB の SQL 時間の 36% を食った。書き込み時に Go で計算して列へ）。
- 実例: 13 icon_hash 列（講評の TRIGGER、NaruseJun 0b6f3f2394、釜中の鯖 +19,325）。11q condition level 列（NaruseJun 5fd5fa04 72k → 107k、fujiwara true_count）。14 chairs.total_distance（takonomura 0f809140、TAK848 7,000 → 9,000、iXi 8,855 → 16,425）。14 is_free/is_matchable（takonomura 541c9cd1）。13 users.total_tips/score（白金動物園 d85152fda8）。11f submission_count/total_score（Mirrativ 83a62af7、NaruseJun 4a83f7bd）。10f leaderboard を `teams.best_score/latest_score/finish_count/student` の列に（takonomura 99384a33、講評「あらかじめ Leaderboard のデータを作っておく」。凍結中の見え方を自チームと他チームで分ける必要あり）。
- 派生: themes のような 1:1 テーブルを親に畳む（13 白金動物園 `users.dark_mode`）。

### T8. 履歴テーブルを「最新行だけ」「1 キー 1 行」に畳む
- 何を: 読み手が「キーごとの最新 1 行」や「MIN/SUM」しか使わない追記テーブルを、キーを PK にした 1 行に置き換える（UPSERT / INSERT IGNORE）。使い終わった行は hard delete。
- 効く条件: そのテーブルの全読み取りが `ORDER BY created_at DESC LIMIT 1` / `MIN()` / 集計だけ。Data growth 表で **ベンチ中に最も増える** テーブル。
- 当日の確かめ方: Data growth 表の Δ 最大 → app-map でそのテーブルの読み取り SQL を全部列挙。
- リスク: 初期データの圧縮を initialize 内で（12q visit_history 320 万 → 20 万行。ソレイユは 2 回失敗）。「最後の 1 行」の意味を保つ（12q CSV の最終出現）。
- 実例: 12q visit_history を (tenant, competition, player) PK に（第7西東京市 28c16690、matsuu 169314e4、講評）。12q player_score を最終行だけ（第7西東京市 16e60eae 45,857 → 66,960）。14 chair_locations 廃止（T7 と同じ commit）。12f sessions/tokens を hard delete + unique index（織時屋 eb97474e、matsuu 115b7759）。11q isu_condition を (uuid, timestamp) の複合 PK に（fujiwara 864a90cd 374k → 685k）。
- 派生: 畳めないなら covering index（12q 公式解説 step 1）。

---

## 2. 書きを減らす（まとめ書き・間引き・捨てる）

### T9. 追記だけの POST をバッファして bulk INSERT（猶予がある年）
- 何を: 受け取った行をチャネルに積んで即 202/200 を返し、10〜500 ms ごとに multi-row INSERT。
- 効く条件: マニュアルに「反映が遅れることを許容」「N 秒の猶予」がある。POST が count 最大で、同じリクエスト内で read-after-write しない。
- 当日の確かめ方: マニュアルの猶予節。endpoint 表で POST の count と sum。Bench shape が固定並列なら POST の応答短縮がそのまま処理数。
- リスク: **バッファはプロセス停止で消える** → フラッシュ間隔を短く、SIGTERM でフラッシュ（追試のデータ保持）。猶予の外にある副作用（14 の PICKUP/ARRIVED 判定は即時）は同期のまま。バリデーション 400 を返せなくなる。都営三田線は 10 ms 窓に留めた。
- 実例: 11q condition（shallowverse 6f7e5f43、NaruseJun d375e46e 107k → 158k、Classi 136k → 222k）。14 coordinate（okashoi e048ef99/8a1f963e、講評「非同期化・バルクインサート化が可能」）。
- 派生: 猶予が無くても「1 リクエスト内の N 行」は bulk にできる（T10）。

### T10. 1 リクエスト内の N 回の INSERT/UPDATE を 1 回に
- 何を: ループ内の `INSERT` を multi-row に、`SELECT → UPDATE` を `UPDATE ... SET x = x + ?` に、種類別にまとめて `ON DUPLICATE KEY UPDATE`。
- 効く条件: SQL 表で「per request」が 2 以上の INSERT/UPDATE（sqltag の called from を見る）。
- 当日の確かめ方: SQL 表の per request 列（1.5 以上は琥珀、3 以上は赤）。
- リスク: `max_allowed_packet` / SQLite の変数上限（12q 織時屋）。バリデーションは先に全件（12f innocent）。
- 実例: 12q CSV bulk（第7西東京市 c764dc13 7,972）。12f presents/items（NaruseJun 8a12622d 系、matsuu ac41d06b 54,744）。11f announcement fan-out を `INSERT ... SELECT`（fujiwara dd9625e8）。11q condition bulk（fujiwara bc9cd176 39,704 → 48,346）。

### T11. 間引く・捨てる（マニュアルが欠損を許す年）
- 何を: 参考実装の dropProbability のような「捨ててよい」を、能力が上がるまで残し、最後に下げる。時間あたり上限で間引く変種も。
- 効く条件: マニュアルに「データ欠損を許容」「理想的には全て保存」の両方がある（11q）。スコアが保存件数で逓減する（11q グラフは n>20 で頭打ち → 1 時間 30 件で十分）。
- 当日の確かめ方: `grep dropProbability`。スコア式の件数依存。
- リスク: **先に下げると DB が飽和**（11q matsuu は 0.6 → 0.9 に戻して +110,006）。下げるのは書き込み経路を直した後。
- 実例: 11q fujiwara 1a5e79e9（drop 0 で 73,570 → 374,366）、shallowverse 0.4、Mirrativ 1 時間 30 件上限 e80a1121。
- 派生: 捨てる代わりに「insert 時に間引く」（リクエストは全部受ける）。

### T12. ID 生成をアプリ側に（MySQL の 1 行カウンタをやめる）
- 何を: `REPLACE INTO id_generator` / `UPDATE id_generator SET id=LAST_INSERT_ID(id+1)` を UUID（ハイフン除去）、xid、Snowflake、`tenant-unixnano` に。
- 効く条件: ID が VARCHAR/BIGINT で連番性に依存しない。SQL 表の count 上位に id_generator。レギュレーションに「文字種を変えない範囲で自由」。
- 当日の確かめ方: SQL 表、スキーマの id 型、レギュレーションの ID 条項。
- リスク: 再起動や複数 app での衝突（12f 織時屋は atomic + serverId から snowflake へ）。Snowflake の下位ビットで shard すると偏る（NaruseJun `id>>23`）。文字種（織時屋は `-` を除去）。
- 実例: 12q matsuu cdf251ee（2,561 → 3,831）、12f NaruseJun f0a73553（8,315 → 24,498）、matsuu は AUTO_INCREMENT 化 ffd64578（12,503 → 20,011）。

### T13. 読み取りのトランザクション・FOR UPDATE・flock を外す（整合性を守れる範囲で）
- 何を: 参考実装が全ハンドラを `BeginTx` で包んでいる → 純粋な読みは外す。DELETE+INSERT の保護に flock を使っている → 1 トランザクションに置き換えて flock を消す。
- 効く条件: CPU が余っているのに p99 が高い（Verdict が wait）。pprof に `syscall.Flock`。SQL 表に BEGIN/COMMIT が多い。
- 当日の確かめ方: `grep BeginTx` の数、Verdict、processlist の lock wait。
- リスク: **複数行の不変条件を守る tx は残す**（13 onk は一括削除を revert、都営三田線は livestream+tags の tx を外して FAIL、12q は tx 無しで flock を外すと整合性チェックで落ちる）。flock を外した後の DELETE+INSERT は MySQL でデッドロック → 1213 リトライか追記 + 最新行（12q 第7西東京市 52c7a816 11,346 → 23,121）。
- 実例: 12q flock 削除（第7西東京市 cb25ef8a、matsuu 488645a6 34,464 → 43,629）。11f 講評「ユーザーのニーズを満たす範囲で整合性を提供すれば良い」。13 織時屋 cfc0795153。9q `/buy` の seller 行の FOR UPDATE だけ外す（methane 0a214ce2）、`SKIP LOCKED` は二重発券の元で 40 分で revert（9f 白金動物園 f65761b8）、READ UNCOMMITTED で +1,000（isucon_friends「ビクビク」）。
- 派生: `SELECT ... FOR UPDATE` → `UPDATE` の 2 文を **条件付き UPDATE 1 文 + RowsAffected** に（10q `UPDATE chair SET stock = stock - 1 WHERE id = ? AND stock > 0`、10f `UPDATE benchmark_jobs SET status=SENT WHERE id=? AND status=PENDING`。hiromu 1,422 → 1,556、一口坂46 10,000 → 12,015、講評）。
- 派生: 同じ行に殺到する POST（人気商品の `/buy` に 80〜160 並列）は **アプリ側の per-key mutex + 「売り切れ」キャッシュで tx を開く前に 403** にする（9q methane f98b1d6d、講評「1 つの商品につき 1 つのトランザクションのみ」）。app が 1 台のときだけ。

### T14. 外部 API: 冪等キー・並列度・確認 API を省く
- 何を: 失敗時に「確認 GET → 件数比較」をしている外部決済などを、`Idempotency-Key`（マニュアルに定義があれば）を付けてそのまま再送に。並列度を絞る。URL を initialize でキャッシュ。
- 効く条件: マニュアルの外部 API 節に冪等キーやリトライ規定がある。外部 API の応答が遅い（endpoint の apptime と SQL 時間の差が大きい）。
- 当日の確かめ方: マニュアルの外部サービス節。pprof で http.Client の待ち。
- リスク: 二重課金・金額誤りはクリティカル（14）。ベンチ終了後 5 秒以内に完了。14 講評は「ブラフだった」と認めている。
- 実例: 14 fujiwara ad8b39ec（2 並列 + URL キャッシュ）、iXi 3,850 → 7,157（複合）。9q は外部 API が 1 回 0.8 秒固定: **一覧から配送 `/status` の呼び出しを消して自前の `shippings.status` を返す**（nil 1cd1a0eb → 17,260、netmarkjp 16,000 → 26,000、講評「実は必要がなく…エラーにならない」）、`/buy` の shipment create と payment token を並列（nil ce3e0853、にがり 22,000 → 35,000）、nginx 由来の 400/502 はリトライ（nil 5f6a7533。ただし決済の再送は多重決済で即 FAIL）、`MaxIdleConnsPerHost` を 1000〜4096 に（にがり → 4,500）。9f はキャンセルをキューに積み、1 秒ごとに `/payment/_bulk` で一括（白金動物園 cancelbot 9972b64c、マニュアル「5 秒以上待ってから整合性チェック」）。
- 派生: 外部 API の応答を **終端状態（done）だけ** キャッシュする。途中状態はベンチが厳密に見る（9q 白金動物園は全キャッシュを撤回、`done` のみに）。

---

## 3. HTTP の仕様を使う

### T15. 条件付き GET に 304（ETag / If-None-Match）
- 何を: 検証子（icon_hash など）が一致すれば本体なしの 304。送るときは `"` で囲む。**条件付きでない GET には絶対に 304 を返さない**。
- 効く条件: マニュアルに「304 で応答してもよい (MAY)」。アプリ側に検証子がある（icon_hash）。endpoint 表の 304% 列が 0% で bytes が大きい GET。access.csv の `inm` 列が非空。
- 当日の確かめ方: endpoint 表の 304% と bytes。deep-dive Q-304。マニュアルの Conditional GET 節。
- リスク: **初回アクセスにも 304 を返して失格**（11f テディーベア）。引用符の不一致で 1 時間ロス（13 百万円ドリブン、NaruseJun）。nginx だけで完結しない（検証子が独自ハッシュ）→ app に当たる（13 sorah「微増」）。
- 実例: 13 icon（NaruseJun 214345ec66、織時屋 bda010f43a、MONOS 16:13 → 63,000）。11f classes の ETag（NaruseJun 73c23344）。
- 派生: ETag をメモリに持ち、書き込みで無効化（11f）。304 ではなく Cache-Control で「来させない」（T16）。

### T16. Cache-Control / expires で再取得をやめさせる
- 何を: 静的アセットとアイコンに `expires 86400s` / `Cache-Control: max-age=3600`。ベンチがブラウザ相当なら「キャッシュから返したとみなす」。
- 効く条件: マニュアルに「ブラウザのキャッシュ有効期限の制御によってリクエストが発生していない場合も…レスポンスがあったものとみなします」（11q）。access log に assets/js/css の大量 GET。
- 当日の確かめ方: endpoint 表の static assets 行の bytes。マニュアルの Conditional GET 節。
- リスク: ユーザーごとのキャッシュ（`public` でも共有されない、11q）。gzip でスコアが下がった例（11q matsuu、原因不明）。index.html に cache-control を付けないと一時的に下がった（都営三田線）。
- 実例: 11q Mirrativ 44608585（max-age=3600）、matsuu nginx expires。
- 実例（9q）: nginx で `/static` と `/upload` を `Cache-Control: public max-age=86400` + `gzip_static`（nil 317a4c74、matsuu）。ベンチは静的ファイルを md5 で検証するので内容は変えない。nil は終盤の静的配信で「計測できる差は無かった」— 効くのは他が片付いた後。10f は観客が増えるたび新しいブラウザセッションが `/packs/*.js` を取るので、nginx + `expires 1d` + gzip_static/brotli が効いた（innocent-team 0835afee、kazeburo、ベンチは checksum 検証で 200/304 どちらも可）。10q はベンチが静的ファイルを一切取らない（追試のブラウザ確認のみ）。

### T17. 応答でクライアントの挙動を制御する（retry_after、429 Retry-After、sleep、bot 拒否）
- 何を: マニュアルが許す範囲で、ベンチの次の行動を遅らせる・減らす。ポーリング間隔を応答で指定、特定 endpoint に 429 + Retry-After、重い非スコア endpoint に意図的な sleep、bot の UA に 503。
- 効く条件: マニュアルに「クライアントは `retry_after_ms` を待つ」「429 を返してよい」「bot には 503 を返してよい」のような条項。非スコアの endpoint が負荷増加のループを回している。
- 当日の確かめ方: マニュアル全文を「retry / 429 / 503 / Retry-After / User-Agent / 間隔」で grep。deep-dive Q-scenario で非スコア endpoint の頻度。
- リスク: 遅らせすぎると評価が下がる（14: 3 秒以内に通知、評価基準は 100 tick）。11q の trend sleep はタイムアウト減点と隣り合わせ。
- 実例: 14 retry_after_ms 30 → 500/1000（takonomura 09471f0d、都営三田線 30 → 100 → 300 → 1000 で 4,981 → 39,027、講評「JSON のままでも 10 万点」）。12q 429 + Retry-After は許可されていたが使用例なし。11q trend に sleep 500ms（NaruseJun 7a79bd16、推測込みの手）。**10q bot の User-Agent → 503**（マニュアルが 10 個の正規表現と「503 を返してよい、減点なし」を明記。nginx の `location /api` で `if ($http_user_agent ~ ...) { return 503; }`。FCCPC 726 → 920。bot はスコア 600 以上で初めて来るので序盤は効果が見えない。nginx の正規表現は `/…/` ではなく引用符で）。10f はチーム上限超えに 403（T37）。**逆に、遅らせると罰がある年もある**: 9q は login を sleep で遅らせると人気者出品が発動せず（講評「ログインをさぼると…人数が集まらず」）。
- 派生: 「待たせる間に溜まった更新をまとめて返す」（T36）。

### T18. ロングポーリング / SSE（通知のタイムアウトが長い年）
- 何を: 通知 endpoint だけタイムアウトが長い（14: 60 秒 vs 10 秒）なら、JSON のまま新しい状態が出るまでブロックする（ロングポーリング）か、`text/event-stream` でプッシュする。書き込み箇所から per-user チャネルで起こす。
- 効く条件: マニュアルのタイムアウト表で通知だけ長い。SSE の書式が定義されている。
- 当日の確かめ方: マニュアルのタイムアウト表、通知の書式節。endpoint 表で通知 GET が count 最大。
- リスク: **多くのチームが時間を溶かした**（14 都営三田線・hitode909・motemen・mazrean）。講評「優勝に必須ではなかった」。複数 app では Redis pub/sub が要る。nginx のバッファ（`X-Accel-Buffering: no`）。
- 実例: 14 takonomura 3dcb1c18/4283a1f7/43632d29、mazrean 感想戦 26,710 → 46,286。
- 派生: 先に T17 の間隔延長 + T36 のまとめ返しで 10 万点、SSE は余力があれば。10f の Web Push も同型: マニュアルが「全通知を push すれば一覧は空でよい」と許したが、優勝の takonomura は実装せず（503 のまま）、kazeburo は通知一覧の tx を外して FAIL。

### T19. 画像・静的ファイル・ZIP を DB/アプリから外し nginx で返す
- 何を: LONGBLOB を書き込み時にファイルへ落とし、GET は `X-Accel-Redirect` / `alias` / `try_files` で nginx が返す。大きな応答は `proxy_buffering off`。ZIP は無圧縮（Store）。
- 効く条件: スキーマに BLOB、`SELECT *` でそれを毎回読んでいる。endpoint 表で bytes 上位。
- 当日の確かめ方: Data model（ER）の BLOB 列。endpoint 表 bytes。pprof の deflate/sha256。
- リスク: **再起動後にファイルが無い/参照が壊れると追試失格**（11q morupoppo 500、さばかんちゃーはんかれー 404。11f の zip 404/500 で 3 チーム失格）。initialize で初期データ分を再生成、古いファイルを消す（11f ichyo は disk full）。複数 app ではファイルがある host に pin（T26）。
- 実例: 13 icon を nginx（NaruseJun b83dc75cd3、釜中の鯖 9c08a103da +54,047、白金動物園 nginx 304 3568cb8653）。11q icon（matsuu 6fd8706e 146,710 → 208,386、fujiwara b7b8da6a）。11f zip Store + X-Accel（fujiwara 261e8b14、matsuu proxy_buffering off）。9q QR PNG（mediumblob）をファイルへ、複数 app では **受け取った host に置き、nginx が `/upload/N/` をその host へ proxy**（白金動物園 b8fb2ed2 img_server_map）。
- 派生: 「初期データのユーザーはアイコン無し」なら initialize はディレクトリを空にするだけ（13）。

---

## 4. DB

### T20. 意図的に消された index を戻す（複合・covering・PK 化・FK 削除）
- 何を: WHERE/ORDER BY 列の index。時系列なら `(key, time)` を PK に昇格して AUTO_INCREMENT を捨てる。FK 制約は消す（INSERT/UPDATE の参照チェックが CPU を食う）。付随 DB（PowerDNS の `records(name)`）も忘れない。
- 効く条件: ほぼ毎回。SQL 表の rows examined/sent 比が 100 以上。
- 当日の確かめ方: report の Next actions（index 提案は既存 index を除外済み）。`SHOW INDEX`。
- リスク: 列名の typo（11q matsuu）。index は schema ファイルに入れて initialize と再起動を越える（kit の `webapp/sql/indexes/*.sql` + apply.sh）。2 台目に当て忘れ（13 織時屋 isudns index）。
- 実例: 14 講評「index だけで 4,000 点」、13「3,000 → 10,000」、12f `(user_id, present_all_id)` で 600 → 13,291（織時屋 a3bddbd6）、11q 複合 PK（T8）、11f FK 削除（fujiwara 6995cb95）。
- 派生: `CHAR(26)` の ULID を latin1 に（11f fujiwara dfcc0282、index が小さくなる）。`(a = ? OR b = ?)` は UNION 2 本に分けて両方の index を使う（9q 講評、nil 6d68a384）。
- 派生: **昇順と降順が混ざった ORDER BY**（`popularity DESC, id ASC`）は MySQL 5.7 では index が効かない → 符号を反転した生成列 `popularity_desc AS (-popularity)` に index（10q 講評、akouryy「DESC INDEX が効かないという発想自体がなかった」）、または MySQL 8 の降順 index（matsuu 1,144 → 1,200、kamipo 感想戦）。
- 派生: 範囲検索の条件を **整数カテゴリ列（`rent_t`、`price_t`）に畳んで `=` 検索 + `(cat, popularity DESC)`**（10q akouryy 464 → 730、matsuu、kamipo PR 14）。ベンチが「価格・家賃の条件を多めに送る」と講評に書いてあった。
- 派生: 地理の多角形検索は `POINT` 列 + `SPATIAL INDEX`（MySQL 8 は `SRID 0` を付けないと index が使われない。matsuu 0.401 s → 0.063 s、shallowverse は新テーブルで対応）。
- **罠**: PK の無いマスタに PK/index を足すと **ヒープ順に依存していた出力順が変わって pretest 落ち**（9f 講評 6.4、いんふらえんじにあー は 15:45 まで復旧不能）。index の前に全読み取りへ `ORDER BY` を入れる。

### T21. N+1 を IN / JOIN / GROUP BY に（統計・ランキング・fill*Response）
- 何を: ループ内 SELECT を `WHERE id IN (...)` に、全ユーザーを回す統計を `GROUP BY` 1 本に、親オブジェクトを 1 回取って子に渡す。
- 効く条件: SQL 表の per request が大きい SELECT。講評が「設計された N+1」と明言する年が多い。
- 当日の確かめ方: SQL 表 per request、sqltag の called from、pprof の call graph。
- リスク: 空配列と null の違い（13 「Empty のときは空配列」）。tie-break の順序（13 ranking は score, name）。
- 実例: 13 stats GROUP BY（釜中の鯖 8c062f6cbf **+143,119**、NaruseJun d1ebb3b830）。14 nearby-chairs を 1 クエリ（都営三田線 39,027 → 43,685）。12q ranking JOIN、11f grades IN。10q なぞって検索の「bounding box で全件 → 1 行ずつ `ST_Contains`」を `WHERE ST_Contains(ST_PolygonFromText(?), POINT(lat, lon)) ORDER BY ... LIMIT 50` の 1 本に（FCCPC 451 → 514、hirosuzuki 1,521 → 1,857。`ORDER BY ..., id` を落とすと検証で落ちる）。10q recommended_estate の 6 通りの OR を「椅子の小さい 2 辺 ≤ ドアの 2 辺」の 2 条件に数学で畳む（innocent-team 858590f1、FCCPC 619 → 687）。
- 派生: 共有ヘルパー（fill*Response）は呼び出し元が多い → 形を変えずに親を渡す引数を足す（HANDOFF §5）。

### T22. クエリに LIMIT と範囲を入れる（取ってから捨てていないか）
- 何を: 全件取って `[0]` を使う → `ORDER BY ... DESC LIMIT 1`。日付パラメータを無視して全期間を読む → `WHERE ts BETWEEN`。アプリで filter → SQL の WHERE へ（T7 の列を使う）。
- 効く条件: SQL 表で rows_sent が大きく、ハンドラは先頭だけ使う。
- 当日の確かめ方: SQL 表 rows examined/sent、ハンドラのループに `break`。
- リスク: フィルタを SQL に移さず LIMIT だけ足すと結果が変わる（11q Mirrativ の整合性 FAIL、isucon_friends 失格）。範囲の両端を切って点を落とした（都営三田線）。
- 実例: 11q trend LIMIT 1（fujiwara 06190135 ~17.9k → 30k）、graph の 24 時間窓（NaruseJun 346k → 631k）。

### T23. SQL でやっている文字列処理をアプリへ（LIKE ループ、全件 DELETE）
- 何を: NG ワードごとの `LIKE CONCAT('%',?,'%')` を Go の `strings.Contains` に。新規ワードだけを対象に 1 本の DELETE。
- 効く条件: SQL 表の count 上位が定数表のトリック（`SELECT ? AS text`）や LIKE。
- 当日の確かめ方: SQL 表、ハンドラのループ。
- リスク: 照合順序（utf8mb4_bin は大文字小文字を区別 → `ToLower` で挙動が変わった、13 ultra_fast_gopher）。逆に、元の実装がプレースホルダ同士を **接続の照合順序（utf8mb4_general_ci、区別しない）** で比較している年もある（13 練習）: 書き換えるときは `COLLATE` を明示して元と同じ比較にする。
- 実例: 13 NaruseJun 75bc8f8a6e、釜中の鯖 +25,261、moderation 単発 DELETE 64054e39b6（15:00 → 204,805）。

### T24. MySQL とドライバの設定
- 何を: `disable-log-bin`、`innodb_flush_log_at_trx_commit=2`、`innodb_doublewrite=0`、`O_DIRECT`、buffer pool、`max_connections`、`interpolateParams=true`、`SetMaxOpenConns` 10 → 50〜128、`performance_schema` は必要なら残す。
- 効く条件: 書き込みが多い。SQL 表に ADMIN PREPARE。`Max_used_connections` が上限付近。
- 当日の確かめ方: kit の `etc/mysql-zz-perf.cnf` が当たっているか（deploy-init の DB ブロック）。SQL 表の PREPARE。
- リスク: `/dev/shm` の redo は再起動で消える（matsuu は通ったが博打）。buffer pool を大きくすると app 同居の 4 GB で逆効果（13 NaruseJun）。接続数を増やすと FOR UPDATE 待ちが悪化（11f matsuu）。
- 実例: 14 takonomura 94072bd9、11q 都営三田線 interpolateParams で 593k → 1,045k（最後に効いた）、12f 織時屋 18,073 → 27,927。10q は MySQL 5.7 の **query cache ON**（`query_cache_type=1`、16M）が SunPro の最大の 1 手（880 → 1,422。同じ検索が繰り返される年だけ。MySQL 8 には無い）。10q innocent-team は pool を 10 → 50 にして FAIL（1 core の DB では増やすほど悪化）。

### T25. シャーディング・二重書き（DB が 2 台以上余るとき）
- 何を: user_id などの単一キーで全クエリが絞れるなら、キーで DB を選ぶ。マスタは全 shard に複製、admin は fan-out。読み専用の複製が欲しければアプリから 2 台に書く（レプリカ遅延なし）。
- 効く条件: 5 台構成（12f）や DB が明確な律速で、WHERE に shard キーが無いクエリが admin だけ。
- 当日の確かめ方: app-map で user_id の無いクエリを数える。initialize が shard ごとに時間内か。
- リスク: セッション → shard を決める前にユーザーを知る必要（session id にユーザー id を埋める）。tx が shard をまたげない。非同期レプリカからの読みは FAIL の元（11f kazeburo）。
- 実例: 12f NaruseJun 4 DB（14:08 → 210,000）、織時屋 xxhash shard、11q NaruseJun uuid 先頭バイトで 3 shard、11f NaruseJun double write。

---

## 5. 構成

### T26. DB 分離 → app 2 台 → endpoint を host に pin
- 何を: DB を 1 台に出す（kit の parallel-setup A）。次に app を 2 台にし、**ファイルや in-memory 状態に触る endpoint（upload、icon、register、DNS 更新、export）は 1 台に** nginx の location で寄せる。
- 効く条件: Verdict が app CPU 律速、svc2 が空き。RUNBOOK §6a の表。
- 当日の確かめ方: `summary.md` の CPU by process、§6a。
- リスク: in-memory キャッシュの無効化と /initialize の fan-out（T29）。ワーカー（matcher）が 2 つ動く（14 講評「排他制御」）。
- 実例: 11q DB 分離 + app 2 台で「大きく伸びる」（講評）。13 NaruseJun S1 nginx+app(DNS/register/icon) / S2 MySQL / S3 app。11f 講評「お知らせ系 3 endpoint を 3 台目に」。
- 派生: 「ベンチの向き先 ≠ app の host」（13: DNS が答える IP を変えて負荷を逃がす）。
- 派生: **JOIN の無いテーブル群は DB を 2 台に分ける**（10q chair と estate を別 MySQL へ。FCCPC 1,948 → 3,171 で最大の 1 手、講評「App×1 / DB×2」。initialize は両方へ並列に）。
- 派生: **3 台のスペックが違う年がある**（10f: 2c/1GB、2c/2GB、4c/1GB）。最初に `nproc` / `free -m` を全台で取る（kit の host:peek）。講評「スペック差に気付いてないと苦しむ」。

### T27. 付随サービスを隔離する（DNS、matcher、bcrypt）
- 何を: アプリ以外で CPU を食うもの（PowerDNS + その MySQL、systemd の matcher、bcrypt）を別 host へ。bcrypt は内部 HTTP で他 host に委譲した例もある（3 台の中なら可）。
- 効く条件: CPU by process で app/mysqld 以外が上位。
- 当日の確かめ方: `summary.md` の CPU by process、`task host:diag`。
- リスク: 隔離先の再起動順序。コスト変更は禁止（bcrypt）。
- 実例: 13 くすサポ はベンチ向き先を PowerDNS の host にしただけで 72,957 → 128,660。13 ultra_fast_gopher bcrypt 967f35bfc8。13 練習（2026-10-18）: svc1 の 2 コアのうち 0.9 コアが DNS（ローカル mysqld 66% + pdns 26%、`records` 引き 1,700/s）。report の Verdict は PowerDNS の SQL 割合を出すようにした。`zone-metadata-cache-ttl=60` で `domainmetadata` 引き 72k 回が消える。`pdnsutil add-record` の exec を isudns への INSERT に置き換えると register 517 → 58 ms。

### T28. nginx: keepalive、HTTP/2 上限、worker_connections、unix socket、buffering
- 何を: `upstream keepalive`、`proxy_http_version 1.1` + `Connection ""`、`http2_max_requests` を上げる（GOAWAY 対策）、`worker_connections` / `worker_rlimit_nofile`、同居 app は unix socket、SSE/ZIP は buffering off。
- 効く条件: ベンチのエラーに connection reset / GOAWAY。TIME_WAIT 多数。
- 当日の確かめ方: timeline の tcp 列、nginx error.log、ベンチのエラー文。
- リスク: 設定を一度に変えてスコア半減（13 hirosuzuki の keepalive）。再起動後に 502（12q SysCa 失格）。
- 実例: 11q shallowverse http2_max_requests 20000 で 35k → 70k、12q 釜中の鯖 worker_connections で +15,000、13 NaruseJun unix socket 158k → 295k → 836k（複合）。

### T29. /initialize を「全部やり直す場所」として設計する
- 何を: スキーマ変更のバックフィル、初期データの再生成（事前に作ったファイルをコピー）、全 host のキャッシュ消去（内部 endpoint に fan-out）、ファイルの掃除、キャッシュのウォーム。
- 効く条件: 常に。T1〜T8 のどれかを入れた時点で必須。
- 当日の確かめ方: initialize の所要（timeline の最初の 1 本）。追加した global 変数ごとに reset 行があるか。
- リスク: 忘れ 1 つで失格（11q icon、11f zip、13 svc3 のキャッシュ）。上限秒数（12q 30 秒 → 物理コピーで戻す）。
- 実例: 13 NaruseJun `/api/initialize/slave` 79c879ca0d、12q 第7西東京市 `/var/lib/mysql` コピー、14 takonomura 初期データ再生成。

### T30. 最終ベンチ前に計測を全部切る
- 何を: nginx access_log、アプリのロガー、slow log、pprof/fgprof、監視エージェント、`ENABLE_TRACING`。kit では `task hosts:measure-off` + IMPLEMENTATION-NOTES §6。
- 効く条件: 常に。+10〜30%。
- 当日の確かめ方: RUNBOOK §8-2。
- リスク: 切り忘れ（13 NaruseJun は pprotein の access log が残っていて最後の 1 時間で 277k → 468k）。VPC に計測用インスタンスを残して envcheck 失格（14）。
- 実例: 13 釜中の鯖 +186,373、12q 第7西東京市 66,960 → 76,721。

### T31. Go ランタイム: GOGC、go-json/sonic、PGO、sync.Pool
- 何を: `GOGC=2000`、JSON シリアライザ置換、直前の良い run の pprof で `-pgo` ビルド、hot な slice を Pool。
- 効く条件: DB を片付けた後、pprof で encoding/json と GC が上位。
- 当日の確かめ方: report の pprof top。
- リスク: フィールド名の差（sonic）。終盤の 1 桁 % 向け。
- 実例: 13 NaruseJun PGO f3fe3145a1、14 mazrean Go 1.24 + sonic、12f NaruseJun GOGC=2000。

---

## 6. ベンチの挙動を利用する（推測を含む。deep-dive §10 で裏取り）

### T32. 点になるリクエストだけを速くする
- 何を: スコア式を読み、点を生む endpoint（13 投げ銭つきコメント、11f 課題提出 3 点、14 乗車距離、12q 10 点の organizer API）に直結する経路を最優先にする。alp の上位 = 点ではない。
- 効く条件: 常に。
- 当日の確かめ方: `docs/kickoff.md` のスコア式、管制盤の score/POST 列、deep-dive Q-score-link。
- リスク: 13 講評「HTTP リクエストの最適化がスコアに結びつきにくい場面」。
- 実例: 13 は tips 合計がスコア、長い配信枠ほど高額 → 予約 slot の処理（T22）が効いた（織時屋 16:52 → 207,292）。14 は迎車距離 ×0.1 + 乗車距離 + 完了 ×5 → マッチングで最寄りを選ぶ（T34）。9q は成立した取引の価格合計（リクエスト数ではない）→ `/buy` を通す人数とログイン能力が全て。9f は endpoint ごとの点 + 仮予約ボーナス + 隣席ボーナス。

### T33. ユーザー（負荷）が増える条件を満たす
- 何を: ベンチが「ユーザーを増やす」トリガー（11q: trend が変化、14: 評価 ≥ 4 で友達招待・オーナーの売上で椅子追加、13: register が捌けると視聴者が増える）を読み、そこを詰まらせない。
- 効く条件: マニュアル/講評に増加モデルの記述。Bench shape が rising load。
- 当日の確かめ方: deep-dive Q-concurrency（クライアント数の推移）、Q-dropoff。
- リスク: 推測。増えすぎて別の上限（fd、接続）に当たる。
- 実例: 13 白金動物園「register が律速で同時ユーザー数が頭打ち」、14 mazrean owner API 高速化 336,850 → 389,765、11q 講評「どこかが詰まると次の行動に移れない」。

### T34. 割り当て・マッチングのアルゴリズムを変える（内部 endpoint は自由）
- 何を: 参考実装のランダム割り当て（`ORDER BY RAND()`、1 件/500ms）を、待ち全件 × 空き全件の貪欲最近傍に。速度や距離上限を考慮。間隔を短く。
- 効く条件: マニュアルに「内部 endpoint は自由に変更可」。評価にマッチ時間や距離の閾値がある。
- 当日の確かめ方: ベンチ出力の不満理由の内訳（14: 「N% のライドはマッチまでの時間」）。
- リスク: matcher が複数 host で二重に動く。高度化しすぎて 4 時間無駄（14 3 位）。
- 実例: 14 takonomura 88eae785、matsuu 6,025 → 21,023、都営三田線 11,068 → 21,372。

### T35. alp に出ない隠れた律速（直列区間）を探す
- 何を: flock、単一 goroutine のワーカー、固定間隔の ticker、`exec.Command`、外部 API の直列呼び出しは、endpoint 表では「遅くない」のに全体を律速する。
- 効く条件: Verdict が wait / 全箱 CPU < 50%、Bench shape が固定並列なのに処理数が伸びない。
- 当日の確かめ方: kit の report「exec detection」、deep-dive の in-flight と CPU の比較、コードの mutex/flock/ticker を grep。
- リスク: 推測で直して当たらないことがある → 1 つずつ計測。
- 実例: 11f 講評「統計に出ない部分が律速」、13 白金動物園「register は遅くないのに律速」（flock + nsd reload）、14 matcher 500ms。10f の `ReceiveBenchmarkJob` は「50 ms sleep のポーリング × チーム数」でロック待ちが律速 → Redis BRPOP / プロセス内チャネルへの push（kazeburo 3be8029f、講評）。9q bcrypt の login が CPU を食い、campaign を上げるとスコアが落ちた（nil 17,260 → 6,850）→ **`/login` だけ別 host 2 台へ**（nil ab8a1565 → 34,240、にがり 10,000 → 22,000、百万円ドリブン）。

### T36. ポーリングは「待たせた分をまとめて返す」
- 何を: 間隔を伸ばした分、1 回の応答に溜まった遷移を全部（順序通り）返す。
- 効く条件: 「全ての状態遷移を順序通り少なくとも 1 回」のような配信要件 + 間隔をサーバーが決められる。
- 当日の確かめ方: マニュアルの通知節、Q-scenario のポーリング比率。
- リスク: 順序違反はクリティカル（14）。
- 実例: 14 都営三田線 30,936 → 39,027、takonomura a00586c4。

### T37. /initialize の応答で負荷レベルを自分で決める（campaign、available_days）
- 何を: マニュアルが「initialize の応答でベンチの強さを選べる」と言う年（9q `campaign` 0〜4 で Load worker +1 ずつと人気者出品、9f `available_days` 10〜366）は、能力が上がったら段階的に上げる。上限が点の上限。
- 効く条件: 当日マニュアルの initialize 節に数値パラメータがある。
- 当日の確かめ方: kickoff で initialize の応答仕様を必ず読む（9 の 2 チームは終盤までノブの存在に気づかず）。
- リスク: 上げすぎはタイムアウト減点（9f 白金動物園「20 日にして即下げる。秒で怒られた」）。先に per-request を下げる。
- 実例: 9q nil は campaign 3 で 53,460（4 で 52,440）、9f ARINE 20 日 → 15,066、chy72 感想戦 100 日で 105,417。10f `TeamCapacity`（11 チーム目から 403 を返す、という仕様の定数）を 10 → 50〜120 に上げると参加者数で ×1.2〜2.0 のボーナス（がんもどき 16,718 → 29,822、takonomura は 150 を revert して 120）。envoy の `LimitNOFILE` と DB pool を先に。
- 派生（推測）: 11 以降は「ユーザーが自動で増える」モデルに変わったので、ノブの代わりに T33 の増加条件を満たす。

### T38. 「ユーザーに合わせた一覧」の加点（マニュアルが招いているとき）
- 何を: 一覧の中身を「そのユーザーが買うカテゴリ」に寄せると、ベンチが閲覧を省いて購入に進む（9q: 先頭 48 件の 80% 超が買い手の親カテゴリなら 2 倍買う）。
- 効く条件: マニュアルに「よりユーザーに合わせた一覧を返すことで購入の機会を増やせる」のような一文。初期データでユーザーの嗜好が 1 つに決まっている。
- 当日の確かめ方: マニュアルを「ユーザにあわせ / おすすめ / 機会」で grep。`SELECT buyer_id, category, COUNT(*) GROUP BY` で嗜好の偏り。
- リスク: 件数・順序・ステータスの制約（「古いデータの削除、非表示は不可」「一度に返す件数は同じ」）を守る。順序のシャッフルは怒られた（methane）。`user_id % n` で決めたチームは加点を取り逃した（白金動物園）。
- 実例: 9q bench `loadIsRecommendNewItems`、講評「数%」（ベンチのコードは 2 倍買い）。
- 派生（推測）: 他の年でも「一覧の並び」を点につなげる文言が無いか kickoff で探す。

### T39. 終了の瞬間に切れたリクエストを壊さない（context.Background）
- 何を: ベンチが 60 秒で接続を切る（nginx 499）と Go の `r.Context()` が cancel され、tx や外部 API が中途半端に終わる → 重い処理は `context.Background()` で。
- 効く条件: access log に 499、app log に context canceled。
- 当日の確かめ方: endpoint 表の 4xx 内訳、deep-dive Q-dropoff。
- リスク: 無し（60 秒ベンチ）。
- 実例: 9q nil cb3cee95。

### T40. 重い処理は応答後に（キュー + バックグラウンドワーカー、猶予秒数の内に）
- 何を: マニュアルが「終了後 N 秒待ってから整合性チェック」と言うなら、外部キャンセルや集計を Redis/DB のキューに積んで返し、ワーカーが N 秒以内に一括処理する。
- 効く条件: マニュアルの整合性チェック節に待ち時間。外部 API に bulk エンドポイント。
- 当日の確かめ方: マニュアル grep（秒 / bulk / _bulk / 一括）。
- リスク: キューはプロセス外（再起動で消えない）に。終了前に drain。急造の bulk 実装で nil ポインタ → FAIL（9f methane）。
- 実例: 9f 白金動物園 cancelbot（systemd unit）、講評「5 秒以内毎にバッチ処理でバルクキャンセル」。レギュレーション「ジョブキュー機構の追加による遅延書き込み」。

### T41. 付随ミドルウェアを最小実装で置き換える（13 DNS）
- 何を: 問題が要求する機能が狭い付随ミドルウェア（DNS、キュー、検索、メディア変換など）を、汎用製品のチューニングではなく、**必要な機能だけを app の中に（または小さなプロセスで）書いて置き換える**。13 は PowerDNS + MySQL をやめ、`github.com/miekg/dns` で app が UDP/53 に答えた。**攻撃トラフィック（水責めのランダムなサブドメイン）には応答しない**（NXDOMAIN も返さない）ので、ベンチ側がタイムアウトを待ち、負荷そのものが減る。
- 効く条件: マニュアルが仕様を狭く書いている（13: 「A レコードのみ」「CNAME の再帰はしない」「TCP へのフォールバックなし」「失敗しても HTTP は来ない」「名前解決の成功数はスコア外（企業賞だけ）」）。report の Verdict で、その付随プロセス（とそのための mysqld）が CPU の 30% 以上。T27（別 host へ隔離）で足りるかを先に考え、足りなければ置き換え。
- 当日の確かめ方: kickoff の構成要素表の「自前実装の候補」列。マニュアルの「〜のみ」「〜しない」「〜はスコアに含まれない」。summary.md の Verdict の DNS 行と CPU by process。
- リスク: **再起動試験**（名前一覧をメモリだけに置くと、登録したユーザーの名前が再起動で消える。起動時と /initialize で DB の `users` から作り直す。§0）。initialize で一覧を戻す（複数 host なら全台に fan-out、T29）。応答しない設計は、企業賞（名前解決成功数）を捨てる判断。実装に 1〜2 時間。UDP/53 の bind は root か `CAP_NET_BIND_SERVICE`（systemd の `AmbientCapabilities`）。
- 実例: 13 NaruseJun `app/go/dns.go`（miekg/dns、TTL 120、知らない名前は無応答、名前一覧は sync.RWMutex のスライス）: 「DNS on App」で 76,951 → 132,390（参加記 https://zenn.dev/tohutohu/articles/923bdf5dcd73af）。最終 468,006。

---

## 8. 無条件で入れる小技（technique-scan を待たない。parallel-setup のサブエージェント D）

9〜14 の上位チームがほぼ全員、問題を見る前にやっていたもの。挙動を変えないので判定不要。1 commit ずつ、`task deploy` → `task smoke` で確認。

| 小技 | 根拠 | kit での入れ方 |
|---|---|---|
| `interpolateParams=true` | T24。都営三田線は最後に入れて 593k → 1,045k、12f 織時屋 18,073 → 27,927、13 onk「ADMIN PREPARE が出ないように」 | D が DSN を直す |
| 接続プール 10 → 64 程度（`SetMaxOpenConns` / `SetMaxIdleConns` / `ConnMaxLifetime`） | 参考実装は 10 固定のことが多い。上げすぎは FOR UPDATE 待ちを増やす（11f matsuu）、1 core DB では逆効果（10q innocent-team は 50 で FAIL） | D。64 を既定、DB が律速なら触らない |
| 起動時に DB を待つ（`sqlx.Open` + ping リトライ、`Restart=always`、`StartLimitIntervalSec=0`） | 再起動試験で DB ホストが後から上がる（14 takonomura afaf2dd5、12q Zatsu-X-Club「再起動対策に DB ping」） | deploy-init の restart.conf + D のコード |
| フレームワークの debug / logger / pretty JSON を切る | 10q 講評「echo の debug モードだと整形 JSON がログに」、12q 第7西東京市 66,960 → 76,721、11q 講評「ログを出力しすぎ」 | D（nginx の access log は計測に残す。最終前は `hosts:measure-off`） |
| `LimitNOFILE=65536` を app と proxy の unit に | 10f buchy は envoy の fd 上限で最終 FAIL、11q 講評「fd 上限を増やさないと fail」 | deploy-init の restart.conf に入れた |
| `GOGC=200`（Go） | 12f NaruseJun / 13 釜中の鯖 は 2000。4 GB で app と同居なら 200 程度 | D が drop-in に |
| nginx: `worker_rlimit_nofile`、`worker_connections 4096`、upstream `keepalive` + `proxy_http_version 1.1` + `Connection ""` | 12q 釜中の鯖 worker_connections で FAIL → +15,000、11q NaruseJun keepalive/unix socket。**誤設定で半減した例あり**（13 hirosuzuki）→ 差分は人が見る | D が `etc/nginx/` を編集、PR 本文に差分 |
| MySQL `zz-perf.cnf`（binlog off、flush_log_at_trx_commit=2、max_connections） | T24。13 練習で initialize 16.3 s → 3.4 s | deploy-init が DB ホストに入れる（済み）。DB 専用ホストになったら `innodb_buffer_pool_size` のコメントを外す（A） |
| index を `webapp/sql/indexes/*.sql` に置いて initialize と再起動を越える | T20。2 台目に当て忘れ（13 織時屋）、schema に書かず消えかけた（9f しゃかラン） | parallel-fix の index 群 + `apply.sh`（済み） |
| apt / unattended-upgrades を止める | 13 練習で bench 機 97%、DB 機 29% | bootstrap（済み） |

入れないもの（判定が要る）: JSON ライブラリの置換（フィールド名の差で壊れる）、トランザクション削除、FOR UPDATE の変更、キャッシュ全般、go-json、PGO。

---

## 7. 守り: 失格・FAIL の実例（やる前に必ず当てる）

| 落とし穴 | 実例 | kit での対策 |
|---|---|---|
| 再起動後に画像/ZIP が無い | 11q morupoppo・さばかんちゃーはんかれー、11f Oops!・SNE・今年こそは出場 | ファイルは initialize で再生成、§8-3 の reboot-test |
| 再起動後にデータが無い（メモリだけ、Redis 未永続、tmpfs） | 14 最上川（データ保持違反）、11f 全 12 チーム FAIL のうち複数 | IMPLEMENTATION-NOTES §0、db:snapshot before/after |
| 別 host のキャッシュが initialize で消えない | 13 tmokmss（10 分前に発覚）、12f innocent | T29 の fan-out、smoke INIT=1 |
| 初回 GET に 304 | 11f テディーベア | T15 の条件 |
| tx を外して瞬間的な不整合 | 13 都営三田線（227,377 → 0） | T13 のルール |
| ブラウザ目視で機能が壊れている（ベンチは通る） | 12f 釜中の鯖・第7西東京市・kyncon | §8-3「ブラウザで開く」 |
| VPC に計測インスタンス、EBS 変更 | 14 TAK848・itohdak・every | §8-1、envcheck を自分で実行 |
| DNS の状態が再起動で壊れる | 13 iceman（T-15 分の再起動で FAIL） | 付随サービスの起動順、§8-3 |
| 2 台目に index/設定を当て忘れ | 13 織時屋 isudns index | `task deploy` は全台、diffcheck |
| 追試の 75% ルール | 13 で 96 チーム追試 FAIL | 最終ベンチ後は触らない |
| index で出力順が変わり pretest 落ち | 9f いんふらえんじにあー・しゃかラン | T20 の罠。`ORDER BY` を先に |
| docker から剥がして自動起動が無い | 9f netmark（17:59 まで復旧） | deploy-init の unit、§8-3 |
| 計測サービスを最終 run で止め忘れ | 9f mirakui（stackprof）、13 NaruseJun | §8-2 |
| ブラウザ目視（SPA が壊れた、検索 10 件未満） | 9f ARINE（2 位相当が無効） | §8-3 |
| 再起動後に MySQL/MariaDB が上がらない、unit の順序 | 10q 予選上位 5 チーム失格（theorem 7,744 など）、to-hutohu「MySQL → MariaDB 入替が原因」 | §8-3 の reboot-test |
| フロントのプロキシの fd 上限（envoy `LimitNOFILE`） | 10f buchy 37,501 → 最終 FAIL | deploy-init の unit drop-in、host:diag の limits |
| 猶予の無い endpoint をキャッシュ | 10f hidekiy（clarifications）、12q innocent-team（COUNT+SELECT 統合） | T5 の条件、マニュアルの「即時反映」 |

---

## 付録 A. 大会別のレギュレーションとスコア式（要点）

| 大会 | スコア | 猶予・特記 | 追試 |
|---|---|---|---|
| 14 ISURIDE | 迎車距離×0.1 + 乗車距離 + 完了×5。評価（マッチ <3 s、迎車距離 < 10×速度、遅延）でユーザー増減 | 座標・走行距離 3 秒。通知は 3 秒以内、retry_after_ms / SSE 可。決済に Idempotency-Key | 2 回再起動、FAIL か ≤75% で失格、データ保持 |
| 13 ISUPIPE | 投げ銭の合計（長い配信枠ほど高額）。DNS 成功数はスコア外 | icon_hash 2 秒、304 は条件付きのみ、bcrypt コスト変更禁止、UDP/53 必須 | 再起動後ベンチ、≤75% で失格、96 チーム FAIL |
| 12q ISUPORTS | 1 点（player 系）/ 10 点（organizer・admin 系）、エラー −1%、クリティカル −10% | finish→billing 3 秒、disqualified 3 秒、429 可 | ≤85% で失格、ブラウザ目視 |
| 12f ISUCONQUEST | login 3、present 3/2、gacha 2、他 1。エラー −15 点、50 超で終了 | **猶予なし（即時反映）**、master 更新 1 回、x-isu-date | 3 回の best、ブラウザ目視で 5 チーム失格 |
| 11q ISUCONDITION | condition 閲覧 Info 20/50 件…、グラフ Good 60/150（n>20）。POST は減点なし、100 ms | condition の反映遅延許容、condition↔graph 1 秒、Conditional GET | 再起動後ベンチ、データ保持、目視 |
| 11f ISUCHOLAR | 課題提出 3、お知らせ一覧 1。エラー −50、タイムアウト 5 s | **即時反映**、Link ヘッダ変更可、GPA 誤差 0.01 | poweroff → 3 回の best、2 回 FAIL で失格 |
| 9q ISUCARI | 成立した取引の価格合計 − 減点。人気者出品のたびに価格が +20。多重決済はクリティカル | `campaign` 0〜4 で負荷を選ぶ、一覧は「ユーザーに合わせてよい」が件数・順序の制約あり、外部 API は 0.8 秒固定、平文パスワード禁止 | 再起動後にデータ保持、ブラウザ目視 |
| 9f ISUTRAIN | endpoint ごとの点（search/seats 3、reserve 5）+ 仮予約 10 + 隣席 10〜25。timeout −5 | `available_days` 10〜366、RAM 1 GB、終了後 5 秒以上待って整合性チェック（決済のキャンセル）、検索 10 件未満は失格 | 主催者再起動後の最初のベンチが最終スコア |
| 10q ISUUMO | 椅子の購入件数 + 物件の資料請求件数 − 減点（検索は点にならない）。エラー −50、10 回で失格。CSV 入稿の失敗/タイムアウトは即失格 | bot UA に 503 可、ベンチは静的ファイルを取らない、1 秒超の API はシナリオやり直し、負荷レベルはスコアで段階的に上がる | 再起動後ベンチ、上位 5 チーム失格 |
| 10f XSUCON | 仮想選手スコア × 大会規模ボーナス（参加者数で ×1.0〜2.0）+ 観客スコア − 減点 | audience dashboard だけ 1 秒の猶予、他は即時。TeamCapacity は 403 で制御、Web Push 可、HTTP 圧縮可、3 台のスペックが不揃い | poweroff → 3 回の best、ブラウザ目視（initialize なし） |

## 付録 B. 出典

大会別の原本（全 URL、commit SHA、スコア推移）: `docs/techniques/sources/isucon14.md`、`isucon13.md`、`isucon12.md`、`isucon11.md`、`isucon10.md`、`isucon9.md`。他チームの計測・分析手法の調査: `docs/techniques/sources/measurement-practices.md`。
