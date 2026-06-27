FROM ubuntu:22.04 AS builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    git build-essential cmake \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN git clone https://github.com/ggerganov/whisper.cpp.git --depth 1
WORKDIR /build/whisper.cpp
RUN cmake -B build -DCMAKE_BUILD_TYPE=Release
RUN cmake --build build --config Release -j$(nproc) --target whisper-server

FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 libstdc++6 curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/whisper.cpp/build/bin/whisper-server /usr/local/bin/
COPY --from=builder /build/whisper.cpp/build/src/libwhisper.so* /usr/local/lib/
COPY --from=builder /build/whisper.cpp/build/ggml/src/libggml*.so* /usr/local/lib/
RUN ldconfig
COPY models /models
EXPOSE 8080
ENV HOST=0.0.0.0
ENV PORT=8080
ENTRYPOINT ["whisper-server", "-m", "/models/ggml-medium-q8_0.bin", "--host", "0.0.0.0", "--port", "8080"]
