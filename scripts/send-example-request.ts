const response = await fetch("http://localhost:8080", {
    method: "POST",
    headers: {
        "x-fusion-tenant": "123",
        "x-fusion-namespace": "example",
        "x-fusion-deployment": "example",
        "x-fusion-assignment": "secret123",
        "content-type": "application/json",
    },
    body: JSON.stringify({
        hello: "world",
        foo: "bar",
    }),
});

console.log({
    status: response.status,
    headers: response.headers,
    body: await response.text(),
});
