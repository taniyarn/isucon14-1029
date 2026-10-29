#!/bin/bash
# Runs ON EACH CONTEST HOST as the app user after `task deploy BRANCH=...` has checked out the branch
# (git fetch + reset --hard origin/<branch>). Edit the three sections on contest day, then set CONFIGURED=1.
#
# What to find out first (5 min):  systemctl cat <app unit>  → WorkingDirectory / ExecStart tell you the build dir,
# binary name and unit name.  bootstrap puts a copy of every isu* unit under etc/systemd/system/.
set -eu
cd "$(dirname "$0")"
CONFIGURED=0
[ "$CONFIGURED" = 1 ] || { echo "deploy.sh: not configured yet — fill in build/config/restart below and set CONFIGURED=1"; exit 1; }
HOST=$(hostname)
echo "== deploy on $HOST: $(git rev-parse --abbrev-ref HEAD)@$(git rev-parse --short HEAD)"

# ---- 1. build (language dir + binary name from the systemd unit) ----
# ( cd webapp/go && go build -o isuride . )

# ---- 2. configs kept in the repo (etc/ mirrors /etc). Copy only what you changed. ----
# sudo cp etc/nginx/nginx.conf /etc/nginx/nginx.conf && sudo cp -r etc/nginx/sites-available /etc/nginx/ && sudo nginx -t && sudo systemctl reload nginx
# sudo cp etc/mysql/mysql.conf.d/*.cnf /etc/mysql/mysql.conf.d/ && sudo systemctl restart mysql
# sudo sysctl --system >/dev/null
# per-host differences (db host etc.) belong in env.sh, which is NOT shared: keep env.sh out of git or template it per host:
# [ -f "env.$HOST.sh" ] && cp "env.$HOST.sh" env.sh

# ---- 3. restart the app ----
# sudo systemctl restart isuride-go.service
# sudo systemctl is-active isuride-go.service

echo "== done on $HOST"
