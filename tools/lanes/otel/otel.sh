#!/usr/bin/env bash
# task otel:up / otel:down — optional local OpenTelemetry Collector (docker) for Claude Code's telemetry: OTLP gRPC on
# 127.0.0.1:4317 → <board dir>/otel/{logs,metrics,traces}.jsonl. Sessions export to it only when their start command
# carries the OTel env (sessions.json "otel": true or LANES_OTEL=1 in .env, then task lane:starts).
# OTEL_IMAGE overrides the collector image.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
NAME=isucon-lanes-otel
IMAGE="${OTEL_IMAGE:-otel/opentelemetry-collector-contrib:0.111.0}"
if ! command -v docker >/dev/null 2>&1; then
  echo "!! docker が無い: task otel:up は省略してよい（hooks の計測 cc-events.jsonl だけで lane:timeline は動く）"; exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "!! docker が動いていない（Docker Desktop を起動する）: OTel は省略してよい"; exit 1
fi
case "${1:-}" in
  up)
    out="$(python3 "$HERE/../lanes.py" dir)/otel" || exit 1
    mkdir -p "$out" && chmod 777 "$out"   # the collector runs as a non-root uid
    docker rm -f "$NAME" >/dev/null 2>&1
    docker run -d --name "$NAME" --restart unless-stopped -p 127.0.0.1:4317:4317 \
      -v "$HERE/collector.yaml:/etc/otelcol-contrib/config.yaml:ro" -v "$out:/out" "$IMAGE" >/dev/null || exit 1
    echo "otel collector up: 127.0.0.1:4317 -> $out/{logs,metrics,traces}.jsonl"
    echo "start commands carry the OTel env only with sessions.json \"otel\": true or LANES_OTEL=1 (then task lane:starts)" ;;
  down)
    docker rm -f "$NAME" >/dev/null 2>&1 && echo "otel collector stopped (files kept)" || echo "no collector running" ;;
  *) echo "usage: otel.sh up|down"; exit 2 ;;
esac
