#!/bin/bash
# Install/upgrade the isumeasure agent on a contest host.
#   scp dist/isumeasure-linux-amd64 install-agent.sh <host>:/tmp/   then on the host:
#   sudo env CONTEST=practice bash /tmp/install-agent.sh [extra agent flags...]
# Env: CONTEST (run label), TRIGGER, IGNORE, UNITS, ACCESS_LOG, SLOW_LOG, EXPVAR, PPROF, REPO, ENV_FILE
set -eu
BIN=/tmp/isumeasure-linux-amd64
[ -f "$BIN" ] || { echo "missing $BIN"; exit 1; }
install -m 755 "$BIN" /usr/local/bin/isumeasure
mkdir -p /var/lib/isumeasure/runs /etc/isumeasure
cat > /etc/isumeasure/agent.env <<EOF
ISUMEASURE_CONTEST=${CONTEST:-practice}
ISUMEASURE_TRIGGER=${TRIGGER:-^POST (/api)?/initialize}
ISUMEASURE_IGNORE=${IGNORE:-/internal/}
ISUMEASURE_UNITS=${UNITS:-}
ISUMEASURE_ACCESS=${ACCESS_LOG:-/var/log/nginx/access.log}
ISUMEASURE_SLOW=${SLOW_LOG:-/var/log/mysql/slow.log}
ISUMEASURE_EXPVAR=${EXPVAR:-http://127.0.0.1:6060/debug/vars}
ISUMEASURE_PPROF=${PPROF:-http://127.0.0.1:6060}
ISUMEASURE_REPO=${REPO:-/home/isucon}
ISUMEASURE_ENVFILE=${ENV_FILE:-/home/isucon/env.sh}
ISUMEASURE_MINREQ=${MIN_REQ:-100}
ISUMEASURE_DBTRIGGER=${DB_TRIGGER:-100}
ISUMEASURE_EXTRA=$*
EOF
cat > /etc/systemd/system/isumeasure-agent.service <<'EOF'
[Unit]
Description=isumeasure agent (per-host measurement)
After=network.target

[Service]
EnvironmentFile=/etc/isumeasure/agent.env
ExecStart=/bin/sh -c 'exec /usr/local/bin/isumeasure agent -contest "$ISUMEASURE_CONTEST" -trigger "$ISUMEASURE_TRIGGER" -min-req "$ISUMEASURE_MINREQ" -db-trigger "$ISUMEASURE_DBTRIGGER" -ignore "$ISUMEASURE_IGNORE" -units "$ISUMEASURE_UNITS" -access "$ISUMEASURE_ACCESS" -slow "$ISUMEASURE_SLOW" -expvar "$ISUMEASURE_EXPVAR" -pprof "$ISUMEASURE_PPROF" -repo "$ISUMEASURE_REPO" -env "$ISUMEASURE_ENVFILE" $ISUMEASURE_EXTRA'
Restart=always
RestartSec=2
User=root
Nice=5

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now isumeasure-agent.service
systemctl restart isumeasure-agent.service
sleep 1
systemctl is-active isumeasure-agent.service && echo "agent: $(isumeasure version) listening on 127.0.0.1:19100"
curl -s 127.0.0.1:19100/status | head -c 400; echo
echo "logs: journalctl -u isumeasure-agent -f"
