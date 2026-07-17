// Capture shim for Claude Code wire-profile calibration.
//
// Terminates the CLI's ANTHROPIC_BASE_URL locally (plain HTTP, no TLS needed),
// logs every request (method/url/headers/body) as one JSON line to $CAP_OUT,
// and returns a canned 200 so the CLI proceeds. A dummy token is enough: the
// CLI emits its full request headers+body before it would ever get a 401, so
// no real Anthropic account is spent.
const http = require('http');
const fs = require('fs');

const out = process.env.CAP_OUT || '/work/cap.jsonl';
const port = Number(process.env.SHIM_PORT || 8788);

http
  .createServer((req, res) => {
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8');
      const headers = {};
      for (let i = 0; i < req.rawHeaders.length; i += 2) {
        headers[req.rawHeaders[i]] = req.rawHeaders[i + 1];
      }
      let body = null;
      try {
        body = JSON.parse(raw);
      } catch (_) {
        body = raw;
      }
      fs.appendFileSync(
        out,
        JSON.stringify({ ts: Date.now(), method: req.method, url: req.url, headers, body }) + '\n'
      );
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          id: 'msg_cal',
          type: 'message',
          role: 'assistant',
          model: 'cal',
          content: [{ type: 'text', text: 'ok' }],
          stop_reason: 'end_turn',
          stop_sequence: null,
          usage: { input_tokens: 1, output_tokens: 1 },
        })
      );
    });
  })
  .listen(port, '127.0.0.1', () => console.log('SHIM_UP ' + port));
