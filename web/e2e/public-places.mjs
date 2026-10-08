// Browser regression against deterministic contract fixtures; not a substitute
// for the real PostgreSQL integration suite or production data verification.
import http from "node:http";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  pageHelpers,
  startBrowser,
  startWebServer,
  waitFor,
} from "../../admin/e2e/harness.mjs";

const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const names = {
  en: "Munich",
  "zh-CN": "慕尼黑",
  de: "München",
  fr: "Munich",
  es: "Múnich",
};
const requests = [];
const fixture = {
  id: "018bcfe5-6800-7000-8000-000000001002",
  slug: "munich",
  name: "Munich",
  type: "city",
  countryCode: "DE",
  parentId: null,
  timezone: null,
  latitude: null,
  longitude: null,
  currency: null,
  languages: [],
  coverageLevel: 1,
  publishedAt: "2026-10-08T00:00:00Z",
};
const api = http.createServer((request, response) => {
  const url = new URL(request.url, "http://localhost");
  requests.push(url);
  const locale = url.searchParams.get("locale") ?? "en";
  const place = { ...fixture, displayName: names[locale] ?? names.en };
  let status = 200;
  let body;
  if (url.pathname === "/api/public/places") {
    const q = url.searchParams.get("q") ?? "";
    if (q === "service-error") {
      status = 503;
      body = { type: "about:blank", title: "service unavailable", status };
    } else
      body = {
        items: !q || ["Munich", "München", "慕尼黑"].includes(q) ? [place] : [],
        nextOffset: null,
      };
  } else if (url.pathname === "/api/public/places/munich") body = place;
  else {
    status = 404;
    body = { type: "about:blank", title: "not found", status };
  }
  response.writeHead(status, {
    "Content-Type":
      status === 200 ? "application/json" : "application/problem+json",
  });
  response.end(JSON.stringify(body));
});
let web, page;
try {
  await new Promise((resolve) => api.listen(0, "127.0.0.1", resolve));
  web = await startWebServer({
    distDir: resolve(root, "web/dist"),
    apiPort: api.address().port,
  });
  page = await startBrowser();
  const base = `http://127.0.0.1:${web.port}`;
  const { bodyText, setValue, clickLast } = pageHelpers;
  await page.goto(`${base}/zh-CN`);
  await waitFor(
    async () => (await bodyText(page)).includes("哪里适合成为你的家"),
    { describe: "Chinese landing page" },
  );
  await setValue(page, "input[name='q']", "慕尼黑");
  await clickLast(page, "button[type='submit']");
  await waitFor(
    async () =>
      (await bodyText(page)).includes("慕尼黑") &&
      page.evaluate("location.pathname === '/zh-CN/places'"),
    { describe: "anonymous alias search" },
  );
  await clickLast(page, "a[href='/zh-CN/places/munich']");
  await waitFor(
    async () => (await bodyText(page)).includes("地点目录发布时间"),
    { describe: "place detail" },
  );
  const detail = await bodyText(page);
  if (!detail.includes("暂无数据") || !detail.includes("目前暂无评分"))
    throw new Error("missing data was presented as known");
  await clickLast(page, "a[lang='de']");
  await waitFor(
    async () =>
      (await bodyText(page)).includes("München") &&
      page.evaluate(
        "location.pathname === '/de/places/munich' && document.documentElement.lang === 'de'",
      ),
    { describe: "language switch preserves place" },
  );
  await page.goto(`${base}/en/places?q=unknown`);
  await waitFor(
    async () => (await bodyText(page)).includes("No published places"),
    { describe: "empty catalogue" },
  );
  await page.goto(`${base}/en/places?q=service-error`);
  await waitFor(async () => (await bodyText(page)).includes("Try again"), {
    describe: "service error with retry",
  });
  await page.goto(`${base}/fr/places/missing`);
  await waitFor(
    async () => (await bodyText(page)).includes("Ce lieu n’est pas disponible"),
    { describe: "unavailable place" },
  );
  await page.goto(`${base}/es/places?offset=-1`);
  await waitFor(
    async () => (await bodyText(page)).includes("Revisa los parámetros"),
    { describe: "invalid pagination" },
  );
  for (const locale of Object.keys(names)) {
    await page.goto(`${base}/${locale}/places/munich`);
    await waitFor(
      async () =>
        page.evaluate(
          `document.querySelector('h1')?.textContent === ${JSON.stringify(names[locale])}`,
        ),
      { describe: `localized detail ${locale}` },
    );
  }
  if (!requests.some((url) => url.searchParams.get("q") === "慕尼黑"))
    throw new Error("search did not reach the API");
  if (requests.some((url) => url.searchParams.has("tenant_id")))
    throw new Error("public browsing sent a tenant selector");
  console.log(
    "web-e2e: five locales, alias search, stable detail URLs, empty/error/missing states passed (contract fixtures)",
  );
} finally {
  if (page) await page.close();
  if (web) await web.close();
  await new Promise((resolve) => api.close(resolve));
}
