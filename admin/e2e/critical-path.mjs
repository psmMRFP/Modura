// Critical-path browser E2E for the WhereToLive admin: tenant login, organization,
// user catalogue, authorization, settings, logout, and platform tenant
// administration — driven through a real browser against the real backend.
//
// Usage (Fish):
//   set -gx WHERETOLIVE_TEST_DATABASE_URL 'postgres://.../wheretolive_test?sslmode=disable'
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
const databaseUrl = process.env.WHERETOLIVE_TEST_DATABASE_URL;
if (!databaseUrl) {
  fail(
    "WHERETOLIVE_TEST_DATABASE_URL is required (must point at a wheretolive_test database)",
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
    seedOutput = execFileSync("go", ["run", "./cmd/wheretolive-e2e-seed"], {
      cwd: backendDir,
      env: {
        ...process.env,
        WHERETOLIVE_E2E_PLATFORM_PASSWORD: platformPassword,
        WHERETOLIVE_E2E_TENANT_PASSWORD: tenantPassword,
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
  const backendProcess = spawn("go", ["run", "./cmd/wheretolive"], {
    cwd: backendDir,
    env: {
      ...process.env,
      WHERETOLIVE_DATABASE_URL: databaseUrl,
      WHERETOLIVE_HTTP_ADDRESS: `127.0.0.1:${backendPort}`,
      WHERETOLIVE_AUTH_SIGNING_KEY: "e2e-signing-key-with-at-least-32-bytes!",
      WHERETOLIVE_AUTH_COOKIE_SECURE: "false",
    },
    // go run starts a child executable; terminate the whole test process group.
    detached: process.platform !== "win32",
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
    await waitFor(
      async () => (await bodyText(page)).includes("登录 WhereToLive"),
      {
        describe: "tenant login page",
      },
    );
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
    await waitFor(
      async () => (await bodyText(page)).includes("登录 WhereToLive"),
      {
        describe: "back at tenant login",
      },
    );

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
    // Candidate pool: real writes, composable filters and publication transition.
    await page.goto(`${base}/platform/places`);
    await waitFor(async () => (await bodyText(page)).includes("新建地点"), {
      describe: "candidate directory",
    });
    await clickByText("新建地点");
    await waitFor(
      async () =>
        await page.evaluate(
          'Boolean(document.querySelector(".ant-modal #slug"))',
        ),
      { describe: "place form" },
    );
    await setValue(page, ".ant-modal #slug", "e2e-germany");
    await setValue(page, ".ant-modal #countryCode", "DE");
    await setValue(page, ".ant-modal #details_name", "E2E Germany");
    await setValue(page, ".ant-modal #reason", "candidate pool browser test");
    await page.evaluate(
      'document.querySelector(".ant-modal .ant-modal-footer button.ant-btn-primary").click()',
    );
    await waitFor(
      async () =>
        await page.evaluate(
          '[...document.querySelectorAll(".ant-list-item")].some(row=>row.textContent.includes("E2E Germany"))',
        ),
      { describe: "draft saved" },
    );
    await page.goto(
      `${base}/platform/places?countryCode=DE&coverageLevel=0&publication=draft`,
    );
    await waitFor(async () => (await bodyText(page)).includes("E2E Germany"), {
      describe: "zero coverage candidate filter",
    });
    await page.goto(
      `${base}/platform/places?q=E2E&countryCode=FR&coverageLevel=0`,
    );
    await waitFor(async () => (await bodyText(page)).includes("暂无地点"), {
      describe: "combined filters exclude another country",
    });
    await clickByText("清除筛选");
    await waitFor(async () => (await bodyText(page)).includes("E2E Germany"), {
      describe: "clear filters",
    });
    await page.evaluate(
      '[...document.querySelectorAll(".ant-list-item button")].find(b=>b.textContent.replace(/\\s+/g, "").includes("发布")).click()',
    );
    await waitFor(
      async () =>
        await page.evaluate(
          'Boolean(document.querySelector(".ant-modal textarea"))',
        ),
      { describe: "publication reason" },
    );
    await setValue(page, ".ant-modal textarea", "basic metadata reviewed");
    await page.evaluate(
      'document.querySelector(".ant-modal .ant-modal-footer button.ant-btn-primary").click()',
    );
    await waitFor(
      async () =>
        await page.evaluate(
          '[...document.querySelectorAll(".ant-list-item")].some(row=>row.textContent.includes("E2E Germany") && row.textContent.includes("Basic"))',
        ),
      { describe: "published basic place" },
    );
    await page.goto(
      `${base}/platform/places?countryCode=DE&coverageLevel=0&publication=draft`,
    );
    await waitFor(async () => (await bodyText(page)).includes("暂无地点"), {
      describe: "published place leaves candidate filter",
    });
    await page.goto(
      `${base}/platform/places?countryCode=DE&coverageLevel=1&publication=published`,
    );
    await waitFor(async () => (await bodyText(page)).includes("E2E Germany"), {
      describe: "basic publication filter",
    });
    console.log("e2e: loading test place fixtures through the platform API");
    const importCandidates = (apply) =>
      execFileSync(
        "go",
        [
          "run",
          "./cmd/wheretolive-place-seed",
          "--base-url",
          `http://127.0.0.1:${backendPort}`,
          ...(apply ? ["--apply", "--reason", "E2E place fixtures"] : []),
        ],
        {
          cwd: backendDir,
          env: {
            ...process.env,
            WHERETOLIVE_SEED_USERNAME: credentials.platformUsername,
            WHERETOLIVE_SEED_PASSWORD: platformPassword,
          },
          encoding: "utf-8",
          timeout: 120_000,
        },
      );
    if (!importCandidates(false).includes("existing: 1; missing: 39")) {
      fail(
        "candidate preview did not report 39 missing places and the existing DE country",
      );
    }
    if (!importCandidates(true).includes("Created drafts: 39")) {
      fail(
        "candidate import did not create 39 drafts alongside the existing country",
      );
    }
    if (
      !importCandidates(true).includes(
        "Created drafts: 0; preserved existing: 40",
      )
    ) {
      fail("candidate rerun was not idempotent");
    }
    await page.goto(
      `${base}/platform/places?countryCode=DE&coverageLevel=0&publication=draft`,
    );
    await waitFor(
      async () => {
        const text = await bodyText(page);
        return (
          text.includes("Berlin") &&
          text.includes("Munich") &&
          text.includes("Hamburg")
        );
      },
      { describe: "imported candidates in the real admin catalogue" },
    );
    const publicDraft = await fetch(
      `http://127.0.0.1:${backendPort}/api/public/places/france`,
    );
    if (publicDraft.status !== 404) fail("import exposed a draft publicly");
    // Manual feedback stays restricted and has a human processing outcome.
    await page.goto(`${base}/platform/feedback`);
    await waitFor(async () => (await bodyText(page)).includes("人工录入"), {
      describe: "feedback intake surface",
    });
    const choose = async (field, label) => {
      await page.evaluate(`(() => {
        const element = document.querySelector(${JSON.stringify(".ant-modal #")} + ${JSON.stringify(field)}).closest(".ant-select");
        element.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
        element.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
        element.click();
      })()`);
      await waitFor(
        async () =>
          await page.evaluate(
            `[...document.querySelectorAll(".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option")].some(e => e.textContent === ${JSON.stringify(label)})`,
          ),
        { describe: `feedback option ${label}` },
      );
      await page.evaluate(
        `[...document.querySelectorAll(".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option")].find(e => e.textContent === ${JSON.stringify(label)}).click()`,
      );
    };
    await clickByText("人工录入");
    await waitFor(
      async () =>
        await page.evaluate(
          'Boolean(document.querySelector(".ant-modal #title"))',
        ),
      { describe: "feedback form" },
    );
    await choose("category", "信息纠错");
    await setValue(page, ".ant-modal #title", "E2E feedback correction");
    await setValue(
      page,
      ".ant-modal #message",
      "Fixture description needs editorial review",
    );
    await setValue(page, ".ant-modal #reason", "E2E manual feedback intake");
    await page.evaluate(
      'document.querySelector(".ant-modal .ant-modal-footer button.ant-btn-primary").click()',
    );
    await waitFor(
      async () =>
        await page.evaluate(
          '[...document.querySelectorAll(".ant-list-item")].some(e => e.textContent.includes("E2E feedback correction") && e.textContent.includes("待处理"))',
        ),
      { describe: "recorded feedback" },
    );
    await waitFor(
      async () =>
        await page.evaluate('!document.querySelector(".ant-modal #title")'),
      { describe: "intake modal closed" },
    );
    const processFeedback = async (state, outcome) => {
      await page.evaluate(
        '[...document.querySelectorAll(".ant-list-item button")].find(b => b.textContent.replace(/\\s+/g, "") === "处理").click()',
      );
      await waitFor(
        async () =>
          await page.evaluate(
            'Boolean(document.querySelector(".ant-modal #status"))',
          ),
        { describe: "review form" },
      );
      await choose("status", state);
      if (outcome) await setValue(page, ".ant-modal #outcome", outcome);
      await setValue(page, ".ant-modal #reason", "E2E human review");
      await page.evaluate(
        'document.querySelector(".ant-modal .ant-modal-footer button.ant-btn-primary").click()',
      );
      await waitFor(
        async () =>
          await page.evaluate(
            `[...document.querySelectorAll(".ant-list-item")].some(e => e.textContent.includes("E2E feedback correction") && e.textContent.includes(${JSON.stringify(state)}))`,
          ),
        { describe: `feedback ${state}` },
      );
      await waitFor(
        async () =>
          await page.evaluate('!document.querySelector(".ant-modal #status")'),
        { describe: "review modal closed" },
      );
    };
    await processFeedback("审核中");
    await processFeedback("已解决", "Test description corrected and checked");
    await page.goto(
      `${base}/platform/feedback?status=resolved&category=correction`,
    );
    await waitFor(
      async () =>
        (await bodyText(page)).includes(
          "Test description corrected and checked",
        ),
      { describe: "filtered resolved outcome" },
    );
    await page.goto(`${base}/platform/feedback`);
    await waitFor(
      async () => (await bodyText(page)).includes("E2E feedback correction"),
      { describe: "feedback before reopen" },
    );
    await processFeedback("审核中");
    await page.goto(`${base}/platform/feedback?status=resolved`);
    await waitFor(async () => (await bodyText(page)).includes("暂无反馈"), {
      describe: "reopened item leaves resolved filter",
    });
    await page.goto(`${base}/platform/audit`);
    await waitFor(
      async () =>
        (await bodyText(page)).includes("tenant.provisioned") ||
        (await bodyText(page)).includes("平台审计"),
      { describe: "platform audit surface" },
    );
    await waitFor(
      async () => (await bodyText(page)).includes("E2E place fixtures"),
      { describe: "candidate import transactional audit" },
    );
    await clickByText("退出登录");
    await waitFor(async () => (await bodyText(page)).includes("平台管理登录"), {
      describe: "platform logged out",
    });

    console.log("e2e: critical path PASSED");
  } finally {
    await page.close();
    await web.close();
    if (process.platform === "win32") backendProcess.kill("SIGTERM");
    else {
      try {
        process.kill(-backendProcess.pid, "SIGTERM");
      } catch (error) {
        if (error.code !== "ESRCH") throw error;
      }
    }
    await sleep(200);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
