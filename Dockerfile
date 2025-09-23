# syntax=docker/dockerfile:1

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.7.0 AS xx

FROM --platform=$BUILDPLATFORM golang:1.25.0-alpine3.22 AS base
RUN apk add clang file
COPY --from=xx / /
ENV CGO_ENABLED=0
WORKDIR /src

FROM base AS builder-base
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download

FROM builder-base AS builder
COPY . ./
ARG GIT_TAG
ARG GIT_COMMIT
ARG BUILD_DATE
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=telemetry_api_key,env=TELEMETRY_API_KEY \
    --mount=type=secret,id=telemetry_endpoint,env=TELEMETRY_ENDPOINT \
    --mount=type=secret,id=telemetry_header,env=TELEMETRY_HEADER <<EOT
    set -ex
    xx-go build -trimpath -ldflags "-s -w -X 'github.com/docker/cagent/pkg/version.Version=$GIT_TAG' -X 'github.com/docker/cagent/pkg/version.Commit=$GIT_COMMIT' -X 'github.com/docker/cagent/pkg/version.BuildTime=$BUILD_DATE' -X 'github.com/docker/cagent/pkg/telemetry.TelemetryEndpoint=$TELEMETRY_ENDPOINT' -X 'github.com/docker/cagent/pkg/telemetry.TelemetryAPIKey=$TELEMETRY_API_KEY' -X 'github.com/docker/cagent/pkg/telemetry.TelemetryHeader=$TELEMETRY_HEADER'" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    file /binaries/cagent-$TARGETos-$TARGETARCH
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
ARG TARGETOS TARGETARCH
ENV DOCKER_MCP_IN_CONTAINER=1
ENTRYPOINT ["/cagent"]
RUN mkdir /data
COPY --from=docker/mcp-gateway:v2 /docker-mcp /usr/local/lib/docker/cli-plugins/
COPY --from=builder /binaries/cagent-$TARGETOS-$TARGETARCH /cagent
