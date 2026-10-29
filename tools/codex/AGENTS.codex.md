## Codex で動くとき（Claude Code との違い）

このファイル（`AGENTS.md`）は `task agents:md` が `CLAUDE.md` とこの節の元（`tools/codex/AGENTS.codex.md`）から作る。**直接編集しない**。当日の値（`CLAUDE.md` 冒頭の「Fill in on contest day」。kickoff が書く）は **`CLAUDE.md` が正**: `task agents:md CHECK=1` が 1 で終わったら古いので、`CLAUDE.md` の該当節を読む（ハブが `task agents:md` で作り直して commit する）。上の本文の「Claude」「session」は Codex の自分にも当てはまる。違うのは次の表だけ。レーンでの回し方は `docs/lanes/README.md` §12。

| Claude Code では | Codex ではこうする |
|---|---|
| skill（レーンのファイルの「〜 skill」） | 下の表の `SKILL.md` を読み、その手順に従う（中の Claude 専用の道具はこの表で読み替える） |
| `@docs/…` の自動読み込み | 上の **Read first** の行のファイルを、仕事の前に自分で読む |
| `SendMessage` / `ListAgents` / セッション名 | 無い。合図は **`task lane:msg FROM=<自分> TO=<宛先> MSG="<docs/MESSAGES.md の 1 行>"`**（宛先はレーン番号。名前は要らない）。`[HELLO]` も `task lane:msg FROM=3 TO=1 MSG="[HELLO] 3 codex"`。ハブとの間だけ（ハブ・アンド・スポーク） |
| メッセージが来るとターンが起きる | 起きない。**手順の始めと終わり、待ちに入る前に `task lane:inbox LANE=<自分>`**。待つ間は `task lane:wait-msg LANE=<自分> TIMEOUT=300` をフォアグラウンドで打ち、`⏳`（exit 2）なら `task lane:board` を見てもう一度 |
| `run_in_background` の完了で起きる（`lane:wait`、`runs:wait`、`hub:ship`） | 起きない。人の todo を待つときは `task lane:wait ID=<id> TIMEOUT=300` をフォアグラウンドで繰り返す（間に `task lane:inbox`）。1 分を超える `task` もフォアグラウンドで終わるまで待つ |
| サブエージェント（Agent ツール、`isolation: "worktree"`、`model:`） | parallel-setup / parallel-fix のグループは **1 つずつ順番に**: `task wt:add NAME=<group>` → 実装 → build / vet → commit、全部終わったら `integ/<ts>` に `--no-ff` で merge（skill の手順どおり）。Codex 自身の multi_agent 機能はあるが（`codex features list`）、worktree の分離とモデル指定の効き方は未確認なので当日は使わない。`lane:status … AGENTS=` は付けない |
| `.claude/settings.json` の許可 / 許可の prompt | Codex は起動のフラグで決まる（下）。ルール 12（`task` / `gh` / `git` は単体で打つ）はそのまま守る。拒否されたら別の形で打ち直さず、人に `CMD="! <コマンド>"` の todo |
| hooks（`cc-events.jsonl`、`lane:timeline` の Claude Code 節） | Codex では書かれない。**計測は盤のイベントだけ**なので `task lane:status` を手順の始め・終わり・待ちで必ず打つ（`lane:timeline` の時間配分と止まっていた時間はそこから出る） |
| `/fast`、`ListAgents` で名前を出す | しない。ハブが Codex なら名前は `lane-1`（2 / 3 / c は `task lane:msg` で送るので名前を使わない） |

**起動**（`tools/lanes/sessions.json` の `templates.codex`。`codex --help` 0.165.0 で確かめたフラグ）:

```
LANE=<lane> codex --model <model> --approve-for-me -c sandbox_workspace_write.network_access=true --add-dir "$PWD/.." '<起動の 1 行>'
```

- `--approve-for-me`: workspace-write の sandbox で動き、承認の要求は人ではなく自動レビューに回る（人の prompt で止まらない）。`--model` / `-m`、`-c key=value`、`--add-dir`、`[PROMPT]`（最初の指示を引数で渡す）も `codex --help` にある。
- `sandbox_workspace_write.network_access=true`: `gh`、`git push`、`task deploy` / `task smoke`（ssh、HTTP）に要る。キー名は codex のバイナリの設定項目で確認、効き方は未確認。
- `--add-dir "$PWD/.."`: 隣の worktree（`../wt-*`）に書けるように。**リスク: repo の親ディレクトリ全体が書ける**（他の repo も）。
- 未確認: workspace-write で `.git` が書き込み禁止になるか。なるなら `git commit` / `git worktree add` / 盤（`.git/lanes/`）の書き込みは自動レビュー経由の昇格になる。**`task lane:status` が permission で落ちる、または自動レビューが `git commit` を拒否し続けるなら、人に返答で伝え**、人が `--approve-for-me` 以下を `--dangerously-bypass-approvals-and-sandbox` に替えて起動し直す（リスク: 全コマンドを確認なし・sandbox なしで実行する。この repo と `../wt-*` 以外を触らないことは指示でしか守れない）。
- 未確認: `codex queue --thread <session> --message <text>`（ヘルプにある）で動いている Codex のセッションに外から 1 ターン送れるか。使えれば wait-msg の代わりになるが、当日は使わない。
- Codex の hooks（`codex features list` の `hooks`）、プロジェクトの `.codex/config.toml`、`.agents/skills` はバイナリに名前があるが、形式を確かめていないので kit では使わない。

**skill の場所**（`.claude/skills/<name>` → 実体。Codex は `SKILL.md` を読む）:

{{SKILLS}}
