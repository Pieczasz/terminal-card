# syntax=docker/dockerfile:1.4
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS builder

WORKDIR /code

ENV CGO_ENABLED=0
ENV GOPATH=/go
ENV GOCACHE=/go-build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod/cache \
    go mod download

COPY . .

# The builder stage runs on the build host (BUILDPLATFORM) for speed, so the target
# has to come from the ARGs buildx sets - otherwise a cross-build ships the builder's
# architecture inside a target-arch image and dies with "exec format error".
# Build the package, not one file: a second file in package main would be dropped.
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod/cache \
    --mount=type=cache,target=/go-build \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -o bin/backend ./cmd/server

FROM alpine:3.22 AS certs
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 65532 -H -s /sbin/nologin nonroot \
    && mkdir -p /data/ssh \
    && chown -R 65532:65532 /data

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=certs /etc/passwd /etc/passwd
# Ship /data/ssh owned by the non-root user so a fresh named volume inherits
# that ownership; otherwise the mount is root-owned and keygen cannot write it.
COPY --from=certs --chown=65532:65532 /data /data
COPY --from=builder /code/bin/backend /usr/local/bin/backend
USER nonroot
CMD ["/usr/local/bin/backend"]
