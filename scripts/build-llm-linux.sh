#!/bin/bash
# One-time build: compiles llama-server for Linux inside Docker and extracts
# only the binary + shared libs. Same pattern as build-whisper-linux.sh.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/../llm-package"

echo "=== Building llm-server for Linux ==="

cat > "$BUILD_DIR/Dockerfile.builder" << 'DOCKERFILE'
FROM ubuntu:22.04 AS builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git build-essential cmake \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN git clone https://github.com/ggerganov/llama.cpp.git --depth 1
WORKDIR /build/llama.cpp
RUN cmake -B build -DCMAKE_BUILD_TYPE=Release -DGGML_CUDA=OFF -DGGML_METAL=OFF
RUN cmake --build build --config Release -j$(nproc) --target llama-server
ENTRYPOINT ["/bin/sh"]
DOCKERFILE

docker build -t llm-linux-builder -f "$BUILD_DIR/Dockerfile.builder" "$BUILD_DIR"
rm "$BUILD_DIR/Dockerfile.builder"

echo "=== Extracting artifacts ==="
CID=$(docker create llm-linux-builder)

# Clean old artifacts
rm -f "$BUILD_DIR"/libggml*.so* "$BUILD_DIR"/libllama*.so* "$BUILD_DIR"/libmtmd*.so* "$BUILD_DIR"/llama-server-linux

# Copy entire bin directory (preserving symlinks)
rm -rf "$BUILD_DIR/_tmp_bin"
docker cp "$CID:/build/llama.cpp/build/bin/." "$BUILD_DIR/_tmp_bin"

cp "$BUILD_DIR/_tmp_bin/llama-server" "$BUILD_DIR/llama-server-linux"
cp -a "$BUILD_DIR/_tmp_bin"/lib*.so* "$BUILD_DIR/"
rm -rf "$BUILD_DIR/_tmp_bin"

docker rm "$CID" >/dev/null
docker rmi llm-linux-builder >/dev/null 2>&1

echo ""
echo "=== Done ==="
ls -lh "$BUILD_DIR"/llama-server-linux "$BUILD_DIR"/libllama-server-impl.so
echo ""
echo "Now run: docker compose up -d --build"
