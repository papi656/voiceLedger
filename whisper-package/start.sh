#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MODELS_DIR="$SCRIPT_DIR/models"
BINARY="$SCRIPT_DIR/whisper-server"
DOWNLOAD_SCRIPT="$SCRIPT_DIR/download-model.sh"
PORT="${PORT:-8080}"
HOST="${HOST:-127.0.0.1}"

# ----- check binary -----
if [ ! -f "$BINARY" ]; then
    echo "Error: whisper-server binary not found at $BINARY"
    exit 1
fi

# ----- find an existing model -----
existing_model=$(find "$MODELS_DIR" -maxdepth 1 -name "ggml-*.bin" -print -quit 2>/dev/null)

if [ -n "$existing_model" ]; then
    echo "Found model: $(basename "$existing_model")"
    exec "$BINARY" -m "$existing_model" --host "$HOST" --port "$PORT"
fi

# ----- no model found: offer download -----
echo "No model found in $MODELS_DIR"
echo ""
echo "Available models:"
echo ""
echo "  English-only:"
echo "    1) tiny.en        (75 MB)"
echo "    2) base.en        (142 MB)"
echo "    3) small.en       (466 MB)"
echo "    4) medium.en      (1.5 GB)"
echo ""
echo "  Multilingual:"
echo "    5) tiny           (75 MB)"
echo "    6) base           (142 MB)"
echo "    7) small          (466 MB)"
echo "    8) medium         (1.5 GB)"
echo "    9) medium-q8_0    (785 MB)"
echo "   10) large-v3-turbo (2.9 GB)"
echo ""
read -rp "Choose a model [1-10]: " choice

case "$choice" in
    1) model="tiny.en" ;;
    2) model="base.en" ;;
    3) model="small.en" ;;
    4) model="medium.en" ;;
    5) model="tiny" ;;
    6) model="base" ;;
    7) model="small" ;;
    8) model="medium" ;;
    9) model="medium-q8_0" ;;
    10) model="large-v3-turbo" ;;
    *) echo "Invalid choice"; exit 1 ;;
esac

sh "$DOWNLOAD_SCRIPT" "$model" "$MODELS_DIR"

model_path="$MODELS_DIR/ggml-$model.bin"
echo ""
echo "Starting server with $model ..."
exec "$BINARY" -m "$model_path" --host "$HOST" --port "$PORT"
