# syntax=docker/dockerfile:1

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.7.0 AS xx

# osxcross contains the MacOSX cross toolchain for xx
FROM crazymax/osxcross:14.5-r0-debian AS osxcross

FROM --platform=$BUILDPLATFORM golang:1.25.0-alpine3.22 AS builder-base
COPY --from=xx / /
ENV CGO_ENABLED=1
ARG TARGETPLATFORM TARGETOS TARGETARCH
WORKDIR /src

FROM builder-base AS ldflags
ARG GIT_TAG
ARG GIT_COMMIT
ARG BUILD_DATE
RUN --mount=type=secret,id=telemetry_api_key,env=TELEMETRY_API_KEY \
    --mount=type=secret,id=telemetry_endpoint,env=TELEMETRY_ENDPOINT \
    --mount=type=secret,id=telemetry_header,env=TELEMETRY_HEADER <<EOT
  set -e
  echo "-s -w -X 'github.com/docker/cagent/cmd/root.Version=$GIT_TAG' -X 'github.com/docker/cagent/cmd/root.Commit=$GIT_COMMIT' -X 'github.com/docker/cagent/cmd/root.BuildTime=$BUILD_DATE' -X 'github.com/docker/cagent/internal/telemetry.TelemetryEndpoint=$TELEMETRY_ENDPOINT' -X 'github.com/docker/cagent/internal/telemetry.TelemetryAPIKey=$TELEMETRY_API_KEY' -X 'github.com/docker/cagent/internal/telemetry.TelemetryHeader=$TELEMETRY_HEADER'" > /tmp/.ldflags;
EOT

FROM builder-base AS builder-darwin
RUN apk add clang
COPY . ./
RUN --mount=type=bind,from=osxcross,src=/osxsdk,target=/xx-sdk \
    --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=source=/tmp/.ldflags,target=/tmp/.ldflags,from=ldflags <<EOT
    set -ex
    xx-go build -trimpath -ldflags "$(cat /tmp/.ldflags)" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    xx-verify --static /binaries/cagent-$TARGETOS-$TARGETARCH
EOT

FROM builder-base AS builder-linux
RUN apk add clang
RUN xx-apk add libx11-dev musl-dev gcc
COPY . ./
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=source=/tmp/.ldflags,target=/tmp/.ldflags,from=ldflags <<EOT
    set -ex
    xx-go build -trimpath -ldflags "-linkmode=external -extldflags '-static' $(cat /tmp/.ldflags)" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    xx-verify --static /binaries/cagent-$TARGETOS-$TARGETARCH
EOT

FROM builder-base AS builder-windows
RUN apk add zig build-base
COPY . ./
RUN --mount=type=cache,target=/root/.cache,id=docker-ai-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=source=/tmp/.ldflags,target=/tmp/.ldflags,from=ldflags <<EOT
    set -ex
    CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu"  xx-go build -trimpath -ldflags "$(cat /tmp/.ldflags)" -o /binaries/cagent-$TARGETOS-$TARGETARCH .
    mv /binaries/cagent-$TARGETOS-$TARGETARCH /binaries/cagent-$TARGETOS-$TARGETARCH.exe
    xx-verify --static /binaries/cagent-$TARGETOS-$TARGETARCH.exe
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
