#!/usr/bin/env python3
"""task index:check — does a new secondary index turn existing write paths into deadlocks?

Same trap three times: icons(user_id) (2026-10-25, POST /api/icon 500 x123), coupons(code) (2026-10-27) and
coupons(used_by) (2026-10-28, POST /api/app/users and /api/app/rides 5xx, bench -14%). A new index gives InnoDB a
narrower path to lock: locking statements on that table that filter by the indexed column now take next-key / gap
locks on the new index, and every INSERT into the table (or UPDATE that moves the indexed value) needs an insert
intention lock in those gaps. Two concurrent transactions that both hold a gap and both insert deadlock (1213).
The sequential `task smoke` never shows it; the bench does.

Usage:
  indexcheck.py [--code-dir webapp/go] FILE.sql ...            index files on disk, code from the work tree
  indexcheck.py --ref <rev> [--base origin/main] [FILE ...]    the files added/changed under webapp/sql/indexes/
                                                                 in base...rev (added lines only), code at <rev>
Exit: 0 no risk, 2 risk found (printed with file:line and the remedies), 1 usage / git error.

Rules, per (table T, leading column C) of every ADD INDEX / CREATE INDEX (UNIQUE too; PRIMARY KEY ignored):
  "gap holder"  = SELECT … FOR UPDATE / FOR SHARE / LOCK IN SHARE MODE, UPDATE, DELETE on T whose WHERE names C,
                  in a function that runs a transaction (Begin*/a *sqlx.Tx or *sql.Tx parameter)
  "inserter"    = INSERT / REPLACE INTO T, or UPDATE T SET C = … (moves the entry in the new index)
  No inserter of T anywhere in the code → no risk (read-only / fixed tables, e.g. livestream_tags(livestream_id)
  only read by livestream_id; reservation_slots only updated).
  (a) DELETE FROM T WHERE …C… followed by INSERT INTO T in the same function (replace-by-delete)
  (b) locking SELECT on T WHERE …C… in a function that also inserts (into T, or into any table)
  (c) an inserter of T while a gap holder on T(C) exists in a transaction elsewhere (each pair printed)
  (d) UPDATE T … WHERE …C… in a transaction, with an inserter of T somewhere
"""
import argparse
import os
import re
import subprocess
import sys

REMEDIES = """remedies (docs/IMPLEMENTATION-NOTES.md §5):
  - make the DELETE / UPDATE / locking read hit the PRIMARY KEY: SELECT the ids first (no FOR UPDATE), then
    DELETE … WHERE id IN (…) / UPDATE … WHERE id = ?
  - lock a parent row instead (e.g. the users row: SELECT … FROM users WHERE id = ? FOR UPDATE), then count / insert
  - INSERT … ON DUPLICATE KEY UPDATE instead of delete + insert
  - retry the transaction on deadlock (MySQL error 1213) — and watch the report's Errors section on the next run
  - or leave this index out
After fixing, the PR body gets a line `index-check: reviewed` (hub:ship holds the PR without it)."""

IDX_RE = re.compile(r"\bINDEX\b|\bKEY\b", re.I)
ALTER_RE = re.compile(r"ALTER\s+TABLE\s+`?(\w+)`?\s+(.*)", re.I | re.S)
ADD_RE = re.compile(r"ADD\s+(UNIQUE\s+)?(?:INDEX|KEY)\s*(?:IF\s+NOT\s+EXISTS\s+)?`?(\w*)`?\s*\(([^;]*?)\)\s*(?:,|$|USING|COMMENT|ALGORITHM|LOCK)", re.I | re.S)
CREATE_RE = re.compile(r"CREATE\s+(UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?`?(\w+)`?\s+ON\s+`?(\w+)`?\s*\(([^;]*?)\)\s*(?:;|$|USING|COMMENT|ALGORITHM|LOCK)", re.I | re.S)


def parse_indexes(sql_text):
    """[(table, [cols], name)] of the ADD INDEX / CREATE INDEX statements (comments stripped)."""
    text = re.sub(r"--[^\n]*|#[^\n]*", "", sql_text)
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    out = []
    for stmt in text.split(";"):
        stmt = stmt.strip()
        m = ALTER_RE.match(stmt)
        if m:
            for a in ADD_RE.finditer(m.group(2) + "\n"):
                out.append((m.group(1).lower(), cols_of(a.group(3)), a.group(2)))
            continue
        m = CREATE_RE.search(stmt + "\n")
        if m:
            out.append((m.group(3).lower(), cols_of(m.group(4)), m.group(2)))
    return [x for x in out if x[1]]


def cols_of(s):
    cols = []
    for part in s.split(","):
        part = part.strip().strip("`")
        m = re.match(r"`?(\w+)`?", part)
        if m:
            cols.append(m.group(1).lower())
    return cols


# ---------- Go code: functions → SQL literals ----------
STR_RE = re.compile(r'"((?:[^"\\\n]|\\.)*)"|`([^`]*)`', re.S)
SQL_START = re.compile(r"^\s*(SELECT|INSERT|REPLACE|UPDATE|DELETE|WITH)\b", re.I)
TX_RE = re.compile(r"\.Begin(?:x|Tx|Txx)?\s*\(|\*sqlx\.Tx\b|\*sql\.Tx\b")
RETRY_RE = re.compile(r"\b1213\b|[Dd]eadlock")


def functions(src):
    """[(name, start_line, text)] split at top-level `func` lines."""
    lines = src.split("\n")
    starts = [i for i, l in enumerate(lines) if l.startswith("func ")]
    out = []
    for k, i in enumerate(starts):
        j = starts[k + 1] if k + 1 < len(starts) else len(lines)
        m = re.match(r"func\s+(?:\([^)]*\)\s*)?(\w+)", lines[i])
        out.append((m.group(1) if m else "?", i + 1, "\n".join(lines[i:j])))
    return out


def sql_literals_with_lines(text, first_line):
    out = []
    lits = list(STR_RE.finditer(text))
    i = 0
    while i < len(lits):
        m = lits[i]
        s = m.group(1) if m.group(1) is not None else m.group(2)
        j = i
        while j + 1 < len(lits) and re.fullmatch(r"\s*\+\s*", text[lits[j].end():lits[j + 1].start()]):
            j += 1
            n = lits[j]
            s += n.group(1) if n.group(1) is not None else n.group(2)
        i = j + 1
        if SQL_START.match(s):
            out.append((first_line + text.count("\n", 0, m.start()), " ".join(s.split())))
    return out


def where_part(sql):
    m = re.search(r"\bWHERE\b(.*)", sql, re.I | re.S)
    return m.group(1) if m else ""


def set_part(sql):
    m = re.search(r"\bSET\b(.*?)(?:\bWHERE\b|$)", sql, re.I | re.S)
    return m.group(1) if m else ""


def names_col(text, col):
    return re.search(r"(?<![\w.])(?:\w+\.)?`?%s`?\b" % re.escape(col), text, re.I) is not None


class Stmt:
    def __init__(self, path, line, func, sql, in_tx, retry):
        self.path, self.line, self.func, self.sql, self.in_tx, self.retry = path, line, func, sql, in_tx, retry
        u = sql.upper()
        self.kind = u.split(None, 1)[0] if u else ""
        self.locking = bool(re.search(r"\bFOR\s+(UPDATE|SHARE)\b|\bLOCK\s+IN\s+SHARE\s+MODE\b", u))
        self.tables = set()
        if self.kind in ("INSERT", "REPLACE"):
            m = re.search(r"\bINTO\s+`?(\w+)`?", sql, re.I)
            if m:
                self.tables.add(m.group(1).lower())
        elif self.kind == "UPDATE":
            m = re.match(r"\s*UPDATE\s+(?:LOW_PRIORITY\s+|IGNORE\s+)*`?(\w+)`?", sql, re.I)
            if m:
                self.tables.add(m.group(1).lower())
        elif self.kind == "DELETE":
            m = re.search(r"\bFROM\s+`?(\w+)`?", sql, re.I)
            if m:
                self.tables.add(m.group(1).lower())
        else:
            for m in re.finditer(r"\b(?:FROM|JOIN)\s+`?(\w+)`?", sql, re.I):
                self.tables.add(m.group(1).lower())

    def loc(self):
        return "%s:%d (%s)" % (self.path, self.line, self.func)

    def short(self, n=110):
        return self.sql if len(self.sql) <= n else self.sql[:n - 1] + "…"


def scan_code(files):
    """files: [(path, text)] → [Stmt]"""
    out = []
    for path, src in files:
        for name, start, text in functions(src):
            in_tx = bool(TX_RE.search(text))
            retry = bool(RETRY_RE.search(text))
            for line, sql in sql_literals_with_lines(text, start):
                out.append(Stmt(path, line, name, sql, in_tx, retry))
    return out


def hits_pk(where):
    """WHERE pins the primary key (id = ? / id IN): InnoDB locks those records, no gap on the secondary index."""
    return re.search(r"(?<![\w.])(?:\w+\.)?`?id`?\s*(=|IN\b)", where, re.I) is not None


def is_write(s):
    return s.kind in ("INSERT", "REPLACE", "UPDATE", "DELETE") or s.locking


def check(indexes, stmts):
    """→ [risk dict] one per risky index: label, holders [(rule, Stmt, note)], inserters [(rule, Stmt, note)].

    A deadlock needs a cycle: a transaction that holds gap locks on the new index and then waits, against another
    that holds something and inserts into the gap. So a gap holder counts when its function writes / locks again
    after it, and an insert counts when it runs in a transaction after another write / locking read."""
    by_func = {}
    for s in stmts:
        by_func.setdefault((s.path, s.func), []).append(s)
    risks = []
    for table, cols, name in indexes:
        col = cols[0]
        on_t = [s for s in stmts if table in s.tables]
        inserters = [s for s in on_t if s.kind in ("INSERT", "REPLACE")
                     or (s.kind == "UPDATE" and any(names_col(set_part(s.sql), c) for c in cols))]
        if not inserters:
            continue  # nothing ever inserts into the gaps: read-only / fixed table
        holders = [s for s in on_t if s.in_tx and names_col(where_part(s.sql), col) and not hits_pk(where_part(s.sql))
                   and ((s.kind in ("SELECT", "WITH") and s.locking) or s.kind in ("UPDATE", "DELETE"))]
        hl, il, used = [], [], set()
        for h in holders:
            fn = by_func[(h.path, h.func)]
            after = [s for s in fn if s.line > h.line and is_write(s)]
            if not after:
                continue  # nothing held while waiting: no cycle from this site
            self_ins = [s for s in after if s in inserters]
            if self_ins:
                rule = "a" if h.kind == "DELETE" else ("b" if h.kind in ("SELECT", "WITH") else "d")
                s = self_ins[0]
                note = "%s then %s %s at :%d in the same tx (two concurrent requests deadlock)" % (
                    "replace-by-delete:" if rule == "a" else "locks by %s," % col,
                    "INSERT INTO" if s.kind in ("INSERT", "REPLACE") else "UPDATE … SET %s on" % col, table, s.line)
                used.add(id(s))
            else:
                rule = "b" if h.kind in ("SELECT", "WITH") else "d"
                note = "the tx goes on writing (:%d)" % after[0].line
            if h.retry:
                note += "; this function mentions 1213 / deadlock: a retry may cover it"
            hl.append((rule, h, note))
        if not hl:
            continue
        holder_keys = {(h.path, h.line) for _, h, _ in hl}
        for s in inserters:
            if (s.path, s.line) in holder_keys or not s.in_tx:
                continue  # an autocommit insert holds nothing before it waits
            fn = by_func[(s.path, s.func)]
            before = [x for x in fn if x.line < s.line and is_write(x)]
            if not before and id(s) not in used:
                continue
            what = "INSERT" if s.kind in ("INSERT", "REPLACE") else "UPDATE … SET %s (moves the index entry)" % col
            il.append(("c", s, what + (" after :%d in the same tx" % before[-1].line if before else "")))
        risks.append({"label": "%s(%s)" % (table, ", ".join(cols)), "table": table, "col": col, "holders": hl, "inserters": il})
    return risks


def render(risks):
    out = []
    for r in risks:
        out.append("!! %s — deadlock risk: %d gap-lock site(s) on %s, %d insert site(s) into %s"
                   % (r["label"], len(r["holders"]), r["col"], len(r["inserters"]), r["table"]))
        out.append("   gap / next-key locks via the new index (transaction):")
        for rule, s, note in r["holders"]:
            out.append("   [%s] %s  %s%s" % (rule, s.loc(), s.short(), ("   <- " + note) if note else ""))
        if r["inserters"]:
            out.append("   inserts into those gaps (insert intention locks):")
            for rule, s, note in r["inserters"]:
                out.append("   [%s] %s  %s   <- %s" % (rule, s.loc(), s.short(), note))
    return "\n".join(out)


# ---------- inputs ----------
def git(*a, cwd=None):
    r = subprocess.run(["git", *a], capture_output=True, text=True, cwd=cwd)
    if r.returncode != 0:
        raise RuntimeError("git %s: %s" % (" ".join(a), r.stderr.strip()))
    return r.stdout


def code_files_disk(code_dir):
    out = []
    for root, dirs, files in os.walk(code_dir):
        dirs[:] = [d for d in dirs if d not in ("vendor", "node_modules", ".git")]
        for f in sorted(files):
            if f.endswith(".go") and not f.endswith("_test.go"):
                p = os.path.join(root, f)
                with open(p, encoding="utf-8", errors="replace") as fh:
                    out.append((os.path.relpath(p), fh.read()))
    return out


def code_files_ref(ref, code_dir):
    out = []
    for p in git("ls-tree", "-r", "--name-only", ref, "--", code_dir).split("\n"):
        if p.endswith(".go") and not p.endswith("_test.go") and "/vendor/" not in p:
            out.append((p, git("show", "%s:%s" % (ref, p))))
    return out


def index_sql_ref(ref, base, files, idx_dir):
    """added lines of the index files changed in base...ref (or of FILEs at ref)."""
    paths = files or [p for p in git("diff", "--name-only", "--diff-filter=AM", "%s...%s" % (base, ref), "--", idx_dir).split("\n")
                      if p.endswith(".sql")]
    chunks = []
    for p in paths:
        diff = git("diff", "%s...%s" % (base, ref), "--", p)
        added = [l[1:] for l in diff.split("\n") if l.startswith("+") and not l.startswith("+++")]
        chunks.append((p, "\n".join(added) if diff else git("show", "%s:%s" % (ref, p))))
    return chunks


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("files", nargs="*")
    ap.add_argument("--code-dir", default=os.environ.get("CODE_DIR") or "webapp/go")
    ap.add_argument("--ref")
    ap.add_argument("--base", default="origin/main")
    ap.add_argument("--index-dir", default="webapp/sql/indexes")
    a = ap.parse_args(argv)
    try:
        if a.ref:
            chunks = index_sql_ref(a.ref, a.base, a.files, a.index_dir)
            code = code_files_ref(a.ref, a.code_dir)
        else:
            if not a.files:
                print("index:check: FILE=webapp/sql/indexes/<x>.sql or BRANCH=<branch> is required", file=sys.stderr)
                return 1
            chunks = []
            for p in a.files:
                with open(p, encoding="utf-8") as fh:
                    chunks.append((p, fh.read()))
            code = code_files_disk(a.code_dir)
    except (RuntimeError, OSError) as e:
        print("index:check: %s" % e, file=sys.stderr)
        return 1
    indexes = []
    for p, text in chunks:
        indexes += parse_indexes(text)
    if not indexes:
        print("index:check: no ADD INDEX / CREATE INDEX in %s — nothing to check" % (", ".join(p for p, _ in chunks) or "the diff"))
        return 0
    if not code:
        print("index:check: no .go files under %s — cannot check (CODE_DIR=…)" % a.code_dir, file=sys.stderr)
        return 1
    stmts = scan_code(code)
    risks = check(indexes, stmts)
    print("index:check: %d index(es): %s; %d SQL statements in %d Go files under %s"
          % (len(indexes), ", ".join("%s(%s)" % (t, ", ".join(c)) for t, c, _ in indexes), len(stmts), len(code), a.code_dir))
    if not risks:
        print("ok: no locking read / UPDATE / DELETE by the indexed column in a transaction on a table that is inserted into")
        return 0
    print(render(risks))
    print(REMEDIES)
    return 2


if __name__ == "__main__":
    sys.exit(main())
