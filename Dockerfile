FROM golang:1.22-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/KrillinAI ./cmd/server

FROM ubuntu:latest

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends wget ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*

RUN mkdir -p bin && \
    ARCH=$(uname -m) && \
    case "$ARCH" in \
    x86_64) \
    YT_DLP_URL="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux"; \
    EDGE_TTS_URL="https://github.com/puji4810/edge-tts-pkg/releases/download/v0.0.1/edge-tts-linux-amd64"; \
    ;; \
    armv7l|armv7) \
    YT_DLP_URL="https://github.com/yt-dlp/yt-dlp/releases/download/2025.01.15/yt-dlp_linux_armv7l"; \
    EDGE_TTS_URL="https://github.com/puji4810/edge-tts-pkg/releases/download/v0.0.1/edge-tts-linux-armv7"; \
    ;; \
    aarch64|arm64) \
    YT_DLP_URL="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64"; \
    EDGE_TTS_URL="https://github.com/puji4810/edge-tts-pkg/releases/download/v0.0.1/edge-tts-linux-arm64"; \
    ;; \
    *) \
    echo "Unsupported architecture: $ARCH" && exit 1; \
    ;; \
    esac && \
    wget -O bin/yt-dlp "$YT_DLP_URL" && \
    wget -O bin/edge-tts "$EDGE_TTS_URL" && \
    chmod +x bin/yt-dlp bin/edge-tts

COPY --from=builder /out/KrillinAI ./KrillinAI
COPY config/config-example.toml ./config/config.toml

RUN sed -i 's/host = "127.0.0.1"/host = "0.0.0.0"/' ./config/config.toml && \
    mkdir -p /app/models /app/tasks /app/uploads && \
    chmod +x ./KrillinAI

VOLUME ["/app/bin", "/app/models", "/app/tasks"]

ENV PATH="/app/bin:${PATH}"

EXPOSE 8888/tcp

ENTRYPOINT ["./KrillinAI"]
