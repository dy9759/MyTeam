import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import electron from "vite-plugin-electron/simple";

export default defineConfig({
  plugins: [
    tailwindcss(),
    react(),
    electron({
      main: {
        entry: "electron/main.ts",
      },
      preload: {
        input: path.join(__dirname, "electron/preload.ts"),
      },
      renderer: {},
    }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
      "@web": path.resolve(__dirname, "../web"),
    },
  },
  server: {
    port: 3333,
    fs: {
      allow: [
        path.resolve(__dirname, "."),
        path.resolve(__dirname, "../web"),
        path.resolve(__dirname, "../../packages"),
      ],
    },
  },
  build: {
    outDir: "dist",
  },
});
