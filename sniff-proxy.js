// Simple HTTP sniffing proxy to capture freebuff CLI requests
const http = require('http');
const https = require('https');
const { URL } = require('url');

const PROXY_PORT = 8888;
const TARGET_HOST = 'www.codebuff.com';

const server = http.createServer((req, res) => {
  // Log the request
  console.log('\n=== REQUEST ===');
  console.log('Method:', req.method);
  console.log('URL:', req.url);
  console.log('Headers:', JSON.stringify(req.headers, null, 2));
  
  let body = [];
  req.on('data', chunk => body.push(chunk));
  req.on('end', () => {
    const bodyStr = Buffer.concat(body).toString();
    console.log('Body:', bodyStr.substring(0, 2000));
    
    // Forward to the real server
    const options = {
      hostname: TARGET_HOST,
      port: 443,
      path: req.url,
      method: req.method,
      headers: {
        ...req.headers,
        host: TARGET_HOST,
      },
      rejectUnauthorized: false,
    };
    
    const proxyReq = https.request(options, (proxyRes) => {
      console.log('\n=== RESPONSE ===');
      console.log('Status:', proxyRes.statusCode);
      console.log('Headers:', JSON.stringify(proxyRes.headers, null, 2));
      
      let respBody = [];
      proxyRes.on('data', chunk => respBody.push(chunk));
      proxyRes.on('end', () => {
        const respStr = Buffer.concat(respBody).toString();
        console.log('Response Body:', respStr.substring(0, 2000));
        res.writeHead(proxyRes.statusCode, proxyRes.headers);
        res.end(Buffer.concat(respBody));
      });
    });
    
    proxyReq.on('error', (e) => {
      console.error('Proxy error:', e);
      res.writeHead(502);
      res.end('Proxy error');
    });
    
    if (bodyStr) proxyReq.write(bodyStr);
    proxyReq.end();
  });
});

server.listen(PROXY_PORT, '127.0.0.1', () => {
  console.log(`Sniffing proxy on http://127.0.0.1:${PROXY_PORT} -> https://${TARGET_HOST}`);
});
