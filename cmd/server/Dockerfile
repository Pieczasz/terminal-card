# syntax=docker/dockerfile:1.4
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /code

ENV CGO_ENABLED=0
ENV GOPATH=/go
ENV GOCACHE=/go-build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod/cache \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod/cache \
    --mount=type=cache,target=/go-build \
    go build -o bin/backend cmd/server/main.go

FROM alpine:3.22 AS certs
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 65532 -H -s /sbin/nologin nonroot

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=certs /etc/passwd /etc/passwd
COPY --from=builder /code/bin/backend /usr/local/bin/backend
USER nonroot
CMD ["/usr/local/bin/backend"]
