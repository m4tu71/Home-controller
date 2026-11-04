import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export function readAppVersion(repoRoot: string): string {
  const version = readFileSync(resolve(repoRoot, "VERSION"), "utf8").trim();
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error("Invalid VERSION");
  const pkg = JSON.parse(readFileSync(resolve(repoRoot, "frontend/package.json"), "utf8"));
  const lock = JSON.parse(readFileSync(resolve(repoRoot, "frontend/package-lock.json"), "utf8"));
  if (pkg.version !== version || lock.version !== version || lock.packages?.[""]?.version !== version) {
    throw new Error("VERSION, frontend/package.json and package-lock.json versions must match");
  }
  return version;
}
