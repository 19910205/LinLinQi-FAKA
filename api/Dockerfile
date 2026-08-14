FROM golang:1.26.5-alpine3.24 AS builder
WORKDIR /src
ENV GOTOOLCHAIN=local
COPY go.mod go.sum* ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /linlinqi-api ./cmd/linlinqi

FROM alpine:3.24.1
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S -g 10001 linlinqi \
    && adduser -S -D -H -u 10001 -G linlinqi linlinqi
COPY --from=builder /linlinqi-api /usr/local/bin/linlinqi-api
USER 10001:10001
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
  CMD wget -q -T 2 -O /dev/null http://127.0.0.1:8080/ready || exit 1
ENTRYPOINT ["linlinqi-api"]
