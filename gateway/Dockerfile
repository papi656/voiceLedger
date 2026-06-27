FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gateway .

FROM alpine:3.20
RUN apk add --no-cache curl ffmpeg
COPY --from=builder /build/gateway /usr/local/bin/gateway
EXPOSE 9090
ENTRYPOINT ["gateway"]
