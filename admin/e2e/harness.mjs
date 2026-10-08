// Zero-dependency browser automation for the Modura admin critical path.
// It drives the system Chromium over the Chrome DevTools Protocol using the
// WebSocket client built into Node, so no browser binary or npm package has
// to be downloaded.
import { spawn } from "node:child_process";
import http from "node:http";
import { rmSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as sleep } from "node:timers/promises";

export function fail(message) {
  console.error(`e2e: ${message}`);
  process.exitCode = 1;
  throw new Error(message);
}

export async function waitFor(
  predicate,
  { timeoutMs = 30000, intervalMs = 250, describe = "condition" } = {},
) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    let value;
    try {
      value = await predicate();
    } catch {
      value = undefined;
    }
    if (value) return value;
    if (Date.now() > deadline) fail(`timed out waiting for ${describe}`);
    await sleep(intervalMs);
  }
}

// --- CDP browser -----------------------------------------------------------

export async function startBrowser() {
  const executable = process.env.MODURA_E2E_CHROMIUM ?? "/usr/sbin/chromium";
  const profileDir = resolve(
    fileURLToPath(new URL("../../.cache/e2e-profile", import.meta.url)),
    randomBytes(8).toString("hex"),
  );
  const child = spawn(
    executable,
    [
      "--headless=new",
      "--no-sandbox",
      "--disable-gpu",
      "--disable-dev-shm-usage",
      "--window-size=1440,900",
      "--remote-debugging-port=0",
      `--user-data-dir=${profileDir}`,
      "about:blank",
    ],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  const wsEndpoint = await waitFor(
    async () => {
      return child.stderr
        .read()
        ?.toString()
        .match(/DevTools listening on (ws:\/\/\S+)/)?.[1];
    },
    { describe: "chromium DevTools endpoint", timeoutMs: 30000 },
  );
  const browser = new WebSocket(wsEndpoint);
  await new Promise((resolve, reject) => {
    browser.addEventListener("open", resolve);
    browser.addEventListener("error", () =>
      reject(new Error("browser websocket failed")),
    );
  });
  const pending = new Map();
  let sequence = 0;
  browser.addEventListener("message", (event) => {
    const message = JSON.parse(event.data);
    if (message.id && pending.has(message.id)) {
      const { resolve, reject } = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) reject(new Error(message.error.message));
      else resolve(message.result);
    }
  });
  const send = (method, params = {}, sessionId) =>
    new Promise((resolve, reject) => {
      const id = ++sequence;
      pending.set(id, { resolve, reject });
      const payload = { id, method, params };
      if (sessionId) payload.sessionId = sessionId;
      browser.send(JSON.stringify(payload));
    });
  const { targetId } = await send("Target.createTarget", {
    url: "about:blank",
  });
  const { sessionId } = await send("Target.attachToTarget", {
    targetId,
    flatten: true,
  });
  const page = {
    async goto(url) {
      await send("Page.enable", {}, sessionId);
      const loaded = new Promise((resolve) => {
        const listener = (event) => {
          if (JSON.parse(event.data).method === "Page.loadEventFired") {
            browser.removeEventListener("message", listener);
            resolve();
          }
        };
        browser.addEventListener("message", listener);
      });
      await send("Page.navigate", { url }, sessionId);
      await loaded;
      await sleep(400);
    },
    async evaluate(expression) {
      const result = await send(
        "Runtime.evaluate",
        { expression, returnByValue: true, awaitPromise: true },
        sessionId,
      );
      if (result.exceptionDetails) {
        throw new Error(
          result.exceptionDetails.exception?.description ??
            "page evaluation failed",
        );
      }
      return result.result.value;
    },
    async close() {
      await send("Target.closeTarget", { targetId }).catch(() => {});
      if (child.exitCode === null && child.signalCode === null) {
        await new Promise((resolve) => {
          child.once("exit", resolve);
          child.kill();
        });
      }
      rmSync(profileDir, {
        recursive: true,
        force: true,
        maxRetries: 3,
        retryDelay: 100,
      });
    },
  };
  return page;
}

// --- Static web server with API proxy --------------------------------------

export async function startWebServer({ distDir, apiPort, port = 0 }) {
  const server = http.createServer((request, response) => {
    const forward = () => {
      const proxied = http.request(
        {
          host: "127.0.0.1",
          port: apiPort,
          path: request.url,
          method: request.method,
          headers: request.headers,
        },
        (upstream) => {
          response.writeHead(upstream.statusCode ?? 502, upstream.headers);
          upstream.pipe(response);
        },
      );
      proxied.on("error", () => {
        response.writeHead(502);
        response.end();
      });
      request.pipe(proxied);
    };
    if (request.url.startsWith("/api")) {
      forward();
      return;
    }
    const path =
      request.url === "/" ? "/index.html" : request.url.split("?")[0];
    (async () => {
      const candidates = [distDir + path, `${distDir}/index.html`];
      for (const candidate of candidates) {
        try {
          const content = await readFile(candidate);
          const type = candidate.endsWith(".html")
            ? "text/html; charset=utf-8"
            : candidate.endsWith(".js")
              ? "text/javascript"
              : candidate.endsWith(".css")
                ? "text/css"
                : "application/octet-stream";
          response.writeHead(200, { "Content-Type": type });
          response.end(content);
          return;
        } catch {
          // try the next candidate (SPA fallback)
        }
      }
      response.writeHead(404);
      response.end();
    })().catch(() => {
      response.writeHead(500);
      response.end();
    });
  });
  await new Promise((resolve) => server.listen(port, "127.0.0.1", resolve));
  return {
    port: server.address().port,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

// --- Page helpers ----------------------------------------------------------

export const pageHelpers = {
  async setValue(page, selector, value) {
    return page.evaluate(`(() => {
      const element = document.querySelector(${JSON.stringify(selector)});
      if (!element) throw new Error("missing element " + ${JSON.stringify(selector)});
      const prototype = element.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set;
      setter?.call(element, ${JSON.stringify(value)});
      element.dispatchEvent(new Event("input", { bubbles: true }));
      element.dispatchEvent(new Event("change", { bubbles: true }));
    })()`);
  },
  async clickLast(page, selector) {
    return page.evaluate(`(() => {
      const element = [...document.querySelectorAll(${JSON.stringify(selector)})].at(-1);
      if (!element) throw new Error("missing element " + ${JSON.stringify(selector)});
      element.click();
    })()`);
  },
  async bodyText(page) {
    return page.evaluate("document.body.innerText");
  },
};
