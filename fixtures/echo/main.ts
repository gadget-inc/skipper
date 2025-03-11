import { pino } from "npm:pino";
import pretty from "npm:pino-pretty";
import { V2 } from "npm:paseto";
const log = pino(pretty());

const assignPath = new URLPattern({ pathname: "/__skipper/assign" });
const healthzPath = new URLPattern({ pathname: "/healthz" });

const tokenFilepath = "/tmp/token";
let assigned = await Deno.stat(tokenFilepath)
  .then(() => true)
  .catch(() => false);

const server = Deno.serve({ port: 8888 }, async (request) => {
  if (request.method === "GET" && healthzPath.test(request.url)) {
    return new Response();
  }

  const headers = Object.fromEntries(request.headers.entries());
  log.info({ method: request.method, url: request.url, headers }, "incoming request");

  if (request.method === "POST" && assignPath.test(request.url)) {
    if (assigned) {
      return new Response("already assigned", { status: 409 });
    }

    const token = request.headers.get("x-skipper-token") ?? "";
    const payload = await V2.verify(token, Deno.env.get("SKIPPER_PUBLIC_KEY")!);
    const fn = JSON.parse(request.headers.get("x-skipper-function")!);
    log.info({ token, payload, fn }, "assigned");

    await Deno.writeTextFile(tokenFilepath, token);
    assigned = true;

    return new Response();
  }

  if (!assigned) {
    return new Response("not assigned", { status: 503 });
  }

  if (request.headers.get("upgrade") !== "websocket") {
    return new Response(
      JSON.stringify({
        method: request.method,
        url: request.url,
        headers: Object.fromEntries(request.headers.entries()),
        body: await request.text(),
      }),
      {
        headers: {
          "content-type": "application/json",
        },
      }
    );
  }

  const { socket, response } = Deno.upgradeWebSocket(request);

  socket.addEventListener("open", () => {
    log.info("websocket opened");
  });

  socket.addEventListener("message", (event) => {
    log.info({ data: event.data }, "websocket message");
    if (event.data === "ping") {
      socket.send("pong");
    }
  });

  return response;
});

Deno.addSignalListener("SIGTERM", async () => {
  log.info("shutting down");
  await server.shutdown();
  log.info("shutdown");
});

await server.finished;
log.info("done");
