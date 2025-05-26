import { $, path } from "npm:zx";
import { copy, emptyDir, exists } from "jsr:@std/fs";

export const isCI = Deno.env.get("CI") === "1" || Deno.env.get("CI") === "true";

$.verbose = isCI;

export const workspaceDir = new URL("..", import.meta.url).pathname;

export function abs(...segments: string[]) {
  return path.join(workspaceDir, ...segments);
}

export async function gitSha() {
  return await $`git rev-parse --short HEAD`.then((res) => res.stdout.trim());
}

export async function currentImageTag() {
  return `sha-${await gitSha()}`;
}

export async function currentImageDigest(name: string) {
  const tag = await currentImageTag();
  const digest = await $`docker images --no-trunc --quiet ${name}:${tag}`.then((res) => res.stdout.trim());
  if (digest === "") {
    throw new Error(`Image digest for ${name}:${tag} not found`);
  }
  return digest;
}

export async function currentDockerPlatform() {
  return await $`docker version --format '{{.Server.Os}}/{{.Server.Arch}}'`.then((res) => res.stdout.trim());
}

export async function renderKraneNamespace(namespace: string, bindings: Record<string, unknown> = {}) {
  const deployDir = abs(`deploy/${namespace}`);
  const renderDir = abs(`tmp/krane/${namespace}`);
  await emptyDir(renderDir);

  if (await exists(`${deployDir}/secrets.ejson`)) {
    await copy(`${deployDir}/secrets.ejson`, `${renderDir}/secrets.ejson`);
  }

  bindings = {
    deploy_dir: deployDir.replace(/\/$/, ""), // remove trailing slash
    render_dir: renderDir.replace(/\/$/, ""),
    workspace_dir: workspaceDir.replace(/\/$/, ""),
    ...bindings,
  };

  await $`krane render --filenames=${deployDir} --bindings=${JSON.stringify(bindings)} > ${renderDir}/rendered.yaml`;

  return renderDir;
}

export async function deployKraneNamespace(namespace: string, bindings: Record<string, unknown> = {}) {
  $.env.SKIPPER_KUBECTL_CONTEXT ??= "orbstack";
  const renderDir = await renderKraneNamespace(namespace, bindings);
  await $`kubectl --context="$SKIPPER_KUBECTL_CONTEXT" create namespace ${namespace}`.nothrow().quiet();
  await $`krane deploy ${namespace} "$SKIPPER_KUBECTL_CONTEXT" -f ${renderDir}/*`;
}

export function isAbortError(error: unknown): error is DOMException {
  return error instanceof DOMException && error.name === "AbortError";
}
