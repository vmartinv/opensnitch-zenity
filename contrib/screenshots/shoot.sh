#!/bin/sh
# Regenerates docs/prompt.png and docs/scope.png without a graphical session:
# zenity renders through GTK's Broadway backend and headless Chromium
# screenshots it. Needs gtk4-broadwayd (gtk4), zenity, chromium, node,
# imagemagick. Keep the zenity arguments in sync with internal/prompt/prompt.go.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

text='<b>/usr/bin/curl</b> wants to connect to
<b>example.com</b> (93.184.215.14) port 443/tcp

Command: curl https://example.com
User: martin (1000)   PID: 48213'

shot() {
    out=$1
    shift
    gtk4-broadwayd :5 --port 8085 --address 127.0.0.1 >/dev/null 2>&1 &
    bp=$!
    sleep 1
    GDK_BACKEND=broadway BROADWAY_DISPLAY=:5 zenity --question --title=OpenSnitch \
        --width=520 --icon=network-wired --timeout=60 "$@" >/dev/null 2>&1 &
    zp=$!
    sleep 2
    node "$here/cdp-shot.mjs" http://127.0.0.1:8085/ "$tmp/raw.png" "$tmp/profile"
    kill "$zp" "$bp" 2>/dev/null || true
    wait 2>/dev/null || true
    magick "$tmp/raw.png" -trim -bordercolor white -border 24 -strip "$out"
    echo "wrote $out"
}

shot "$repo/docs/prompt.png" --text="$text" \
    --ok-label="Allow once" --cancel-label="Deny once" \
    --extra-button="Deny always" --extra-button="Allow always" --extra-button="Copy details"

shot "$repo/docs/scope.png" --text="<b>Allow always</b> for which connections?

$text" \
    --ok-label="Any program to example.com" --cancel-label="Back" \
    --extra-button="curl to any host" --extra-button="curl to example.com only" \
    --default-cancel
