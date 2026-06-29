FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY whisper-server-linux /usr/local/bin/whisper-server

COPY libwhisper.so.1.9.1 \
     libggml.so.0.15.3 \
     libggml-base.so.0.15.3 \
     libggml-cpu.so.0.15.3 \
     /usr/local/lib/

RUN cd /usr/local/lib && \
    ln -s libwhisper.so.1.9.1 libwhisper.so.1 && \
    ln -s libwhisper.so.1 libwhisper.so && \
    ln -s libggml.so.0.15.3 libggml.so.0 && \
    ln -s libggml.so.0 libggml.so && \
    ln -s libggml-base.so.0.15.3 libggml-base.so.0 && \
    ln -s libggml-base.so.0 libggml-base.so && \
    ln -s libggml-cpu.so.0.15.3 libggml-cpu.so.0 && \
    ln -s libggml-cpu.so.0 libggml-cpu.so && \
    ldconfig

COPY models /models

EXPOSE 8080
ENV HOST=0.0.0.0
ENV PORT=8080
ENTRYPOINT ["whisper-server", "-m", "/models/ggml-medium-q8_0.bin", "--host", "0.0.0.0", "--port", "8080"]
