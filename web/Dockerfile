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
COPY web/package.json ./web/
RUN pnpm install --frozen-lockfile --prod

# Final image
ARG NODE_VERSION
FROM node:${NODE_VERSION}-slim
RUN useradd -ms /bin/bash web
WORKDIR /home/web
USER web
COPY --from=build --chown=web /build/node_modules ./node_modules
COPY --from=build --chown=web /build/web/node_modules ./web/node_modules
COPY --chown=web package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY --chown=web web/package.json web/tsconfig.json ./web/
COPY --chown=web web/src ./web/src
WORKDIR /home/web/web
CMD ["npm", "run", "start"]
