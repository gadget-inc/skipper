import process from "node:process";

import z from "zod";

export const routerUrl = process.env["SKIPPER_ROUTER_URL"] ?? "http://127.0.0.1:8081";

export const fixtureFunction = {
  namespace: "skipper-development-fixtures",
  deployment: "fixture",
  tenant: "tenant-1",
  metadata: JSON.stringify({ foo: "bar" }),
  scale: {
    min_instances: 0,
    max_instances: 5,
    target_cpu_usage_milli: 100,
    target_memory_usage_mib: 200,
    target_in_flight_requests: 50,
  },
};

export const FixtureResponseBody = z.object({
  method: z.string(),
  url: z.string(),
  headers: z.record(z.string(), z.string()),
  body: z.string(),
});

export type FixtureResponseBody = z.infer<typeof FixtureResponseBody>;
