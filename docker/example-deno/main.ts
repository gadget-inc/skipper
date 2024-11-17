import { pino } from "npm:pino";
import pretty from "npm:pino-pretty";
const log = pino(pretty());

let isAssigned = false;

const assignPath = new URLPattern({ pathname: "/__fusion/assign" });

const server = Deno.serve({ port: 8080 }, (request) => {
    log.info({
        method: request.method,
        url: request.url,
        headers: Object.fromEntries(request.headers.entries()),
    }, "incoming request");

    if (request.method === "POST" && assignPath.test(request.url)) {
        if (isAssigned) {
            return new Response("already assigned", { status: 409 });
        }

        isAssigned = true;
        log.info({ headers: request.headers }, "assigned");
        return new Response();
    }

    if (!isAssigned) {
        return new Response("not assigned", { status: 503 });
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
