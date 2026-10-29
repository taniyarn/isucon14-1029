#!/bin/bash
# Read-only diagnosis of a contest host (task host:diag). Env: UNIT (app unit, auto-detected), NAME (a DNS name to resolve locally)
echo "## units"; systemctl list-units --type=service --no-legend --plain | grep -Ei "isu|nginx|mysql|maria|postgres|redis|pdns|memcache"
echo "## enabled but inactive"
for u in $(systemctl list-unit-files --state=enabled --no-legend | awk '{print $1}' | grep -Ei "isu|pdns|redis"); do [ "$(systemctl is-active "$u")" = active ] || echo "$u: $(systemctl is-active "$u")"; done
# with sudo, ss shows every socket's owner: "who holds :8080" (another language's unit grabbing the app port)
echo "## listening (address  process[pid])"
(sudo -n ss -ltnp 2>/dev/null || ss -ltnp) | awk 'NR>1 { p=$6; out=""; n=0
  while (match(p, /"[^"]+",pid=[0-9]+/)) { s=substr(p, RSTART, RLENGTH); p=substr(p, RSTART+RLENGTH); gsub(/"/, "", s); sub(/,pid=/, "[", s); n++; if (n <= 3) out=out (out ? " " : "") s "]" }
  if (n > 3) out=out " +" (n-3); print $4, (out ? out : "?") }' | sort -u
U="${UNIT:-}"
if [ -z "$U" ]; then # auto: the running app unit, preferring the Go one (not isumeasure / helpers)
  run=$(systemctl list-units --type=service --state=running --no-legend --plain | awk '{print $1}' | grep -i isu | grep -v isumeasure)
  U=$(echo "$run" | grep -E -- '-go(lang)?\.service$' | head -1); [ -n "$U" ] || U=$(echo "$run" | head -1)
fi
# ExecStop=kill $MAINPID fails with an empty MAINPID and floods the journal with "kill[..]: usage" lines: drop them
echo "## journal of $U (last 30, kill/usage noise dropped)"
J=$(sudo journalctl -u "$U" -n 400 --no-pager 2>/dev/null)
noise=$(printf '%s\n' "$J" | grep -cE ' kill\[[0-9]+\]: |kill: usage:|kill -l \[|^\s+(or )?kill ')
printf '%s\n' "$J" | grep -vE ' kill\[[0-9]+\]: |kill: usage:|kill -l \[|^\s+(or )?kill ' | tail -30
[ "${noise:-0}" -gt 0 ] && echo "   ($noise kill/usage lines hidden)"
echo "## dns"; N="${NAME:-}"; [ -n "$N" ] && { dig +short +time=2 +tries=1 @127.0.0.1 "$N" 2>/dev/null || echo "(dig failed)"; }
echo "## etc modes (actual vs repo etc/MODES.txt)"
for f in $(awk '{print $4}' /home/isucon/etc/MODES.txt 2>/dev/null | grep -E 'nginx.conf$|zz-|pdns.conf|mysqld.cnf' | head -12); do
  printf "%s  actual=%s  repo=%s\n" "$f" "$(stat -c '%a %U:%G' "$f" 2>/dev/null)" "$(grep " $f$" /home/isucon/etc/MODES.txt | cut -d' ' -f1-3)"
done
if command -v mysql >/dev/null && sudo mysql -Nse 'SELECT 1' >/dev/null 2>&1; then
  echo "## mysql variables"; sudo mysql -Nse "SHOW VARIABLES WHERE Variable_name IN ('innodb_flush_log_at_trx_commit','sync_binlog','log_bin','max_connections','innodb_buffer_pool_size','slow_query_log','long_query_time','bind_address')" | awk '{printf "  %s=%s\n", $1, $2}'
  echo "## mysql connections"; sudo mysql -Nse "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Max_used_connections','Connections')" | awk '{printf "  %s=%s\n", $1, $2}'
  echo "## indexes (non-system schemas)"; sudo mysql -Nse "SELECT CONCAT('  ', table_schema, '.', table_name, '  ', index_name, '(', GROUP_CONCAT(column_name ORDER BY seq_in_index), ')') FROM information_schema.statistics WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema') GROUP BY table_schema, table_name, index_name ORDER BY 1"
fi
echo "## disk / load"; df -h / | tail -1; uptime
