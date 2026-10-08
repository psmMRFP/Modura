// Critical-path browser E2E for the Modura admin: tenant login, organization,
// user catalogue, authorization, settings, logout, and platform tenant
// administration — driven through a real browser against the real backend.
//
// Usage (Fish):
//   set -gx MODURA_TEST_DATABASE_URL 'postgres://.../modura_test?sslmode=disable'
//   make admin-e2e
import { execFileSync, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as sleep } from "node:timers/promises";

import {
  fail,
  pageHelpers,
  startBrowser,
  startWebServer,
  waitFor,
} from "./harness.mjs";

const repoRoot = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const backendDir = join(repoRoot, "backend");
const adminDir = join(repoRoot, "admin");
const databaseUrl = process.env.MODURA_TEST_DATABASE_URL;
if (!databaseUrl) {
  fail(
    "MODURA_TEST_DATABASE_URL is required (must point at a modura_test database)",
  );
}
// Passwords are generated per run and passed only through the environment,
// so no credential material is stored in source or on disk.
const platformPassword = randomBytes(20).toString("hex");
const tenantPassword = randomBytes(20).toString("hex");

async function main() {
  console.log("e2e: seeding database through the real runtime workflow");
  let seedOutput;
  try {
    seedOutput = execFileSync("go", ["run", "./cmd/modura-e2e-seed"], {
      cwd: backendDir,
      env: {
        ...process.env,
        MODURA_E2E_PLATFORM_PASSWORD: platformPassword,
        MODURA_E2E_TENANT_PASSWORD: tenantPassword,
      },
      encoding: "utf-8",
    });
  } catch (error) {
    if (error.code === "ENOENT") {
      fail(
        `go is not on the PATH seen by node: ${JSON.stringify(process.env.PATH)}`,
      );
    }
    throw error;
  }
  const credentials = JSON.parse(seedOutput.trim().split("\n").at(-1));

  console.log("e2e: starting backend");
  const backendPort = 18080 + Math.floor(Math.random() * 1000);
  const backendProcess = spawn("go", ["run", "./cmd/modura"], {
    cwd: backendDir,
    env: {
      ...process.env,
      MODURA_DATABASE_URL: databaseUrl,
      MODURA_HTTP_ADDRESS: `127.0.0.1:${backendPort}`,
      MODURA_AUTH_SIGNING_KEY: "e2e-signing-key-with-at-least-32-bytes!",
      MODURA_AUTH_COOKIE_SECURE: "false",
    },
    stdio: ["ignore", "ignore", "pipe"],
  });
  backendProcess.stderr.on("data", (chunk) =>
    process.stderr.write(`[backend] ${chunk}`),
  );
  await waitFor(
    async () => {
      const response = await fetch(`http://127.0.0.1:${backendPort}/api/livez`);
      return response.ok;
    },
    { describe: "backend liveness" },
  );

  console.log("e2e: starting web server");
  const web = await startWebServer({
    distDir: join(adminDir, "dist"),
    apiPort: backendPort,
  });

  console.log("e2e: starting browser");
  const page = await startBrowser();
  const { setValue, clickLast, bodyText } = pageHelpers;
  const base = `http://127.0.0.1:${web.port}`;
  // Ant Design inserts a space between the two characters of CJK button
  // labels, so matching compares with all whitespace removed.
  const clickByText = (text) =>
    page.evaluate(
      `(() => {
      const wanted = ${JSON.stringify(text)}.replace(/\\s+/g, "");
      const element = [...document.querySelectorAll("button")].find((candidate) => candidate.textContent.replace(/\\s+/g, "").includes(wanted));
      if (!element) throw new Error("missing button " + wanted);
      element.click();
    })()`,
    );

  try {
    // --- tenant login ------------------------------------------------------
    await page.goto(`${base}/login`);
    await waitFor(async () => (await bodyText(page)).includes("登录 Modura"), {
      describe: "tenant login page",
    });
    await setValue(
      page,
      "input[autocomplete='organization']",
      credentials.tenantSlug,
    );
    await setValue(
      page,
      "input[autocomplete='username']",
      credentials.tenantUsername,
    );
    await setValue(
      page,
      "input[autocomplete='current-password']",
      tenantPassword,
    );
    await clickByText("登录");
    await waitFor(async () => (await bodyText(page)).includes("退出登录"), {
      describe: "workspace after tenant login",
    });

    // --- organization: department list shows the provisioned root ----------
    await page.goto(`${base}/organization/departments`);
    await waitFor(async () => (await bodyText(page)).includes("E2E Root"), {
      describe: "department list",
    });

    // --- user catalogue: open the selector to reveal account names -----------
    await page.goto(`${base}/organization/users`);
    await waitFor(async () => (await bodyText(page)).includes("用户目录"), {
      describe: "user catalogue page",
    });
    await waitFor(
      async () =>
        page.evaluate("document.querySelector('.ant-select') !== null"),
      { describe: "user selector rendered" },
    );
    // Ant Design opens the dropdown on mousedown, not on click.
    await page.evaluate(`(() => {
      const element = document.querySelector(".ant-select");
      element.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
      element.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
      element.click();
    })()`);
    await waitFor(
      async () =>
        page.evaluate(
          "document.querySelector('.ant-select-dropdown') !== null",
        ),
      { describe: "user dropdown open" },
    );
    await waitFor(
      async () => (await bodyText(page)).includes(credentials.tenantUsername),
      { describe: "tenant user catalogue" },
    );

    // --- authorization: reserved role is visible ------------------------------
    await page.goto(`${base}/authorization/roles`);
    await waitFor(
      async () => (await bodyText(page)).includes("Tenant Administrator"),
      { describe: "roles list" },
    );

    // --- settings: dictionaries page renders -----------------------------------
    await page.goto(`${base}/settings/dictionaries`);
    await waitFor(async () => (await bodyText(page)).includes("字典"), {
      describe: "dictionaries page",
    });

    // --- tenant logout ----------------------------------------------------------
    await clickByText("退出登录");
    await waitFor(async () => (await bodyText(page)).includes("登录 Modura"), {
      describe: "back at tenant login",
    });

    // --- platform administration -------------------------------------------------
    await page.goto(`${base}/platform/login`);
    await waitFor(async () => (await bodyText(page)).includes("平台管理登录"), {
      describe: "platform login page",
    });
    await setValue(
      page,
      "input[autocomplete='username']",
      credentials.platformUsername,
    );
    await setValue(
      page,
      "input[autocomplete='current-password']",
      platformPassword,
    );
    await clickByText("登录");
    await waitFor(async () => (await bodyText(page)).includes("E2E Tenant"), {
      describe: "platform tenant list",
    });
    await page.goto(`${base}/platform/audit`);
    await waitFor(
      async () =>
        (await bodyText(page)).includes("tenant.provisioned") ||
        (await bodyText(page)).includes("平台审计"),
      { describe: "platform audit surface" },
    );
    await clickByText("退出登录");
    await waitFor(async () => (await bodyText(page)).includes("平台管理登录"), {
      describe: "platform logged out",
    });

    console.log("e2e: critical path PASSED");
  } finally {
    await page.close();
    await web.close();
    backendProcess.kill("SIGTERM");
    await sleep(200);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
