import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformAuthContext } from "../platform/platform-auth-context";
import { PlatformFeedbackPage } from "./PlatformFeedbackPage";

const state = vi.hoisted(() => ({ failure: false }));
vi.mock("../../api/generated/wheretolive", async (load) => {
  const actual = await load<object>();
  return {
    ...actual,
    useListPlatformFeedback: () => ({
      isError: state.failure,
      data: state.failure
        ? undefined
        : {
            status: 200,
            data: {
              items: [
                {
                  id: "018bcfe5-6800-7000-8000-000000007201",
                  category: "bug",
                  title: "<script>private</script>",
                  message: "<img src=x onerror=attack>",
                  status: "resolved",
                  outcome: "Actual correction",
                  version: 3,
                  placeId: null,
                },
              ],
              nextOffset: null,
            },
          },
      refetch: vi.fn(),
    }),
    useListPlatformFeedbackCategories: () => ({
      data: { status: 200, data: [{ key: "bug", label: "Bug" }] },
    }),
    useListPlatformPlaces: () => ({}),
    useCreatePlatformFeedback: () => ({ mutate: vi.fn() }),
    useReviewPlatformFeedback: () => ({ mutate: vi.fn() }),
  };
});
function render() {
  return renderToStaticMarkup(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <PlatformAuthContext.Provider
          value={{
            status: "authenticated",
            csrfToken: "csrf",
            fetchOptions: {},
            login: async () => {},
            logout: async () => {},
          }}
        >
          <PlatformFeedbackPage />
        </PlatformAuthContext.Provider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
describe("restricted feedback surface", () => {
  beforeEach(() => {
    state.failure = false;
  });
  it("escapes untrusted feedback while displaying the recorded outcome", () => {
    const html = render();
    expect(html).toContain("Actual correction");
    expect(html).toContain("&lt;script&gt;private&lt;/script&gt;");
    expect(html).not.toContain("<script>private</script>");
    expect(html).not.toContain("<img src=x");
  });
  it("shows a retriable error instead of feedback content when loading fails", () => {
    state.failure = true;
    const html = render();
    expect(html).toContain("反馈加载失败");
    expect(html.replace(/\s+/g, "")).toContain("重试");
    expect(html).not.toContain("Actual correction");
  });
});
