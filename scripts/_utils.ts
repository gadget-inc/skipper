import { $, fs, path } from "npm:zx";
import { dedent as tsDedent } from "npm:ts-dedent";

export const dedent = tsDedent;

export const isCI = Deno.env.get("CI") === "1" || Deno.env.get("CI") === "true";

export const workspaceDir = new URL("..", import.meta.url).pathname;

export function abs(...segments: string[]) {
  return path.join(workspaceDir, ...segments);
}

export async function gitSha() {
  return await $`git rev-parse --short HEAD`.then((res) => res.stdout.trim());
}

export async function defaultImageTag() {
  return `sha-${await gitSha()}`;
}

export async function currentDockerPlatform() {
  return await $`docker version --format '{{.Server.Os}}/{{.Server.Arch}}'`.then((res) => res.stdout.trim());
}

export async function renderKraneNamespace(namespace: string, bindings: Record<string, unknown> = {}) {
  const deployDir = abs(`deploy/${namespace}`);
  const renderDir = abs(`tmp/krane/${namespace}`);
  await fs.emptyDir(renderDir);

  if (await fs.pathExists(`${deployDir}/secrets.ejson`)) {
    await fs.copy(`${deployDir}/secrets.ejson`, `${renderDir}/secrets.ejson`);
  }

  bindings = {
    deploy_dir: deployDir,
    render_dir: renderDir,
    workspace_dir: workspaceDir,
    ...bindings,
  };

  await $`krane render --filenames=${deployDir} --bindings=${JSON.stringify(bindings)} > ${renderDir}/rendered.yaml`;

  return renderDir;
}

export async function deployKraneNamespace(namespace: string, bindings: Record<string, unknown> = {}) {
  $.env.KUBECTL_CONTEXT ??= "orbstack";
  const renderDir = await renderKraneNamespace(namespace, bindings);
  await $`kubectl --context="$KUBECTL_CONTEXT" create namespace ${namespace}`.nothrow().quiet();
  await $`krane deploy ${namespace} "$KUBECTL_CONTEXT" -f ${renderDir}/*`;
  if (!isCI) {
    await $`krane restart ${namespace} "$KUBECTL_CONTEXT"`;
  }
}
