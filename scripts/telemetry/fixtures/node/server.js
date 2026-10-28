'use strict';
const http = require('node:http');
const winston = require('winston');
const logger = winston.createLogger({
  level: 'info',
  format: winston.format.json(),
  transports: [new winston.transports.Console()],
});

http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://fixture');
    if (url.pathname === '/health') { res.end('ok'); return; }
    if (!url.pathname.startsWith('/work/')) { res.writeHead(404); res.end(); return; }
    const marker = decodeURIComponent(url.pathname.slice(6));
    await new Promise(resolve => setTimeout(resolve, Math.max(0, Math.min(1000, Number(process.env.RIO_FIXTURE_DELAY_MS || 0)))));
    const headers = {};
    for (let i = 0; i < req.rawHeaders.length; i += 2) {
      const key = req.rawHeaders[i].toLowerCase();
      (headers[key] ||= []).push(req.rawHeaders[i + 1]);
    }
    logger.info('rio-fixture ' + marker);
    const body = { marker, language: 'node', headers };
    if (url.searchParams.has('next')) {
      body.downstream = await new Promise((resolve, reject) => {
        http.get(url.searchParams.get('next'), response => {
          let data = '';
          response.on('data', chunk => { data += chunk; });
          response.on('end', () => {
            try {
              if (response.statusCode !== 200) throw new Error('downstream status ' + response.statusCode);
              resolve(JSON.parse(data));
            } catch (err) { reject(err); }
          });
        }).on('error', reject);
      });
    }
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify(body));
  } catch (error) {
    res.writeHead(500); res.end(String(error));
  }
}).listen(8080, '0.0.0.0');
