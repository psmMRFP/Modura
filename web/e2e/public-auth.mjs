// UI contract fixtures only: fake CAPTCHA and mail codes never enter production code.
import http from "node:http";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  startBrowser,
  startWebServer,
  pageHelpers,
  waitFor,
} from "../../admin/e2e/harness.mjs";

const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
let enabled = false,
  registered = false,
  verified = false,
  sessionValid = false;
let password = "a sufficiently long test password";
const operations = [];
const api = http.createServer(async (request, response) => {
  const path = new URL(request.url, "http://localhost").pathname;
  let body = "";
  for await (const chunk of request) body += chunk;
  const data = body ? JSON.parse(body) : {};
  operations.push({
    path,
    method: request.method,
    query: new URL(request.url, "http://localhost").search,
  });
  function answer(status, data) {
    response.writeHead(status, { "Content-Type": "application/json" });
    response.end(data ? JSON.stringify(data) : undefined);
  }
  if (path === "/api/public/auth/status")
    return answer(200, {
      enabled,
      challengeSiteKey: enabled ? "fixture-site-key" : null,
    });
  if (!enabled)
    return answer(503, {
      type: "about:blank",
      title: "unavailable",
      status: 503,
    });
  if (
    request.method === "POST" &&
    !["/api/public/auth/refresh", "/api/public/auth/logout"].includes(path) &&
    data.challenge !== "fixture-proof"
  )
    return answer(403, {
      type: "about:blank",
      title: "verification failed",
      status: 403,
    });
  if (path.endsWith("/register")) {
    registered = true;
    return answer(202);
  }
  if (path.endsWith("/verify-email")) {
    verified =
      registered &&
      data.code === "fixture-email-code-with-32-or-more-characters";
    return answer(verified ? 204 : 401);
  }
  if (path.endsWith("/resend-verification") || path.endsWith("/recover"))
    return answer(202);
  if (path.endsWith("/reset-password")) {
    if (data.code !== "fixture-reset-code-with-32-or-more-characters")
      return answer(401);
    password = data.password;
    sessionValid = false;
    return answer(204);
  }
  if (path.endsWith("/login") || path.endsWith("/refresh")) {
    if (
      path.endsWith("/login")
        ? !verified || data.password !== password
        : !sessionValid || request.headers["x-csrf-token"] !== "fixture-csrf"
    )
      return answer(401);
    sessionValid = true;
    response.setHeader("Set-Cookie", [
      "wheretolive_refresh=fixture-refresh; HttpOnly; SameSite=Strict; Path=/api/public/auth",
      "wheretolive_csrf=fixture-csrf; SameSite=Strict; Path=/",
    ]);
    return answer(200, {
      accessToken: "fixture-access",
      tokenType: "Bearer",
      expiresIn: 300,
      csrfToken: "fixture-csrf",
    });
  }
  if (path.endsWith("/me")) {
    if (
      !sessionValid ||
      request.headers.authorization !== "Bearer fixture-access"
    )
      return answer(401);
    return answer(200, {
      id: "018bcfe5-6800-7000-8000-000000000201",
      username: "member",
      email: "person@example.org",
      status: "active",
      updatedAt: "2026-10-08T00:00:00Z",
    });
  }
  if (path.endsWith("/logout")) {
    sessionValid = false;
    return answer(204);
  }
  return answer(404);
});
let web, page;
try {
  await new Promise((resolve) => api.listen(0, "127.0.0.1", resolve));
  web = await startWebServer({
    distDir: resolve(root, "web/dist"),
    apiPort: api.address().port,
  });
  page = await startBrowser();
  await page.addInitScript(
    `window.turnstile={render:(node,options)=>{if(options.action!=='public_identity')throw new Error('missing challenge action');queueMicrotask(()=>options.callback('fixture-proof'));return 'fixture-widget';},remove:()=>{}};`,
  );
  const base = `http://127.0.0.1:${web.port}`;
  for (const locale of ["en", "zh-CN", "de", "fr", "es"]) {
    await page.goto(`${base}/${locale}/register`);
    await waitFor(
      () => page.evaluate("document.querySelector('.ant-alert')!==null"),
      { describe: "closed account UI" },
    );
    if (
      await page.evaluate(
        "document.querySelector('input[type=password]')!==null",
      )
    )
      throw new Error("disabled account form rendered");
  }
  enabled = true;
  async function form(path, values) {
    await page.goto(`${base}/en/${path}`);
    await waitFor(
      () =>
        page.evaluate(
          "document.querySelector('button[type=submit]')?.disabled===false",
        ),
      { describe: "challenge-ready form" },
    );
    for (const [key, value] of Object.entries(values))
      await pageHelpers.setValue(page, `#${key}`, value);
    await pageHelpers.clickLast(page, "button[type=submit]");
  }
  await form("register", { email: "person@example.org", password });
  await waitFor(
    async () =>
      (await pageHelpers.bodyText(page)).includes(
        "If this request is eligible",
      ),
    { describe: "generic enrollment response" },
  );
  await form("verify-email", {
    code: "fixture-email-code-with-32-or-more-characters",
  });
  await waitFor(
    async () => (await pageHelpers.bodyText(page)).includes("Done."),
    { describe: "email completion" },
  );
  await form("login", { email: "person@example.org", password });
  await waitFor(
    async () =>
      (await pageHelpers.bodyText(page)).includes(
        "Signed in: person@example.org",
      ),
    { describe: "consumer sign-in" },
  );
  if (await page.evaluate("localStorage.length!==0||sessionStorage.length!==0"))
    throw new Error("credentials persisted in browser storage");
  if (await page.evaluate("document.cookie.includes('fixture-refresh')"))
    throw new Error("refresh cookie readable by browser script");
  await page.goto(`${base}/en/login`);
  await pageHelpers.clickLast(page, "button:not([type=submit])");
  await waitFor(
    async () =>
      (await pageHelpers.bodyText(page)).includes(
        "Signed in: person@example.org",
      ),
    { describe: "restore memory-only access token" },
  );
  await form("recover", { email: "unknown@example.org" });
  await waitFor(
    async () =>
      (await pageHelpers.bodyText(page)).includes(
        "If this request is eligible",
      ),
    { describe: "non-enumerating recovery" },
  );
  await form("reset-password", {
    code: "fixture-reset-code-with-32-or-more-characters",
    password: "a replacement test password",
  });
  await waitFor(
    async () => (await pageHelpers.bodyText(page)).includes("Done."),
    { describe: "password reset" },
  );
  await form("login", {
    email: "person@example.org",
    password: "a replacement test password",
  });
  await waitFor(
    async () => (await pageHelpers.bodyText(page)).includes("Signed in:"),
    { describe: "new credential login" },
  );
  if (operations.some((operation) => operation.query))
    throw new Error("authentication input entered a URL");
  console.log(
    "public-auth e2e: five-language closed state, enrollment, verification, login, restoration, recovery, reset and storage privacy passed (fixtures)",
  );
} finally {
  if (page) await page.close();
  if (web) await web.close();
  await new Promise((resolve) => api.close(resolve));
}
