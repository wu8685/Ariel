import baseConfig from "./playwright.config";
import { defineConfig } from "@playwright/test";

export default defineConfig({
  ...baseConfig,
  webServer: {
    command: "npm run preview -- --host 127.0.0.1 --port 4173 --strictPort",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: false,
  },
});
