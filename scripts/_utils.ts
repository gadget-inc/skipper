import { $, fs, path } from "npm:zx";

export const workDir = new URL("..", import.meta.url).pathname;
export const abs = (...segments: string[]) => path.join(workDir, ...segments);

export const gitSha = async () => await $`git rev-parse --short HEAD`.then((res) => res.stdout.trim());

const imageTag = async (arch: string) => `sha-${await gitSha()}-${arch}`;
export const amd64DockerImageTag = async () => await imageTag("amd64");
export const arm64DockerImageTag = async () => await imageTag("arm64v8");

export const renderKraneNamespace = async (
    namespace: string,
    bindings: Record<string, unknown> = {},
) => {
    const deployDir = abs(`deploy/${namespace}`);
    const renderDir = abs(`tmp/krane/${namespace}`);
    await fs.emptyDir(renderDir);

    if (await fs.pathExists(`${deployDir}/secrets.ejson`)) {
        await fs.copy(`${deployDir}/secrets.ejson`, `${renderDir}/secrets.ejson`);
    }

    await $`krane render --filenames=${deployDir} --bindings=${JSON.stringify(bindings)} > ${renderDir}/rendered.yaml`;

    return renderDir;
};

export const deployKraneNamespace = async (
    namespace: string,
    bindings: Record<string, unknown> = {},
) => {
    const renderDir = await renderKraneNamespace(namespace, bindings);
    await $`kubectl --context=orbstack create namespace ${namespace} || true`;
    await $`krane deploy ${namespace} orbstack -f ${renderDir}/*`;
    await $`krane restart ${namespace} orbstack`;
};
