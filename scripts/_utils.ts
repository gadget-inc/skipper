import assert from "node:assert";
import { existsSync } from "node:fs";
import { copyFile, mkdir, rm } from "node:fs/promises";
import { relative } from "node:path";
import process from "node:process";

import cleanStack from "clean-stack";
import { $, path } from "zx";

export const isCI = process.env["CI"] === "1" || process.env["CI"] === "true";

$.verbose = isCI;

process.on("unhandledRejection", reportErrorAndExit);
process.on("uncaughtException", reportErrorAndExit);

export async function $nothrow(template: TemplateStringsArray, ...args: unknown[]) {
  const result = await $(template, ...args).nothrow();
  if (result.exitCode) {
    process.exitCode = result.exitCode;
  }
}

export async function $stdout(template: TemplateStringsArray, ...args: unknown[]) {
  const result = await $(template, ...args);
  return result.stdout.trim();
}

export const workspaceDir = new URL("..", import.meta.url).pathname;

export function abs(...segments: string[]) {
  if (segments.length > 0 && path.isAbsolute(segments[0]!)) {
    return path.join(...segments);
  }
  return path.join(workspaceDir, ...segments);
}

export function rel(...segments: string[]): string {
  const filepath = path.isAbsolute(segments[0]!) ? path.join(...segments) : abs(...segments);
  return relative(workspaceDir, filepath);
}

export async function gitSha() {
  return await $stdout`git rev-parse --short HEAD`;
}

export async function currentDockerOs() {
  return await $stdout`docker version --format '{{.Server.Os}}'`;
}

export async function currentDockerArch() {
  return await $stdout`docker version --format '{{.Server.Arch}}'`;
}

export async function currentDockerPlatform() {
  return `${await currentDockerOs()}/${await currentDockerArch()}`;
}

export async function currentImageTag() {
  return `sha-${await gitSha()}-${await currentDockerArch()}`;
}

export async function currentImageDigest(name: string) {
  const tag = await currentImageTag();
  const digest = await $stdout`docker images --no-trunc --quiet ${name}:${tag}`;
  if (digest === "") {
    throw new Error(`Image digest for ${name}:${tag} not found`);
  }
  return digest;
}

export async function renderKraneNamespace(namespace: string, bindings: Record<string, unknown> = {}) {
  const deployDir = abs(`deploy/${namespace}`);
  const renderDir = abs(`tmp/krane/${namespace}`);
  await emptyDir(renderDir);

  if (existsSync(`${deployDir}/secrets.ejson`)) {
    await copyFile(`${deployDir}/secrets.ejson`, `${renderDir}/secrets.ejson`);
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
  $.env["SKIPPER_KUBECTL_CONTEXT"] ??= "orbstack";
  const renderDir = await renderKraneNamespace(namespace, bindings);
  await $`kubectl --context="$SKIPPER_KUBECTL_CONTEXT" create namespace ${namespace}`.nothrow().quiet();
  await $`krane deploy ${namespace} "$SKIPPER_KUBECTL_CONTEXT" -f ${renderDir}/*`;
}

export function isAbortError(error: unknown): error is DOMException {
  return error instanceof DOMException && error.name === "AbortError";
}

export async function emptyDir(dir: string) {
  await rm(dir, { recursive: true, force: true });
  await mkdir(dir, { recursive: true });
}

export function unwrap<T>(value: T, message?: string): NonNullable<T> {
  assert(value != null, message);
  return value;
}

export function reportErrorAndExit(error: unknown) {
  if (error instanceof Error) {
    console.error(cleanStack(error.stack, { basePath: workspaceDir }));
  } else {
    console.error(error);
  }
  process.exit(1);
}

export function expandSkipperAlias(only: string): string {
  return only
    .split(",")
    .flatMap((token) => (token === "skipper" ? ["controller", "router"] : [token]))
    .join(",");
}
