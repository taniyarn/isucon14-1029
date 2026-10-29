/* Client-side app for report.html. Expects window.MODEL. No external deps. Bilingual (ja/en). */
(() => {
  const M = window.MODEL;
  const $ = (sel, el = document) => el.querySelector(sel);
  const h = (tag, attrs = {}, ...children) => {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") el.className = v;
      else if (k === "style") el.style.cssText = v;
      else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else if (v != null) el.setAttribute(k, v);
    }
    for (const c of children.flat()) if (c != null && c !== false) el.append(c.nodeType ? c : document.createTextNode(String(c)));
    return el;
  };

  // ---------- i18n ----------
  const T = {
    en: {
      tabs: { now: "Now", overview: "Overview", map: "Map", flows: "Bench flows", storyboard: "Storyboard", endpoints: "Endpoints", data: "Data model", rules: "Rules", hosts: "Host placement", screens: "Screens", scan: "Code scan / perf hints" },
      dataHead: "Tables, inferred relations (<x>_id → x) and row counts", dataRows: (src) => (src ? `rows from ${src}` : "rows: run `task db:snapshot LABEL=latest` after a bench and re-run report"), dataCols: ["table", "rows", "columns", "indexes", "refers to"], erHint: "boxes = tables (PK bold, (i) = first column of an index); arrows = <x>_id column → table x. Hover a column for its type.",
      perfHints: "Performance hints — code snapshot at crawl time", perfNote: "These come from reading the code when the crawl ran. The bench report (summary.md) is the source of truth; after the first fix round most of these are gone.",
      notice: "the benchmarker calls the API directly, so every count here is the browser UI's behaviour, not the bench's. Use this page to learn the service (actors, screens, state machine, which API does what); confirm the real request mix with the access log (alp) after the first bench.",
      readFirst: "Read this first: ",
      serviceSummary: "Service summary (auto)", actor: "actor", screens: "screens", endpointsRW: "endpoints (read/write)", polling: "polling", resources: "resources in paths", noUI: "no UI (API only)",
      stateMachine: (l) => `State machine (${l})`, triggerNote: "trigger = last non-polling client call within 5 s before the state changed. ", storyboardLink: "storyboard →",
      topFindings: "Top findings (start here)", numbers: "Numbers",
      system: "System (what runs on the host, where data lives, how big the code is)", sysComponents: "components (systemd units captured by bootstrap)", sysWeb: "web (nginx)", sysStorage: "storage", sysCode: "code size", sysNotes: "server-info", sysEnv: "env.sh keys", sysNone: "no etc/ in the repo yet (run bootstrap, git pull)",
      sysCols: { unit: "unit", exec: "ExecStart", dir: "WorkingDirectory", deps: "depends on", server: "server_name", listen: "listen", root: "root", locations: "locations" },
      sysTiles: { files: "Go files", lines: "lines", funcs: "functions", routes: "routes", sql: "SQL literals", tables: "tables", columns: "columns", indexes: "secondary indexes" }, biggest: "biggest files",
      tiles: { screens: "screens (routes)", flows: "flows", endpoints: "distinct endpoints observed", polling: "polling endpoints", errors: (a) => `app-origin errors (${a} crawl artefacts)`, openapi: "OpenAPI ops not observed" },
      matrix: "Screen × endpoint matrix (count per screen, grouped by actor; red top border = write; click a cell to open the screen)", matrixHead: "screen \\ endpoint",
      chipLegend: "chip colours: ", legend: { poll: "polling", slow: "≥300ms", err: "app error", soft: "crawl artefact" },
      mapHint: "Click a screen to see its API calls and a larger screenshot. Hover to highlight links. Drag to pan, wheel / buttons to zoom.",
      mapLegend: "solid = link on the page, dashed orange = redirect. Buttons and forms are not followed (see Storyboard / flows). Screens not reachable by links from the top page (e.g. behind a login button) are drawn as islands on the right. Columns ≈ clicks from the top page.",
      openDetails: "open details →", noApi: "no API calls on this screen", endpoint: "endpoint", count: "count", medianMs: "median ms", role: "role", buttons: "buttons: ", fit: "fit",
      noFlows: "No flows recorded. Add persona.flows in the config to capture click-driven screens.", steps: (n) => `${n} steps · `, openLog: "open request log →",
      transitionsHead: "State transitions and the client call that triggered each (actor badge = who called it):", serverSide: "no client call (server-side)",
      timelineHead: (k) => `Timeline of ${k} values seen in JSON responses (numbers = step index):`,
      frameNote: "Frames = screen state at the start of each step (highlighted when a state change happened during it). Bold chips after a frame = the non-polling calls made during that step, i.e. what drives the next frame. Polling is folded.",
      noNonPolling: "(no non-polling calls)", pollingFold: (n) => `+ polling (${n})`,
      filter: "filter (path, method, actor, role, screen, handler, table)", presets: "presets: ", all: "all", nEndpoints: (n) => `${n} endpoints`, preset: (p) => ` · preset: ${p}`,
      cols: { actor: "actor", role: "role", method: "method", path: "path", count: "count", medianMs: "median ms", statuses: "statuses", screens: "#screens" },
      write: " (write)", appErrors: (n) => ` ${n} app error(s)`, artefacts: (n) => ` ${n} crawl artefact(s)`, serverDirected: "server-directed client behaviour field",
      handlerTables: "handler → tables", calledFrom: "called from", schemaHead: (st) => `response schema (status ${st})`, noRoute: "no route matched", setCodeDir: "set codeDir in config", noJson: "(no JSON body)",
      request: (st) => `request (${st})`, response: (st) => `response (${st})`, tables: "tables: ", sql: (n) => `${n} SQL`, loopSql: "SQL in loop (N+1?)", locks: "row locks", external: "external HTTP call", via: (l) => ` via ${l}`,
      reqLog: "Request log per screen", requests: (n) => `${n} requests`, revisit: " (revisit)", forms: "forms: ", links: "links: ", noReq: "(no API requests)", collapsed: "collapsed repeats: ",
      reqCols: ["t(ms)", "method", "path", "status", "ms", "response schema", "request", "response"], crawlPrefix: "crawl: ", step: "step: ",
      scanLine: (fr) => `scripts: ${(fr.scripts ?? []).length} · setInterval: ${fr.setIntervalCount ?? 0}${fr.intervalLiterals?.length ? ` (${fr.intervalLiterals.join(", ")} ms)` : ""} · setTimeout literals: ${(fr.timeoutLiterals ?? []).join(", ") || "-"} · EventSource: ${fr.hasEventSource ? "yes" : "no"} · WebSocket: ${fr.hasWebSocket ? "yes" : "no"}`,
      unobservedOps: (a, b) => `OpenAPI operations not observed (${a} / ${b})`, opCols: ["actor", "operation", "why it matters", "handler → tables"],
      opNote: { internal: "internal path, never called by the frontend → likely a server-side batch / cron / systemd timer. Find who calls it, how often and how many items per call.", initialize: "called once by the benchmarker before each run (reset data). Has a time limit; keep it fast if you add precomputation.", register: "registration; probably called by the benchmarker during setup / user growth.", write: "write API not reached by the crawl; exercise it with a flow if it matters.", read: "not reached by the crawl (behind a click or a state the crawl did not produce)." },
      refsNotObserved: (n) => `Referenced in frontend code but not observed (${n})`, allRefs: (n) => `All endpoints referenced in code (${n})`, pollingPerScreen: "Polling / repeated calls (per screen)", pollCols: ["screen", "endpoint", "count", "median gap ms", "min gap ms", "server-directed field"], none: "none",
      codeDirTip: ["Tip: set ", " in the config (path to the Go reference implementation) to get handler names, tables and N+1 suspects next to every endpoint."],
      roles: {},
      purpose: { inputs: "inputs: ", actions: "actions: ", apis: "APIs: ", none: "no API calls" },
      flagsHead: "behaviour facts from code (external calls, locks, waits)", sqlHead: (n) => `SQL statements (${n})`, viaHelper: (v) => `via ${v}`, metrics: (c) => `${c.loc} LOC · ${c.branches} branches · ${c.loops} loops · ${c.sql} SQL${c.helpers?.length ? ` · helpers: ${c.helpers.join(", ")}` : ""}`,
      flag: { loop: "SQL inside a loop (N+1)", tx: "transaction", lock: "row lock (FOR UPDATE/SHARE)", external: "external HTTP call", retry: "retry loop", sleep: "time.Sleep", sortInApp: "sorts in app code", rand: "ORDER BY RAND()", window: "window function (OVER)", noWhere: "SELECT without WHERE (full table)", selectStar: "SELECT *", subquery: "subquery", noIndexWhere: "WHERE column has no index", noIndexOrder: "ORDER BY column has no index", latestRow: "latest-row query (ORDER BY … LIMIT 1)", count: "COUNT()" },
      epSummary: (e, c) => `${e.actor}-facing ${e.role}${c ? ` · handler ${c.handler}: ${c.sql} SQL (${c.statements.filter((x) => x.inLoop).length} in loop)${c.tx ? ", transaction" : ""}${c.external ? ", external call" : ""}` : ""}`,
      finding: {
        polling: (p) => [`${p.n} polling endpoint(s) = ${p.pct}% of observed requests`, `${p.list}${p.hints ? `. Server-directed interval field(s) in the response: ${p.hints} → raising the value or switching to SSE changes client behaviour.` : ""}${p.sse ? " The frontend bundle already contains EventSource code." : ""}`],
        slow: (p) => [`${p.n} endpoint(s) slow even without load (≥300ms)`, p.list],
        internal: (p) => [`${p.n} internal endpoint(s) never called by the UI — server-side batch candidates`, `${p.list}. Who calls it, how often, how many items per call? This often bounds throughput.`],
        loop: (p) => [`${p.n} handler(s) run SQL inside a loop (N+1 suspects)`, p.list],
        repeatedWrites: (p) => [`Repeated write(s) from clients: ${p.list}`, `${p.detail}. Under the bench this is the main write stream; consider batching / async writes.`],
        writeHot: (p) => [`Write hot path: ${p.ep} (${p.count} calls, median ${p.ms}ms)`, `${p.role}${p.code ? `; ${p.code}` : ""}. Other writes: ${p.others}`],
        stateMachine: (p) => [`State machine observed in ${p.label}: ${p.seq}`, p.transitions],
        external: (p) => [`${p.n} handler(s) call an external HTTP service`, `${p.list}. Latency and retries there are outside DB tuning.`],
        errors: (p) => [`${p.app} app-origin error(s); ${p.art} crawl artefact(s) (missing cookie, re-run duplicates, aborted)`, p.leaks ? `DB error text leaks to clients on: ${p.leaks} — handlers are thin; expect little validation around writes.` : ""],
      },
    },
    ja: {
      tabs: { now: "今解決すべきこと", overview: "概要", map: "画面マップ", flows: "ベンチの流れ", storyboard: "ストーリーボード", endpoints: "エンドポイント", data: "データモデル", rules: "ルール", hosts: "ホストの割り当て", screens: "画面", scan: "コード走査 / 性能ヒント" },
      dataHead: "テーブル、推定した関連（<x>_id → x）、行数", dataRows: (src) => (src ? `行数は ${src}` : "行数: ベンチ後に `task db:snapshot LABEL=latest` を実行して report を再生成すると入る"), dataCols: ["テーブル", "行数", "カラム", "index", "参照先"], erHint: "箱 = テーブル（PK は太字、(i) = index の先頭カラム）、矢印 = <x>_id カラム → テーブル x。カラムに hover で型。",
      perfHints: "性能ヒント — 巡回時点のコードから", perfNote: "巡回した時点のコードを読んだ結果。正は bench の report（summary.md）。最初の改善ループの後は大半が解消しているはず。",
      notice: "ベンチマーカーはブラウザを使わず API を直接叩く。ここにある回数はすべてブラウザ UI の挙動であり、ベンチの挙動ではない。このページはサービス理解（アクター、画面、状態遷移、どの API が何をするか）に使い、実際のリクエスト比率は初回ベンチ後にアクセスログ（alp）で確認すること。",
      readFirst: "最初に読む: ",
      serviceSummary: "サービス要約（自動）", actor: "アクター", screens: "画面", endpointsRW: "API 数（読/書）", polling: "ポーリング", resources: "パスに現れるリソース", noUI: "UI なし（API のみ）",
      stateMachine: (l) => `状態遷移（${l}）`, triggerNote: "引き金 = 状態が変わる直前 5 秒以内の、ポーリング以外のクライアント呼び出し。", storyboardLink: "ストーリーボード →",
      topFindings: "主な所見（ここから読む）", numbers: "数値",
      system: "システム（ホストで動いているもの、データの置き場所、コード規模）", sysComponents: "構成要素（bootstrap が取り込んだ systemd ユニット）", sysWeb: "Web（nginx）", sysStorage: "ストレージ", sysCode: "コード規模", sysNotes: "server-info", sysEnv: "env.sh のキー", sysNone: "repo に etc/ がまだ無い（bootstrap 後に git pull）",
      sysCols: { unit: "ユニット", exec: "ExecStart", dir: "WorkingDirectory", deps: "依存", server: "server_name", listen: "listen", root: "root", locations: "location" },
      sysTiles: { files: "Go ファイル", lines: "行", funcs: "関数", routes: "ルート", sql: "SQL リテラル", tables: "テーブル", columns: "カラム", indexes: "セカンダリ index" }, biggest: "大きいファイル",
      tiles: { screens: "画面（ルート）", flows: "フロー", endpoints: "観測したエンドポイント", polling: "ポーリング API", errors: (a) => `アプリ起因のエラー（クロール起因 ${a}）`, openapi: "未観測の OpenAPI 操作" },
      matrix: "画面 × エンドポイント行列（画面ごとの回数。アクター別にグループ化。赤い上線 = 書き込み。セルをクリックで画面へ）", matrixHead: "画面 \\ エンドポイント",
      chipLegend: "チップの色: ", legend: { poll: "ポーリング", slow: "300ms 以上", err: "アプリ起因のエラー", soft: "クロール起因" },
      mapHint: "画面をクリックすると API 一覧と大きいスクショを表示。ホバーでリンクを強調。ドラッグで移動、ホイールまたはボタンで拡大縮小。",
      mapLegend: "実線 = ページ内のリンク、橙の点線 = リダイレクト。ボタンやフォームは辿らない（ストーリーボード / flow を参照）。トップからリンクで到達できない画面（ログインボタンの先など）は右側に島として置く。列 ≈ トップからのクリック数。",
      openDetails: "詳細を開く →", noApi: "この画面は API を呼ばない", endpoint: "エンドポイント", count: "回数", medianMs: "中央値 ms", role: "役割", buttons: "ボタン: ", fit: "全体",
      noFlows: "flow が記録されていない。設定の persona.flows にボタン操作の導線を書くと記録できる。", steps: (n) => `${n} ステップ · `, openLog: "リクエストログを開く →",
      transitionsHead: "状態遷移と、それを起こしたクライアント呼び出し（バッジ = 呼び出したアクター）:", serverSide: "クライアント呼び出しなし（サーバー側）",
      timelineHead: (k) => `レスポンス JSON に現れた ${k} の値の推移（数字 = ステップ番号）:`,
      frameNote: "フレーム = 各ステップ開始時の画面状態（そのステップ中に状態が変わったものを強調）。フレームの後の太字チップ = そのステップ中のポーリング以外の呼び出し、つまり次のフレームを引き起こしたもの。ポーリングは折りたたみ。",
      noNonPolling: "（ポーリング以外の呼び出しなし）", pollingFold: (n) => `+ ポーリング (${n})`,
      filter: "絞り込み（パス、メソッド、アクター、役割、画面、ハンドラ、テーブル）", presets: "プリセット: ", all: "すべて", nEndpoints: (n) => `${n} エンドポイント`, preset: (p) => ` · プリセット: ${p}`,
      cols: { actor: "アクター", role: "役割", method: "メソッド", path: "パス", count: "回数", medianMs: "中央値 ms", statuses: "ステータス", screens: "画面数" },
      write: "（書き込み）", appErrors: (n) => ` アプリ起因 ${n} 件`, artefacts: (n) => ` クロール起因 ${n} 件`, serverDirected: "サーバーがクライアントの挙動を指示するフィールド",
      handlerTables: "ハンドラ → テーブル", calledFrom: "呼び出し元", schemaHead: (st) => `レスポンスの構造（status ${st}）`, noRoute: "対応するルートなし", setCodeDir: "設定に codeDir を指定すると表示", noJson: "（JSON 本文なし）",
      request: (st) => `リクエスト (${st})`, response: (st) => `レスポンス (${st})`, tables: "テーブル: ", sql: (n) => `SQL ${n} 本`, loopSql: "ループ内 SQL（N+1 疑い）", locks: "行ロック", external: "外部 HTTP 呼び出し", via: (l) => `（${l} 経由）`,
      reqLog: "画面ごとのリクエストログ", requests: (n) => `${n} リクエスト`, revisit: "（再訪）", forms: "フォーム: ", links: "リンク: ", noReq: "（API リクエストなし）", collapsed: "繰り返しを省略: ",
      reqCols: ["t(ms)", "メソッド", "パス", "status", "ms", "レスポンス構造", "リクエスト", "レスポンス"], crawlPrefix: "クロール起因: ", step: "ステップ: ",
      scanLine: (fr) => `スクリプト: ${(fr.scripts ?? []).length} · setInterval: ${fr.setIntervalCount ?? 0}${fr.intervalLiterals?.length ? `（${fr.intervalLiterals.join(", ")} ms）` : ""} · setTimeout のリテラル: ${(fr.timeoutLiterals ?? []).join(", ") || "-"} · EventSource: ${fr.hasEventSource ? "あり" : "なし"} · WebSocket: ${fr.hasWebSocket ? "あり" : "なし"}`,
      unobservedOps: (a, b) => `画面から観測できなかった OpenAPI 操作（${a} / ${b}）`, opCols: ["アクター", "操作", "なぜ重要か", "ハンドラ → テーブル"],
      opNote: { internal: "internal パスでフロントからは呼ばれない → サーバー側のバッチ / cron / systemd timer の可能性が高い。誰が・何秒ごとに・1 回に何件処理するかを調べる。", initialize: "ベンチが各走行の前に 1 回呼ぶ（データ初期化）。制限時間があるので、事前計算を足すなら速く保つ。", register: "登録系。ベンチがセットアップやユーザー増加の過程で呼ぶ可能性が高い。", write: "クロールでは到達しなかった書き込み API。重要なら flow で叩く。", read: "クロールでは到達しなかった（クリックの先か、クロールが作らなかった状態）。" },
      refsNotObserved: (n) => `フロントのコードにあるが観測されなかった（${n}）`, allRefs: (n) => `コード中で参照されている全エンドポイント（${n}）`, pollingPerScreen: "ポーリング / 繰り返し呼び出し（画面ごと）", pollCols: ["画面", "エンドポイント", "回数", "間隔の中央値 ms", "最小間隔 ms", "サーバー指示フィールド"], none: "なし",
      codeDirTip: ["ヒント: 設定に ", "（Go 参考実装のパス）を指定すると、各エンドポイントにハンドラ名・テーブル・N+1 疑いが付く。"],
      purpose: { inputs: "入力: ", actions: "操作: ", apis: "API: ", none: "API 呼び出しなし" },
      flagsHead: "コードから読み取った挙動（外部呼び出し・ロック・待ち）", sqlHead: (n) => `SQL 文（${n}）`, viaHelper: (v) => `${v} 経由`, metrics: (c) => `${c.loc} 行 · 分岐 ${c.branches} · ループ ${c.loops} · SQL ${c.sql} 本${c.helpers?.length ? ` · 補助関数: ${c.helpers.join(", ")}` : ""}`,
      flag: { loop: "ループ内 SQL（N+1）", tx: "トランザクション", lock: "行ロック（FOR UPDATE/SHARE）", external: "外部 HTTP 呼び出し", retry: "リトライループ", sleep: "time.Sleep", sortInApp: "アプリ側でソート", rand: "ORDER BY RAND()", window: "ウィンドウ関数（OVER）", noWhere: "WHERE なしの SELECT（全件）", selectStar: "SELECT *", subquery: "サブクエリ", noIndexWhere: "WHERE 列に index なし", noIndexOrder: "ORDER BY 列に index なし", latestRow: "最新 1 行取得（ORDER BY … LIMIT 1）", count: "COUNT()" },
      epSummary: (e, c) => `${e.actor} 向けの${role(e.role)}${c ? ` · ハンドラ ${c.handler}: SQL ${c.sql} 本（ループ内 ${c.statements.filter((x) => x.inLoop).length}）${c.tx ? "、トランザクション" : ""}${c.external ? "、外部呼び出し" : ""}` : ""}`,
      roles: { polling: "ポーリング", notification: "通知", internal: "内部", initialize: "初期化", "state transition": "状態遷移", "position report": "位置報告", evaluate: "評価", "register/login": "登録/ログイン", "calc (read)": "計算（読み）", write: "書き込み", "aggregate (range)": "集計（期間）", aggregate: "集計", "list/search": "一覧/検索", read: "読み取り" },
      finding: {
        polling: (p) => [`ポーリング API ${p.n} 本が観測リクエストの ${p.pct}%`, `${p.list}${p.hints ? `。レスポンスにサーバー指示の間隔フィールドあり: ${p.hints} → 値を上げるか SSE に切り替えるとクライアントの挙動が変わる。` : ""}${p.sse ? " フロントのバンドルには EventSource のコードが既に入っている。" : ""}`],
        slow: (p) => [`無負荷でも遅いエンドポイント ${p.n} 本（300ms 以上）`, p.list],
        internal: (p) => [`UI から呼ばれない internal エンドポイント ${p.n} 本 — サーバー側バッチの候補`, `${p.list}。誰が、何秒ごとに、1 回に何件処理するか？ ここがスループットの上限になりやすい。`],
        loop: (p) => [`ループ内で SQL を実行するハンドラ ${p.n} 本（N+1 疑い）`, p.list],
        repeatedWrites: (p) => [`クライアントからの繰り返し書き込み: ${p.list}`, `${p.detail}。ベンチ下ではこれが主要な書き込みストリーム。バッチ化 / 非同期化を検討。`],
        writeHot: (p) => [`書き込みのホットパス: ${p.ep}（${p.count} 回、中央値 ${p.ms}ms）`, `${role(p.role)}${p.code ? `; ${p.code}` : ""}。他の書き込み: ${p.others}`],
        stateMachine: (p) => [`${p.label} で観測した状態遷移: ${p.seq}`, p.transitions],
        external: (p) => [`外部 HTTP サービスを呼ぶハンドラ ${p.n} 本`, `${p.list}。そこのレイテンシとリトライは DB チューニングの外側。`],
        errors: (p) => [`アプリ起因のエラー ${p.app} 件、クロール起因 ${p.art} 件（Cookie なし・再実行の重複・中断）`, p.leaks ? `DB のエラー文がクライアントに漏れている: ${p.leaks} — ハンドラが薄く、書き込み周りの検証は少ないと見込む。` : ""],
      },
    },
  };
  let lang = localStorage.getItem("isucon-report-lang") || (navigator.language?.startsWith("ja") ? "ja" : "en");
  const t = (k) => T[lang][k] ?? T.en[k];
  // strings for the bench overlay (Now / flows / rules, bench columns)
  const B2 = {
    en: {
      noBench: "No bench run attached. After a bench: task app:refresh TS=<ts> (pulls the run and re-renders this page in ~1 s, no crawl).",
      nowHead: (ts, prev) => `What to solve now — bench run ${ts}${prev ? ` (vs ${prev})` : ""}`,
      nowNote: "Open problems only, ranked by share of the bench's time × relation to the score (score POST ×3, its loop ×2); regressions and P1 first. Things the measurement shows as fixed are folded at the bottom.",
      nextStep: "Next step (report rules)", cols: ["#", "what", "share", "p99 ms", "score", "how to fix", "to do", "code"], more: (n) => `+${n} more open items (Endpoints tab, sorted by share)`,
      errLine: (n, all, m) => `5xx ${n} / ${all} requests — ${m}`, resolved: (n) => `Resolved ${n} ▸`, regression: (a, b) => `regression ${a}→${b} ms`, rule: "rule", score: "score POST", loop: "score loop",
      how: { "parallel-fix": "parallel-fix", design: "design first — assign an owner", investigate: "investigate — deep-dive" },
      howNote: "parallel-fix = mechanical (an index, SQL in a known file): one subagent per unit. design = needs an owner and a plan (caching, polling, external commands, structure). investigate = the cause is not known yet.",
      opsReport: "ops report", flowsHead: "Bench flows (推測, from the access log)", role: "role", allRoles: "all",
      loops: "Score loops (one client's steps from a score POST to the next) — a step is marked red only when it stands out", slowest: "slowest — stands out", maxStep: "(max, not unusual)", times: (n, s) => `${n} times · cycle median ${s} s`,
      trans: "Transitions", transCols: ["from", "to", "count", "share"],
      drops: (e, c) => `Drop-off: ${e} of ${c} clients stopped > 10 s before the end`, dropCols: ["last step", "status", "reason", "clients", "stops here", "last ms", "endpoint median ms"],
      dropDef: "Definition: a client (session cookie) whose last request ended more than 10 s before the run's last request — normal ends are included. Reasons (推測): error = its last response was 4xx/5xx · slow = the last response took > 2× the endpoint's median (≥ 200 ms), the bench may have given up · end = most clients that reach this step stop there (the scenario's natural end) · unknown = none of these (a time limit, a fixed-length scenario, or a cookie change at login).",
      reasons: { error: "error", slow: "slow", end: "normal end", unknown: "unknown (stopped midway)" }, isNew: "new", grew: (n) => `was ${n}`,
      roleNote: "Role = the bench's clients grouped by what they write most (their two most frequent POST/PUT/DELETE endpoints), or by cookie name when the app uses several. Name them in docs/app-annotations.json → roles.", readOnly: "browsing only (no writes)", otherRole: "other (small groups)",
      waitWarn: (p, st, s) => `${p}% of a cycle is waiting between steps (longest: ${s} s after ${st}). Faster responses do not shorten this part; it is usually the scenario itself (leaving and entering another stream, the bench's think time)`,
      resp: "response", wait: "wait until next", median: "median", deep: "dig deeper", cycleDist: (a, b, c, d) => `cycle p10 ${a} s · median ${b} s · p90 ${c} s · max ${d} s`, deepCmd: "Sankey from this step / per-client sequence (deep-dive Q-scenario):", endShare: "stops here", unknownNote: "Many 'unknown' = the clients stopped at a step where most others continue (< 50% end there). Check 'stops here': a step with 20–50% may still be a normal end; a step with a few % in a score loop is a real drop (look at the role and the step's p99).",
      internalHead: "Periodic internal endpoints (measured in the bench)", internalNote: "A job that runs every N ms and handles K items per call caps the score at K/N items per second, however fast each call is. Process more per call or run it more often.", internalCols: ["endpoint", "host", "calls", "median interval ms", "p90 interval ms", "per call ms (avg)", "per call ms (p99)", "writes per call"],
      sankey: "Sankey diagram (open locally)", noFlows: "No flows in this run (no session cookies, or the run has no report.json).",
      rulesHead: "Rules from the manual (docs/app-annotations.json) — what every change must keep", rulesCols: ["rule", "source", "endpoints", "tables"], noRules: "No rules yet: the app-analysis skill writes docs/app-annotations.json (rules + screen/endpoint purposes) at the start of the day.",
      benchCols: ["share", "p99", "bench req"], dataCards: "Tables", dataCardCols: ["table", "rows", "Δ/s in the bench", "written by", "read by", "indexes"], matrixHead: "Endpoint × table (R = reads, W = writes; colour = bench share)", erAll: "Show the full ER diagram", erFocus: (t) => `ER around ${t}`, erHint2: "Click a table name to see it with the tables it links to.", backCards: "← tables", backAll: "full ER",
      weight: (p) => `${p}`, contract: "Contract (keep identical)", measuredSql: "SQL measured in the bench (code line)", staticFacts: "Code facts at crawl time (may be outdated)",
      noPurpose: "Purpose not written yet (docs/app-annotations.json → screens).", autoSummary: "Auto summary from the crawl", scenario: "Scenario (role in the bench, 推測)", none: "none",
      filters: { method: "method", actor: "actor", heavy: "≥ 3% of the bench", write: "writes", unseen: "not seen by the crawl", all: "all" }, unseen: "not seen by the crawl", screensHead: (n) => `${n} screens (click to open)`,
    },
    ja: {
      noBench: "ベンチの run が付いていません。ベンチの後に task app:refresh TS=<ts>（run を取ってきて、巡回なしで約 1 秒で描き直す）。",
      nowHead: (ts, prev) => `今解決すべきこと — ベンチ run ${ts}${prev ? `（前回 ${prev} と比較）` : ""}`,
      nowNote: "未解決のものだけ。ベンチの時間の割合 × 点との関係（スコアの POST ×3、そのループ ×2）の順で、悪化と P1 を先に。計測で直ったと分かったものは下に畳む。",
      nextStep: "次の手順（report のルール判定）", cols: ["#", "対象", "割合", "p99 ms", "点", "直し方", "やること", "コード"], more: (n) => `ほかに未解決 ${n} 件（エンドポイントのタブ、割合順）`,
      errLine: (n, all, m) => `5xx ${n} 件 / ${all} 件中 — ${m}`, resolved: (n) => `解決済み ${n} 件 ▸`, regression: (a, b) => `悪化 ${a}→${b} ms`, rule: "ルール", score: "スコアの POST", loop: "スコアのループ",
      how: { "parallel-fix": "parallel-fix で直る", design: "設計から（担当を決める）", investigate: "要調査（deep-dive）" },
      howNote: "parallel-fix で直る = 機械的に直せる（index、直すファイルが分かっている SQL）。作業単位ごとにサブエージェント 1 つ。設計から = 担当を決めて方針を立てる（キャッシュ、ポーリング、外部コマンド、構成）。要調査 = 原因がまだ分からない。",
      opsReport: "ops の report", flowsHead: "ベンチの流れ（推測。アクセスログから）", role: "役割", allRoles: "全体",
      loops: "スコアのループ（1 クライアントがスコアの POST から次の POST までに踏むステップ）— 突出して遅いステップだけ赤で示す", slowest: "一番遅い（突出）", maxStep: "（最大。ほかと差は小さい）", times: (n, s) => `${n} 回 · 1 周の中央値 ${s} 秒`,
      trans: "遷移", transCols: ["from", "to", "件数", "割合"],
      drops: (e, c) => `離脱: ${c} クライアント中 ${e} が終了 10 秒より前に止まった`, dropCols: ["最後のステップ", "status", "理由", "クライアント数", "ここで終わる割合", "最後の応答 ms", "endpoint の中央値 ms"],
      dropDef: "定義: 最後のリクエストが、run 全体の最後のリクエストより 10 秒以上前に終わったクライアント（セッション Cookie 単位）。正常に終わったものも含む。理由（推測）: エラー = 最後の応答が 4xx/5xx · 遅延 = 最後の応答がその endpoint の中央値の 2 倍超（200 ms 以上）で、ベンチが諦めた可能性 · 正常終了らしい = このステップに来たクライアントの過半がここで終わる（シナリオの自然な終わり）· 不明 = どれにも当たらない（時間制限、決まった長さのシナリオ、ログインで Cookie が変わった など）。",
      reasons: { error: "エラー", slow: "遅延", end: "正常終了らしい", unknown: "不明（途中で止まった）" }, isNew: "新", grew: (n) => `前回 ${n}`,
      roleNote: "役割 = ベンチのクライアント（セッション Cookie）を、よく書き込む endpoint（POST/PUT/DELETE の上位 2 つ）の組み合わせで分けたもの（推測）。Cookie 名が複数あるアプリでは Cookie 名で分ける。名前は docs/app-annotations.json の roles で付けられる。", readOnly: "閲覧のみ（書き込みなし）", otherRole: "その他（少数の組み合わせ）",
      waitWarn: (p, st, s) => `1 周の ${p}% はステップ間の待ち（最長: ${st} の後 ${s} 秒）。この部分は応答を速くしても縮まない。多くはシナリオそのもの（退出して別の配信に入る、ベンチの待機）`,
      resp: "応答", wait: "次までの待ち", median: "中央値", deep: "深掘り", cycleDist: (a, b, c, d) => `1 周 p10 ${a} 秒 · 中央値 ${b} 秒 · p90 ${c} 秒 · 最大 ${d} 秒`, deepCmd: "このステップから始まる Sankey / クライアントごとの並び（deep-dive の Q-scenario）:", endShare: "ここで終わる割合", unknownNote: "「不明」が多いのは、ほかのクライアントの過半が先に進むステップで止まったもの（ここで終わる割合が 5 割未満）。「ここで終わる割合」を見る: 2〜5 割なら正常な終わり方の 1 つかもしれない。スコアのループの途中で数 % しか終わらないステップなら本当の離脱（その役割と、ステップの p99 を見る）。",
      internalHead: "定期実行の内部 endpoint（ベンチ中の実測）", internalNote: "N ms ごとに動き 1 回 K 件を処理するジョブは、1 回をどれだけ速くしても毎秒 K/N 件が上限。1 回で多く処理するか、もっと頻繁に動かす。", internalCols: ["endpoint", "host", "呼び出し数", "間隔 中央値 ms", "間隔 p90 ms", "1 回 ms（平均）", "1 回 ms（p99）", "1 回の書き込み数"],
      sankey: "Sankey 図（手元で開く）", noFlows: "この run には流れのデータがありません（セッション Cookie が無い、または report.json が無い）。",
      rulesHead: "マニュアルのルール（docs/app-annotations.json）— どの変更でも守ること", rulesCols: ["ルール", "出典", "endpoint", "テーブル"], noRules: "ルールはまだありません。app-analysis skill が開始時に docs/app-annotations.json（ルール、画面と endpoint の役割）を書きます。",
      benchCols: ["割合", "p99", "ベンチの件数"], dataCards: "テーブル", dataCardCols: ["テーブル", "行数", "ベンチ中の Δ/s", "書く endpoint", "読む endpoint", "index"], matrixHead: "endpoint × テーブル（R = 読む、W = 書く。色 = ベンチの割合）", erAll: "ER 図を全部表示", erFocus: (t) => `${t} の周辺の ER`, erHint2: "テーブル名をクリックすると、そのテーブルと直接つながるテーブルだけを表示します。", backCards: "← テーブルの表", backAll: "ER 全体",
      weight: (p) => `${p}`, contract: "壊してはいけない仕様", measuredSql: "ベンチで計測した SQL（コードの行）", staticFacts: "巡回時のコードの所見（古い可能性あり）",
      noPurpose: "役割が未記入です（docs/app-annotations.json の screens に書く）。", autoSummary: "巡回からの自動要約", scenario: "シナリオ（ベンチでの役割。推測）", none: "なし",
      filters: { method: "method", actor: "アクター", heavy: "ベンチの 3% 以上", write: "書き込み", unseen: "巡回で未観測", all: "すべて" }, unseen: "巡回で未観測", screensHead: (n) => `${n} 画面（クリックで開く）`,
    },
  };
  const b2 = (k) => B2[lang][k] ?? B2.en[k];
  const pk = (k) => String(k ?? "").replace(/:\w+/g, ":id");
  // screen annotations: exact path, else with ids normalized (/watch/1 and /watch/:id are the same screen)
  const normScreen = (p) => String(p ?? "").split("/").map((x) => (/^\d+$|^:\w+$/.test(x) ? ":id" : x)).join("/");
  const annScreen = (p) => { const A = M.annotations?.screens ?? {}; if (A[p]) return A[p]; const n = normScreen(p); for (const [k, v] of Object.entries(A)) if (normScreen(k) === n) return v; return null; };
  const pctS = (x) => (x == null ? "" : `${(100 * x).toFixed(x < 0.1 ? 1 : 0)}%`);
  const benchBy = Object.fromEntries((M.bench?.endpoints ?? []).map((e) => [e.key, e]));
  const codeLinks = (q) => (q.code?.length ? q.code.slice(0, 3).map((c) => (c.url ? h("a", { href: c.url, target: "_blank" }, h("code", {}, c.loc + (c.n ? ` ×${c.n}` : ""))) : h("code", {}, c.loc))) : q.grep ? [h("code", { class: "muted" }, `git grep -n "${q.grep}"`)] : []);
  const role = (r) => (T[lang].roles?.[r] ?? r);

  const median = (a) => (a.length ? [...a].sort((x, y) => x - y)[Math.floor(a.length / 2)] : null);
  const fmtQ = (q) => (q ? Object.entries(q).map(([k, v]) => `${k}=${v}`).join("&") : "");
  const screenById = Object.fromEntries(M.screens.map((s) => [s.id, s]));
  const epByKey = Object.fromEntries(M.endpoints.map((e) => [e.key, e]));
  const apiReqOf = (s) => s.requests.filter((r) => r.type !== "document" || r.path.startsWith(M.apiPrefix));
  const epKey = (r) => `${r.method} ${r.origin || ""}${r.normPath}`;
  const shortPath = (e) => e.path.replace(M.apiPrefix, "").replace(new RegExp("^" + e.actor + "/"), "");
  const ACTOR_ORDER = M.actors.map((a) => a.actor);

  // ---------- shared widgets ----------
  const chip = (text, cls = "", title = "", onclick) => h("span", { class: `chip ${cls}`, title: title || text, onclick }, text);
  const actorBadge = (a) => h("span", { class: "badge actor" }, a);
  const chipsFor = (s, opts = {}) => {
    const m = new Map();
    for (const r of apiReqOf(s)) {
      const k = epKey(r);
      const e = m.get(k) ?? m.set(k, { n: 0, app: 0, art: 0, ms: [] }).get(k);
      e.n++;
      if (r.errClass === "app") e.app++; else if (r.errClass) e.art++;
      if (r.ms != null) e.ms.push(r.ms);
    }
    const poll = new Set((s.polling ?? []).map((p) => p.endpoint));
    return [...m].sort((a, b) => b[1].n - a[1].n).map(([k, e]) => {
      const med = median(e.ms);
      const cls = e.app ? "err" : e.art && e.art === e.n ? "soft" : poll.has(k) ? "poll" : med != null && med >= 300 ? "slow" : "";
      const title = `${k} ×${e.n}${med != null ? ` · median ${med}ms` : ""}${e.app ? t("appErrors")(e.app) : ""}${e.art ? t("artefacts")(e.art) : ""}`;
      return chip(`${k}${e.n > 1 ? ` ×${e.n}` : ""}${med != null && med >= 300 ? ` ${med}ms` : ""}`, cls + (opts.link ? " link" : ""), title, opts.link ? () => show("endpoints", k) : undefined);
    });
  };
  const lightbox = (src, caption) => { const ov = h("div", { class: "lightbox", onclick: () => ov.remove() }, h("img", { src }), h("div", { class: "cap" }, caption || "")); document.body.append(ov); };
  const shot = (s, cls = "") => (s.screenshot ? h("img", { class: `shot ${cls}`, src: s.screenshot, loading: "lazy", onclick: (e) => { e.stopPropagation(); lightbox(s.screenshot, s.label || s.path); } }) : h("div", { class: "noshot" }, "—"));
  const pre = (text) => h("pre", {}, text);
  const detailsRow = (label, text) => (text ? h("details", { class: "row" }, h("summary", {}, label), pre(text)) : null);
  const codeInfo = (c) => (c ? h("div", { class: "muted" }, h("code", {}, c.handler), ` ${c.file}:${c.line}`, c.tables.length ? h("div", {}, t("tables"), ...c.tables.map((x) => chip(x))) : null, h("div", {}, t("sql")(c.sql), c.loopSql ? chip(t("loopSql"), "err") : null, c.locks ? chip(t("locks"), "poll") : null, c.external ? chip(t("external"), "slow") : null, (c.helpers ?? []).length ? t("via")(c.helpers.join(", ")) : null)) : null);
  const purposeText = (s) => {
    const p = s.purpose; if (!p) return "";
    const P = t("purpose");
    const head = p.desc ?? p.h1 ?? p.metaTitle ?? s.title ?? "";
    const parts = [head];
    if (p.h2?.length) parts.push(p.h2.join(" / "));
    if (p.inputs?.length) parts.push(P.inputs + p.inputs.join(", "));
    if (p.buttons?.length) parts.push(P.actions + p.buttons.join(" · "));
    const apis = (p.apis ?? []).map((a) => `${role(a.role)} ${a.key.replace(/^GET /, "")}`);
    parts.push(apis.length ? P.apis + apis.join("; ") : P.none);
    return parts.filter(Boolean).join("。 ");
  };
  // only behaviour facts are shown on the Endpoints tab (performance hints live on the scan tab and go stale fast)
  const BEHAVIOUR_FLAGS = new Set(["external", "exec", "retry", "sleep", "lock", "tx"]);
  const behaviourFlag = (f) => BEHAVIOUR_FLAGS.has(f);
  const flagChips = (flags) => (flags ?? []).filter(behaviourFlag).map((f) => chip(t("flag")[f] ?? f, /lock|external|exec/.test(f) ? "poll" : "soft"));
  const sqlList = (c) => (c?.statements?.length ? h("details", { class: "row" }, h("summary", {}, t("sqlHead")(c.statements.length)), h("ul", { class: "sqls" }, ...c.statements.map((st) => h("li", { class: st.inLoop ? "loop" : "" }, st.inLoop ? h("span", { class: "badge state" }, "loop") : null, ...st.flags.filter(behaviourFlag).filter((f) => f !== "loop").map((f) => h("span", { class: `badge ${/noIndex|noWhere|rand|window/.test(f) ? "warn" : ""}` }, t("flag")[f] ?? f)), h("code", {}, st.sql), st.via ? h("span", { class: "muted" }, " " + t("viaHelper")(st.via)) : null)))) : null);
  const findingText = (f) => { const fn = T[lang].finding?.[f.key] ?? T.en.finding[f.key]; return fn && f.params ? fn(f.params) : [f.title, f.detail]; };

  const requestTable = (s) => {
    const reqs = apiReqOf(s);
    if (!reqs.length) return h("div", { class: "muted" }, t("noReq"));
    const stepsAt = new Map((s.steps ?? []).map((st) => [st.t, st.label]));
    const seen = new Map();
    const rows = [];
    s.requests.forEach((r, idx) => {
      if (stepsAt.has(idx)) rows.push(h("tr", { class: "step" }, h("td", { colspan: 8 }, h("strong", {}, t("step")), stepsAt.get(idx))));
      if (!(r.type !== "document" || r.path.startsWith(M.apiPrefix))) return;
      const k = `${r.method} ${r.normPath}`;
      const n = (seen.get(k) ?? 0) + 1;
      seen.set(k, n);
      if (n > 3) return;
      rows.push(h("tr", {},
        h("td", { class: "num" }, r.t), h("td", { class: "nw" }, h("code", {}, r.method)),
        h("td", { class: "nw" }, h("code", {}, (r.origin || "") + r.path), r.query ? h("div", { class: "muted" }, "?" + fmtQ(r.query)) : null),
        h("td", { class: `num ${r.errClass === "app" ? "err" : ""}` }, r.status ?? r.failed ?? "", r.errClass && r.errClass !== "app" ? h("div", { class: "muted" }, t("crawlPrefix") + r.errClass.replace("artefact:", "")) : null),
        h("td", { class: "num" }, r.ms ?? ""), h("td", {}, r.resSchema ? h("div", { class: "schema" }, r.resSchema) : null),
        h("td", {}, detailsRow(t("request")(r.status ?? ""), r.reqBody)), h("td", {}, detailsRow(t("response")(r.status ?? ""), r.resBody)),
      ));
    });
    const collapsed = [...seen].filter(([, n]) => n > 3);
    return h("div", {}, h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...t("reqCols").map((x) => h("th", { class: "nw" }, x))), ...rows)), collapsed.length ? h("div", { class: "muted" }, t("collapsed") + collapsed.map(([k, n]) => `${k} ×${n}`).join(", ")) : null);
  };

  const timeline = (s, width = 600) => {
    const reqs = apiReqOf(s);
    if (!reqs.length) return null;
    const lanes = [...new Set(reqs.map(epKey))];
    const tmax = Math.max(...reqs.map((r) => r.t), 1);
    const laneH = 14, left = 4, labelW = 260;
    const ns = (tag, attrs) => { const e = document.createElementNS("http://www.w3.org/2000/svg", tag); for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v); return e; };
    const svg = ns("svg", { width, height: lanes.length * laneH + 18, class: "tl" });
    lanes.forEach((lane, i) => {
      const y = i * laneH + 10;
      const tx = ns("text", { x: left, y: y + 4, "font-size": 10, fill: "#374151" });
      tx.textContent = lane.length > 42 ? lane.slice(0, 40) + "…" : lane;
      svg.append(tx, ns("line", { x1: labelW, x2: width - 4, y1: y, y2: y, stroke: "#e5e7eb" }));
      for (const r of reqs.filter((r) => epKey(r) === lane)) {
        const x = labelW + ((width - 8 - labelW) * r.t) / tmax;
        const c = ns("circle", { cx: x, cy: y, r: 2.5, fill: r.errClass === "app" ? "#b91c1c" : r.errClass ? "#9ca3af" : "#0f766e" });
        const tt = ns("title", {});
        tt.textContent = `${r.t}ms ${lane} → ${r.status ?? r.failed ?? "?"}${r.ms != null ? ` (${r.ms}ms)` : ""}`;
        c.append(tt);
        svg.append(c);
      }
    });
    const ax = ns("text", { x: width - 4, y: lanes.length * laneH + 14, "font-size": 10, "text-anchor": "end", fill: "#6b7280" });
    ax.textContent = `0 … ${tmax} ms`;
    svg.append(ax);
    return svg;
  };

  const transitionChain = (sb) => {
    if (!sb.transitions.length) return null;
    const el = h("div", { class: "chain" }, h("span", { class: "st" }, sb.transitions[0].from));
    for (const tr of sb.transitions) el.append(h("span", { class: "tr" }, h("span", { class: "arrow" }, "→"), tr.trigger ? h("span", {}, actorBadge(tr.actor), h("code", {}, tr.trigger)) : h("span", { class: "muted" }, t("serverSide"))), h("span", { class: "st" }, tr.to));
    return el;
  };

  // ---------- views ----------
  const views = {};

  views.overview = () => {
    // read once at the start: what runs where, where data lives, and the state machine. Numbers and findings live in
    // "Now" (bench) — the crawl's browser counts are not the bench's behaviour.
    const el = h("div", {});
    {
      const S = M.system ?? {};
      const sys = h("div", { class: "system" }, h("h3", {}, t("system")));
      if (!S.root && !S.code) sys.append(h("div", { class: "muted" }, t("sysNone")));
      const sc = t("sysCols");
      if (S.components?.length) sys.append(h("h4", {}, t("sysComponents")), h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...[sc.unit, sc.exec, sc.dir, sc.deps].map((x) => h("th", {}, x))), ...S.components.map((c) => h("tr", {}, h("td", {}, h("code", {}, c.unit)), h("td", {}, h("code", {}, c.exec)), h("td", {}, h("code", {}, c.dir)), h("td", {}, c.deps))))));
      if (S.web?.length) sys.append(h("h4", {}, t("sysWeb")), h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...[sc.server, sc.listen, sc.root, sc.locations].map((x) => h("th", {}, x))), ...S.web.map((w) => h("tr", {}, h("td", {}, h("code", {}, w.serverName)), h("td", {}, w.listen), h("td", {}, h("code", {}, w.root)), h("td", {}, ...w.locations.map((l) => h("div", {}, h("code", {}, l)))))))));
      if (S.storage?.length) sys.append(h("h4", {}, t("sysStorage")), h("ul", {}, ...S.storage.map((st) => h("li", {}, h("strong", {}, st.kind), " ", h("code", {}, st.detail), st.note ? h("span", { class: "warn" }, " — " + st.note) : null))));
      if (S.notes?.length) sys.append(h("h4", {}, t("sysNotes")), h("ul", {}, ...S.notes.map((n) => h("li", { class: /^NOTE/.test(n) ? "warn" : "" }, n))));
      if (S.code) {
        const tl = t("sysTiles");
        const tiles = [[S.code.files, tl.files], [S.code.lines, tl.lines], [S.code.funcs, tl.funcs], [S.code.routes, tl.routes], [S.code.sqlStatements, tl.sql]];
        if (S.schema) tiles.push([S.schema.tables, tl.tables], [S.schema.columns, tl.columns], [S.schema.indexes, tl.indexes]);
        sys.append(h("h4", {}, t("sysCode"), " ", h("span", { class: "muted" }, S.code.dir)), h("div", { class: "tiles" }, ...tiles.map(([v, k]) => h("div", { class: "tile" }, h("div", { class: "v" }, v), h("div", { class: "k" }, k)))), h("div", { class: "muted", style: "margin-top:6px" }, t("biggest") + ": ", ...S.code.biggest.map((b) => chip(`${b.file} ${b.lines}L/${b.sql}sql`))));
      }
      if (S.env?.length) sys.append(h("div", { class: "muted", style: "margin-top:6px" }, t("sysEnv") + ": ", ...S.env.map((k) => h("code", { style: "margin-right:6px" }, k))));
      el.append(sys);
    }
    for (const sb of M.storyboards.filter((x) => x.transitions.length)) el.append(h("h3", {}, t("stateMachine")(sb.label)), transitionChain(sb), h("div", { class: "muted" }, t("triggerNote"), h("a", { class: "jump", href: "#", onclick: (e) => { e.preventDefault(); show("storyboard"); } }, t("storyboardLink"))));
    return el;
  };

  let scenIdx = 0;
  let scen = null;
  // a role's endpoints in order (its score loop first, then its busiest transitions); a screen's position = the first
  // of those endpoints it calls (推測: the bench calls APIs, the screen is where a person would do the same)
  const scenOrder = (p) => { const calls = new Set(M.screenWeights?.[p]?.calls ?? []); let best = null; scen.forEach((k, i) => { if (best == null && calls.has(k)) best = i; }); return best; };
  views.map = () => {
    const RS = flowRoles().filter((r) => !r.all);
    scen = null;
    if (scenIdx > 0 && RS[scenIdx - 1]) {
      const r = RS[scenIdx - 1], seq = [];
      for (const st of r.loops?.[0]?.steps ?? []) seq.push(pk(st));
      for (const t of r.transitions ?? []) { seq.push(pk(t.from), pk(t.to)); if (seq.length > 24) break; }
      // endpoints that most screens call (tag lists, /me) say nothing about a scenario
      const scr = Object.values(M.screenWeights ?? {}), cnt = {};
      for (const w of scr) for (const k of w.calls ?? []) cnt[k] = (cnt[k] ?? 0) + 1;
      scen = [...new Set(seq)].filter((k) => (cnt[k] ?? 0) < Math.max(2, scr.length / 2));
    }
    const nodes = M.nav.nodes.map((n) => ({ ...n }));
    const byPath = Object.fromEntries(nodes.map((n) => [n.path, n]));
    const edges = M.nav.edges.filter((e) => byPath[e.from] && byPath[e.to]);
    const indeg = Object.fromEntries(nodes.map((n) => [n.path, 0]));
    for (const e of edges) if (e.from !== e.to) indeg[e.to]++;
    let roots = byPath["/"] ? ["/"] : nodes.filter((n) => indeg[n.path] === 0).slice(0, 1).map((n) => n.path);
    if (!roots.length) roots = [nodes[0]?.path].filter(Boolean);
    const depth = {};
    const q = [];
    for (const r of roots) { depth[r] = 0; q.push(r); }
    while (q.length) { const p = q.shift(); for (const e of edges) if (e.from === p && depth[e.to] == null) { depth[e.to] = depth[p] + 1; q.push(e.to); } }
    let maxD = Math.max(0, ...Object.values(depth));
    for (;;) {
      const rest = nodes.filter((n) => depth[n.path] == null);
      if (!rest.length) break;
      const restPaths = new Set(rest.map((n) => n.path));
      const r0 = rest.find((n) => !edges.some((e) => e.to === n.path && restPaths.has(e.from))) ?? rest[0];
      depth[r0.path] = maxD + 1;
      const q2 = [r0.path];
      while (q2.length) { const p = q2.shift(); for (const e of edges) if (e.from === p && depth[e.to] == null) { depth[e.to] = depth[p] + 1; q2.push(e.to); } }
      maxD = Math.max(...Object.values(depth).filter((d) => d != null));
    }
    const W = 190, H = 190, GX = 150, GY = 28;
    const cols = {};
    for (const n of nodes) (cols[depth[n.path]] ??= []).push(n);
    for (const d of Object.keys(cols)) cols[d].sort((a, b) => a.personas.join().localeCompare(b.personas.join()) || a.path.localeCompare(b.path));
    const maxRows = Math.max(...Object.values(cols).map((c) => c.length));
    for (const [d, list] of Object.entries(cols)) list.forEach((n, i) => { n.x = 20 + d * (W + GX); n.y = 20 + i * (H + GY) + ((maxRows - list.length) * (H + GY)) / 2; });
    const width = 40 + (Math.max(...Object.keys(cols).map(Number)) + 1) * (W + GX);
    let height = 40 + maxRows * (H + GY);
    const svgNS = "http://www.w3.org/2000/svg";
    const ns = (tag, attrs = {}) => { const e = document.createElementNS(svgNS, tag); for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v); return e; };
    const svg = ns("svg", { width, height, class: "map-svg" });
    const defs = ns("defs");
    defs.innerHTML = `<marker id="arr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="#64748b"/></marker><marker id="arrh" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="#0f766e"/></marker>`;
    svg.append(defs);
    const edgeEls = [];
    const gEdges = ns("g");
    svg.append(gEdges);
    for (const e of edges) {
      const a = byPath[e.from], b = byPath[e.to];
      const back = depth[b.path] <= depth[a.path];
      let x1, y1, x2, y2, d;
      if (!back) { x1 = a.x + W; y1 = a.y + H / 2; x2 = b.x; y2 = b.y + H / 2; const mx = (x1 + x2) / 2; d = `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`; }
      else { x1 = a.x + W / 2; y1 = a.y + H; x2 = b.x + W / 2; y2 = b.y + H; const dy = Math.max(40, Math.abs(x2 - x1) / 4); d = `M${x1},${y1} C${x1},${y1 + dy} ${x2},${y2 + dy} ${x2},${y2}`; height = Math.max(height, Math.max(y1, y2) + dy + 30); svg.setAttribute("height", height); }
      const p = ns("path", { d, fill: "none", stroke: e.type === "redirect" ? "#f59e0b" : "#94a3b8", "stroke-width": 1.6, "marker-end": "url(#arr)", "stroke-dasharray": e.type === "redirect" ? "6 4" : "" });
      const g = ns("g", { class: "edge" });
      g.append(p);
      if (e.label) {
        const mid = { x: (x1 + x2) / 2, y: back ? Math.max(y1, y2) + Math.max(40, Math.abs(x2 - x1) / 4) * 0.75 : (y1 + y2) / 2 };
        const txt = ns("text", { x: mid.x, y: mid.y - 4, "font-size": 11, "text-anchor": "middle", fill: "#334155" });
        txt.textContent = e.label.length > 22 ? e.label.slice(0, 21) + "…" : e.label;
        g.append(ns("rect", { x: mid.x - 60, y: mid.y - 16, width: 120, height: 16, rx: 3, fill: "#fff", opacity: 0.85 }), txt);
      }
      g.__e = e;
      gEdges.append(g);
      edgeEls.push(g);
    }
    const gNodes = ns("g");
    svg.append(gNodes);
    const panel = h("div", { class: "map-panel" });
    const highlight = (path) => { for (const g of edgeEls) { const on = !path || g.__e.from === path || g.__e.to === path; g.classList.toggle("dim", !!path && !on); g.classList.toggle("hi", !!path && on); } };
    const openPanel = (n) => {
      panel.innerHTML = "";
      for (const id of n.screenIds) {
        const s = screenById[id];
        const eps = Object.values(apiReqOf(s).reduce((acc, r) => { const k = epKey(r); (acc[k] ??= { k, n: 0, ms: [] }).n++; if (r.ms != null) acc[k].ms.push(r.ms); return acc; }, {})).sort((a, b) => b.n - a.n);
        panel.append(h("div", {},
          h("h3", {}, h("span", { class: "badge" }, s.persona), h("code", {}, s.label), " ", h("a", { class: "jump", href: "#", onclick: (e) => { e.preventDefault(); show("screens", s.id); } }, t("openDetails"))),
          annScreen(s.path) ? h("div", { class: "purpose", style: "font-weight:600;font-size:14px" }, annScreen(s.path).purpose) : h("div", { class: "muted" }, b2("noPurpose")),
          shot(s, "mid"), h("details", {}, h("summary", { class: "muted" }, b2("autoSummary")), h("div", { class: "purpose" }, purposeText(s))),
          (s.buttons ?? []).length ? h("div", { class: "muted" }, t("buttons") + s.buttons.slice(0, 8).join(" · ")) : null,
          eps.length ? h("div", { class: "tw" }, h("table", {}, h("tr", {}, h("th", {}, t("endpoint")), h("th", {}, t("count")), h("th", {}, t("medianMs")), h("th", {}, t("role"))), ...eps.map((e) => h("tr", {}, h("td", { class: "nw" }, h("code", {}, e.k)), h("td", { class: "num" }, e.n), h("td", { class: "num" }, median(e.ms) ?? ""), h("td", {}, role(epByKey[e.k]?.role ?? "")))))) : h("div", { class: "muted" }, t("noApi")),
          timeline(s, 430),
        ));
      }
    };
    for (const n of nodes) {
      const fo = ns("foreignObject", { x: n.x, y: n.y, width: W, height: H });
      const s = n.screenIds.map((id) => screenById[id]).find((x) => x.screenshot) ?? screenById[n.screenIds[0]];
      const wgt = M.screenWeights?.[n.path] ?? null, ann = annScreen(n.path);
      const f = wgt ? Math.min(1, 3 * wgt.share) : 0, ord = scen ? scenOrder(n.path) : null;
      fo.append(h("div", { class: `map-node ${scen && ord == null ? "dimmed" : ""}`, style: `position:relative;${wgt && wgt.share > 0 ? `box-shadow:inset 0 0 0 ${(3 + 5 * f).toFixed(0)}px rgba(220,38,38,${(0.45 + 0.55 * f).toFixed(2)});` : ""}`, onmouseenter: () => highlight(n.path), onmouseleave: () => highlight(null), onclick: () => openPanel(n) }, ord != null ? h("div", { class: "order" }, ord + 1) : null, h("div", { class: "nw" }, ...n.personas.map((p) => h("span", { class: "badge" }, p)), wgt && wgt.share > 0 ? h("span", { class: "badge", style: "background:#dc2626;color:#fff" }, pctS(wgt.share) + (wgt.score ? " ★" : "")) : null), h("code", { style: "display:block" }, n.path), ann?.purpose ? h("div", { class: "ell", style: "font-weight:600" }, ann.purpose) : null, s ? shot(s, "thumb") : null, h("div", { class: "chips" }, ...(s ? chipsFor(s).slice(0, 2) : []))));
      gNodes.append(fo);
    }
    const vp = h("div", { class: "map-vp" });
    const inner = h("div", { class: "map-inner" }, svg);
    vp.append(inner);
    let scale = 1, tx = 0, ty = 0, drag = null;
    const apply = () => { inner.style.transform = `translate(${tx}px,${ty}px) scale(${scale})`; };
    const fit = () => { const cw = vp.clientWidth || 900, ch = vp.clientHeight || 600; scale = Math.max(0.2, Math.min(1, (cw - 20) / width, (ch - 20) / height)); tx = 10; ty = Math.max(10, (ch - height * scale) / 2); apply(); };
    requestAnimationFrame(fit);
    vp.append(h("div", { class: "map-ctl" }, h("button", { onclick: () => { scale *= 1.2; apply(); } }, "+"), h("button", { onclick: () => { scale /= 1.2; apply(); } }, "−"), h("button", { onclick: fit }, t("fit"))));
    vp.addEventListener("wheel", (e) => { e.preventDefault(); scale = Math.min(2.5, Math.max(0.2, scale * (e.deltaY < 0 ? 1.1 : 0.9))); apply(); }, { passive: false });
    vp.addEventListener("mousedown", (e) => { if (e.target.closest(".map-node")) return; drag = { x: e.clientX - tx, y: e.clientY - ty }; });
    window.addEventListener("mousemove", (e) => { if (drag) { tx = e.clientX - drag.x; ty = e.clientY - drag.y; apply(); } });
    window.addEventListener("mouseup", () => (drag = null));
    const sel = RS.length ? h("div", { class: "radios", title: b2("roleNote") }, h("span", { class: "muted" }, b2("scenario") + ": "), ...[{ name: b2("none") }, ...RS].map((r, i) => h("label", { style: "margin-right:10px;white-space:nowrap" }, h("input", { type: "radio", name: "scen", ...(i === scenIdx ? { checked: "" } : {}), onchange: () => { scenIdx = i; show("map"); } }), " " + (i ? `${roleLabel(r)} (${r.clients})` : r.name)))) : null;
    const selNote = RS.length ? h("div", { class: "muted", style: "font-size:12px" }, b2("roleNote")) : null;
    return h("div", {}, selNote, sel, h("div", { class: "map-wrap" }, h("div", { class: "map-left" }, h("div", { class: "muted", style: "margin:6px 0" }, t("mapLegend")), vp), panel));
  };

  views.storyboard = () => {
    const el = h("div", {});
    if (!M.storyboards.length) { el.append(h("div", { class: "muted" }, t("noFlows"))); return el; }
    for (const sb of M.storyboards) {
      const s = screenById[sb.screenId];
      el.append(h("h3", {}, h("span", { class: "badge" }, s.persona), h("code", {}, s.label), " ", ...sb.actors.map(actorBadge), h("span", { class: "muted" }, " " + t("steps")(sb.parts.length)), h("a", { class: "jump", href: "#", onclick: (e) => { e.preventDefault(); show("screens", s.id); } }, t("openLog"))));
      if (sb.transitions.length) el.append(h("div", { class: "muted" }, t("transitionsHead")), transitionChain(sb));
      if (sb.states.length) {
        const tmax = Math.max(sb.tEnd, 1);
        const colors = ["#0f766e", "#2563eb", "#d97706", "#7c3aed", "#db2777", "#059669", "#dc2626", "#4b5563"];
        const cmap = new Map();
        const bar = h("div", { class: "statebar" });
        sb.states.forEach((seg) => { if (!cmap.has(seg.value)) cmap.set(seg.value, colors[cmap.size % colors.length]); const left = (100 * seg.from) / tmax, w = Math.max(0.4, (100 * (seg.to - seg.from)) / tmax); bar.append(h("div", { class: "seg", style: `left:${left}%;width:${w}%;background:${cmap.get(seg.value)}`, title: `${seg.key}=${seg.value} (${seg.from}–${seg.to}ms, ${seg.endpoint})` }, w > 6 ? seg.value : "")); });
        el.append(h("div", { class: "muted", style: "margin-top:8px" }, t("timelineHead")(sb.stateKeys.join("/"))), h("div", { class: "stepmarks" }, ...sb.parts.map((p, i) => h("div", { class: "mark", style: `left:${(100 * p.tStart) / tmax}%`, title: p.label }, i))), bar);
      }
      const strip = h("div", { class: "strip" });
      sb.parts.forEach((p, i) => {
        const main = p.calls.filter((c) => !c.polling), polls = p.calls.filter((c) => c.polling);
        strip.append(h("div", { class: `frame ${p.stateChanges.length ? "changed" : ""}` }, h("div", { class: "fh" }, h("span", { class: "badge" }, i), h("strong", {}, p.label), ...p.stateChanges.map((v) => h("span", { class: "badge state", style: "margin-left:4px" }, v))), p.screenshot ? h("img", { class: "shot", src: p.screenshot, loading: "lazy", onclick: () => lightbox(p.screenshot, `${i}: ${p.label}`) }) : h("div", { class: "noshot" }, "—"), h("div", { class: "muted" }, `${p.tStart}–${p.tEnd} ms`)));
        strip.append(h("div", { class: "between" }, h("div", { class: "arrow" }, "→"), ...(main.length ? main.map((c) => chip(c.text, c.cls === "app" ? "err" : c.cls ? "soft" : "main")) : [h("span", { class: "muted" }, t("noNonPolling"))]), polls.length ? h("details", { class: "row" }, h("summary", {}, t("pollingFold")(polls.length)), ...polls.map((c) => chip(c.text, "poll"))) : null));
      });
      el.append(h("div", { class: "muted" }, t("frameNote")), strip);
    }
    return el;
  };

  // every endpoint: observed by the crawl, registered in the code, or seen in the bench
  const allEndpoints = (() => {
    const out = new Map();
    const seg = (p) => (p.replace(M.apiPrefix ?? "/api/", "").split("/")[0] || "-");
    for (const e of M.endpoints.filter((x) => x.path.startsWith("/"))) out.set(pk(`${e.method} ${e.path}`), { ...e, actor: e.actor && e.actor !== "-" ? e.actor : seg(e.path) });
    for (const r of M.routes ?? []) if (!out.has(r.key)) out.set(r.key, { method: r.method, path: r.path, actor: seg(r.path), kind: r.method === "GET" ? "read" : "write", count: 0, screens: [], statuses: {}, errors: { app: 0, artefact: 0, samples: [] }, hints: [], allSamples: [], unseen: true, route: r });
    for (const b of M.bench?.endpoints ?? []) if (!out.has(b.key)) { const [m, ...rest] = b.key.split(" "); const p = rest.join(" "); out.set(b.key, { method: m, path: p, actor: seg(p), kind: m === "GET" ? "read" : "write", count: 0, screens: [], statuses: {}, errors: { app: 0, artefact: 0, samples: [] }, hints: [], allSamples: [], unseen: true }); }
    return [...out.entries()].map(([k, e]) => ({ ...e, pkey: k }));
  })();
  const epFilter = { method: "", actor: "", heavy: false, write: false, unseen: false, q: "" };
  views.endpoints = () => {
    const el = h("div", {});
    const F = b2("filters"), BC = b2("benchCols"), C = t("cols");
    const methods = [...new Set(allEndpoints.map((e) => e.method))].sort(), actors = [...new Set(allEndpoints.map((e) => e.actor))].sort();
    const radio = (name, list, key) => h("span", {}, h("span", { class: "muted" }, F[key] + ": "), ...["", ...list].map((v) => h("label", {}, h("input", { type: "radio", name, ...(epFilter[key] === v ? { checked: "" } : {}), onchange: () => { epFilter[key] = v; render(); } }), " " + (v || F.all))));
    const check = (key) => h("label", {}, h("input", { type: "checkbox", ...(epFilter[key] ? { checked: "" } : {}), onchange: (ev) => { epFilter[key] = ev.target.checked; render(); } }), " " + F[key]);
    const input = h("input", { placeholder: t("filter"), class: "filter", value: epFilter.q, oninput: (ev) => { epFilter.q = ev.target.value.toLowerCase(); render(); } });
    const bar = h("div", { class: "filterbar" }, radio("fm", methods, "method"), radio("fa", actors, "actor"), M.bench ? check("heavy") : null, check("write"), check("unseen"));
    const holder = h("div", {});
    const render = () => {
      let rows = allEndpoints.filter((e) => (!epFilter.method || e.method === epFilter.method) && (!epFilter.actor || e.actor === epFilter.actor) && (!epFilter.heavy || (benchBy[e.pkey]?.share ?? 0) >= 0.03) && (!epFilter.write || e.method !== "GET") && (!epFilter.unseen || e.unseen) && (!epFilter.q || `${e.method} ${e.path} ${e.actor}`.toLowerCase().includes(epFilter.q)));
      rows.sort((a, b) => (benchBy[b.pkey]?.share ?? -1) - (benchBy[a.pkey]?.share ?? -1) || a.path.localeCompare(b.path));
      const table = h("table", { class: "eps" }, h("tr", {}, ...(M.bench ? [h("th", {}, BC[0]), h("th", {}, BC[1]), h("th", {}, BC[2])] : []), h("th", {}, C.method), h("th", {}, C.path), h("th", {}, C.screens)));
      for (const e of rows) {
        const be = benchBy[e.pkey], ann = M.annotations?.endpoints?.[e.pkey];
        const qs = (M.bench?.sqls ?? []).filter((q) => q.tags?.[e.pkey]).slice(0, 4);
        const sub = h("tr", { class: "sub", style: "display:none" }, h("td", { colspan: M.bench ? 6 : 3 },
          h("div", { class: "sub3" },
            h("div", {}, h("div", { class: "muted" }, h("strong", {}, b2("contract"))), Object.keys(e.statuses).length ? h("div", { class: "muted" }, "status: " + Object.entries(e.statuses).map(([st, n]) => `${st}×${n}`).join(" ")) : null, e.sampleQuery ? h("div", { class: "muted" }, "?" + e.sampleQuery) : null, e.sampleSchema ? h("div", { class: "schema" }, e.sampleSchema) : h("span", { class: "muted" }, e.unseen ? b2("unseen") : t("noJson")), detailsRow(t("request")(e.sampleStatus), e.sampleReq), detailsRow(t("response")(e.sampleStatus), e.sampleRes)),
            h("div", {}, qs.length ? h("div", {}, h("div", { class: "muted" }, b2("measuredSql")), ...qs.map((q) => h("div", { style: "margin-bottom:3px" }, h("code", { title: q.norm }, q.norm.length > 90 ? q.norm.slice(0, 89) + "…" : q.norm), " ", ...codeLinks(q)))) : null,
              e.route ? h("div", { class: "muted" }, h("code", {}, e.route.handler), ` ${e.route.file}:${e.route.line}`) : null,
              e.code ? h("details", { class: "row" }, h("summary", { class: "muted" }, b2("staticFacts")), codeInfo(e.code), sqlList(e.code), h("div", {}, ...flagChips(e.code.flags))) : null),
            h("div", {}, h("div", { class: "muted" }, t("calledFrom")), ...e.screens.map((x) => { const sid = M.screens.find((sc) => `${sc.persona}:${sc.label}` === x)?.id; return chip(x, "link", "", () => sid && show("screens", sid)); })))));
        const main = h("tr", { class: "main", style: "cursor:pointer", onclick: () => { sub.style.display = sub.style.display === "none" ? "" : "none"; } },
          ...(M.bench ? [h("td", { class: `num ${be && be.share >= 0.2 ? "err" : ""}` }, be ? pctS(be.share) : ""), h("td", { class: "num" }, be ? Math.round(be.p99Ms) : ""), h("td", { class: "num" }, be ? be.count : "")] : []),
          h("td", { class: "nw" }, h("code", {}, e.method)),
          h("td", {}, h("code", {}, e.path), e.unseen ? h("span", { class: "chip", style: "margin-left:6px" }, b2("unseen")) : null, ann?.purpose ? h("div", { class: "muted" }, ann.purpose) : null),
          h("td", { class: "num" }, e.screens.length || ""));
        table.append(main, sub);
      }
      holder.innerHTML = "";
      holder.append(h("div", { class: "muted", style: "margin:4px 0" }, t("nEndpoints")(rows.length)), h("div", { class: "tw" }, table));
    };
    render();
    el.append(input, bar, holder);
    return el;
  };

  views.screens = (focusId) => {
    // folded by default: one line per screen (role, bench weight); open one to see the screenshot and its requests
    const el = h("div", {}, h("div", { class: "muted", style: "margin-bottom:6px" }, b2("screensHead")(M.screens.length)));
    for (const s of M.screens) {
      const w = M.screenWeights?.[s.path], ann = annScreen(s.path);
      el.append(h("details", { id: `scr-${s.id}`, class: "scr", open: focusId === s.id ? "" : null },
        h("summary", {}, h("span", { class: "badge" }, s.persona), h("code", {}, s.label), w && w.share > 0 ? h("span", { class: "badge", style: "background:#dc2626;color:#fff;margin-left:6px" }, pctS(w.share) + (w.score ? " ★" : "")) : null, ann?.purpose ? h("span", { style: "margin-left:8px" }, ann.purpose) : null, " ", h("span", { class: "muted" }, t("requests")(apiReqOf(s).length))),
        h("div", { class: "two" }, h("div", {}, shot(s, "mid")), h("div", {}, annScreen(s.path) ? h("div", { class: "purpose", style: "font-weight:600" }, annScreen(s.path).purpose) : null, h("details", {}, h("summary", { class: "muted" }, b2("autoSummary")), h("div", { class: "purpose" }, purposeText(s))), h("div", {}, ...chipsFor(s, { link: true })), (s.buttons ?? []).length ? h("div", { class: "muted" }, t("buttons") + s.buttons.join(" · ")) : null, (s.forms ?? []).length ? h("div", { class: "muted" }, t("forms") + s.forms.map((f) => `${f.method} ${f.action ?? ""} [${f.fields.join(",")}]`).join(" | ")) : null, (s.links ?? []).length ? h("div", { class: "muted" }, t("links") + [...new Set(s.links.map((l) => l.href))].slice(0, 12).join(" ")) : null, timeline(s, 640))),
        requestTable(s),
      ));
    }
    if (focusId) setTimeout(() => $(`#scr-${focusId}`)?.scrollIntoView({ behavior: "smooth", block: "start" }), 50);
    return el;
  };

  views.scan = () => {
    const fr = M.scan ?? {};
    const el = h("div", {});
    const perf = M.findings.filter((f) => f.perf);
    if (perf.length) el.append(h("h3", {}, t("perfHints")), h("div", { class: "notice" }, t("perfNote")), h("ul", { class: "findings" }, ...perf.map((f) => { const [title, detail] = findingText(f); return h("li", { class: f.level }, h("div", { class: "ft" }, title, " ", f.view ? h("a", { class: "jump", href: "#", onclick: (e) => { e.preventDefault(); show(f.view, f.arg); } }, "→") : null), detail ? h("div", { class: "fd" }, detail) : null); })));
    el.append(h("div", {}, t("scanLine")(fr)));
    if (M.openapi) el.append(h("h3", {}, t("unobservedOps")(M.openapi.unobserved.length, M.openapi.operations.length)), h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...t("opCols").map((x) => h("th", {}, x))), ...M.openapi.unobserved.map((o) => h("tr", { class: o.path.includes("/internal/") ? "step" : "" }, h("td", {}, actorBadge(o.actor)), h("td", { class: "nw" }, h("code", {}, `${o.method} ${o.path}`)), h("td", {}, t("opNote")[o.noteKey] ?? o.note), h("td", {}, codeInfo(o.code ? { helpers: [], ...o.code } : null), o.code ? h("div", { class: "flags" }, ...flagChips(o.code.flags)) : null, sqlList(o.code)))))));
    el.append(h("details", { open: "" }, h("summary", {}, t("refsNotObserved")((M.unobservedRefs ?? []).length)), ...(M.unobservedRefs ?? []).map((e) => chip(`${e.path} (refs ${e.refs})`))));
    el.append(h("details", {}, h("summary", {}, t("allRefs")((fr.endpoints ?? []).length)), ...(fr.endpoints ?? []).map((e) => chip(`${e.path} (refs ${e.refs})`))));
    el.append(h("h3", {}, t("pollingPerScreen")), M.polling.length ? h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...t("pollCols").map((x) => h("th", {}, x))), ...M.polling.map((p) => h("tr", {}, h("td", { class: "nw" }, p.screen), h("td", { class: "nw" }, h("code", {}, p.endpoint)), h("td", { class: "num" }, p.count), h("td", { class: "num" }, p.medianGapMs), h("td", { class: "num" }, p.minGapMs), h("td", {}, (epByKey[p.endpoint]?.hints ?? []).join(", ")))))) : h("div", { class: "muted" }, t("none")));
    if (!M.codeDir) { const tip = t("codeDirTip"); el.append(h("div", { class: "notice", style: "margin-top:12px" }, tip[0], h("code", {}, "codeDir"), tip[1])); }
    return el;
  };

  views.data = (focus) => {
    const D = M.dataModel ?? { tables: [], relations: [] };
    const el = h("div", {}, h("h3", {}, focus && focus !== "all" ? b2("erFocus")(focus) : t("dataHead")), h("div", { class: "muted" }, t("dataRows")(D.rowsSource)));
    if (!D.tables.length) { el.append(h("div", { class: "notice" }, "no schema (set schemaPath / codeDir in the config)")); return el; }
    // default: table cards + endpoint × table matrix (readable); the ER only for one table and its neighbours, or "all"
    const growth = Object.fromEntries((M.bench?.growth ?? []).map((g) => [g.table.split(".").pop(), g]));
    const use = M.tableUse ?? {};
    const users = (tb, mode) => Object.entries(use).filter(([, m]) => mode === "W" ? m[tb] === "W" : m[tb] === "R").map(([k]) => k).sort((a, b) => (benchBy[b]?.share ?? 0) - (benchBy[a]?.share ?? 0));
    const tlink = (name) => h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); show("data", name); } }, h("code", {}, name));
    if (!focus) {
      const cc = b2("dataCardCols");
      const order = [...D.tables].sort((a, b) => (growth[b.name]?.per_s ?? -1) - (growth[a.name]?.per_s ?? -1) || (b.rows ?? 0) - (a.rows ?? 0));
      el.append(h("div", { class: "muted", style: "margin:6px 0" }, b2("erHint2"), " ", h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); show("data", "all"); } }, b2("erAll"))));
      el.append(h("h4", {}, b2("dataCards")), h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...cc.map((c) => h("th", {}, c))), ...order.map((tb) => { const g = growth[tb.name]; const w = users(tb.name, "W"), r = users(tb.name, "R"); return h("tr", {}, h("td", {}, tlink(tb.name)), h("td", { class: "num" }, g ? Number(g.end).toLocaleString() : tb.rows != null ? Number(tb.rows).toLocaleString() : ""), h("td", { class: `num ${g && g.per_s >= 10 ? "warn" : ""}` }, g ? g.per_s.toFixed(1) : ""), h("td", {}, ...w.slice(0, 4).map((k) => chip(k, "err")), w.length > 4 ? h("span", { class: "muted" }, ` +${w.length - 4}`) : null), h("td", {}, ...r.slice(0, 4).map((k) => chip(k)), r.length > 4 ? h("span", { class: "muted" }, ` +${r.length - 4}`) : null), h("td", { class: "muted" }, [...(tb.indexes ?? []).filter((x) => !x.startsWith("PRIMARY")), ...(M.addedIndexes?.[tb.name] ?? []).map((x) => x + " ✚")].join(", "))); }))));
      const eps = Object.keys(use).sort((a, b) => (benchBy[b]?.share ?? 0) - (benchBy[a]?.share ?? 0));
      const tabs = order.map((tb) => tb.name).filter((n) => eps.some((k) => use[k][n]));
      if (eps.length && tabs.length) {
        const mt = h("table", { class: "matrix" }, h("tr", {}, h("th", {}), ...tabs.map((n) => h("th", { class: "col" }, n))));
        for (const k of eps) { const sh = benchBy[k]?.share ?? 0; mt.append(h("tr", {}, h("td", { class: "nw" }, h("code", {}, k), sh ? h("span", { class: "muted" }, " " + pctS(sh)) : null), ...tabs.map((n) => { const m = use[k][n]; return h("td", { class: `cell ${m === "W" ? "w" : ""}`, style: m ? `background:rgba(${m === "W" ? "220,38,38" : "15,118,110"},${(0.12 + Math.min(0.8, 3 * sh)).toFixed(2)})` : "" }, m ?? ""); }))); }
        el.append(h("h4", {}, b2("matrixHead")), h("div", { class: "tw" }, mt));
      }
      return el;
    }
    let tables = D.tables, rels = D.relations;
    if (focus !== "all") {
      const near = new Set([focus, ...D.relations.filter((r) => r.from === focus || r.to === focus).flatMap((r) => [r.from, r.to])]);
      tables = D.tables.filter((tb) => near.has(tb.name));
      rels = D.relations.filter((r) => near.has(r.from) && near.has(r.to));
    }
    el.append(h("div", { style: "margin:4px 0" }, h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); show("data"); } }, b2("backCards")), focus !== "all" ? h("span", {}, " · ", h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); show("data", "all"); } }, b2("backAll"))) : null));
    // ER: layered by foreign-key direction (a referenced table sits left of the tables that reference it), rows ordered
    // by the barycenter of their neighbours (two sweeps) so lines cross less; arrows go right → left
    const W = 230, ROW = 15, PAD = 10, GAPX = 90, GAPY = 26;
    const names = tables.map((tb) => tb.name), byName = Object.fromEntries(tables.map((tb) => [tb.name, tb]));
    const outs = Object.fromEntries(names.map((n) => [n, rels.filter((r) => r.from === n && byName[r.to] && r.to !== n).map((r) => r.to)]));
    const ins = Object.fromEntries(names.map((n) => [n, rels.filter((r) => r.to === n && byName[r.from] && r.from !== n).map((r) => r.from)]));
    const depth = {};
    const dep = (n, stack = new Set()) => { if (depth[n] != null) return depth[n]; if (stack.has(n)) return 0; stack.add(n); const d = outs[n].length ? 1 + Math.max(...outs[n].map((m) => dep(m, stack))) : 0; stack.delete(n); return (depth[n] = d); };
    names.forEach((n) => dep(n));
    // tables with no relation at all go to a last column of their own
    const lone = names.filter((n) => !outs[n].length && !ins[n].length);
    const maxD = Math.max(0, ...names.filter((n) => !lone.includes(n)).map((n) => depth[n]));
    for (const n of lone) depth[n] = names.length > lone.length ? maxD + 1 : 0;
    const cols = [];
    for (const n of names) (cols[depth[n]] ??= []).push(n);
    for (const c of cols) c?.sort((a, b) => (ins[b].length + outs[b].length) - (ins[a].length + outs[a].length));
    const pos = () => { const p = {}; cols.forEach((c) => c?.forEach((n, k) => (p[n] = k))); return p; };
    for (let sweep = 0; sweep < 2; sweep++) {
      let p = pos();
      for (let d = 1; d < cols.length; d++) if (cols[d]) { const bc = (n) => (outs[n].length ? outs[n].reduce((x, m) => x + p[m], 0) / outs[n].length : p[n]); cols[d].sort((a, b) => bc(a) - bc(b)); p = pos(); }
      for (let d = cols.length - 2; d >= 0; d--) if (cols[d]) { const bc = (n) => (ins[n].length ? ins[n].reduce((x, m) => x + p[m], 0) / ins[n].length : p[n]); cols[d].sort((a, b) => bc(a) - bc(b)); p = pos(); }
    }
    const boxes = new Map();
    let maxX = 0, maxY = 0;
    cols.forEach((c, d) => { let yy = 0; (c ?? []).forEach((n) => { const tb = byName[n], hgt = 26 + tb.columns.length * ROW + 8; const xx = d * (W + GAPX); boxes.set(n, { x: xx, y: yy, w: W, h: hgt, tb }); yy += hgt + GAPY; maxX = Math.max(maxX, xx + W + 20); maxY = Math.max(maxY, yy); }); });
    const H = maxY + 20;
    const ns = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(ns, "svg"); svg.setAttribute("width", maxX); svg.setAttribute("height", H); svg.setAttribute("class", "er");
    const defs = document.createElementNS(ns, "defs"); defs.innerHTML = '<marker id="arr" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="#0f766e"/></marker>'; svg.append(defs);
    const esc = (v) => String(v ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;");
    const relEls = [];
    for (const r of rels) {
      const a = boxes.get(r.from), b = boxes.get(r.to); if (!a || !b) continue;
      const ci = a.tb.columns.findIndex((c) => c.name === r.col);
      const ay = a.y + 26 + ci * ROW + ROW / 2, ax = a.x + a.w, bx = b.x, by = b.y + 13;
      const l = document.createElementNS(ns, "path");
      const sx = ax, ex = (bx > ax ? bx : b.x + b.w), c1 = sx + (ex > sx ? 30 : -30), c2 = ex + (ex > sx ? -30 : 30);
      l.setAttribute("d", `M${bx > ax ? sx : a.x},${ay} C${bx > ax ? c1 : a.x - 30},${ay} ${c2},${by} ${ex},${by}`); l.setAttribute("fill", "none"); l.setAttribute("stroke", "#0f766e"); l.setAttribute("stroke-width", "1.2"); l.setAttribute("marker-end", "url(#arr)"); l.setAttribute("opacity", "0.7");
      const tt = document.createElementNS(ns, "title"); tt.textContent = `${r.from}.${r.col} → ${r.to}`; l.append(tt); svg.append(l);
      relEls.push({ el: l, r });
    }
    for (const [name, b] of boxes) {
      const g = document.createElementNS(ns, "g");
      g.innerHTML = `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="6" fill="#fff" stroke="#9ca3af"/><rect x="${b.x}" y="${b.y}" width="${b.w}" height="24" rx="6" fill="#0f766e"/><text x="${b.x + PAD}" y="${b.y + 16}" font-size="12" font-weight="700" fill="#fff">${esc(name)}${b.tb.rows != null ? ` <tspan font-weight="400" opacity=".85">· ${Number(b.tb.rows).toLocaleString()} rows</tspan>` : ""}</text>` +
        b.tb.columns.map((c, i) => `<text x="${b.x + PAD}" y="${b.y + 26 + i * ROW + 11}" font-size="11" fill="#111" ${c.pk ? 'font-weight="700"' : ""}><title>${esc(c.type)}${c.nullable ? " NULL" : " NOT NULL"}</title>${esc(c.name)}${c.indexed ? ' <tspan fill="#0f766e">(i)</tspan>' : ""}${/_id$/.test(c.name) && !c.pk ? ' <tspan fill="#6b7280">→</tspan>' : ""}</text>`).join("");
      g.style.cursor = "pointer"; g.addEventListener("click", () => show("data", name));
      g.dataset.t = name;
      g.addEventListener("mouseenter", () => { const near = new Set([name]); for (const { el, r } of relEls) { const on = r.from === name || r.to === name; if (on) { near.add(r.from); near.add(r.to); } el.setAttribute("stroke-width", on ? "3" : "1.2"); el.setAttribute("opacity", on ? "1" : "0.08"); } for (const x of svg.querySelectorAll("g[data-t]")) x.setAttribute("opacity", near.has(x.dataset.t) ? "1" : "0.35"); });
      g.addEventListener("mouseleave", () => { for (const { el } of relEls) { el.setAttribute("stroke-width", "1.2"); el.setAttribute("opacity", "0.7"); } for (const x of svg.querySelectorAll("g[data-t]")) x.setAttribute("opacity", "1"); });
      svg.append(g);
    }
    el.append(h("div", { class: "muted", style: "margin:6px 0" }, t("erHint")), h("div", { class: "tw" }, svg));
    el.append(h("div", { class: "tw", style: "margin-top:14px" }, h("table", {}, h("tr", {}, ...t("dataCols").map((x) => h("th", {}, x))), ...tables.map((tb) => h("tr", {}, h("td", {}, h("code", {}, tb.name)), h("td", { class: "num" }, tb.rows != null ? Number(tb.rows).toLocaleString() : ""), h("td", {}, tb.columns.map((c) => c.name + (c.pk ? "(PK)" : c.indexed ? "(i)" : "")).join(", ")), h("td", {}, tb.indexes.join(", ") || "-"), h("td", {}, D.relations.filter((r) => r.from === tb.name).map((r) => `${r.col} → ${r.to}`).join(", ")))))));
    return el;
  };

  // ---------- bench overlay views ----------
  const HOWCLS = { "parallel-fix": "main", design: "soft", investigate: "poll" };
  views.now = () => {
    const el = h("div", {});
    if (!M.bench || !M.now) { el.append(h("div", { class: "notice" }, b2("noBench"))); return el; }
    const B = M.bench, N = M.now;
    el.append(h("h3", {}, b2("nowHead")(B.ts, B.prevTS), B.opsReport ? h("a", { class: "jump", href: B.opsReport, target: "_blank" }, " " + b2("opsReport") + " →") : null));
    if (B.nextStep?.length) el.append(h("div", { class: "notice" }, h("strong", {}, b2("nextStep")), h("ol", { style: "margin:4px 0 0 18px;padding:0" }, ...B.nextStep.map((c) => h("li", {}, h("strong", {}, c.name), h("div", {}, h("code", {}, c.cmd)), h("div", { class: "muted" }, (c.why ?? []).join(" / ")))))));
    el.append(h("div", { class: "muted", style: "margin:6px 0" }, b2("nowNote")), h("div", { class: "muted", style: "margin:0 0 6px" }, b2("howNote")));
    const C = b2("cols"), HW = b2("how");
    const table = h("table", { class: "eps" }, h("tr", {}, ...C.map((c) => h("th", {}, c))));
    N.open.forEach((it, i) => {
      const rel = it.rel === "score" ? h("span", { class: "chip err" }, b2("score")) : it.rel === "loop" ? h("span", { class: "chip soft" }, b2("loop")) : "";
      const todos = [it.errors ? b2("errLine")(it.errors.c5, it.errors.count, it.errors.messages.map((m) => m.message).join(" / ")) : null, it.regression ? b2("regression")(Math.round(it.prevAvgMs), Math.round(it.avgMs)) : null, it.todo ?? null, ...(it.actions ?? []).map((a) => a.todo)].filter(Boolean);
      table.append(h("tr", { class: "main" },
        h("td", { class: "num" }, i + 1),
        h("td", { style: "min-width:200px" }, it.type === "endpoint" ? h("code", {}, it.title) : h("span", {}, h("span", { class: "chip" }, it.priority), " ", h("code", {}, it.group ?? "")), it.purpose ? h("div", { class: "muted" }, it.purpose) : null, ...(it.rules ?? []).map((r) => h("div", { class: "muted", style: "font-size:11px" }, `${b2("rule")}: ${r}`))),
        h("td", { class: `num ${it.share >= 0.2 ? "err" : ""}` }, pctS(it.share)), h("td", { class: "num" }, it.p99Ms != null ? Math.round(it.p99Ms) : ""), h("td", {}, rel),
        h("td", { class: "nw" }, h("span", { class: `chip ${HOWCLS[it.how] ?? ""}` }, HW[it.how] ?? it.how), ...(it.units ?? []).map((u) => h("div", {}, h("code", { style: "font-size:11px" }, u)))),
        h("td", { style: "min-width:320px;max-width:560px" }, ...todos.map((w) => h("div", { class: /悪化|regression|^5xx/.test(w) ? "err" : "", style: "margin-bottom:3px" }, w))),
        h("td", { style: "min-width:200px;max-width:280px" }, ...(it.sqls ?? []).slice(0, 2).map((q) => h("div", { title: q.norm, style: "margin-bottom:3px" }, ...codeLinks(q).slice(0, 2).flatMap((x) => [x, " "]))), it.file && !(it.sqls ?? []).length ? h("code", {}, it.file) : null)));
    });
    el.append(h("div", { class: "tw" }, table));
    if (N.more) el.append(h("div", { class: "muted" }, b2("more")(N.more)));
    if (N.resolved.length) el.append(h("details", { style: "margin-top:10px" }, h("summary", { class: "muted" }, b2("resolved")(N.resolved.length)), h("ul", {}, ...N.resolved.map((r) => h("li", {}, h("code", {}, r.title), " ", r.improved ? h("span", { class: "muted" }, `${pctS(r.prevShare)} → ${pctS(r.share)}`) : null)))));
    return el;
  };

  // roles in the bench (from the ops Flows): "all" + each inferred role
  const flowRoles = () => { const F = M.bench?.flows; if (!F) return []; return [{ name: b2("allRoles"), all: true, ...F }, ...(F.roles ?? [])]; };
  // role names: docs/app-annotations.json → roles, else the endpoints' purposes ("コメント投稿 + リアクション投稿"), else short paths
  const epShort = (k) => { const a = M.annotations?.endpoints?.[pk(k)]?.purpose; return a ? a.split(/[。（(]/)[0] : k.replace(/\/api\//g, "/").replace(/:\w+/g, ":id"); };
  const roleLabel = (r) => { if (r.all) return r.name; const named = M.annotations?.roles?.[r.name] ?? M.annotations?.roles?.[pk(r.name)]; if (named) return named; if (r.name === "read only") return b2("readOnly"); if (r.name === "other") return b2("otherRole"); return r.name.split(" + ").map(epShort).join(" + "); };
  const newChip = (x) => (x?.new ? h("span", { class: "chip err", style: "margin-left:4px" }, b2("isNew")) : x?.grew != null ? h("span", { class: "chip soft", style: "margin-left:4px" }, b2("grew")(x.grew)) : null);
  let flowRoleIdx = 0;
  // periodic internal endpoints (ops report.json "internal"): call interval × work per call
  const internalTable = () => {
    const I = M.bench?.internal ?? [];
    if (!I.length) return null;
    const n0 = (v) => (v == null ? "" : Math.round(v).toLocaleString()), n1 = (v) => (v == null ? "" : (+v).toFixed(1));
    return h("div", {}, h("h4", {}, b2("internalHead")), h("div", { class: "muted", style: "margin-bottom:4px" }, b2("internalNote")),
      h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...b2("internalCols").map((c) => h("th", {}, c))),
        ...I.map((x) => h("tr", {}, h("td", {}, h("code", {}, x.endpoint)), h("td", {}, x.host ?? ""), h("td", { class: "num" }, n0(x.calls)), h("td", { class: "num" }, n0(x.median_gap_ms)), h("td", { class: "num" }, n0(x.p90_gap_ms)), h("td", { class: "num" }, n0(x.avg_ms)), h("td", { class: "num" }, n0(x.p99_ms)), h("td", { class: "num" }, n1(x.writes_per_call)))))));
  };
  views.flows = () => {
    const el = h("div", {}, h("h3", {}, b2("flowsHead")));
    const RS = flowRoles();
    if (!RS.length || !RS[0].clients) { el.append(h("div", { class: "notice" }, M.bench ? b2("noFlows") : b2("noBench"))); const it = internalTable(); if (it) el.append(it); return el; }
    if (M.bench.flowsHtml) el.append(h("div", { style: "margin-bottom:6px" }, h("a", { href: M.bench.flowsHtml, target: "_blank" }, b2("sankey") + " →")));
    el.append(h("div", { class: "muted" }, b2("roleNote")), h("div", { class: "radios" }, h("span", { class: "muted" }, b2("role") + ": "), ...RS.map((r, i) => h("label", { style: "margin-right:12px;white-space:nowrap", title: r.all ? "" : r.name }, h("input", { type: "radio", name: "frole", ...(i === flowRoleIdx ? { checked: "" } : {}), onchange: () => { flowRoleIdx = i; show("flows"); } }), ` ${roleLabel(r)} (${r.clients})`, newChip(r)))));
    const F = RS[Math.min(flowRoleIdx, RS.length - 1)];
    if (F.loops?.length) {
      el.append(h("h4", {}, b2("loops")));
      for (const l of F.loops) {
        const gaps = l.gap_ms ?? l.steps.map(() => 0), mx = Math.max(...l.step_ms, ...gaps, 1), cyc = 1000 * l.cycle_med_s;
        const waitSum = gaps.reduce((a, b) => a + b, 0), gi = gaps.indexOf(Math.max(...gaps)), slowI = l.step_ms.indexOf(Math.max(...l.step_ms));
        const waitMost = cyc > 0 && waitSum / cyc >= 0.5 && cyc >= 300;
        // the slowest step is emphasised only when it really stands out: ≥ 100 ms, or ≥ 20 ms and ≥ 2× the other steps' median
        const others = l.step_ms.filter((_, k) => k !== slowI).sort((a, b) => a - b), otherMed = others.length ? others[Math.floor(others.length / 2)] : 0, top = l.step_ms[slowI] ?? 0;
        const standsOut = l.steps.length > 1 && (top >= 100 || (top >= 20 && top >= 2 * otherMed));
        const scale = (ms) => Math.max(2, Math.min(260, (260 * Math.sqrt(ms)) / Math.sqrt(mx))).toFixed(0);
        const fmt = (ms) => (ms >= 1000 ? (ms / 1000).toFixed(1) + " s" : Math.round(ms) + " ms");
        const qtxt = (q) => (q ? ` (p10 ${fmt(q[0])} / p90 ${fmt(q[2])})` : "");
        el.append(h("div", { class: "loopbox" },
          h("div", {}, h("strong", {}, b2("times")(l.times, l.cycle_med_s.toFixed(3))), newChip(l)),
          waitMost ? h("div", { class: "muted", style: "margin:4px 0" }, "ℹ " + b2("waitWarn")(Math.round((100 * waitSum) / cyc), l.steps[gi], (gaps[gi] / 1000).toFixed(1))) : null,
          h("ol", { class: "steps" }, ...l.steps.map((st, k) => { const ms = l.step_ms[k], g = gaps[k] ?? 0, slow = k === slowI && standsOut, maxOnly = k === slowI && !standsOut && l.steps.length > 1; return h("li", { class: slow ? "slow" : "" },
            h("code", {}, st),
            h("div", { class: "barrow" }, h("span", { class: "bar", style: `width:${scale(ms)}px` }), h("span", { class: "num" }, ` ${b2("resp")} ${b2("median")} ${fmt(ms)}`), h("span", { class: "muted" }, qtxt(l.step_q_ms?.[k])), slow ? h("span", { class: "chip err", style: "margin-left:6px" }, b2("slowest")) : maxOnly ? h("span", { class: "muted", style: "margin-left:6px;font-size:11px" }, b2("maxStep")) : null),
            g >= 1 ? h("div", { class: "barrow" }, h("span", { class: "bar gap", style: `width:${scale(g)}px` }), h("span", { class: "num muted" }, ` ${b2("wait")} ${b2("median")} ${fmt(g)}`), h("span", { class: "muted" }, qtxt(l.gap_q_ms?.[k]))) : null); })),
          h("details", {}, h("summary", { class: "muted" }, b2("deep")), h("div", { class: "muted" }, b2("cycleDist")((l.cycle_p10_s ?? 0).toFixed(2), l.cycle_med_s.toFixed(2), (l.cycle_p90_s ?? 0).toFixed(2), (l.cycle_max_s ?? 0).toFixed(1))), h("div", { class: "muted" }, b2("deepCmd")), h("code", {}, `task flows TS=${M.bench.ts} FROM='${l.steps[0]}' DEPTH=${l.steps.length + 3}`))));
      }
    }
    if (F.drops?.length) {
      const R = b2("reasons");
      el.append(h("h4", {}, b2("drops")(F.ended, F.clients)), h("div", { class: "muted", style: "margin-bottom:4px" }, b2("dropDef")), F.drops.some((d) => d.reason === "unknown") ? h("div", { class: "muted", style: "margin-bottom:4px" }, b2("unknownNote")) : null,
        h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...b2("dropCols").map((c) => h("th", {}, c))), ...F.drops.map((d) => h("tr", {}, h("td", {}, h("code", {}, d.ep), newChip(d)), h("td", { class: d.status !== "ok" ? "err" : "" }, d.status), h("td", { class: d.reason === "error" || d.reason === "slow" ? "err" : d.reason === "end" ? "muted" : "" }, R[d.reason] ?? d.reason ?? ""), h("td", { class: "num" }, d.n), h("td", { class: "num" }, d.terminal != null ? pctS(d.terminal) : ""), h("td", { class: "num" }, d.last_ms != null ? Math.round(d.last_ms) : ""), h("td", { class: "num" }, d.ep_med_ms != null ? Math.round(d.ep_med_ms) : ""))))));
    }
    if (F.transitions?.length) el.append(h("h4", {}, b2("trans")), h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...b2("transCols").map((c) => h("th", {}, c))), ...F.transitions.slice(0, 20).map((x) => h("tr", {}, h("td", {}, h("code", {}, x.from)), h("td", {}, h("code", {}, x.to)), h("td", { class: "num" }, x.n), h("td", { class: "num" }, pctS(x.share)))))));
    { const it = internalTable(); if (it) el.append(it); }
    return el;
  };

  views.rules = () => {
    const el = h("div", {}, h("h3", {}, b2("rulesHead")));
    const R = M.annotations?.rules ?? [];
    if (!R.length) { el.append(h("div", { class: "notice" }, b2("noRules"))); return el; }
    el.append(h("div", { class: "tw" }, h("table", {}, h("tr", {}, ...b2("rulesCols").map((c) => h("th", {}, c))), ...R.map((r) => h("tr", {}, h("td", {}, r.text), h("td", { class: "muted" }, r.source ?? ""), h("td", {}, ...(r.endpoints ?? []).map((x) => chip(x))), h("td", {}, ...(r.tables ?? []).map((x) => chip(x))))))));
    return el;
  };

  // ---------- router + language ----------
  const tabKeys = ["now", "overview", "map", M.storyboards.length ? "storyboard" : null, (M.bench?.flows?.clients || M.bench?.internal?.length) ? "flows" : null, "endpoints", "data", "rules", "screens"].filter(Boolean);
  const content = $("#content");
  let current = { name: "overview", arg: undefined };
  const renderNav = () => { const nav = $("nav"); nav.innerHTML = ""; nav.append(...tabKeys.map((k) => h("a", { href: "#" + k, "data-v": k, class: current.name === k ? "on" : "", onclick: (e) => { e.preventDefault(); show(k); } }, t("tabs")[k]))); };
  const show = (name, arg) => {
    current = { name, arg };
    renderNav();
    content.innerHTML = "";
    content.append(views[name](arg));
    location.hash = arg ? `${name}/${encodeURIComponent(arg)}` : name;
    window.scrollTo(0, 0);
  };
  const setLang = (l) => { lang = l; localStorage.setItem("isucon-report-lang", l); document.documentElement.lang = l; for (const b of document.querySelectorAll("header .lang button")) b.classList.toggle("on", b.dataset.lang === l); show(current.name, current.arg); };
  for (const b of document.querySelectorAll("header .lang button")) { b.classList.toggle("on", b.dataset.lang === lang); b.addEventListener("click", () => setLang(b.dataset.lang)); }
  document.documentElement.lang = lang;
  const [v0, a0] = location.hash.slice(1).split("/");
  show(views[v0] && tabKeys.includes(v0) ? v0 : M.bench ? "now" : "overview", a0 ? decodeURIComponent(a0) : undefined);
})();
