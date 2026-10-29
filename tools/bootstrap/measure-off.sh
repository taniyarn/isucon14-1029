#!/bin/bash
# Final-bench mode: undo what bootstrap turned on for measurement. Run with sudo on each host (task hosts:measure-off).
#   - stop + disable the isumeasure agent
#   - remove the slow-log / statement-log config and restart the DB (the app units that Require it restart with it)
#   - nginx: access_log off (keeps the ltsv format definition, so `measure-on` is just the reverse sed)
#   - the app's own measurement (sqltag driver, pprof/expvar listener, uid header — the measurement patch reads MEASURE):
#     a drop-in Environment=MEASURE=0 on every running isu* unit, then restart it
# Re-enable: sudo bash measure-off.sh on   (restores logging; the agent is started again)
set -u
MODE=${1:-off}
ok() { echo "OK   $*"; }; ng() { echo "NG   $*"; }; info() { printf -- '--   %s\n' "$*"; }
NC=/etc/nginx/nginx.conf
MYCNF_DIR=""; DBSVC=""
if [ -d /etc/mysql/mysql.conf.d ] && systemctl list-unit-files mysql.service >/dev/null 2>&1; then MYCNF_DIR=/etc/mysql/mysql.conf.d; DBSVC=mysql
elif [ -d /etc/mysql/mariadb.conf.d ]; then MYCNF_DIR=/etc/mysql/mariadb.conf.d; DBSVC=mariadb; fi
PGDIR=$(ls -d /etc/postgresql/*/main 2>/dev/null | head -1)
APP_UNITS=$(systemctl list-units --type=service --state=running --no-legend --plain 'isu*' 2>/dev/null | awk '{print $1}' | grep -v '^isumeasure')
app_measure() { # $1 = off|on; only units whose drop-in actually changes are restarted
  changed=""
  for u in $APP_UNITS; do
    d=/etc/systemd/system/$u.d; f=$d/zz-measure-off.conf
    if [ "$1" = off ] && [ ! -f "$f" ]; then mkdir -p "$d"; printf '[Service]\nEnvironment=MEASURE=0\n' > "$f"; changed="$changed $u"
    elif [ "$1" = on ] && [ -f "$f" ]; then rm -f "$f"; changed="$changed $u"; fi
  done
  [ -z "$changed" ] && return 0
  systemctl daemon-reload
  for u in $changed; do systemctl restart "$u" && ok "$u restarted with MEASURE=$([ "$1" = off ] && echo 0 || echo default)" || ng "$u restart"; done
}
restart_db() {
  # only dependents that were active BEFORE the DB restart come back: the AMI ships disabled units of the other languages
  # (isuride-node, -php, ...) that Require the DB too; restarting them makes them grab :8080 from the real app
  deps=$(systemctl list-dependencies --reverse --plain "$1" 2>/dev/null | grep -Ei '^\s*isu' | awk '{print $1}')
  was_active=""; for d in $deps; do systemctl is-active --quiet "$d" && was_active="$was_active $d"; done
  systemctl restart "$1" && ok "$1 restarted" || ng "$1 restart"
  for d in $deps; do
    case " $was_active " in
      *" $d "*) systemctl restart "$d" 2>/dev/null && info "restarted dependent $d (was active)" || ng "restart dependent $d" ;;
      *) systemctl is-active --quiet "$d" && { systemctl stop "$d"; info "stopped $d (was inactive before; started by the DB restart)"; } || info "left $d inactive" ;;
    esac
  done
}
if [ "$MODE" = off ]; then
  echo "## measure off on $(hostname)"
  systemctl stop isumeasure-agent 2>/dev/null; systemctl disable isumeasure-agent 2>/dev/null; ok "isumeasure-agent stopped+disabled"
  if [ -n "$MYCNF_DIR" ] && [ -f "$MYCNF_DIR/zz-isucon.cnf" ]; then
    mkdir -p /var/lib/isumeasure; mv "$MYCNF_DIR/zz-isucon.cnf" /var/lib/isumeasure/zz-isucon.cnf.off; restart_db "$DBSVC"
    [ "$(mysql -Nse 'SELECT @@slow_query_log' 2>/dev/null)" = 0 ] && ok "slow_query_log=0" || ng "slow_query_log still on?"
  fi
  if [ -n "$PGDIR" ] && [ -f "$PGDIR/conf.d/zz-isucon.conf" ]; then mv "$PGDIR/conf.d/zz-isucon.conf" /var/lib/isumeasure/zz-isucon.pg.off; systemctl reload postgresql && ok "postgresql statement log off"; fi
  if [ -f "$NC" ] && grep -q 'access_log /var/log/nginx/access.log ltsv;' "$NC"; then
    sed -i 's|access_log /var/log/nginx/access.log ltsv;|access_log off; # ltsv (measure-off)|' "$NC"
    nginx -t >/dev/null 2>&1 && systemctl reload nginx && ok "nginx access_log off" || { ng "nginx config test failed; reverting"; sed -i 's|access_log off; # ltsv (measure-off)|access_log /var/log/nginx/access.log ltsv;|' "$NC"; }
  fi
  grep -rn "access_log" /etc/nginx/sites-enabled /etc/nginx/conf.d 2>/dev/null | grep -v '#' | grep -v 'off' | sed 's/^/     note: site-level access_log still on: /'
  app_measure off
else
  echo "## measure on on $(hostname)"
  [ -n "$MYCNF_DIR" ] && [ -f /var/lib/isumeasure/zz-isucon.cnf.off ] && { mv /var/lib/isumeasure/zz-isucon.cnf.off "$MYCNF_DIR/zz-isucon.cnf"; restart_db "$DBSVC"; }
  [ -n "$PGDIR" ] && [ -f /var/lib/isumeasure/zz-isucon.pg.off ] && { mv /var/lib/isumeasure/zz-isucon.pg.off "$PGDIR/conf.d/zz-isucon.conf"; systemctl reload postgresql; }
  [ -f "$NC" ] && sed -i 's|access_log off; # ltsv (measure-off)|access_log /var/log/nginx/access.log ltsv;|' "$NC" && nginx -t >/dev/null 2>&1 && systemctl reload nginx && ok "nginx ltsv log on"
  app_measure on
  systemctl enable --now isumeasure-agent 2>/dev/null && ok "isumeasure-agent started"
fi
