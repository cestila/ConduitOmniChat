#!/usr/bin/env bash
set -e

# Locate Electron: $ELECTRON_PATH, then a system electron, then node_modules
if [ -n "$ELECTRON_PATH" ] && [ -x "$ELECTRON_PATH" ]; then
    ELECTRON_BIN="$ELECTRON_PATH"
elif which electron >/dev/null 2>&1; then
    ELECTRON_BIN=$(which electron)
elif [ -f "./node_modules/electron/dist/electron" ]; then
    ELECTRON_BIN="./node_modules/electron/dist/electron"
else
    echo "❌ No se encontró un binario de Electron compatible."
    exit 1
fi

echo "🚀 Iniciando Conduit GUI con: $ELECTRON_BIN"
exec "$ELECTRON_BIN" . "$@"
