import { $, cd, fs, path } from "npm:zx";

export const workDir = new URL("..", import.meta.url).pathname;

export const absolute = (...segments: string[]) =>
    path.join(workDir, ...segments);

export const renderKraneNamespace = async (
    namespace: string,
    bindings: Record<string, unknown> = {},
) => {
    cd(workDir);

    await fs.mkdirp("tmp/krane");
    const renderDir = await fs.mkdtemp(`tmp/krane/${namespace}-`);

    const secretsPath = `deploy/${namespace}/secrets.ejson`;
    if (await fs.pathExists(secretsPath)) {
        await fs.copy(secretsPath, `${renderDir}/secrets.ejson`);
    }

    const bindingsJson = JSON.stringify(bindings);
    await $`krane render -f deploy/${namespace} --bindings=${bindingsJson} > ${renderDir}/krane.yaml`;

    return renderDir;
};

export const deployKraneNamespace = async (
    namespace: string,
    bindings: Record<string, unknown> = {},
) => {
    const renderDir = await renderKraneNamespace(namespace, bindings);
    await $`kubectl --context=orbstack create namespace ${namespace} || true`;
    await $`krane deploy ${namespace} orbstack -f ${renderDir}/*`;
};
