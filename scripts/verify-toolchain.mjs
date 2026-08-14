import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const read = (path) => readFileSync(join(root, path), "utf8");
const json = (path) => JSON.parse(read(path));
const versions = json("toolchain.json");

function equal(actual, expected, label) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${expected}, received ${actual}`);
  }
}

function includes(path, expected) {
  if (!read(path).includes(expected)) {
    throw new Error(`${path}: missing ${JSON.stringify(expected)}`);
  }
}

equal(read(".node-version").trim(), versions.node, ".node-version");
equal(read(".nvmrc").trim(), versions.node, ".nvmrc");
equal(read(".go-version").trim(), versions.go, ".go-version");
includes(".tool-versions", `golang ${versions.go}`);
includes(".tool-versions", `nodejs ${versions.node}`);
includes(".tool-versions", `postgres ${versions.postgres}`);
includes(".tool-versions", `redis ${versions.redis}`);
includes("api/go.mod", `go ${versions.go}`);

for (const app of ["admin", "user"]) {
  const manifest = json(`${app}/package.json`);
  const lock = json(`${app}/package-lock.json`).packages[""];
  equal(
    manifest.packageManager,
    `npm@${versions.npm}`,
    `${app} packageManager`,
  );
  equal(
    manifest.dependencies["@lucide/vue"],
    versions.lucideVue,
    `${app} @lucide/vue`,
  );
  equal(manifest.dependencies.vue, versions.vue, `${app} Vue`);
  equal(manifest.dependencies["vue-i18n"], versions.vueI18n, `${app} vue-i18n`);
  equal(
    manifest.devDependencies.typescript,
    versions.typescript,
    `${app} TypeScript`,
  );
  equal(manifest.devDependencies["vue-tsc"], versions.vueTsc, `${app} vue-tsc`);
  equal(manifest.devDependencies.vite, versions.vite, `${app} Vite`);
  equal(
    lock.dependencies["@lucide/vue"],
    versions.lucideVue,
    `${app} lock @lucide/vue`,
  );
  equal(lock.dependencies.vue, versions.vue, `${app} lock Vue`);
  equal(
    lock.dependencies["vue-i18n"],
    versions.vueI18n,
    `${app} lock vue-i18n`,
  );
  equal(
    lock.devDependencies.typescript,
    versions.typescript,
    `${app} lock TypeScript`,
  );
}

for (const path of ["admin/Dockerfile", "user/Dockerfile"]) {
  includes(path, `FROM node:${versions.node}-alpine3.24`);
  includes(path, `npm@${versions.npm}`);
  includes(path, `FROM nginx:${versions.nginx}-alpine3.24`);
}
includes("api/Dockerfile", `FROM golang:${versions.go}-alpine3.24`);
includes("api/Dockerfile", `FROM alpine:${versions.alpine}`);
includes("docker-compose.yml", `image: postgres:${versions.postgres}-alpine`);
includes("docker-compose.yml", `image: redis:${versions.redis}-alpine`);
includes("docker-compose.yml", `image: alpine:${versions.alpine}`);
includes(".github/workflows/ci.yml", `go-version: ${versions.go}`);
includes(".github/workflows/ci.yml", `node-version: ${versions.node}`);
includes(".github/workflows/ci.yml", `npm@${versions.npm}`);
includes(
  ".github/workflows/ci.yml",
  `golang.org/x/vuln/cmd/govulncheck@v${versions.govulncheck}`,
);
includes(
  ".github/workflows/ci.yml",
  `actions/checkout@v${versions.checkoutAction}`,
);
includes(
  ".github/workflows/ci.yml",
  `actions/setup-go@v${versions.setupGoAction}`,
);
includes(
  ".github/workflows/ci.yml",
  `actions/setup-node@v${versions.setupNodeAction}`,
);
includes(
  ".github/workflows/ci.yml",
  `image: postgres:${versions.postgres}-alpine`,
);
includes(".github/workflows/ci.yml", `image: redis:${versions.redis}-alpine`);

console.log("LinLinQi toolchain versions are synchronized.");
