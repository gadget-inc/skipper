# syntax=docker/dockerfile:1

FROM golang:1.24.1 AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify
COPY . .
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v -o /out/skipper .

FROM debian:bookworm-slim
RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update -qqy && \
    apt-get install -qqy --no-install-recommends --no-install-suggests \
    ca-certificates \
    curl \
    jq \
    less \
    vim
RUN update-ca-certificates
RUN useradd -ms /bin/bash skipper
WORKDIR /home/skipper
USER skipper
COPY --from=build --chown=skipper /out/skipper .
ENTRYPOINT ["./skipper"]
