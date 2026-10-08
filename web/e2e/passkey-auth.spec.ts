import { test, expect, type Browser, type CDPSession } from "@playwright/test";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { createServer, type Server } from "node:https";
import { request as httpRequest } from "node:http";
import type { Socket } from "node:net";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(webRoot, "..");
const relayPort = 4180;
const httpsPort = 4443;
const publicOrigin = `https://localhost:${httpsPort}`;
const setupToken = "ariel-first-passkey-setup-token-2026";
let scratch = "";
let relay: ChildProcess | null = null;
let proxy: Server | null = null;
const proxySockets = new Set<Socket>();

async function waitForRelay() {
  for (let attempt = 0; attempt < 80; attempt++) {
    const ready = await new Promise<boolean>(resolveReady => {
      const request = httpRequest(`http://127.0.0.1:${relayPort}/healthz`, response => {
        response.resume();
        resolveReady(response.statusCode === 200);
      });
      request.on("error", () => resolveReady(false));
      request.end();
    });
    if (ready) return;
    await new Promise(resolveWait => setTimeout(resolveWait, 100));
  }
  throw new Error("Passkey test Relay did not become healthy");
}

function startTLSProxy(keyPath: string, certPath: string): Server {
  const server = createServer({ key: readFileSync(keyPath), cert: readFileSync(certPath) }, (request, response) => {
    const upstream = httpRequest({ hostname: "127.0.0.1", port: relayPort, path: request.url, method: request.method, headers: { ...request.headers, host: `127.0.0.1:${relayPort}` } }, upstreamResponse => {
      response.writeHead(upstreamResponse.statusCode || 502, upstreamResponse.headers);
      upstreamResponse.pipe(response);
    });
    upstream.on("error", () => { response.writeHead(502); response.end(); });
    request.pipe(upstream);
  });
  server.on("upgrade", (request, socket, head) => {
    const upstream = httpRequest({ hostname: "127.0.0.1", port: relayPort, path: request.url, method: request.method, headers: { ...request.headers, host: `127.0.0.1:${relayPort}` } });
    upstream.on("upgrade", (response, upstreamSocket, upstreamHead) => {
      socket.write(`HTTP/1.1 ${response.statusCode} ${response.statusMessage}\r\n`);
      for (const [name, value] of Object.entries(response.headers)) {
        if (value !== undefined) socket.write(`${name}: ${Array.isArray(value) ? value.join(", ") : value}\r\n`);
      }
      socket.write("\r\n");
      if (head.length) upstreamSocket.write(head);
      if (upstreamHead.length) socket.write(upstreamHead);
      socket.pipe(upstreamSocket).pipe(socket);
    });
    upstream.on("error", () => socket.destroy());
    upstream.end();
  });
  server.on("connection", socket => {
    proxySockets.add(socket);
    socket.once("close", () => proxySockets.delete(socket));
    socket.setNoDelay(true);
  });
  server.listen(httpsPort, "127.0.0.1");
  return server;
}

test.beforeAll(async () => {
  scratch = mkdtempSync(join(tmpdir(), "ariel-passkey-e2e-"));
  const relayBinary = join(scratch, "ariel-relay");
  const keyPath = join(scratch, "localhost.key");
  const certPath = join(scratch, "localhost.crt");
  for (const result of [
    spawnSync("npm", ["run", "build"], { cwd: webRoot, stdio: "pipe" }),
    spawnSync("go", ["build", "-o", relayBinary, "./cmd/relay"], { cwd: repoRoot, stdio: "pipe", env: { ...process.env, GO111MODULE: "on", GOTOOLCHAIN: "auto" } }),
    spawnSync("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", keyPath, "-out", certPath, "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost"], { stdio: "pipe" }),
  ]) {
    if (result.status !== 0) throw new Error(result.stderr.toString() || result.stdout.toString());
  }
  relay = spawn(relayBinary, [], {
    cwd: repoRoot,
    stdio: "pipe",
    env: {
      ...process.env,
      ARIEL_TOKEN: "agent-token-for-passkey-e2e-only",
      ARIEL_WEB_AUTH: "passkey",
      ARIEL_ORIGINS: publicOrigin,
      ARIEL_PUBLIC_ORIGIN: publicOrigin,
      ARIEL_WEBAUTHN_RP_ID: "localhost",
      ARIEL_WEBAUTHN_CREDENTIALS_FILE: join(scratch, "auth.json"),
      ARIEL_SESSION_KEY: "00".repeat(32),
      ARIEL_PASSKEY_SETUP_TOKEN: setupToken,
      ARIEL_WEB_DIST: join(webRoot, "dist"),
      ARIEL_LISTEN: `127.0.0.1:${relayPort}`,
    },
  });
  await waitForRelay();
  proxy = startTLSProxy(keyPath, certPath);
});

test.afterAll(async () => {
  for (const socket of proxySockets) socket.destroy();
  await new Promise<void>(resolveClose => proxy?.close(() => resolveClose()) ?? resolveClose());
  relay?.kill("SIGTERM");
  if (relay && relay.exitCode === null) await new Promise(resolveExit => relay?.once("exit", resolveExit));
  if (scratch) rmSync(scratch, { recursive: true, force: true });
});

async function addAuthenticator(cdp: CDPSession): Promise<string> {
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", { options: {
    protocol: "ctap2", transport: "internal", hasResidentKey: true, hasUserVerification: true,
    isUserVerified: true, automaticPresenceSimulation: true,
  } });
  return authenticatorId;
}

async function addVirtualPasskey(browser: Browser) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, locale: "zh-CN", viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true, colorScheme: "dark", reducedMotion: "reduce" });
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  const authenticatorId = await addAuthenticator(cdp);
  return { context, page, cdp, authenticatorId };
}

test("enrolls a primary and backup Passkey, then signs in again", async ({ browser }) => {
  const { context, page, cdp, authenticatorId } = await addVirtualPasskey(browser);
  await page.goto(publicOrigin);
  await expect(page.getByRole("heading", { name: "Agent 联络中继器" })).toBeVisible();
  await expect(page.getByLabel("6 位连接码")).toHaveCount(0);
  await expect(page).toHaveScreenshot("passkey-first-enrollment.png", { animations: "disabled" });
  await page.getByLabel("首次设置密钥").fill(setupToken);
  await page.getByRole("button", { name: "创建 Passkey" }).click();
  await expect(page.locator(".masthead .connection.ready")).toContainText("Relay 已连接");

  await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
  await addAuthenticator(cdp);
  await page.getByRole("button", { name: "添加 Passkey" }).click();
  await expect(page.getByRole("alert")).toContainText("新的 Passkey 已添加");

  await page.getByRole("button", { name: "退出" }).click();
  await expect(page.getByRole("button", { name: "使用 Passkey 登录" })).toBeVisible();
  await expect(page).toHaveScreenshot("passkey-login.png", { animations: "disabled" });
  await page.getByRole("button", { name: "使用 Passkey 登录" }).click();
  await expect(page.locator(".masthead .connection.ready")).toContainText("Relay 已连接");
  await context.close();
});
