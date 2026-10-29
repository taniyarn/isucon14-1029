#!/bin/bash
# One-time setup of the shared ops host (control panel + collector + report store).
# Run as root on the ops host:  sudo bash /tmp/ops/setup-ops-host.sh
# Idempotent. Does NOT open any inbound port: team access is via Tailscale, contest hosts are reached by outbound ssh.
set -u
OPS_DIR=/opt/isucon-ops
OPS_USER=${OPS_USER:-ubuntu}
ok()   { printf 'OK   %s\n' "$*"; }
ng()   { printf 'NG   %s\n' "$*"; FAILED=$((FAILED+1)); }
info() { printf -- '--   %s\n' "$*"; }
FAILED=0
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
export DEBIAN_FRONTEND=noninteractive

echo "## 1. packages"
apt-get update -q >/dev/null 2>&1
for p in curl ca-certificates gnupg rsync jq git autossh; do dpkg -s "$p" >/dev/null 2>&1 || { apt-get install -y -q "$p" >/dev/null 2>&1 && ok "apt: $p" || ng "apt: $p"; }; done

echo "## 2. docker"
if ! command -v docker >/dev/null; then
  curl -fsSL https://get.docker.com -o /tmp/get-docker.sh && sh /tmp/get-docker.sh >/dev/null 2>&1 && ok "docker installed" || ng "docker install"
fi
usermod -aG docker "$OPS_USER" 2>/dev/null
docker compose version >/dev/null 2>&1 && ok "docker compose $(docker compose version --short)" || ng "docker compose plugin"

echo "## 3. tailscale"
if ! command -v tailscale >/dev/null; then
  curl -fsSL https://tailscale.com/install.sh | sh >/dev/null 2>&1 && ok "tailscale installed" || ng "tailscale install"
fi
if tailscale status >/dev/null 2>&1; then ok "tailscale up: $(tailscale ip -4 2>/dev/null | head -1)"; else info "tailscale not logged in yet → run: sudo tailscale up   (opens a login URL; do it once)"; fi

echo "## 4. ops dir + ssh key for contest hosts"
install -d -o "$OPS_USER" -g "$OPS_USER" "$OPS_DIR" "$OPS_DIR/data" "$OPS_DIR/data/measurements" "$OPS_DIR/data/reports" "$OPS_DIR/keys" "$OPS_DIR/compose"
chmod 700 "$OPS_DIR/keys"
if [ ! -f "$OPS_DIR/keys/contest_ed25519" ]; then
  sudo -u "$OPS_USER" ssh-keygen -t ed25519 -N "" -C "isucon-ops@$(hostname)" -f "$OPS_DIR/keys/contest_ed25519" >/dev/null && ok "ssh key generated: $OPS_DIR/keys/contest_ed25519"
else ok "ssh key exists"; fi
cat > "$OPS_DIR/isucon.env.example" <<'EOF'
# copy to isucon.env and fill in on the day. Hosts are reached by ssh from this machine.
SVC1=10.0.0.1
SVC2=10.0.0.2
SVC3=10.0.0.3
SSH_USER=isucon
SSH_KEY=/opt/isucon-ops/keys/contest_ed25519
# repo that the contest hosts pull from (used by deploy)
GIT_REMOTE=git@github.com:ytakaya/isucon2026.git
EOF
chown "$OPS_USER:$OPS_USER" "$OPS_DIR/isucon.env.example"
ok "$OPS_DIR ready (data/, keys/, compose/, isucon.env.example)"

echo "## 5. self as a practice target (optional): allow ops key to ssh into this host as isucon/ubuntu"
PUB=$(cat "$OPS_DIR/keys/contest_ed25519.pub")
for u in isucon "$OPS_USER"; do
  h=$(getent passwd "$u" | cut -d: -f6); [ -d "$h" ] || continue
  install -d -m 700 -o "$u" -g "$u" "$h/.ssh"; touch "$h/.ssh/authorized_keys"; chown "$u:$u" "$h/.ssh/authorized_keys"; chmod 600 "$h/.ssh/authorized_keys"
  grep -qF "$PUB" "$h/.ssh/authorized_keys" || echo "$PUB" >> "$h/.ssh/authorized_keys"
done
ok "ops key authorized for isucon/$OPS_USER on this host (so SVC1=127.0.0.1 works for development)"

echo "## 5b. swap (insurance against a report that grows beyond RAM: the OOM killer then hits the report child, not the panel)"
if ! swapon --show | grep -q /swapfile; then
  fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile && ok "2 GB swapfile enabled" || ng "swapfile"
  grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
else
  ok "swap already on"
fi
sysctl -w vm.swappiness=10 >/dev/null && grep -q '^vm.swappiness' /etc/sysctl.conf || echo 'vm.swappiness=10' >> /etc/sysctl.conf

echo "## 6. verify"
docker info >/dev/null 2>&1 && ok "docker daemon" || ng "docker daemon"
df -h / | awk 'NR==2 {print "     disk free: "$4}'
free -m | awk '/Mem:/ {print "     mem free: "$7" MB"}'
echo
echo "## PUBLIC KEY to register on GitHub (Settings → SSH and GPG keys) of one team member before the contest:"
cat "$OPS_DIR/keys/contest_ed25519.pub"
echo "## done (${FAILED} NG)"
exit $FAILED
