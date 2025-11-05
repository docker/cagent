# syntax=docker/dockerfile:1

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.7.0 AS xx

FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine3.22 AS builder-base
COPY --from=xx / /
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download

FROM builder-base AS builder
COPY . ./
ARG GIT_TAG
ARG GIT_COMMIT
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod <<EOT
    set -ex
    xx-go build -trimpath -ldflags "-s -w -X 'github.com/docker/cagent/pkg/version.Version=$GIT_TAG' -X 'github.com/docker/cagent/pkg/version.Commit=$GIT_COMMIT'" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    xx-verify --static /binaries/cagent-$TARGETOS-$TARGETARCH
    if [ "$TARGETOS" = "windows" ]; then
      mv /binaries/cagent-$TARGETOS-$TARGETARCH /binaries/cagent-$TARGETOS-$TARGETARCH.exe
    fi
EOT

FROM scratch AS local
ARG TARGETOS TARGETARCH
COPY --from=builder /binaries/cagent-$TARGETOS-$TARGETARCH cagent

FROM scratch AS cross
COPY --from=builder /binaries .

FROM alpine
RUN apk add --no-cache ca-certificates docker-cli
RUN addgroup -S cagent && adduser -S -G cagent cagent
ARG TARGETOS TARGETARCH
ENV DOCKER_MCP_IN_CONTAINER=1
ENV TERM=xterm-256color
RUN mkdir /data /work && chmod 777 /data /work
COPY --from=docker/mcp-gateway:v2 /docker-mcp /usr/local/lib/docker/cli-plugins/
COPY --from=builder /binaries/cagent-$TARGETOS-$TARGETARCH /cagent
USER cagent
WORKDIR /work
ENTRYPOINT ["/cagent"]
