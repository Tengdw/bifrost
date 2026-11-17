import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

export default defineConfig({
  base: "./",
  plugins: [viteSingleFile()],
  resolve: { dedupe: ["antd", "echarts", "react", "react-dom"] },
  server: {
    fs: {
      allow: [
        fileURLToPath(new URL(".", import.meta.url)),
        fileURLToPath(
          new URL("../web/src/features/dashboard", import.meta.url),
        ),
      ],
    },
  },
  build: { reportCompressedSize: false },
});
