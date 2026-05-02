# syntax=docker/dockerfile:1

# Setup Nix and direnv (mirrors the controller/router Dockerfile build environment).
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

# Download Go module deps as a cacheable layer.
FROM build-base AS deps
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Build the fixture binary.
FROM deps AS build
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v -o /out/fixture ./cmd/fixture

# Runtime image.
FROM debian:bookworm-slim
RUN apt-get update -qqy && \
    apt-get install -qqy --no-install-recommends --no-install-suggests \
    ca-certificates curl jq && \
    rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates
RUN useradd -ms /bin/bash fixture
WORKDIR /home/fixture
USER fixture
COPY --from=build --chown=fixture /out/fixture ./fixture
ENTRYPOINT ["./fixture"]
