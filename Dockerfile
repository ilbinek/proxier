# syntax=docker/dockerfile:1

# The UI and Go stages run on the build machine's architecture and cross-compile,
# so multi-arch builds (linux/amd64 + linux/arm64) don't need QEMU emulation.

# ---- UI ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- binary ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/proxier ./cmd/proxier \
 && mkdir -p /out/data

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/proxier /usr/local/bin/proxier
COPY --from=build --chown=65532:65532 /out/data /data
ENV PROXIER_DATA_DIR=/data \
    PROXIER_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/proxier", "serve"]
