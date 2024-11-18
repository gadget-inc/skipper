import { pino } from "npm:pino";
import pretty from "npm:pino-pretty";
const log = pino(pretty());

const assignPath = new URLPattern({ pathname: "/__fusion/assign" });
const assignFilePath = "/tmp/assignment.json";
let assignmentHeaders = await Deno.readTextFile(assignFilePath).then((txt) => JSON.parse(txt)).catch(() => null);

const server = Deno.serve({ port: 8080 }, async (request) => {
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
