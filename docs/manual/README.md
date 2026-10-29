# docs/manual — 当日マニュアルとアプリケーションマニュアルの置き場

競技開始直後、kickoff ① で Claude が URL から取って保存する（ログインが要るポータルの場合は本文を貼る）:

- `day.md` — 当日マニュアル（規約、禁止事項、hosts / DNS、初期化、再起動の扱い）
- `app.md` — アプリケーションマニュアル（スコア式、整合性の猶予、ユーザー種別）

その後 Claude に「kickoff skill で docs/manual/ と repo を読んで kickoff を実行して」。要点は `docs/kickoff.md` にまとまる。
過去問の練習では公式 repo の `docs/` の URL をここに書くだけで良い（例: ISUCON13 は `cautionary_note.md` と `isupipe.md`）。
