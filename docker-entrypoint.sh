#!/bin/sh
set -eu

if [ "$(id -u)" = "0" ]; then
  mkdir -p /data
  chown -R appuser:appgroup /data
  exec su-exec appuser:appgroup "$@"
fi

exec "$@"
