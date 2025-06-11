import z from "zod";

export const routerUrl = "http://127.0.0.1:31020";
// export const routerUrl = "http://skipper-development-router.skipper-development.svc.cluster.local";

export const echoFunction = {
  namespace: "skipper-development-fixtures",
  deployment: "echo",
  tenant: "123",
  metadata: JSON.stringify({ foo: "bar" }),
  scale: {
    min_instances: 0,
    max_instances: 5,
    target_cpu_usage_milli: 100,
    target_memory_usage_mib: 200,
    target_in_flight_requests: 50,
  },
};

export const EchoResponseBody = z.object({
  method: z.string(),
  url: z.string(),
  headers: z.record(z.string(), z.string()),
  body: z.string(),
});

export type EchoResponseBody = z.infer<typeof EchoResponseBody>;
