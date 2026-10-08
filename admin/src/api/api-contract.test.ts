import { describe, expect, it } from "vitest";

import { getGetLivenessUrl, getGetReadinessUrl } from "./generated/wheretolive";

describe("generated API routes", () => {
  it("uses the shared API prefix", () => {
    expect(getGetLivenessUrl()).toBe("/api/livez");
    expect(getGetReadinessUrl()).toBe("/api/readyz");
  });
});
