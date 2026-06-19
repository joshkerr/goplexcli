import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

// emptyOutDir wipes dist on every build, including the tracked .gitkeep that
// keeps the directory present for the Go go:embed directive on a fresh clone.
// Re-create it after the bundle is written.
function keepDist(): Plugin {
  return {
    name: "keep-dist-gitkeep",
    closeBundle() {
      writeFileSync(
        resolve(__dirname, "dist/.gitkeep"),
        "# Keeps dist present so the go:embed in gui/main.go compiles before the\n" +
          "# frontend is built. Real assets are produced by `wails build`.\n"
      );
    },
  };
}

export default defineConfig({
  plugins: [react(), keepDist()],
  // Wails serves the built assets relative to the embedded FS root.
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
