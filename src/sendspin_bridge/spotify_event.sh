#!/bin/sh
# librespot's stdout is PCM; event notifications go to the bridge's private FIFO.
case "$PLAYER_EVENT" in
    playing|paused|stopped|seeked)
        printf '%s\n' "$PLAYER_EVENT" > "$SENDSPIN_EVENT_FIFO" ;;
esac
