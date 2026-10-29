#!/bin/bash
# Resolve a host argument to an IP using HOSTS from .env (exported by task), so nobody types IPs after .env is written:
#   ""            -> 1st of HOSTS (svc1)
#   1 | svc1      -> Nth of HOSTS
#   db            -> DB_HOST if set in .env, else 3rd of HOSTS (falls back to the 1st when there is only one host)
#   anything else -> returned as is (an IP or a hostname)
# Usage: HOSTS="ip1 ip2 ip3" [DB_HOST=ip] host-ip.sh [HOST]
set -u
want=${1:-}
hosts=(${HOSTS:-})
n=${#hosts[@]}
pick() { # 1-based index into HOSTS
  if [ "$1" -ge 1 ] && [ "$1" -le "$n" ]; then echo "${hosts[$(( $1 - 1 ))]}"; else echo "HOST=$want but HOSTS in .env has $n entr$( [ "$n" = 1 ] && echo y || echo ies) (${HOSTS:-empty})" >&2; exit 1; fi
}
case "$want" in
  "")          [ "$n" -ge 1 ] || { echo "HOSTS is empty in .env (task needs it to pick svc1)" >&2; exit 1; }; echo "${hosts[0]}" ;;
  db|DB)       if [ -n "${DB_HOST:-}" ]; then echo "$DB_HOST"; elif [ "$n" -ge 3 ]; then echo "${hosts[2]}"; else pick 1; fi ;;
  svc[1-9]|svc-[1-9]) pick "${want##*[a-z-]}" ;;
  [1-9])       pick "$want" ;;
  *)           echo "$want" ;;
esac
