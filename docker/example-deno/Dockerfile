FROM denoland/deno:latest

WORKDIR /app

USER deno

COPY main.ts .

RUN deno cache main.ts

CMD ["run", "--allow-net", "main.ts"]
