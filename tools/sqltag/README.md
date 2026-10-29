# sqltag — SQL にハンドラ名のコメントを付ける

MySQL のスローログに `/* GET /api/app/notification @ notification_handler.go:88 */ SELECT ...` と残るので、`isumeasure report` が **エンドポイントごとの SQL** と、**その SQL を発行したコードの行**（SQL 表の「code」列。GitHub の該当行へのリンク付き）を出せるようになる。静的解析（`codeDir`）より正確（Sprintf で組み立てた SQL、`IN (?)` の展開、ヘルパー関数経由でも当たる）。

行は、database/sql・sqlx・sqltag 以外で最初のスタックフレーム。呼び出し元ごとに 1 回だけ解決してキャッシュするので、1 クエリあたり 1 µs 未満（手元の計測で約 0.75 µs）。行が要らなければ `SQLTAG_CALLER=0`（または `sqltag.Caller = false`）。

## 入れ方（Go + sqlx。3 行）

1. `sqltag.go` を `webapp/go/sqltag/sqltag.go` にコピー（`go.mod` の module 名に合わせて import する）。
2. 起動時、`sqlx.Connect` の前に driver を登録し、driver 名を差し替える。

```go
import "isuride/sqltag" // module 名に合わせる

sqltag.Register("mysqltag", mysql.MySQLDriver{})       // go-sql-driver/mysql
db, err := sqlx.Connect("mysqltag", dbConfig.FormatDSN()) // "mysql" → "mysqltag"
```

3. ルーターに middleware を足す（他の middleware より前）。

```go
// net/http / chi
sqltag.Pattern = func(r *http.Request) string { return chi.RouteContext(r.Context()).RoutePattern() } // chi なら（任意）
mux.Use(sqltag.Middleware)

// echo
e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		c.SetRequest(r.WithContext(sqltag.With(r.Context(), r.Method+" "+c.Path())))
		return next(c)
	}
})
```

## 同時に入れる: pprof / expvar（report の pprof top と Go ランタイム・DB プールの系列に使う）

```go
import (
	"expvar"
	"net/http"
	_ "net/http/pprof"
)
// main() の先頭で（127.0.0.1 のみ。agent が +20s で /debug/pprof/profile を取り、毎秒 /debug/vars を読む）
expvar.Publish("db", expvar.Func(func() any { return db.Stats() }))   // WaitCount / WaitDuration が出る
go func() { _ = http.ListenAndServe("127.0.0.1:6060", nil) }()
```

アプリが `http.DefaultServeMux` を使っていると `/debug/pprof` が外に露出するので、その場合は専用の `http.NewServeMux()` に `pprof.Index` 等を登録する。

ハンドラが `db.GetContext(ctx, ...)` / `tx.ExecContext(ctx, ...)` のように **request の ctx を渡していれば** 自動で付く。ctx を渡していない呼び出し（`db.Get(...)`）には付かないので、気になる所だけ `Context` 版に直す。goroutine やバッチ（マッチング処理など）は `ctx = sqltag.With(ctx, "matcher")` で名前を付ける。

## 同時に入れる: ユーザー ID をレスポンスヘッダに（ユーザー行動分析用。NaruseJun / Mirrativ の手）

認証ミドルウェアでユーザーが分かった直後に 1 行。nginx の ltsv が `uid:$upstream_http_x_isu_uid` で記録し、report / deep-dive の「クライアント」の鍵になる（Cookie だとログイン前後で鍵が変わる）。

```go
c.Response().Header().Set("X-Isu-Uid", strconv.FormatInt(user.ID, 10)) // echo。net/http なら w.Header().Set
```

ユーザー ID が秘密扱いの年は HMAC や先頭 8 文字で良い（同じユーザーが同じ値になれば足りる）。最終ベンチ前は `task hosts:measure-off` で nginx の access_log ごと止まるので、ヘッダは残しても害はない。

## 計測のキルスイッチ

計測パッチの各行は `// measurement:` を付け、`MEASURE=0` のときは何もしないように囲む（sqltag の driver 差し替え、pprof リスナー、uid ヘッダ）。最終ベンチ前に unit の drop-in で `Environment=MEASURE=0` を入れれば、コードを消さずに全部止まる（IMPLEMENTATION-NOTES §6）。

## 注意

- `interpolateParams=true` でも、プリペアドステートメントでも、コメントはそのまま送られる。
- 性能影響は文字列結合 1 回と、呼び出し元の行の解決（キャッシュ後 1 µs 未満）。最終ベンチで外す必要はない（気になるなら `MEASURE=0`）。
- `Pattern` を設定しない（または echo 以外のルーター）では `/api/rides/01J...` のような生パスがタグになる。report 側で ID を `:id` に正規化するので集計は問題ない。
- 外部依存なし（`database/sql` だけ）。コピーした時点で `go mod tidy` は不要。
