# syntax=docker/dockerfile:1.7

# ---------------------------------------------------------------------------
# Stage 1: build - compiles a static binary with no external dependencies
# ---------------------------------------------------------------------------
FROM golang:1.27-alpine AS builder

# Build-time metadata injected by docker compose / Ansible.
ARG APP_VERSION=dev

WORKDIR /src

# Dependency layer first, so it is cached between source-only changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled => fully static binary that runs on a scratch image.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/http-server-projeto-korp .

# ---------------------------------------------------------------------------
# Stage 2: runtime - minimal, non-root, distroless-like image
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian13:nonroot AS runtime

ARG APP_VERSION=dev

LABEL org.opencontainers.image.title="http-server-projeto-korp" \
      org.opencontainers.image.description="Golang HTTP service for the Korp DevOps challenge" \
      org.opencontainers.image.version="${APP_VERSION}"

ENV LISTEN_ADDR=":8080" \
    APP_VERSION="${APP_VERSION}" \
    GO_VERSION="1.27"

COPY --from=builder /out/http-server-projeto-korp /usr/local/bin/http-server-projeto-korp

# Runs as UID 65532 (nonroot) - never as root.
USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/http-server-projeto-korp"]
