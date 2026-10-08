import { describe, expect, it } from "vitest";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { AuthPage } from "./AuthPage";
import { ConsumerSession } from "./ConsumerSession";
import { getGetConsumerAuthStatusQueryKey } from "../../api/generated/places";
import { locales } from "../../app/locales";
import { authMessages } from "./messages";
function render(locale: string, enabled: boolean) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(getGetConsumerAuthStatusQueryKey(), {
    status: 200,
    data: { enabled, challengeSiteKey: enabled ? "site-key" : null },
  });
  const router = createMemoryRouter(
    [{ path: "/:locale/login", element: <AuthPage /> }],
    { initialEntries: [`/${locale}/login`] },
  );
  return renderToString(
    <QueryClientProvider client={client}>
      <ConsumerSession>
        <RouterProvider router={router} />
      </ConsumerSession>
    </QueryClientProvider>,
  );
}
describe("public identity UI", () => {
  it("keeps unavailable registration closed in every UI language", () => {
    for (const locale of locales) {
      const html = render(locale, false);
      expect(html).toContain(authMessages[locale].disabled);
      expect(html).not.toContain('type="password"');
    }
  });
  it("renders generated API forms and locale-preserving account links", () => {
    const html = render("zh-CN", true);
    expect(html).toContain('type="password"');
    expect(html).toContain("/zh-CN/verify-email");
    expect(html).toContain("/zh-CN/reset-password");
    expect(html).toContain('disabled=""');
    expect(html).not.toContain("localStorage");
  });
});
