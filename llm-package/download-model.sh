#!/bin/sh
set -e

MODEL="${1:-gemma-4-E4B-it-Q4_0}"
MODELS_DIR="${2:-$(dirname "$0")/models}"

case "$MODEL" in
    e4b-q4|gemma-4-E4B-it-Q4_0)
        HF_REPO="ggml-org/gemma-4-E4B-it-GGUF"
        HF_FILE="gemma-4-E4B-it-Q4_0.gguf"
        ;;
    e4b-q8|gemma-4-E4B-it-Q8_0)
        HF_REPO="ggml-org/gemma-4-E4B-it-GGUF"
        HF_FILE="gemma-4-E4B-it-Q8_0.gguf"
        ;;
    *)
        HF_REPO="ggml-org/gemma-4-E4B-it-GGUF"
        HF_FILE="$MODEL"
        ;;
esac

DEST="$MODELS_DIR/$HF_FILE"
mkdir -p "$MODELS_DIR"

if [ -f "$DEST" ] && [ "$(stat -f%z "$DEST" 2>/dev/null || stat -c%s "$DEST" 2>/dev/null)" -gt 4000000000 ]; then
    echo "Model already exists (>4GB): $DEST"
    echo "Run: cd llm-package && ./start.sh"
    exit 0
fi

URL="https://huggingface.co/$HF_REPO/resolve/main/$HF_FILE"
echo "Downloading $HF_FILE (~4.4 GB)..."
echo ""

# --max-time 0  = no timeout
# -C -          = resume partial download
curl -L --fail --retry 3 --retry-delay 10 -C - --max-time 0 -o "$DEST" "$URL"

echo ""
echo "Done: $DEST"
echo "Run: ./start.sh"
