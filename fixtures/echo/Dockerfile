# syntax=docker/dockerfile:1

# Setup Nix and direnv
ARG NODE_VERSION
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

# Install dependencies
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY fixtures/echo/package.json ./fixtures/echo/
RUN pnpm install --frozen-lockfile --prod

# Final image
ARG NODE_VERSION
FROM node:${NODE_VERSION}-slim
RUN apt-get update -qqy && \
    apt-get install -qqy --no-install-recommends --no-install-suggests \
    ca-certificates curl jq less vim && \
    rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates
RUN useradd -ms /bin/bash echo
WORKDIR /home/echo
USER echo
COPY --from=build --chown=echo /build/node_modules ./node_modules
COPY --from=build --chown=echo /build/fixtures/echo/node_modules ./fixtures/echo/node_modules
COPY --chown=echo package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY --chown=echo fixtures/echo/package.json fixtures/echo/*.ts ./fixtures/echo/
WORKDIR /home/echo/fixtures/echo
CMD ["npm", "run", "start"]
