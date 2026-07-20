FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY llama-server-linux /usr/local/bin/llama-server

COPY libggml-base.so* /usr/local/lib/
COPY libggml-cpu.so* /usr/local/lib/
COPY libggml.so* /usr/local/lib/
COPY libllama-common.so* /usr/local/lib/
COPY libllama-server-impl.so /usr/local/lib/
COPY libllama.so* /usr/local/lib/
COPY libmtmd.so* /usr/local/lib/

RUN ldconfig

EXPOSE 8081
ENV HOST=0.0.0.0
ENV PORT=8081
ENTRYPOINT ["llama-server", "-m", "/models/gemma-4-E4B-it-Q4_0.gguf", "--host", "0.0.0.0", "--port", "8081", "-ngl", "99"]
