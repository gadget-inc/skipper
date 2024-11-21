import { pino } from "npm:pino";
import pretty from "npm:pino-pretty";
const log = pino(pretty());

const healthzPath = new URLPattern({ pathname: "/healthz" });

const assignPath = new URLPattern({ pathname: "/__fusion/assign" });
const assignFilePath = "/tmp/assignment.json";
let assignmentHeaders = await Deno.readTextFile(assignFilePath).then((txt) => JSON.parse(txt)).catch(() => null);

const server = Deno.serve({ port: 8888 }, async (request) => {
    if (request.method === "GET" && healthzPath.test(request.url)) {
        return new Response();
    }

    log.info({
        method: request.method,
        url: request.url,
        headers: Object.fromEntries(request.headers.entries()),
    }, "incoming request");

    if (request.method === "POST" && assignPath.test(request.url)) {
        if (assignmentHeaders) {
            return new Response("already assigned", { status: 409 });
        }

        assignmentHeaders = Object.fromEntries(request.headers.entries());
        await Deno.writeTextFile(assignFilePath, JSON.stringify(assignmentHeaders, null, 2));

        log.info({ assignmentHeaders }, "assigned");
        return new Response();
    }

    if (!assignmentHeaders) {
        return new Response("not assigned", { status: 503 });
    }

    if (request.headers.get("upgrade") === "websocket") {
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
    }

    return new Response(request.body, {
        headers: {
            "content-type": request.headers.get("content-type")!,
        },
    });
});

Deno.addSignalListener("SIGTERM", async () => {
    log.info("shutting down");
    await server.shutdown();
    log.info("shutdown");
});

await server.finished;

log.info("done");
