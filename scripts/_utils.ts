import { path } from "npm:zx";

export const workDir = new URL("..", import.meta.url).pathname;

export const absolute = (...segments: string[]) =>
    path.join(workDir, ...segments);
