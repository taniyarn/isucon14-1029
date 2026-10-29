#!/bin/bash
# Apply every webapp/sql/indexes/*.sql to the app DB (and *.pdns.sql to the PowerDNS DB), idempotently:
# duplicate-index errors (1061) are skipped, everything else fails loudly. Prints ok / skip / NG per statement.
# Called from deploy.sh (after a deploy). Add `bash $(dirname $0)/indexes/apply.sh` to the end of webapp/sql/init.sh
# ONLY if /initialize drops/recreates tables (DROP TABLE / CREATE TABLE in init.sql); when it only TRUNCATEs, the
# indexes survive and the call is unnecessary (kickoff phase 2 decides this).
# Index groups in parallel-fix only ADD files here (one per group) — nobody edits init.sh or schema files, so no conflicts.
# Connection: /home/isucon/env.sh (+ env.db.sh) picked by variable-name pattern; pdns creds from /etc/powerdns/*.conf.
set -u
cd "$(dirname "$0")"
for f in /home/isucon/env.sh /home/isucon/env.db.sh; do [ -f "$f" ] && { set -a; . "$f"; set +a; }; done
pick() { env | grep -E "$1" | head -1 | cut -d= -f2-; }
H=${DB_HOST:-$(pick '^[A-Z0-9_]*(DB|MYSQL)[A-Z0-9_]*(HOST|ADDRESS)=')}; H=${H:-127.0.0.1}
P=${DB_PORT:-$(pick '^[A-Z0-9_]*(DB|MYSQL)[A-Z0-9_]*PORT=')}; P=${P:-3306}
U=${DB_USER:-$(pick '^[A-Z0-9_]*(DB|MYSQL)[A-Z0-9_]*USER=')}; U=${U:-isucon}
W=${DB_PASS:-$(pick '^[A-Z0-9_]*(DB|MYSQL)[A-Z0-9_]*PASS[A-Z0-9_]*=')}; W=${W:-isucon}
N=${DB_NAME:-$(pick '^[A-Z0-9_]*(DB|MYSQL)[A-Z0-9_]*(DBNAME|DATABASE|NAME)=')}
# pdns (gmysql backend) — only when its config is readable here (the DB host)
PH=$(grep -hsE '^gmysql-host=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf 2>/dev/null | head -1 | cut -d= -f2); PH=${PH:-127.0.0.1}
PU=$(grep -hsE '^gmysql-user=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf 2>/dev/null | head -1 | cut -d= -f2)
PW=$(grep -hsE '^gmysql-password=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf 2>/dev/null | head -1 | cut -d= -f2)
PN=$(grep -hsE '^gmysql-dbname=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf 2>/dev/null | head -1 | cut -d= -f2)
ok=0; skip=0; ng=0
run_file() { # file host port user pass db
  local f=$1 h=$2 p=$3 u=$4 w=$5 n=$6
  while IFS= read -r stmt; do
    [ -n "$stmt" ] || continue
    out=$(MYSQL_PWD="$w" mysql -h"$h" -P"$p" -u"$u" "$n" -e "$stmt" 2>&1 | grep -v '^mysql: \[Warning\]')
    case "$out" in
      "") echo "ok   $f: ${stmt:0:90}"; ok=$((ok+1));;
      *"1061"*|*"Duplicate key name"*|*"1060"*|*"Duplicate column name"*|*"1050"*|*"already exists"*|*"1091"*|*"check that column/key exists"*) echo "skip $f: already applied (${stmt:0:70})"; skip=$((skip+1));;
      *) echo "NG   $f: $out (${stmt:0:90})"; ng=$((ng+1));;
    esac
  done < <(grep -vE '^\s*(--|$)' "$f")
}
for f in *.sql; do
  [ -f "$f" ] || continue
  case "$f" in
    *.pdns.sql)
      if [ -n "$PN" ] && [ -n "$PU" ]; then run_file "$f" "$PH" 3306 "$PU" "$PW" "$PN"; else echo "skip $f: pdns config not on this host"; fi;;
    *)
      [ -n "$N" ] || { echo "NG   $f: cannot determine the app DB name from env.sh (set DB_NAME)"; ng=$((ng+1)); continue; }
      run_file "$f" "$H" "$P" "$U" "$W" "$N";;
  esac
done
echo "indexes: $ok applied, $skip already present, $ng failed"
[ "$ng" = 0 ]
