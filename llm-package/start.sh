#!/bin/bash
# Start llama-server locally on macOS for testing.
# Same pattern as whisper-package/start.sh.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MODELS_DIR="$SCRIPT_DIR/models"
BINARY="$SCRIPT_DIR/llama-server"
PORT="${PORT:-8081}"
HOST="${HOST:-127.0.0.1}"

# Find binary: prefer macOS native, skip linux binary
if [ ! -f "$BINARY" ]; then
    echo "Error: No llama-server binary found at $BINARY"
    echo ""
    echo "On M4 Mac, build locally:"
    echo ""
    echo "  git clone https://github.com/ggerganov/llama.cpp.git /tmp/llama.cpp"
    echo "  cd /tmp/llama.cpp && cmake -B build && cmake --build build -j --target llama-server"
    echo "  cp build/bin/llama-server $SCRIPT_DIR/"
    echo ""
    exit 1
fi

# Find an existing gguf model
existing_model=$(find "$MODELS_DIR" -maxdepth 1 -name "*.gguf" -print -quit 2>/dev/null)

if [ -z "$existing_model" ]; then
    echo "No .gguf model found in $MODELS_DIR"
    echo ""
    echo "Options:"
    echo "  1) gemma-4-E4B-it-Q4_0  (~2.5 GB, fast)"
    echo "  2) gemma-4-E4B-it-Q8_0  (~5 GB, best quality)"
    echo ""
    read -rp "Choose [1-2] (default: 1): " choice
    choice="${choice:-1}"

    case "$choice" in
        1) model="gemma-4-E4B-it-Q4_0" ;;
        2) model="gemma-4-E4B-it-Q8_0" ;;
        *) echo "Invalid choice"; exit 1 ;;
    esac

    sh "$SCRIPT_DIR/download-model.sh" "$model" "$MODELS_DIR"
    existing_model=$(find "$MODELS_DIR" -maxdepth 1 -name "*.gguf" -print -quit)
fi

echo "Model: $(basename "$existing_model")"
echo "Starting llama-server on http://$HOST:$PORT ..."
echo "OpenAI-compatible API: http://$HOST:$PORT/v1/chat/completions"
echo ""

exec "$BINARY" \
    -m "$existing_model" \
    --host "$HOST" \
    --port "$PORT" \
    -ngl 99
