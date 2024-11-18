import { sleep } from "npm:zx";

// const url = "http://localhost:8080";
const url = "http://fusion-router.fusion-development.svc.cluster.local:8080";

let i = 0;

async function sendRequest() {
    const response = await fetch(url, {
        method: "POST",
        headers: {
            "x-fusion-tenant": "123",
            "x-fusion-namespace": "example-development",
            "x-fusion-deployment": "example-deno",
            "x-fusion-metadata": "secret123",
            "x-fusion-min-replicas": "0",
            "x-fusion-max-replicas": "5",
            "x-fusion-target-cpu-utilization": "100",
            "x-fusion-target-memory-utilization": "200",
            "content-type": "application/json",
        },
        body: JSON.stringify({ hello: "world" }),
    });

    if (response.ok) {
        console.log("request", ++i, "status", response.status);
    } else {
        console.error("request", ++i, "status", response.status, "error", await response.text());
    }
}

// await sendRequest();

const responses = [];
while (true) {
    for (let i = 0; i < 1000; i++) {
        responses.push(sendRequest());
        await sleep(1);
    }
    await Promise.all(responses);
    responses.length = 0;
}
