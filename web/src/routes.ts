import type { Router } from "@remix-run/fetch-router";
import { controller } from "./client.ts";
import { controllerPage } from "./views/controller.ts";
import { controllersPage } from "./views/controllers.ts";
import { configPage } from "./views/config.ts";
import { dashboardPage } from "./views/dashboard.ts";
import { eventsPage } from "./views/events.ts";
import { functionKey } from "./views/format.ts";
import { functionPage } from "./views/function.ts";
import { functionsPage } from "./views/functions.ts";
import { instancePage } from "./views/instance.ts";
import { routerPage } from "./views/router.ts";
import { routersPage } from "./views/routers.ts";
import { supervisorsPartial } from "./views/partials/supervisors.ts";
import { instancesPartial } from "./views/partials/instances.ts";
import { healthPartial } from "./views/partials/health.ts";
import { eventsPartial } from "./views/partials/events.ts";
import { recentActivityPartial } from "./views/partials/recent-activity.ts";

async function getState() {
  const response = await controller.getClusterState({});
  return response.clusterState!;
}

export function registerRoutes(router: Router): void {
  router.get("/", async () => {
    const state = await getState();
    return dashboardPage(state);
  });

  router.get("/functions", async () => {
    const state = await getState();
    return functionsPage(state);
  });

  router.get("/functions/:key", async (ctx) => {
    const state = await getState();
    const params = ctx.params as Record<string, string>;
    return functionPage(state, decodeURIComponent(params["key"]!));
  });

  router.get("/partials/supervisors", async () => {
    const state = await getState();
    return supervisorsPartial(state);
  });

  router.get("/partials/instances/:key", async (ctx) => {
    const state = await getState();
    const params = ctx.params as Record<string, string>;
    const key = decodeURIComponent(params["key"]!);
    const sup = state.supervisors.find((s: (typeof state.supervisors)[number]) => {
      const fn = s.function;
      return fn != null && functionKey(fn) === key;
    });
    return instancesPartial(sup?.instances ?? [], sup?.activeReplicaSet);
  });

  router.get("/partials/health", async () => {
    const state = await getState();
    return healthPartial(state);
  });

  router.get("/partials/events", async (ctx) => {
    const state = await getState();
    const url = new URL(ctx.request.url);
    const functionFilter = url.searchParams.get("function") ?? undefined;
    const severityFilter = url.searchParams.get("severity") ?? undefined;
    return eventsPartial(state.events, functionFilter, severityFilter);
  });

  router.get("/partials/recent-activity", async () => {
    const state = await getState();
    return recentActivityPartial(state.events);
  });

  router.get("/controllers", async () => {
    const state = await getState();
    return controllersPage(state);
  });

  router.get("/controllers/:ip", async (ctx) => {
    const state = await getState();
    const params = ctx.params as Record<string, string>;
    return controllerPage(state, decodeURIComponent(params["ip"]!));
  });

  router.get("/routers", async () => {
    const state = await getState();
    return routersPage(state);
  });

  router.get("/routers/:ip", async (ctx) => {
    const state = await getState();
    const params = ctx.params as Record<string, string>;
    return routerPage(state, decodeURIComponent(params["ip"]!));
  });

  router.get("/instances/:name", async (ctx) => {
    const state = await getState();
    const params = ctx.params as Record<string, string>;
    return instancePage(state, decodeURIComponent(params["name"]!));
  });

  router.get("/events", async (ctx) => {
    const state = await getState();
    const url = new URL(ctx.request.url);
    const functionFilter = url.searchParams.get("function") ?? undefined;
    const severityFilter = url.searchParams.get("severity") ?? undefined;
    return eventsPage(state, functionFilter, severityFilter);
  });

  router.get("/config", async () => {
    const state = await getState();
    return configPage(state);
  });

  router.get("/healthz", () => {
    return new Response("ok");
  });
}
