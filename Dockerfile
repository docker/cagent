# syntax=docker/dockerfile:1

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.7.0 AS xx

FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine3.22 AS builder-base
COPY --from=xx / /
RUN apk add --no-cache file git
ENV CGO_ENABLED=0
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download

FROM builder-base AS version
RUN --mount=target=. <<'EOT'
  git rev-parse HEAD 2>/dev/null || {
    echo >&2 "Failed to get git revision, make sure --build-arg BUILDKIT_CONTEXT_KEEP_GIT_DIR=1 is set when building from Git directly"
    exit 1
  }
  set -ex
  export PKG=github.com/docker/cagent VERSION=$(git describe --match 'v[0-9]*' --dirty='.m' --always --tags) COMMIT=$(git rev-parse HEAD)$(if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi);
  echo "-X ${PKG}/pkg/version.Version=${VERSION} -X ${PKG}/pkg/version.Commit=${COMMIT}" > /tmp/.ldflags;
  echo -n "${VERSION}" > /tmp/.version;
EOT

FROM builder-base AS builder
COPY . ./
ARG TARGETPLATFORM
ARG TARGETOS
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=source=/tmp/.ldflags,target=/tmp/.ldflags,from=version \
    --mount=type=cache,target=/go/pkg/mod <<EOT
    set -ex
    xx-go build -trimpath -ldflags "-s -w $(cat /tmp/.ldflags)" -o /binaries/cagent .
    xx-verify --static /binaries/cagent
    if [ "$TARGETOS" = "windows" ]; then
      mv /binaries/cagent /binaries/cagent.exe
    fi
EOT

FROM scratch AS binaries
COPY --from=builder /binaries .

FROM --platform=$BUILDPLATFORM alpine AS releaser
WORKDIR /work
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN --mount=from=binaries <<EOT
  set -e
  mkdir /out
  [ "$TARGETOS" = "windows" ] && ext=".exe"
  for f in *; do
    cp "$f" "/out/${f%.*}-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}${ext}"
  done
EOT

FROM scratch AS release
COPY --from=releaser /out/ /

FROM alpine
RUN apk add --no-cache ca-certificates docker-cli
RUN addgroup -S cagent && adduser -S -G cagent cagent
ENV DOCKER_MCP_IN_CONTAINER=1
ENV TERM=xterm-256color
RUN mkdir /data /work && chmod 777 /data /work
COPY --from=docker/mcp-gateway:v2 /docker-mcp /usr/local/lib/docker/cli-plugins/
COPY --from=builder /binaries/cagent /cagent
USER cagent
WORKDIR /work
ENTRYPOINT ["/cagent"]
