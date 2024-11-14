import { sleep } from "npm:zx";

// const url = "http://localhost:8080";
const url = "http://fusion.fusion-development.svc.cluster.local:8080";

const responses = [];
while (true) {
    // for (let i = 0; i < 100; i++) {
    responses.push(sendRequest());
    // }
    await Promise.all(responses);
    responses.length = 0;
    await sleep(1000);
}

async function sendRequest() {
    const response = await fetch(url, {
        method: "POST",
        headers: {
            "x-fusion-tenant": "123",
            "x-fusion-namespace": "example-development",
            "x-fusion-deployment": "example-deno",
            "x-fusion-assignment": "secret123",
            "content-type": "application/json",
        },
        body: JSON.stringify({
            hello: "world",
            foo: "bar",
            jason: "gedge",
        }),
    });

    console.log({
        status: response.status,
        headers: response.headers,
        body: await response.text(),
    });
}
