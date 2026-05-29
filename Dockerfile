# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/sub2api-fast-proxy \
    ./cmd/sub2api-fast-proxy

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/sub2api-fast-proxy /sub2api-fast-proxy

USER nonroot:nonroot
EXPOSE 8787

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/sub2api-fast-proxy", "-healthcheck"]

ENTRYPOINT ["/sub2api-fast-proxy"]
