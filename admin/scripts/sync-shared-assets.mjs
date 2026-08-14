import { cp, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const adminRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const projectRoot = resolve(adminRoot, "..");
const source = resolve(projectRoot, "user/public/assets/brand");
const target = resolve(adminRoot, "public/assets/brand");

await mkdir(target, { recursive: true });
await cp(source, target, { recursive: true, force: true });
console.log(`Synced shared brand assets: ${source} -> ${target}`);
