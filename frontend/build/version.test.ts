// @vitest-environment node
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, it } from "vitest";
import { readAppVersion } from "./version";
const roots: string[] = [];
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true }); });
function fixture(pkg = "1.0.0", lock = "1.0.0", nested = lock, version = "1.0.0") {
  const root = mkdtempSync(join(tmpdir(), "web-controller-version-")); roots.push(root);
  mkdirSync(join(root, "frontend")); writeFileSync(join(root, "VERSION"), version + "\n");
  writeFileSync(join(root, "frontend/package.json"), JSON.stringify({ version: pkg }));
  writeFileSync(join(root, "frontend/package-lock.json"), JSON.stringify({ version: lock, packages: { "": { version: nested } } }));
  return root;
}
it("accepts matching release versions", () => { expect(readAppVersion(fixture())).toBe("1.0.0"); });
it.each([["0.11.0", "1.0.0", "1.0.0"], ["1.0.0", "0.11.0", "1.0.0"], ["1.0.0", "1.0.0", "0.11.0"]])("rejects a mismatched version %s/%s/%s", (pkg, lock, nested) => {
  expect(() => readAppVersion(fixture(pkg, lock, nested))).toThrow("must match");
});
it("rejects malformed VERSION", () => { expect(() => readAppVersion(fixture("1.0.0", "1.0.0", "1.0.0", "v1.0.0"))).toThrow("Invalid VERSION"); });
