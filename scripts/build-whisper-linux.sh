#!/bin/bash
# One-time build: compiles whisper-server for Linux inside Docker and extracts
# only the binary + shared libs. Run this once, then docker compose uses the
# slim Dockerfile that just copies pre-built artifacts.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/whisper-package"

echo "=== Building whisper-server for Linux (this clones the repo once) ==="

# Build only the builder stage using a temporary Dockerfile
cat > "$BUILD_DIR/Dockerfile.builder" << 'DOCKERFILE'
FROM ubuntu:22.04 AS builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git build-essential cmake \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN git clone https://github.com/ggerganov/whisper.cpp.git --depth 1
WORKDIR /build/whisper.cpp
RUN cmake -B build -DCMAKE_BUILD_TYPE=Release
RUN cmake --build build --config Release -j$(nproc) --target whisper-server
ENTRYPOINT ["/bin/sh"]
DOCKERFILE

docker build -t whisper-linux-builder -f "$BUILD_DIR/Dockerfile.builder" "$BUILD_DIR"
rm "$BUILD_DIR/Dockerfile.builder"

echo "=== Extracting artifacts ==="
CID=$(docker create whisper-linux-builder)
docker cp "$CID:/build/whisper.cpp/build/bin/whisper-server" "$BUILD_DIR/whisper-server-linux"
docker cp "$CID:/build/whisper.cpp/build/bin/libwhisper.so" "$BUILD_DIR/libwhisper.so"
docker cp "$CID:/build/whisper.cpp/build/bin/libwhisper.so.1" "$BUILD_DIR/libwhisper.so.1" 2>/dev/null || true
docker cp "$CID:/build/whisper.cpp/build/bin/libggml.so" "$BUILD_DIR/libggml.so"
docker cp "$CID:/build/whisper.cpp/build/bin/libggml-cpu.so" "$BUILD_DIR/libggml-cpu.so"
docker cp "$CID:/build/whisper.cpp/build/bin/libggml-base.so" "$BUILD_DIR/libggml-base.so"
docker cp "$CID:/build/whisper.cpp/build/bin/libggml.so.0" "$BUILD_DIR/libggml.so.0" 2>/dev/null || true
docker cp "$CID:/build/whisper.cpp/build/bin/libggml-base.so.0" "$BUILD_DIR/libggml-base.so.0" 2>/dev/null || true
docker cp "$CID:/build/whisper.cpp/build/bin/libggml-cpu.so.0" "$BUILD_DIR/libggml-cpu.so.0" 2>/dev/null || true
docker rm "$CID"
docker rmi whisper-linux-builder

echo ""
echo "=== Done ==="
echo "Linux artifacts in whisper-package/:"
ls -lh "$BUILD_DIR"/whisper-server-linux "$BUILD_DIR"/libwhisper*.so "$BUILD_DIR"/libggml*.so 2>/dev/null
echo ""
echo "Now run: docker compose up -d --build"
