import { createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { ControllerService } from "./gen/controller_pb.ts";

const controllerHost = process.env["CONTROLLER_HOST"] ?? "localhost";
const controllerPort = process.env["CONTROLLER_PORT"] ?? "50051";

const transport = createGrpcTransport({
  baseUrl: `http://${controllerHost}:${controllerPort}`,
});

export const controller = createClient(ControllerService, transport);
