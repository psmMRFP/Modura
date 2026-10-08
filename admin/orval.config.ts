import { defineConfig } from "orval";

export default defineConfig({
  wheretolive: {
    input: "../api/openapi.yaml",
    output: {
      target: "src/api/generated/wheretolive.ts",
      client: "react-query",
      httpClient: "fetch",
      clean: true,
      formatter: "prettier",
      baseUrl: "/api",
    },
  },
});
