import { renderToStaticMarkup } from "react-dom/server";
import {
  MemoryRouter,
  createMemoryRouter,
  RouterProvider,
} from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import {
  getGetPublicPlaceQueryKey,
  getSearchPublicPlacesQueryKey,
  type PublicPlace,
} from "../../api/generated/places";
import { PlacePage, PlacesPage } from "./PlacePages";
import { PlaceCard } from "./PlaceCard";
import { placeSearchUrl, searchInput } from "./search";

const place: PublicPlace = {
  id: "018bcfe5-6800-7000-8000-000000001002",
  slug: "munich",
  name: "Munich",
  displayName: "慕尼黑",
  type: "city",
  parentId: null,
  countryCode: "DE",
  timezone: null,
  latitude: null,
  longitude: null,
  currency: null,
  languages: [],
  coverageLevel: 1,
  publishedAt: "2026-10-08T00:00:00Z",
};
function renderPage(path: string, key: readonly unknown[], response: unknown) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(key, response);
  const router = createMemoryRouter(
    [
      { path: "/:locale/places", element: <PlacesPage /> },
      { path: "/:locale/places/:slug", element: <PlacePage /> },
    ],
    { initialEntries: [path] },
  );
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}
describe("public place browsing", () => {
  it("keeps multilingual query text shareable and bounds pagination", () => {
    const url = placeSearchUrl("zh-CN", "München & 慕尼黑", 20);
    const input = searchInput(
      new URL(url, "https://example.test").searchParams,
      "zh-CN",
    );
    expect(input).toEqual({
      q: "München & 慕尼黑",
      locale: "zh-CN",
      offset: 20,
      limit: 20,
    });
    expect(searchInput(new URLSearchParams("offset=-1"), "en")).toBeNull();
    expect(searchInput(new URLSearchParams("offset=10001"), "en")).toBeNull();
    expect(
      searchInput(new URLSearchParams({ q: "a".repeat(121) }), "en"),
    ).toBeNull();
  });
  it("links stable slugs and escapes untrusted display names", () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <PlaceCard
          place={{ ...place, displayName: "<script>alert(1)</script>" }}
          locale="de"
        />
      </MemoryRouter>,
    );
    expect(html).toContain("/de/places/munich");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("<script>");
  });
  it("renders a populated catalogue with locale-specific detail links", () => {
    const html = renderPage(
      "/zh-CN/places",
      getSearchPublicPlacesQueryKey({
        q: "",
        locale: "zh-CN",
        offset: 0,
        limit: 20,
      }),
      { status: 200, data: { items: [place], nextOffset: null } },
    );
    expect(html).toContain("慕尼黑");
    expect(html).toContain("/zh-CN/places/munich");
  });
  it("distinguishes empty results from service errors", () => {
    const key = getSearchPublicPlacesQueryKey({
      q: "",
      locale: "en",
      offset: 0,
      limit: 20,
    });
    expect(
      renderPage("/en/places", key, {
        status: 200,
        data: { items: [], nextOffset: null },
      }),
    ).toContain("No published places");
    expect(renderPage("/en/places", key, { status: 503, data: {} })).toContain(
      "Try again",
    );
  });
  it("renders missing metadata honestly and handles unavailable detail", () => {
    const key = getGetPublicPlaceQueryKey("munich", { locale: "zh-CN" });
    const html = renderPage("/zh-CN/places/munich", key, {
      status: 200,
      data: place,
    });
    expect(html).toContain("暂无数据");
    expect(html).toContain("不代表各项事实的核验时间");
    expect(
      renderPage("/zh-CN/places/munich", key, { status: 404, data: {} }),
    ).toContain("此地点暂不可用");
  });
});
