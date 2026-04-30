# check=skip=InvalidDefaultArgInFrom
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
COPY fixture/package.json ./fixture/
RUN pnpm install --frozen-lockfile --prod --filter fixture

# Final image
ARG NODE_VERSION
FROM node:${NODE_VERSION}-slim
RUN apt-get update -qqy && \
    apt-get install -qqy --no-install-recommends --no-install-suggests \
    ca-certificates curl jq less vim && \
    rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates
RUN useradd -ms /bin/bash fixture
WORKDIR /home/fixture
USER fixture
COPY --from=build --chown=fixture /build/node_modules ./node_modules
COPY --from=build --chown=fixture /build/fixture/node_modules ./fixture/node_modules
COPY --chown=fixture package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY --chown=fixture fixture/package.json fixture/*.ts ./fixture/
WORKDIR /home/fixture/fixture
CMD ["node", "--no-warnings", "--experimental-strip-types", "index.ts"]
