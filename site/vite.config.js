import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// base "./" makes the build work under any GitHub Pages path
// (https://<user>.github.io/<repo>/).
export default defineConfig({
  base: "./",
  plugins: [svelte()],
});
