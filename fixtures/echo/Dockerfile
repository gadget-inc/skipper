FROM denoland/deno:2.2.1
WORKDIR /app
USER deno
COPY main.ts .
RUN deno cache main.ts
CMD ["run", "-A", "--unstable-otel", "main.ts"]
