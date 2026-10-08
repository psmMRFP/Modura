import { defineConfig } from "orval";

export default defineConfig({
  whereToLive: {
    input: {
      target: "../api/openapi.yaml",
      filters: { mode: "include", tags: ["public-places"] },
    },
    output: {
      target: "src/api/generated/places.ts",
      client: "react-query",
      httpClient: "fetch",
      clean: true,
      formatter: "prettier",
      baseUrl: "/api",
    },
  },
});
