#!/bin/sh
# Keeps one autossh session per contest host (SVC1..SVC9 from isucon.env), forwarding on the compose network:
#   1910N -> 127.0.0.1:19100 (isumeasure agent)   1906N -> 127.0.0.1:6060 (pprof)   1330N -> 127.0.0.1:3306 (mysql)
# Outbound ssh only; contest hosts need no inbound rule beyond 22.
KEY=/keys/contest_ed25519
USER=${SSH_USER:-isucon}
mkdir -p /root/.ssh && chmod 700 /root/.ssh
cp "$KEY" /root/.ssh/id && chmod 600 /root/.ssh/id
export AUTOSSH_GATETIME=0
i=1
while [ $i -le 9 ]; do
  eval ip=\$SVC$i
  if [ -n "$ip" ]; then
    echo "tunnel: svc$i -> $USER@$ip (agent 1910$i, pprof 1906$i, mysql 1330$i)"
    autossh -M 0 -N \
      -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes \
      -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/root/.ssh/known_hosts \
      -i /root/.ssh/id \
      -L "0.0.0.0:1910$i:127.0.0.1:19100" -L "0.0.0.0:1906$i:127.0.0.1:6060" -L "0.0.0.0:1330$i:127.0.0.1:3306" \
      "$USER@$ip" &
  fi
  i=$((i+1))
done
wait
