#!/bin/bash
# ISUCON host bootstrap: git-ify /home/isucon, capture OS config into the repo, install measurement tools,
# enable ltsv access log + slow query log. Idempotent: safe to re-run.
#
# Usage (on the host, as root):
#   sudo env ROLE=init  LANGS=go GIT_REMOTE=git@github.com:org/repo.git bash /tmp/bootstrap/bootstrap.sh   # first host
#   sudo env ROLE=join  GIT_REMOTE=git@github.com:org/repo.git bash /tmp/bootstrap/bootstrap.sh             # other hosts
#   sudo env ROLE=local bash /tmp/bootstrap/bootstrap.sh                                                     # no remote (practice)
# PHASE (join only; default all): prep = everything that does not need the repo (apt off, tools, nginx ltsv, slow log,
#   server-info) - hosts-setup.sh runs it on svc2/svc3 while svc1 is still in init; git = only the fetch/reset from
#   origin/main (after svc1 pushed). all = both, in that order.
# Optional env: DEPLOY_KEY=/tmp/bootstrap/deploy_key (installed as /home/isucon/.ssh/id_ed25519 for github.com)
#               ISUCON_USER=isucon  SKIP_TOOLS=1  SKIP_MYSQL=1  SKIP_NGINX=1  PERCONA=1 (also install percona-toolkit, slow)
# Assumes nothing about the app: service names, ports and paths are discovered by server-info.sh, not hard-coded here.
set -u
ROLE=${ROLE:-local}
PHASE=${PHASE:-all}
case "$PHASE" in all|prep|git) ;; *) echo "PHASE must be all|prep|git"; exit 1;; esac
[ "$PHASE" = all ] || [ "$ROLE" = join ] || { echo "PHASE=$PHASE is for ROLE=join only (init/local always run all)"; exit 1; }
LANGS=${LANGS:-go}
GIT_REMOTE=${GIT_REMOTE:-}
DEPLOY_KEY=${DEPLOY_KEY:-}
U=${ISUCON_USER:-isucon}
HOME_DIR=$(getent passwd "$U" | cut -d: -f6)
HERE=$(cd "$(dirname "$0")" && pwd)
ALP_VERSION=${ALP_VERSION:-v1.0.21}
ok()   { printf 'OK   %s\n' "$*"; }
ng()   { printf 'NG   %s\n' "$*"; FAILED=$((FAILED+1)); }
info() { printf -- '--   %s\n' "$*"; }
FAILED=0
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)"; exit 1; }
[ -d "$HOME_DIR" ] || { echo "user $U not found"; exit 1; }
cd "$HOME_DIR"

# step <key> <title> [label]: prints "## <key>. <title> (+Ns since start)" and records how long the previous step took,
# so the last line is one summary per host ("## timing ...: total 125s (1b apt 93s, 2 tools 1s, ...)").
T0=$SECONDS; _sk=""; _st=0; TIMES=""
_step_end() { [ -n "$_sk" ] && TIMES="$TIMES${TIMES:+, }$_sk $((SECONDS-_st))s"; _sk=""; }
step() { _step_end; _sk="$1 ${3:-${2%% *}}"; _st=$SECONDS; echo "## $1. $2 (+$((SECONDS-T0))s)"; }
# apt/dpkg lock: 1b no longer waits for a running unattended-upgrade, so whoever calls apt-get waits here (bounded).
apt_lock_wait() {
  local i=0 max=${APT_LOCK_WAIT:-90}
  while [ $i -lt $max ]; do
    if command -v fuser >/dev/null; then
      fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock >/dev/null 2>&1 || break
    else
      pgrep -x 'apt|apt-get|dpkg' >/dev/null || pgrep -f '/usr/bin/unattended-upgrade( |$)' >/dev/null || break
    fi
    [ $i = 0 ] && info "apt/dpkg lock held by a background apt; waiting up to ${max}s"
    sleep 2; i=$((i+2))
  done
  [ $i -lt $max ] || info "apt/dpkg lock still held after ${max}s; trying anyway"
}
APT_OPTS="-o DPkg::Lock::Timeout=60"
echo "## bootstrap on $(hostname) role=$ROLE phase=$PHASE langs=$LANGS user=$U home=$HOME_DIR"

# ---------- 1. git-ify ----------
step 1 "git"
if ! command -v git >/dev/null; then apt_lock_wait; apt-get install -y -q $APT_OPTS git >/dev/null 2>&1 && ok "git installed" || ng "git install"; fi
sudo -u "$U" git config --global --get user.name >/dev/null || sudo -u "$U" git config --global user.name "isucon-$(hostname)"
sudo -u "$U" git config --global --get user.email >/dev/null || sudo -u "$U" git config --global user.email "isucon@$(hostname)"
sudo -u "$U" git config --global init.defaultBranch main
sudo -u "$U" git config --global --add safe.directory "$HOME_DIR"
git config --global --add safe.directory "$HOME_DIR" 2>/dev/null

if [ -n "$DEPLOY_KEY" ] && [ -f "$DEPLOY_KEY" ]; then
  install -d -m 700 -o "$U" -g "$U" "$HOME_DIR/.ssh"
  install -m 600 -o "$U" -g "$U" "$DEPLOY_KEY" "$HOME_DIR/.ssh/id_ed25519"
  grep -q "github.com" "$HOME_DIR/.ssh/config" 2>/dev/null || printf 'Host github.com\n  IdentityFile %s/.ssh/id_ed25519\n  StrictHostKeyChecking accept-new\n' "$HOME_DIR" >> "$HOME_DIR/.ssh/config"
  chown "$U:$U" "$HOME_DIR/.ssh/config"; chmod 600 "$HOME_DIR/.ssh/config"
  ok "deploy key installed"
fi

if [ "$PHASE" != git ]; then
# .gitignore: static defaults + language dirs we don't use + anything >1MB or ELF under webapp
GI="$HOME_DIR/.gitignore"
if [ ! -f "$GI" ] || ! grep -q "isucon-bootstrap" "$GI"; then
  {
    echo "# isucon-bootstrap (generated; edit freely below the marker)"
    cat "$HERE/gitignore.base"
    for d in "$HOME_DIR"/webapp/*/; do
      l=$(basename "$d")
      case ",$LANGS," in *",$l,"*) ;; *) case "$l" in go|ruby|perl|php|python|node|nodejs|rust|java|kotlin|csharp|dotnet|scala|elixir|deno|bun) echo "/webapp/$l/";; esac;; esac
    done
    echo "# large files (>1MB) and ELF binaries found at bootstrap time"
    # large files (>1MB) and ELF binaries: a cheap find for size, and a 4-byte header check (not `file`) for executables.
    # Language dirs we do not use are already ignored above, so prune them (they hold thousands of files).
    prune=""; for d in "$HOME_DIR"/webapp/*/; do l=$(basename "$d"); case ",$LANGS," in *",$l,"*) ;; *) case "$l" in go|ruby|perl|php|python|node|nodejs|rust|java|kotlin|csharp|dotnet|scala|elixir|deno|bun) prune="$prune -path $d* -prune -o";; esac;; esac; done
    {
      eval find "$HOME_DIR" -xdev $prune -path "$HOME_DIR/.*" -prune -o -path "$HOME_DIR/local" -prune -o -path "$HOME_DIR/go" -prune -o -name node_modules -prune -o -name vendor -prune -o -name target -prune -o -name .venv -prune -o -type f -size +1M -print 2>/dev/null
      eval find "$HOME_DIR" -xdev $prune -path "$HOME_DIR/.*" -prune -o -path "$HOME_DIR/local" -prune -o -path "$HOME_DIR/go" -prune -o -name node_modules -prune -o -name vendor -prune -o -name target -prune -o -name .venv -prune -o -type f -perm -u+x -size +16k -not -name "*.sh" -not -name "*.sql" -not -name "*.go" -print 2>/dev/null | while read -r f; do [ "$(head -c 4 "$f" 2>/dev/null | tr -d '\0')" = "ELF" ] && echo "$f"; done
    } | sed "s|^$HOME_DIR/|/|" | sort -u
    echo "# --- below this line: yours ---"
  } > "$GI"
  chown "$U:$U" "$GI"
  ok ".gitignore generated ($(grep -vc '^#' "$GI") rules)"
else
  info ".gitignore already generated; left as is"
fi

# capture OS config into the repo (etc/ mirrors /etc paths)
mkdir -p etc/nginx etc/mysql etc/systemd/system etc/sysctl.d
rsync -a --delete --exclude tls /etc/nginx/ etc/nginx/ 2>/dev/null
rsync -a --delete /etc/mysql/ etc/mysql/ 2>/dev/null
for d in powerdns redis valkey memcached.conf h2o caddy haproxy varnish apache2 postgresql envoy; do [ -e "/etc/$d" ] && rsync -a --delete "/etc/$d" etc/ 2>/dev/null; done
cp -f /etc/systemd/system/*isu*.service etc/systemd/system/ 2>/dev/null
cp -f /etc/sysctl.d/*.conf etc/sysctl.d/ 2>/dev/null
cp -f /etc/hosts etc/hosts 2>/dev/null
# modes and owners are not kept by git: record them so deploy.sh can re-install files without changing permissions
# (ISUCON13: pdns.conf installed as 640 root broke pdnsutil run by the app user)
(cd / && find $(for d in etc/nginx etc/mysql etc/powerdns etc/redis etc/valkey etc/memcached.conf etc/h2o etc/caddy etc/haproxy etc/varnish etc/apache2 etc/postgresql etc/envoy etc/sysctl.d; do [ -e "/$d" ] && echo "$d"; done) -type f -printf '%m %u %g /%p\n' 2>/dev/null) > etc/MODES.txt
chown -R "$U:$U" etc
ok "etc/ captured (nginx, mysql, systemd isu*, sysctl, hosts)"

if [ ! -d .git ]; then
  sudo -u "$U" git init -q
  ok "git init"
fi
# ---------- 1b. no background apt ----------
# unattended-upgrades / apt-daily ran during a practice bench (97% of a core on the bench host, 29% on the DB host).
# mask = stays off after the organizers' reboot test too. Nothing here waits: `systemctl disable --now` / `mask --now`
# waited for a running unattended-upgrade to finish (93 s on svc1, 2026-10-25). It only has to be gone by the first
# bench: stop is queued (--no-block), the running apt gets SIGTERM (unattended-upgrade stops after the current
# package; dpkg itself is never killed), and our own apt-get calls wait for the lock (apt_lock_wait, bounded).
step 1b "stop background apt" apt
bg_apt() { pgrep -f 'unattended-upgr|apt\.systemd\.daily' >/dev/null; }
systemctl stop --no-block apt-daily.timer apt-daily-upgrade.timer apt-daily.service apt-daily-upgrade.service >/dev/null 2>&1
systemctl disable apt-daily.timer apt-daily-upgrade.timer >/dev/null 2>&1
systemctl mask apt-daily.service apt-daily-upgrade.service >/dev/null 2>&1
if systemctl list-unit-files --no-legend unattended-upgrades.service 2>/dev/null | grep -q '^unattended-upgrades'; then
  systemctl stop --no-block unattended-upgrades.service >/dev/null 2>&1
  systemctl disable unattended-upgrades.service >/dev/null 2>&1; systemctl mask unattended-upgrades.service >/dev/null 2>&1
fi
if bg_apt; then
  pkill -TERM -f 'unattended-upgr|apt\.systemd\.daily' 2>/dev/null
  info "background apt was running: SIGTERM sent, units masked; it exits on its own (re-checked in 6. verify)"
else
  ok "background apt disabled (apt-daily timers, unattended-upgrades masked)"
fi

# ---------- 2. tools ----------
step 2 "measurement tools" tools
if [ "${SKIP_TOOLS:-0}" != 1 ]; then
  # Keep this step apt-free: apt-get update alone costs 20-30 s per host. alp/slp are single static binaries;
  # duckdb (20 MB) is used on the laptop, not here (DUCKDB=1 to install); sysstat/jq are not needed by the agent.
  export DEBIAN_FRONTEND=noninteractive
  pk="rsync curl"; [ "${PERCONA:-0}" = 1 ] && pk="$pk percona-toolkit"; [ "${SYSSTAT:-0}" = 1 ] && pk="$pk sysstat"
  need=""; for p in $pk; do dpkg -s "$p" >/dev/null 2>&1 || need="$need $p"; done
  if [ -n "$need" ]; then
    apt_lock_wait
    apt-get update -q >/dev/null 2>&1
    for p in $need; do apt-get install -y -q $APT_OPTS "$p" >/dev/null 2>&1 && ok "apt: $p" || ng "apt: $p"; done
  fi
  dl() { curl -fsSL --retry 3 -o "$2" "$1"; }
  command -v alp >/dev/null || dl "https://github.com/tkuchiki/alp/releases/download/${ALP_VERSION}/alp_linux_amd64.tar.gz" /tmp/alp.tgz &
  command -v slp >/dev/null || { url=$(curl -fsSL https://api.github.com/repos/tkuchiki/slp/releases/latest | grep -o 'https://[^"]*linux_amd64[^"]*\.tar\.gz' | head -1); [ -n "$url" ] && dl "$url" /tmp/slp.tgz; } &
  if [ "${DUCKDB:-0}" = 1 ] && ! command -v duckdb >/dev/null; then dl "https://github.com/duckdb/duckdb/releases/latest/download/duckdb_cli-linux-amd64.zip" /tmp/duckdb.zip & fi
  wait
  if ! command -v alp >/dev/null; then
    [ -s /tmp/alp.tgz ] && tar -xzf /tmp/alp.tgz -C /tmp alp && install -m 755 /tmp/alp /usr/local/bin/alp && ok "alp $(alp --version 2>&1 | head -1)" || ng "alp download"
  else ok "alp present ($(alp --version 2>&1 | head -1))"; fi
  if ! command -v slp >/dev/null; then
    [ -s /tmp/slp.tgz ] && tar -xzf /tmp/slp.tgz -C /tmp slp && install -m 755 /tmp/slp /usr/local/bin/slp && ok "slp $(slp --version 2>&1 | head -1)" || ng "slp download"
  else ok "slp present"; fi
  if [ "${DUCKDB:-0}" = 1 ]; then
    if ! command -v duckdb >/dev/null; then
      [ -s /tmp/duckdb.zip ] && python3 -c "import zipfile; zipfile.ZipFile('/tmp/duckdb.zip').extract('duckdb', '/tmp')" && install -m 755 /tmp/duckdb /usr/local/bin/duckdb && ok "duckdb $(duckdb --version 2>&1 | head -1)" || ng "duckdb download"
    else ok "duckdb present"; fi
  else info "duckdb not installed on hosts (deep-dive runs on the laptop; DUCKDB=1 to install here)"; fi
  command -v pt-query-digest >/dev/null && ok "pt-query-digest present" || info "pt-query-digest absent (ok)"
  [ -x /tmp/bootstrap/isumeasure ] && install -m 755 /tmp/bootstrap/isumeasure /usr/local/bin/isumeasure && ok "isumeasure installed"
fi

# ---------- 3. nginx ltsv access log ----------
step 3 "web server" nginx
if [ "${SKIP_NGINX:-0}" != 1 ] && [ ! -f /etc/nginx/nginx.conf ]; then
  info "nginx not found; access-log setup skipped. Detected:"; pgrep -fa 'apache2|httpd|h2o|caddy|envoy|haproxy|traefik' | cut -c1-100 | sed 's/^/     /'
  info "add an ltsv-like access log for that server by hand (see docs/server-info)."
elif [ "${SKIP_NGINX:-0}" != 1 ]; then
  NC=/etc/nginx/nginx.conf
  FMT="$HERE/etc/nginx-ltsv-format.conf"
  [ -f "$NC.orig" ] || cp "$NC" "$NC.orig"
  if [ ! -s "$FMT" ]; then ng "nginx: $FMT missing or empty (is /tmp/bootstrap a nested copy? ls $HERE); access-log setup skipped"; SKIP_NGINX=1; fi
fi
if [ "${SKIP_NGINX:-0}" != 1 ] && [ -f /etc/nginx/nginx.conf ]; then
  # safe replace: only swap the file in when the new version is non-empty and still has an http block
  safe_mv() { if [ -s "$1" ] && grep -q "^http" "$1" && grep -q "events" "$1"; then mv "$1" "$2"; else ng "nginx: generated config looked wrong; not applied"; rm -f "$1"; return 1; fi; }
  if grep -q "log_format ltsv" "$NC" && ! grep -q 'upconnect:\$upstream_connect_time' "$NC"; then
    # older ltsv block from a previous bootstrap: drop it so the current one gets inserted below
    awk '/log_format ltsv/ {skip=1} skip && /vhost:\$host";/ {skip=0; next} !skip {print}' "$NC" > "$NC.new" && safe_mv "$NC.new" "$NC"
    sed -i 's|access_log /var/log/nginx/access.log ltsv;|access_log /var/log/nginx/access.log;|' "$NC"
    info "nginx: replaced older ltsv format"
  fi
  if ! grep -q "log_format ltsv" "$NC"; then
    # insert the ltsv format right before the first access_log line in the http block, then switch that line to ltsv
    awk 'FNR==NR { fmt = fmt $0 "\n"; next }
      /^[[:space:]]*access_log[[:space:]]/ && !done { printf "%s", fmt; sub(/access_log[[:space:]]+[^;]+;/, "access_log /var/log/nginx/access.log ltsv;"); done=1 }
      { print }' "$FMT" "$NC" > "$NC.new" && safe_mv "$NC.new" "$NC"
  fi
  if nginx -t >/dev/null 2>&1; then
    systemctl reload nginx && ok "nginx: ltsv access log enabled (nginx.conf.orig kept)"
  else
    nginx -t 2>&1 | sed 's/^/     /'
    cp "$NC" "$NC.failed"
    ng "nginx -t failed; restored nginx.conf.orig (failed version kept as nginx.conf.failed)"
    cp "$NC.orig" "$NC"
  fi
  grep -rn "access_log" /etc/nginx/sites-enabled /etc/nginx/conf.d 2>/dev/null | grep -v "#" | sed 's/^/     note: site-level access_log overrides http-level: /'
  chmod o+rx /var/log/nginx; usermod -aG adm "$U" 2>/dev/null; ok "log dir readable by $U (group adm)"
fi

# ---------- 4. database slow log ----------
step 4 "database" db
MYCNF_DIR=""; DBSVC=""
if [ -d /etc/mysql/mysql.conf.d ] && systemctl list-unit-files mysql.service >/dev/null 2>&1; then MYCNF_DIR=/etc/mysql/mysql.conf.d; DBSVC=mysql
elif [ -d /etc/mysql/mariadb.conf.d ]; then MYCNF_DIR=/etc/mysql/mariadb.conf.d; DBSVC=mariadb; fi
if [ "${SKIP_MYSQL:-0}" != 1 ] && [ -z "$MYCNF_DIR" ] && [ -d /etc/postgresql ]; then
  PGDIR=$(ls -d /etc/postgresql/*/main 2>/dev/null | head -1)
  if [ -n "$PGDIR" ]; then
    mkdir -p "$PGDIR/conf.d"
    printf "# isucon bootstrap: log every statement with duration (remove before the final bench)\nlog_min_duration_statement = 0\nlog_line_prefix = '%%m [%%p] %%u@%%d '\nlog_statement = 'none'\n" > "$PGDIR/conf.d/zz-isucon.conf"
    grep -q "^include_dir = 'conf.d'" "$PGDIR/postgresql.conf" || info "postgresql.conf has no include_dir 'conf.d'; add it"
    systemctl reload postgresql && ok "postgresql: statement logging enabled ($PGDIR/conf.d/zz-isucon.conf); analyze with pt-query-digest --type pglog" || ng "postgresql reload"
  fi
elif [ "${SKIP_MYSQL:-0}" != 1 ] && [ -z "$MYCNF_DIR" ]; then
  info "no MySQL/MariaDB/PostgreSQL config found; DB slow-log setup skipped (SQLite or something else? see server-info)"
elif [ "${SKIP_MYSQL:-0}" != 1 ]; then
  install -m 644 "$HERE/etc/mysql-zz-isucon.cnf" "$MYCNF_DIR/zz-isucon.cnf"
  mkdir -p /var/log/mysql; chown mysql:adm /var/log/mysql; chmod 750 /var/log/mysql
  # units that Require/Want the DB stop with it on restart and do NOT come back (clean stop != failure): remember and restart them
  DEPS=$(systemctl list-dependencies --reverse --plain --no-legend "$DBSVC.service" 2>/dev/null | awk '{print $1}' | grep -v "^$DBSVC.service$" | grep -v '\.target$' | while read -r u; do [ "$(systemctl is-active "$u" 2>/dev/null)" = active ] && echo "$u"; done | tr '\n' ' ')
  if systemctl restart "$DBSVC"; then
    sleep 1
    for u in $DEPS; do systemctl start "$u" 2>/dev/null && info "restarted dependent unit $u" || ng "could not restart $u"; done
    mysql -e "SHOW VARIABLES WHERE Variable_name IN ('slow_query_log','slow_query_log_file','long_query_time')" | tail -n +2 | sed 's/^/     /'
    ok "$DBSVC: slow log enabled via $MYCNF_DIR/zz-isucon.cnf (remove it or run measure:off before the final bench)"
  else ng "$DBSVC restart failed; removing zz-isucon.cnf"; rm -f "$MYCNF_DIR/zz-isucon.cnf"; systemctl restart "$DBSVC"; fi
  touch /var/log/mysql/slow.log; chown mysql:adm /var/log/mysql/slow.log; chmod 640 /var/log/mysql/slow.log
fi
# re-capture etc after our changes so the repo holds the current state
rsync -a --delete --exclude tls /etc/nginx/ "$HOME_DIR/etc/nginx/" 2>/dev/null; rsync -a --delete /etc/mysql/ "$HOME_DIR/etc/mysql/" 2>/dev/null; chown -R "$U:$U" "$HOME_DIR/etc"

# ---------- 4a. server info for the repo (before the commit, so it is pushed with the initial import) ----------
step 4a "server-info"
cd "$HOME_DIR"
mkdir -p docs; bash "$HERE/server-info.sh" > docs/server-info-$(hostname).txt 2>&1; chown -R "$U:$U" docs
ok "docs/server-info-$(hostname).txt written"
grep -E '^(detected|NOTE)' docs/server-info-$(hostname).txt | sed 's/^/     /'
fi # PHASE != git

# ---------- 4b. commit / sync (after all config changes, so the repo matches the hosts) ----------
if [ "$PHASE" != prep ]; then
step 4b "git commit / sync" sync
cd "$HOME_DIR"
[ -d .git ] || { sudo -u "$U" git init -q && ok "git init"; }
case "$ROLE" in
  init|local)
    sudo -u "$U" git add -A
    if ! sudo -u "$U" git diff --cached --quiet; then
      sudo -u "$U" git commit -q -m "bootstrap: initial import ($(hostname))" && ok "initial commit" || ng "commit"
    else info "nothing to commit"; fi
    sz=$(du -sh .git | cut -f1); info "repo size: $sz"
    if [ -n "$GIT_REMOTE" ]; then
      sudo -u "$U" git remote get-url origin >/dev/null 2>&1 || sudo -u "$U" git remote add origin "$GIT_REMOTE"
      # the remote usually already has content (repo created from the team template: tools/, docs/, Taskfile, CLAUDE.md).
      # merge it under /home/isucon so one repo holds the app, the configs and the tooling.
      if sudo -u "$U" git ls-remote --exit-code --heads origin main >/dev/null 2>&1; then
        sudo -u "$U" git fetch -q origin main
        if sudo -u "$U" git merge -q --allow-unrelated-histories --no-edit -m "merge template ($GIT_REMOTE) into $(hostname)" origin/main >/dev/null 2>&1; then
          ok "merged template from origin/main"
        else
          # both-added files (.gitignore, README.md ...): keep both halves
          for f in $(sudo -u "$U" git diff --name-only --diff-filter=U); do
            { sudo -u "$U" git show ":2:$f" 2>/dev/null; echo; sudo -u "$U" git show ":3:$f" 2>/dev/null; } > "$f.merged" && mv "$f.merged" "$f" && chown "$U:$U" "$f" && sudo -u "$U" git add "$f"
            info "merge: concatenated both versions of $f"
          done
          sudo -u "$U" git -c core.editor=true commit -q -m "merge template ($GIT_REMOTE) into $(hostname)" && ok "merged template (conflicts concatenated)" || ng "merge template"
        fi
      fi
      # push: the hub may push to main at the same moment (kickoff, 2026-10-28 ISUCON11: rejected, but `| tail -1`
      # hid the exit code and the others joined a main without webapp/). On a reject merge origin/main and retry.
      pushed=""
      for try in 1 2 3; do
        out=$(sudo -u "$U" git push -u origin main 2>&1); rc=$?
        echo "$out" | tail -1
        if [ $rc -eq 0 ]; then pushed=1; break; fi
        info "push rejected (try $try/3): merging origin/main and retrying"
        sudo -u "$U" git fetch -q origin main && sudo -u "$U" git merge -q --no-edit -m "merge origin/main into $(hostname) import" origin/main >/dev/null 2>&1 || { sudo -u "$U" git merge --abort 2>/dev/null; break; }
        sleep 2
      done
      if [ -n "$pushed" ]; then ok "pushed to $GIT_REMOTE"
      else ng "push to $GIT_REMOTE failed (reject / deploy key / branch protection?) - fix, then on $(hostname): cd $HOME_DIR && sudo -u $U git pull --no-rebase origin main && sudo -u $U git push origin main; then task host:bootstrap HOST=<svc2/3> ROLE=join"; fi
    fi ;;
  join)
    [ -n "$GIT_REMOTE" ] || { ng "ROLE=join needs GIT_REMOTE"; exit 1; }
    sudo -u "$U" git remote get-url origin >/dev/null 2>&1 || sudo -u "$U" git remote add origin "$GIT_REMOTE"
    if sudo -u "$U" git fetch -q origin main && sudo -u "$U" git reset -q --hard origin/main; then ok "synced to origin/main ($(sudo -u "$U" git rev-parse --short HEAD))"; else ng "fetch/reset from $GIT_REMOTE"; fi ;;
esac
fi # PHASE != prep

# ---------- 6. verify ----------
step 6 "verify"
if [ "$PHASE" != git ]; then
for u in $(systemctl list-units --type=service --state=running,failed,inactive --no-legend --plain 2>/dev/null | awk '{print $1}' | grep -Ei '^isu'); do
  st=$(systemctl is-active "$u" 2>/dev/null); en=$(systemctl is-enabled "$u" 2>/dev/null)
  if [ "$en" = enabled ] && [ "$st" != active ]; then ng "$u is enabled but $st (start it: systemctl start $u)"; fi
done
df -h / | awk 'NR==2 {print "     disk free: "$4}'
command -v alp >/dev/null && ok "alp" || ng "alp"
command -v slp >/dev/null && ok "slp" || ng "slp"
command -v duckdb >/dev/null && ok "duckdb" || info "duckdb absent on host (ok)"
tail -1 /var/log/nginx/access.log 2>/dev/null | grep -q "reqtime:" && ok "access.log is ltsv" || info "access.log: no ltsv line yet (send a request and re-check)"
if [ -n "$MYCNF_DIR" ]; then
  sq=$(mysql -Nse "SELECT @@slow_query_log, @@slow_query_log_file, @@long_query_time" 2>&1 | head -1)
  case "$sq" in 1*) ok "slow_query_log=1 ($sq)";; *) ng "slow_query_log: got '$sq' (config dir $MYCNF_DIR; is zz-isucon.cnf included by /etc/mysql/my.cnf? does root have socket auth?)";; esac
fi
# 1b does not wait for a running unattended-upgrade; it must be gone by the first bench, not by now
if bg_apt; then info "background apt still finishing (masked, SIGTERM sent; exits after the current package) - task host:diag before the bench"
else ok "no background apt running"; fi
fi # PHASE != git
cd "$HOME_DIR" && sudo -u "$U" git log --oneline 2>/dev/null | head -1 | sed 's/^/     HEAD: /'
_step_end
echo "## done (${FAILED} NG, $((SECONDS-T0))s)"
echo "## timing $(hostname) role=$ROLE phase=$PHASE: total $((SECONDS-T0))s ($TIMES)"
exit $FAILED
