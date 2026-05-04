# syntax=docker/dockerfile:1

# Setup Nix and direnv
FROM nixos/nix:2.29.0 AS build-base
RUN tee -a /etc/nix/nix.conf <<EOF
experimental-features = nix-command flakes
filter-syscalls = false
EOF
RUN nix-channel --update
RUN nix-env -iA nixpkgs.direnv
WORKDIR /build
COPY .envrc ./
COPY nix ./nix
RUN direnv allow && direnv exec . true
SHELL ["direnv", "exec", ".", "bash", "-lc"]

# Download dependencies (shared layer)
FROM build-base AS deps
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Build controller
FROM deps AS build-controller
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v -o /out/controller ./cmd/controller

# Build router
FROM deps AS build-router
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v -o /out/router ./cmd/router

# Build fixture
FROM deps AS build-fixture
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v -o /out/fixture ./cmd/fixture

# Runtime base
FROM debian:bookworm-slim AS runtime-base
RUN apt-get update -qqy && \
    apt-get install -qqy --no-install-recommends --no-install-suggests \
    ca-certificates curl jq less vim && \
    rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates
RUN useradd -u 1000 -ms /bin/bash skipper
WORKDIR /home/skipper
USER 1000

# Controller image
FROM runtime-base AS controller
COPY --from=build-controller --chown=1000 /out/controller ./controller
ENTRYPOINT ["./controller"]

# Router image
FROM runtime-base AS router
COPY --from=build-router --chown=1000 /out/router ./router
ENTRYPOINT ["./router"]

# Fixture image
FROM runtime-base AS fixture
COPY --from=build-fixture --chown=1000 /out/fixture ./fixture
ENTRYPOINT ["./fixture"]
