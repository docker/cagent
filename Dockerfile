# syntax=docker/dockerfile:1

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.7.0 AS xx

# osxcross contains the MacOSX cross toolchain for xx
FROM crazymax/osxcross:14.5-r0-debian AS osxcross

FROM --platform=$BUILDPLATFORM golang:1.25.0-alpine3.22 AS builder-base
COPY --from=xx / /
ENV CGO_ENABLED=1
ARG GIT_TAG GIT_COMMIT BUILD_DATE
ARG TARGETPLATFORM TARGETOS TARGETARCH
WORKDIR /src

FROM builder-base AS builder-darwin
RUN apk add clang
COPY . ./
RUN --mount=type=bind,from=osxcross,src=/osxsdk,target=/xx-sdk \
    --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=telemetry_api_key \
    --mount=type=secret,id=telemetry_endpoint \
    --mount=type=secret,id=telemetry_header <<EOT
    set -ex
    TELEMETRY_API_KEY=$(cat /run/secrets/telemetry_api_key 2>/dev/null || echo "")
    TELEMETRY_ENDPOINT=$(cat /run/secrets/telemetry_endpoint 2>/dev/null || echo "")
    TELEMETRY_HEADER=$(cat /run/secrets/telemetry_header 2>/dev/null || echo "")
    xx-go build -trimpath -ldflags "-s -w -X 'github.com/docker/cagent/cmd/root.Version=$GIT_TAG' -X 'github.com/docker/cagent/cmd/root.Commit=$GIT_COMMIT' -X 'github.com/docker/cagent/cmd/root.BuildTime=$BUILD_DATE' -X 'github.com/docker/cagent/internal/telemetry.TelemetryEndpoint=$TELEMETRY_ENDPOINT' -X 'github.com/docker/cagent/internal/telemetry.TelemetryAPIKey=$TELEMETRY_API_KEY' -X 'github.com/docker/cagent/internal/telemetry.TelemetryHeader=$TELEMETRY_HEADER'" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    xx-verify --static /binaries/cagent-darwin-$TARGETARCH
EOT

FROM builder-base AS builder-linux
RUN apk add clang
RUN xx-apk add libx11-dev musl-dev gcc
COPY . ./
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=telemetry_api_key \
    --mount=type=secret,id=telemetry_endpoint \
    --mount=type=secret,id=telemetry_header <<EOT
    set -ex
    TELEMETRY_API_KEY=$(cat /run/secrets/telemetry_api_key 2>/dev/null || echo "")
    TELEMETRY_ENDPOINT=$(cat /run/secrets/telemetry_endpoint 2>/dev/null || echo "")
    TELEMETRY_HEADER=$(cat /run/secrets/telemetry_header 2>/dev/null || echo "")
    xx-go build -trimpath -ldflags "-s -w -linkmode=external -extldflags '-static' -X 'github.com/docker/cagent/cmd/root.Version=$GIT_TAG' -X 'github.com/docker/cagent/cmd/root.Commit=$GIT_COMMIT' -X 'github.com/docker/cagent/cmd/root.BuildTime=$BUILD_DATE' -X 'github.com/docker/cagent/internal/telemetry.TelemetryEndpoint=$TELEMETRY_ENDPOINT' -X 'github.com/docker/cagent/internal/telemetry.TelemetryAPIKey=$TELEMETRY_API_KEY' -X 'github.com/docker/cagent/internal/telemetry.TelemetryHeader=$TELEMETRY_HEADER'" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    xx-verify --static /binaries/cagent-linux-$TARGETARCH
EOT

FROM builder-base AS builder-windows
RUN apk add zig build-base
COPY . ./
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=telemetry_api_key \
    --mount=type=secret,id=telemetry_endpoint \
    --mount=type=secret,id=telemetry_header <<EOT
    set -ex
    TELEMETRY_API_KEY=$(cat /run/secrets/telemetry_api_key 2>/dev/null || echo "")
    TELEMETRY_ENDPOINT=$(cat /run/secrets/telemetry_endpoint 2>/dev/null || echo "")
    TELEMETRY_HEADER=$(cat /run/secrets/telemetry_header 2>/dev/null || echo "")
    CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu"  xx-go build -trimpath -ldflags "-s -w -X 'github.com/docker/cagent/cmd/root.Version=$GIT_TAG' -X 'github.com/docker/cagent/cmd/root.Commit=$GIT_COMMIT' -X 'github.com/docker/cagent/cmd/root.BuildTime=$BUILD_DATE' -X 'github.com/docker/cagent/internal/telemetry.TelemetryEndpoint=$TELEMETRY_ENDPOINT' -X 'github.com/docker/cagent/internal/telemetry.TelemetryAPIKey=$TELEMETRY_API_KEY' -X 'github.com/docker/cagent/internal/telemetry.TelemetryHeader=$TELEMETRY_HEADER'" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    ls -la /binaries
    mv /binaries/cagent-$TARGETOS-$TARGETARCH /binaries/cagent-$TARGETOS-$TARGETARCH.exe
    xx-verify --static /binaries/cagent-windows-$TARGETARCH.exe
EOT

FROM builder-$TARGETOS AS builder

FROM scratch AS local
ARG TARGETOS TARGETARCH
COPY --from=builder /binaries/cagent-$TARGETOS-$TARGETARCH cagent

FROM scratch AS cross
COPY --from=builder /binaries .

FROM alpine:3.22@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1
ARG TARGETOS TARGETARCH
COPY --from=builder /binaries/cagent-$TARGETOS-$TARGETARCH /cagent
RUN mkdir /data
ENTRYPOINT ["/cagent"]
