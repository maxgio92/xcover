#!/usr/bin/env bash

set -euo pipefail

SOCKET_PATH="/tmp/xcover.sock"
TIMEOUT="${1:-60}" # seconds
RETRY_INTERVAL=0.5 # seconds between retries
START_TIME=$(date +%s)

while true; do
	# Retry if the socket does not exist yet.
	[ -S $SOCKET_PATH ] || { sleep $RETRY_INTERVAL && continue; }

	# Try connecting and reading one byte.
	READY=$(socat - UNIX-CONNECT:"$SOCKET_PATH" 2>/dev/null | head -c 1)

	if [ "$READY" = "$(printf '\x01')" ]; then
		echo "xcover is ready!"
		exit 0
	fi

	# Check if timeout is reached.
	NOW=$(date +%s)
	ELAPSED=$((NOW - START_TIME))
	if [ $ELAPSED -ge $TIMEOUT ]; then
		echo "Timeout waiting for xcover readiness."
		exit 1
	fi

	# Retry after a short sleep.
	sleep $RETRY_INTERVAL
done

