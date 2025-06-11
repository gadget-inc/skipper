# syntax=docker/dockerfile:1

# Setup Nix and direnv
FROM nixos/nix:2.29.0 AS build
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

# Build skipper
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v -o /out/skipper .

# Final image
FROM debian:bookworm-slim
RUN apt-get update -qqy && \
    apt-get install -qqy --no-install-recommends --no-install-suggests \
    ca-certificates curl jq less vim && \
    rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates
RUN useradd -ms /bin/bash skipper
WORKDIR /home/skipper
USER skipper
COPY --from=build --chown=skipper /out/skipper .
ENTRYPOINT ["./skipper"]
