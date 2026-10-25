#!/usr/bin/env bun
// sub2api Bun Forwarder — per-account HTTPS exit point for Claude Code.
//
// Claude Code connects here via ANTHROPIC_BASE_URL=https://127.0.0.1:<port>.
// The forwarder keeps the client's own OAuth credentials (passthrough) and
// forwards to Anthropic with Bun's native TLS (BoringSSL).
//
// Usage:
//   bun run forwarder.js --port 19842 --upstream https://api.anthropic.com
//
// The old Unix-socket / token-injection mode used by the Go gateway has been
// removed together with the OAuth HTTP mimicry path.

// ── CLI args ──────────────────────────────────────────────────────────

function parseArgs() {
  const args = process.argv.slice(2);
  const opts = {
    port: 0,
    upstream: "https://api.anthropic.com",
    certFile: "",
    keyFile: "",
    verbose: false,
  };

  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case "--port":     opts.port = parseInt(args[++i], 10); break;
      case "--upstream": opts.upstream = args[++i]; break;
      case "--cert":     opts.certFile = args[++i]; break;
      case "--key":      opts.keyFile = args[++i]; break;
      case "-v":
      case "--verbose":  opts.verbose = true; break;
    }
  }

  if (!Number.isInteger(opts.port) || opts.port <= 0) {
    console.error("Error: --port is required");
    process.exit(1);
  }
  return opts;
}

const opts = parseArgs();

// ── Self-signed cert generation ──────────────────────────────────────

async function ensureCerts() {
  if (opts.certFile && opts.keyFile) return { cert: opts.certFile, key: opts.keyFile };

  const tmpDir = `/tmp/sub2api-certs-${process.pid}`;
  const certPath = `${tmpDir}/cert.pem`;
  const keyPath = `${tmpDir}/key.pem`;

  await Bun.$`mkdir -p ${tmpDir}`;
  await Bun.$`openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
    -keyout ${keyPath} -out ${certPath} \
    -days 365 -nodes -subj "/CN=localhost" 2>/dev/null`;

  console.log(`[tls] Generated self-signed cert at ${certPath}`);
  return { cert: certPath, key: keyPath };
}

// ── Stats ─────────────────────────────────────────────────────────────

const stats = {
  totalRequests: 0,
  apiMessages: 0,
  telemetryBatches: 0,
  errors: 0,
  startTime: Date.now(),
};

// ── Core forwarding logic ─────────────────────────────────────────────

async function handleRequest(req) {
  stats.totalRequests++;

  const url = new URL(req.url);
  const path = url.pathname + url.search;
  const method = req.method;

  if (path.startsWith("/v1/messages")) stats.apiMessages++;
  else if (path.includes("event_logging")) stats.telemetryBatches++;

  if (opts.verbose) {
    console.log(`[https] ${method} ${path}`);
  }

  const headers = new Headers(req.headers);
  headers.delete("host");
  headers.delete("x-sub2api-account-id");
  headers.delete("x-sub2api-source");

  const upstreamUrl = `${opts.upstream}${path}`;

  try {
    const upstreamResp = await fetch(upstreamUrl, {
      method,
      headers,
      body: req.body,
      duplex: "half",
    });

    const respHeaders = new Headers(upstreamResp.headers);
    respHeaders.delete("content-encoding");
    respHeaders.set("x-accel-buffering", "no");

    if (opts.verbose) {
      const ct = upstreamResp.headers.get("content-type") || "";
      const streaming = ct.includes("text/event-stream") ? " [SSE]" : "";
      console.log(`  → ${upstreamResp.status}${streaming}`);
    }

    return new Response(upstreamResp.body, {
      status: upstreamResp.status,
      statusText: upstreamResp.statusText,
      headers: respHeaders,
    });
  } catch (e) {
    stats.errors++;
    console.error(`[https] Upstream error: ${method} ${path}: ${e.message}`);
    return new Response(JSON.stringify({ error: `upstream error: ${e.message}` }), {
      status: 502,
      headers: { "Content-Type": "application/json" },
    });
  }
}

// ── HTTPS listener ────────────────────────────────────────────────────

const certs = await ensureCerts();
const server = Bun.serve({
  port: opts.port,
  hostname: "127.0.0.1",
  tls: {
    cert: Bun.file(certs.cert),
    key: Bun.file(certs.key),
  },
  fetch(req, srv) {
    srv.timeout(req, 0);
    return handleRequest(req);
  },
  error(err) {
    console.error(`[https] Server error: ${err.message}`);
    return new Response("Internal error", { status: 500 });
  },
});

console.log(`[https] Listening on https://127.0.0.1:${opts.port}`);

// ── Stats printer ─────────────────────────────────────────────────────

setInterval(() => {
  const elapsed = ((Date.now() - stats.startTime) / 1000).toFixed(0);
  console.log(
    `[stats] ${elapsed}s | total=${stats.totalRequests} api=${stats.apiMessages} ` +
    `telemetry=${stats.telemetryBatches} errors=${stats.errors}`
  );
}, 60_000);

// ── Graceful shutdown ─────────────────────────────────────────────────

function shutdown() {
  console.log("\n[shutdown] Stopping...");
  server.stop();
  console.log("[shutdown] Done.");
  process.exit(0);
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);

console.log(`[ready] Forwarding to ${opts.upstream} | auth=passthrough`);
