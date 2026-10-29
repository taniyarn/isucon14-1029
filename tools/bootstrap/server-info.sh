#!/bin/bash
# Collect everything bootstrap/measurement tooling needs to know about a contest host.
# Run:  ssh -i <key> <user>@<host> 'sudo bash -s' < tools/bootstrap/server-info.sh
# Read-only. Output is plain text; paste it back or save it as docs/server-info.md.
set -u
sec() { echo; echo "## $1"; }

sec "host"
echo "hostname: $(hostname)  user: $(whoami)  date: $(date -Is)"
grep -E '^(PRETTY_NAME|VERSION_ID)=' /etc/os-release
echo "kernel: $(uname -r)  arch: $(uname -m)"
echo "cpu: $(nproc) cores  mem: $(free -m | awk '/Mem:/ {print $2}') MB  disk: $(df -h / | awk 'NR==2 {print $2" total, "$4" free"}')"
echo "private ip(s): $(hostname -I)"

sec "isucon home"
ls -la /home/isucon 2>/dev/null | head -30
echo "--- env files"
for f in /home/isucon/env.sh /home/isucon/.env /home/isucon/webapp/env.sh; do [ -f "$f" ] && { echo "[$f]"; sed 's/\(PASS[A-Z_]*=\).*/\1***/' "$f"; }; done
echo "--- webapp dirs"
ls /home/isucon/webapp 2>/dev/null
echo "--- toolchains"
ls /home/isucon/local 2>/dev/null
for g in /home/isucon/local/golang/bin/go /usr/local/go/bin/go /usr/bin/go; do [ -x "$g" ] && { echo "go: $g $($g version)"; break; }; done

sec "services (systemd) — all running, system noise removed"
systemctl list-units --type=service --state=running --no-pager --no-legend | awk '{print $1}' | grep -Ev '^(systemd-|dbus|cron|rsyslog|snapd|ssh|polkit|udisks|unattended|apparmor|networkd|resolved|chrony|getty|serial-getty|ModemManager|multipathd|packagekit|irqbalance|amazon-ssm|acpid|cloud-|apport|console-setup|keyboard-setup|setvtrgb|plymouth|user@|fwupd|thermald|ec2)' | while read -r u; do echo "$u: $(systemctl is-enabled "$u" 2>/dev/null) / $(systemctl is-active "$u" 2>/dev/null)"; done
echo "--- disabled units that look app-related"
systemctl list-unit-files --type=service --no-legend | awk '$2=="disabled" {print $1}' | grep -Ei 'isu|app|web|api|worker|mock|payment' | tr '\n' ' '; echo

sec "middleware detection (by process / port / config dir)"
detect() { # name, process regex, config dirs...
  local name=$1 re=$2; shift 2
  local pids; pids=$(pgrep -f -d, "$re" 2>/dev/null)
  local cfg=""; for d in "$@"; do [ -e "$d" ] && cfg="$cfg $d"; done
  if [ -n "$pids" ] || [ -n "$cfg" ]; then printf '%-12s running:%-5s config:%s\n' "$name" "$([ -n "$pids" ] && echo yes || echo no)" "${cfg:- -}"; fi
}
detect nginx      '^nginx: master'           /etc/nginx/nginx.conf /etc/nginx/sites-enabled /etc/nginx/conf.d
detect apache2    'apache2|httpd'            /etc/apache2 /etc/httpd
detect h2o        '\bh2o\b'                /etc/h2o
detect caddy      '\bcaddy\b'              /etc/caddy
detect envoy      '\benvoy\b'              /etc/envoy
detect haproxy    '\bhaproxy\b'            /etc/haproxy
detect traefik    '\btraefik\b'            /etc/traefik
detect mysql      'mysqld'                   /etc/mysql/mysql.conf.d /etc/mysql/my.cnf
detect mariadb    'mariadbd'                 /etc/mysql/mariadb.conf.d
detect postgresql 'postgres'                 /etc/postgresql
detect redis      'redis-server'             /etc/redis
detect valkey     'valkey-server'            /etc/valkey
detect memcached  'memcached'                /etc/memcached.conf
detect pdns       'pdns_server'              /etc/powerdns
detect varnish    'varnishd'                 /etc/varnish
echo "--- sqlite files (app data or middleware backends: ISUCON12-style tenant DBs, pdns gsqlite3, ...)"
find /home/isucon /var/lib /opt /srv -xdev -maxdepth 5 -type f \( -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3' \) -size +8k -printf '%10s %p\n' 2>/dev/null | head -15
for f in $(find /home/isucon /var/lib /opt /srv -xdev -maxdepth 5 -type f \( -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3' \) -size +8k 2>/dev/null | head -5); do head -c 16 "$f" 2>/dev/null | grep -q 'SQLite format' && echo "NOTE: SQLite database: $f ($(stat -c %s "$f") bytes)"; done
echo "--- backend of each detected middleware (where does its data live?)"
if [ -d /etc/powerdns ]; then
  grep -hE '^(launch|gmysql-host|gmysql-dbname|gsqlite3-database|gpgsql-host|bind-config|cache-ttl|query-cache-ttl)=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf 2>/dev/null | sed 's/^/pdns: /'
  grep -qsE '^launch=.*gsqlite3' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf && echo "NOTE: PowerDNS uses SQLite ($(grep -hsE '^gsqlite3-database=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf | head -1))"
  grep -qsE '^launch=.*gmysql' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf && echo "NOTE: PowerDNS queries MySQL (db $(grep -hsE '^gmysql-dbname=' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/*.conf | head -1 | cut -d= -f2)): DNS load lands on the DB"
fi
echo "--- processes with SQLite/DB files open right now (runtime truth, independent of configs)"
for pd in /proc/[0-9]*; do
  pid=${pd#/proc/}; n=$(cat "$pd/comm" 2>/dev/null) || continue
  ls -l "$pd/fd" 2>/dev/null | grep -oE ' -> .*\.(db|sqlite|sqlite3)(-wal|-shm|-journal)?$' | sed "s/^ -> //" | sort -u | while read -r f; do echo "NOTE: $n (pid $pid) has open: $f"; done
done 2>/dev/null | sort -u | head -20
echo "--- packages that hint at storage / middleware"
dpkg -l 2>/dev/null | awk '/^ii/ {print $2}' | grep -Ei 'sqlite|pdns|powerdns|redis|valkey|memcached|mysql-server|mariadb-server|postgresql-[0-9]|varnish|h2o|caddy' | tr '\n' ' '; echo
echo "--- env.sh keys (values hidden): DB / driver hints"
[ -f /home/isucon/env.sh ] && sed -E 's/=.*//' /home/isucon/env.sh | grep -v '^#' | tr '\n' ' '; echo
echo "--- DB / cache connection hints in env"
grep -hEi 'DB_|DATABASE|MYSQL|POSTGRES|PG|REDIS|MEMCACHE|DSN' /home/isucon/env.sh /home/isucon/.env /home/isucon/webapp/env.sh 2>/dev/null | sed 's/\(PASS[A-Z_]*=\).*/\1***/' | sort -u
echo "--- unit files"
for u in /etc/systemd/system/*isu*.service /etc/systemd/system/*payment*.service /etc/systemd/system/*mock*.service; do [ -f "$u" ] && { echo "[$u]"; grep -Ev '^\s*(#|$)' "$u"; }; done

sec "listening ports"
ss -ltnp | awk 'NR==1 || /LISTEN/' | sed 's/users:((//; s/))//' | cut -c1-140

sec "nginx"
command -v nginx >/dev/null && nginx -v 2>&1 || echo "nginx: not installed"
echo "--- conf layout"
ls -la /etc/nginx/ | awk '{print $NF}' | tr '\n' ' '; echo
ls /etc/nginx/sites-enabled /etc/nginx/conf.d 2>/dev/null
echo "--- http block includes / log settings (nginx.conf)"
grep -nE 'include|access_log|log_format|worker_|keepalive|gzip' /etc/nginx/nginx.conf | head -30
echo "--- site files (non-comment lines)"
for f in /etc/nginx/sites-enabled/* /etc/nginx/conf.d/*.conf; do [ -f "$f" ] && { echo "[$f]"; grep -Ev '^\s*(#|$)' "$f" | sed 's/^\s*//' | head -60; }; done
echo "--- log files"
ls -la /var/log/nginx/ 2>/dev/null

sec "postgresql (if any)"
if command -v psql >/dev/null; then psql --version; ls /etc/postgresql/*/main 2>/dev/null; sudo -u postgres psql -Atc "SHOW log_min_duration_statement" 2>/dev/null | sed 's/^/log_min_duration_statement: /'; else echo "not installed"; fi

sec "mysql / mariadb"
command -v mysql >/dev/null && mysql --version 2>&1 | head -1 || echo "mysql client: not installed"
echo "--- conf layout"
ls /etc/mysql /etc/mysql/mysql.conf.d /etc/mysql/conf.d 2>/dev/null
echo "--- effective settings (non-comment)"
for f in /etc/mysql/my.cnf /etc/mysql/mysql.conf.d/*.cnf /etc/mysql/conf.d/*.cnf; do [ -f "$f" ] && { echo "[$f]"; grep -Ev '^\s*(#|$|!)' "$f" | head -40; }; done
echo "--- can we query as root via socket?"
if mysql -e 'SELECT VERSION()' >/dev/null 2>&1; then
  mysql -e "SHOW VARIABLES WHERE Variable_name IN ('slow_query_log','slow_query_log_file','long_query_time','log_output','innodb_buffer_pool_size','max_connections','datadir','socket','port','bind_address')"
  echo "--- databases / table sizes"
  mysql -e "SELECT table_schema, table_name, table_rows, ROUND((data_length+index_length)/1024/1024,1) AS mb FROM information_schema.tables WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema') ORDER BY mb DESC LIMIT 30"
  echo "--- users"
  mysql -e "SELECT user, host FROM mysql.user"
else
  echo "mysql root via socket: NOT available (try: mysql -uisucon -pisucon)"
fi
echo "--- log files"
ls -la /var/log/mysql/ 2>/dev/null

sec "app"
echo "--- processes"
ps -eo pid,user,etime,rss,cmd | grep -Ei 'isu|webapp|payment|mock' | grep -v grep | cut -c1-140
echo "--- app log (journal, last 5 lines of each isu* unit)"
for u in $(systemctl list-units --type=service --no-legend | awk '{print $1}' | grep -i isu); do echo "[$u]"; journalctl -u "$u" -n 5 --no-pager 2>/dev/null | cut -c1-160; done

sec "measurement tools present?"
for b in alp slp pt-query-digest sar sadf dstat duckdb netdata perf; do printf '%-16s %s\n' "$b" "$(command -v $b 2>/dev/null || echo -)"; done
echo "sysstat: $(dpkg -l sysstat 2>/dev/null | awk '/^ii/ {print $3}' || echo -)"

sec "kernel / limits"
sysctl net.core.somaxconn net.ipv4.ip_local_port_range net.ipv4.tcp_tw_reuse fs.file-max 2>/dev/null
echo "nofile (isucon): $(sudo -u isucon bash -c 'ulimit -n' 2>/dev/null)"
grep -E 'LimitNOFILE' /etc/systemd/system/*isu*.service 2>/dev/null

sec "git state of /home/isucon"
[ -d /home/isucon/.git ] && (cd /home/isucon && git remote -v && git log --oneline | head -3) || echo "not a git repo"
echo "--- large files (>1MB) under /home/isucon (top 20)"
find /home/isucon -xdev -type f -size +1M -not -path '*/local/*' -not -path '*/.cache/*' -printf '%10s %p\n' 2>/dev/null | sort -rn | head -20
echo
echo "## done"
