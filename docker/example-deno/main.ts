import * as log from "jsr:@std/log";

let isAssigned = false;

const shutdownController = new AbortController();

Deno.addSignalListener("SIGTERM", () => {
    log.info("shutting down");
    shutdownController.abort();
});

Deno.serve({ port: 8080, signal: shutdownController.signal }, (request) => {
    if (request.method === "POST" && request.url === "/__fusion/assign") {
        if (isAssigned) {
            return new Response("already assigned", { status: 400 });
        }
        isAssigned = true;

        log.info("assigned", { headers: request.headers });

        return new Response();
    }

    log.info("received request", {
        method: request.method,
        url: request.url,
        headers: request.headers,
    });

    return new Response(request.body, {
        headers: {
            "content-type": request.headers.get("content-type")!,
        },
    });
});
