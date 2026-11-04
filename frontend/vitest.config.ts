import { resolve } from "node:path";
import { readAppVersion } from "./build/version";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  define: { __APP_VERSION__: JSON.stringify(`v${readAppVersion(resolve(__dirname, ".."))}`) },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    clearMocks: true,
    restoreMocks: true,
  },
});
