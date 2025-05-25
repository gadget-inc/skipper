ARG DENO_VERSION=latest
FROM denoland/deno:${DENO_VERSION}
WORKDIR /app
USER deno
COPY main.ts .
RUN deno cache main.ts
CMD ["run", "-A", "--unstable-otel", "main.ts"]
