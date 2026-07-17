// Turn a captured cap.jsonl into a versioned calibration profile JSON, and run
// the fingerprint regression guard.
//
// Usage: node extract-profile.js <cap.jsonl> <cliVersion> [--salt=59cf53e54c78]
//
// The profile is the machine-readable "truth source" the gateway will load
// (constants.go stays as compiled-in fallback). Output goes to stdout.
//
// Guard: for every captured request that carries an `x-anthropic-billing-header`
// system block (cc_version=X.Y.Z.{fp}), recompute {fp} from that request's first
// user text using the repo's algorithm (sha256(salt + chars[4,7,20] + version)[:3])
// and compare. A mismatch means the salt/index algorithm drifted -> the guard
// fails loudly instead of shipping a wrong fingerprint that gets accounts flagged.
const fs = require('fs');
const crypto = require('crypto');

const [, , capPath, cliVersion, ...rest] = process.argv;
if (!capPath || !cliVersion) {
  console.error('usage: node extract-profile.js <cap.jsonl> <cliVersion> [--salt=...]');
  process.exit(2);
}
let salt = '59cf53e54c78';
for (const a of rest) if (a.startsWith('--salt=')) salt = a.slice('--salt='.length);

const lines = fs.readFileSync(capPath, 'utf8').trim().split('\n').filter(Boolean);
const reqs = [];
for (const l of lines) {
  try {
    const r = JSON.parse(l);
    if (/\/v1\/messages/.test(r.url)) reqs.push(r);
  } catch (_) {}
}
if (reqs.length === 0) {
  console.error('no /v1/messages requests captured');
  process.exit(1);
}

const H = (h, k) => {
  const kk = Object.keys(h || {}).find((x) => x.toLowerCase() === k.toLowerCase());
  return kk ? h[kk] : '';
};
const firstUserText = (b) => {
  const msgs = (b && b.messages) || [];
  for (const m of msgs) {
    if (m.role !== 'user') continue;
    const c = m.content;
    if (typeof c === 'string') return c;
    if (Array.isArray(c)) for (const blk of c) if (blk.type === 'text') return blk.text || '';
  }
  return '';
};
const computeFp = (text, version) => {
  const buf = Buffer.from(text, 'utf8');
  const idx = [4, 7, 20];
  const chars = Buffer.from(idx.map((i) => (i < buf.length ? buf[i] : 0x30)));
  return crypto.createHash('sha256').update(salt + chars.toString('latin1') + version).digest('hex').slice(0, 3);
};

// --- guard ---
const guard = { checked: 0, ok: 0, mismatches: [] };
for (const r of reqs) {
  const sys = (r.body && r.body.system) || [];
  const billing = Array.isArray(sys) ? sys.find((s) => String(s.text || '').includes('x-anthropic-billing-header')) : null;
  if (!billing) continue;
  const m = String(billing.text).match(/cc_version=([\d.]+)\.([0-9a-f]{3});/);
  if (!m) continue;
  guard.checked++;
  const realFp = m[2];
  const got = computeFp(firstUserText(r.body), m[1]);
  if (got === realFp) guard.ok++;
  else guard.mismatches.push({ url: r.url, realFp, computed: got, version: m[1] });
}

// --- beta rules matrix (endpoint x model family x request features -> beta set) ---
// Key format: "<endpoint>|<family>|<features>" where endpoint is messages or
// count_tokens (the two carry different beta sets), family is haiku/fable/sonnet/opus,
// and features is the sorted "+"-joined subset of {json_schema, tools}. This matches
// backend/internal/pkg/claude/calibrated_profile.go (CalibratedProfile.lookupBetas).
const endpointOf = (url) => (/count_tokens/.test(String(url || '')) ? 'count_tokens' : 'messages');
const familyOf = (model) => {
  const s = String(model || '').toLowerCase();
  if (s.includes('haiku')) return 'haiku';
  if (s.includes('fable')) return 'fable';
  if (s.includes('sonnet')) return 'sonnet';
  return 'opus';
};
const featuresOf = (b) => {
  const f = [];
  if (b && b.output_config && b.output_config.format && b.output_config.format.type === 'json_schema') f.push('json_schema');
  const tools = (b && b.tools) || [];
  if (Array.isArray(tools) && tools.length > 0) f.push('tools');
  return f;
};
const betaRules = {};
let headerTemplate = null;
const absentHeaders = new Set(['x-client-request-id']);
for (const r of reqs) {
  const b = r.body || {};
  const key = endpointOf(r.url) + '|' + familyOf(b.model) + '|' + featuresOf(b).sort().join('+');
  const beta = H(r.headers, 'anthropic-beta');
  if (beta && !betaRules[key]) betaRules[key] = beta.split(',').map((x) => x.trim());
  if (H(r.headers, 'x-client-request-id')) absentHeaders.delete('x-client-request-id');
  if (!headerTemplate) {
    headerTemplate = {
      'User-Agent': H(r.headers, 'user-agent'),
      'X-Stainless-Lang': H(r.headers, 'x-stainless-lang'),
      'X-Stainless-Package-Version': H(r.headers, 'x-stainless-package-version'),
      'X-Stainless-OS': H(r.headers, 'x-stainless-os'),
      'X-Stainless-Arch': H(r.headers, 'x-stainless-arch'),
      'X-Stainless-Runtime': H(r.headers, 'x-stainless-runtime'),
      'X-Stainless-Runtime-Version': H(r.headers, 'x-stainless-runtime-version'),
      'X-Stainless-Retry-Count': H(r.headers, 'x-stainless-retry-count'),
      'X-Stainless-Timeout': H(r.headers, 'x-stainless-timeout'),
      'X-App': H(r.headers, 'x-app'),
      'anthropic-version': H(r.headers, 'anthropic-version'),
      'Anthropic-Dangerous-Direct-Browser-Access': H(r.headers, 'anthropic-dangerous-direct-browser-access'),
      Accept: H(r.headers, 'accept'),
      'Accept-Encoding': H(r.headers, 'accept-encoding'),
    };
  }
}

const profile = {
  // schema_version must match claude.CalibratedProfileSchemaVersion in the Go loader;
  // a mismatch makes the gateway refuse the profile and fall back to compiled-in constants.
  schema_version: 1,
  cli_version: cliVersion,
  captured_at: new Date().toISOString(),
  source: 'cc-calibrate',
  headers: { template: headerTemplate, absent: [...absentHeaders] },
  beta_rules: betaRules,
  guard: { salt_verified: guard.checked > 0 && guard.mismatches.length === 0, ...guard },
};

if (guard.mismatches.length > 0) {
  console.error('FINGERPRINT GUARD FAILED (salt/index drift?):', JSON.stringify(guard.mismatches, null, 2));
}
console.log(JSON.stringify(profile, null, 2));
process.exit(guard.mismatches.length > 0 ? 3 : 0);
