// Publish a calibrated profile JSON to the gateway admin API using the admin API key.
// Uses Node's global fetch (Node 18+), so no curl is required — handy in the slim
// node container used by the docker-compose cc-calibrate sidecar.
//
// Usage: node publish.js <profile.json> <gatewayUrl> <adminApiKey>
// Exit: 0 on HTTP 200; non-zero otherwise (message on stderr).
const fs = require('fs');

const [, , file, gatewayUrl, adminApiKey] = process.argv;
if (!file || !gatewayUrl || !adminApiKey) {
  console.error('usage: node publish.js <profile.json> <gatewayUrl> <adminApiKey>');
  process.exit(2);
}

const url = gatewayUrl.replace(/\/+$/, '') + '/api/v1/admin/settings/claude-calibrated-profile';
const body = fs.readFileSync(file, 'utf8');

(async () => {
  try {
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'x-api-key': adminApiKey, 'Content-Type': 'application/json' },
      body,
    });
    const text = await res.text();
    if (res.status === 200) {
      console.log(`published ${file} -> ${url} (gateway hot-loads within ~60s)`);
      process.exit(0);
    }
    console.error(`publish failed (HTTP ${res.status}): ${text}`);
    process.exit(1);
  } catch (err) {
    console.error(`publish request error: ${err && err.message ? err.message : err}`);
    process.exit(1);
  }
})();
