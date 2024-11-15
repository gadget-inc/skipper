FROM denoland/deno:latest

WORKDIR /app

USER deno

COPY main.ts .

RUN deno cache main.ts

CMD ["run", "-A", "--v8-flags=--max-old-space-size=448", "main.ts"]
