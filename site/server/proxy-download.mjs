import http from "node:http";
import https from "node:https";
import tls from "node:tls";

class HttpConnectAgent extends https.Agent {
  constructor(proxyUrl) {
    super({ keepAlive: false });
    this.proxy = new URL(proxyUrl);
    if (this.proxy.protocol !== "http:") throw new Error("Only HTTP CONNECT proxy URLs are supported");
  }

  createConnection(options, callback) {
    const targetHost = options.servername || options.host;
    const targetPort = Number(options.port || 443);
    const headers = { host: `${targetHost}:${targetPort}` };
    if (this.proxy.username || this.proxy.password) {
      const value = Buffer.from(`${decodeURIComponent(this.proxy.username)}:${decodeURIComponent(this.proxy.password)}`).toString("base64");
      headers["proxy-authorization"] = `Basic ${value}`;
    }
    const connect = http.request({
      host: this.proxy.hostname,
      port: Number(this.proxy.port || 80),
      method: "CONNECT",
      path: `${targetHost}:${targetPort}`,
      headers,
    });
    connect.once("connect", (response, socket, head) => {
      if (response.statusCode !== 200) {
        socket.destroy();
        callback(new Error(`Proxy CONNECT failed (${response.statusCode})`));
        return;
      }
      if (head?.length) socket.unshift(head);
      const secure = tls.connect({ socket, servername: targetHost, rejectUnauthorized: true });
      secure.once("secureConnect", () => callback(null, secure));
      secure.once("error", callback);
    });
    connect.once("error", callback);
    connect.setTimeout(30_000, () => connect.destroy(new Error("Proxy CONNECT timeout")));
    connect.end();
  }
}
export async function downloadHttpsViaProxy(url, proxyUrl, options = {}) {
  const maxBytes = options.maxBytes ?? 100 * 1024 * 1024;
  const timeoutMs = options.timeoutMs ?? 120_000;
  const agent = new HttpConnectAgent(proxyUrl);
  async function request(target, redirects = 0) {
    if (redirects > 5) throw new Error("Proxy download exceeded redirect limit");
    const parsed = new URL(target);
    if (parsed.protocol !== "https:") throw new Error("Proxy download only permits HTTPS targets");
    return new Promise((resolve, reject) => {
      const req = https.request(parsed, { method: "GET", agent, headers: { accept: "application/zip,application/octet-stream;q=0.9,*/*;q=0.1" } }, (response) => {
        if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
          response.resume();
          request(new URL(response.headers.location, parsed).toString(), redirects + 1).then(resolve, reject);
          return;
        }
        const chunks = [];
        let size = 0;
        response.on("data", (chunk) => {
          size += chunk.length;
          if (size > maxBytes) response.destroy(new Error("Proxy download exceeds size limit"));
          else chunks.push(chunk);
        });
        response.once("error", reject);
        response.once("end", () => resolve(new Response(Buffer.concat(chunks), { status: response.statusCode, headers: response.headers })));
      });
      req.once("error", reject);
      req.setTimeout(timeoutMs, () => req.destroy(new Error("Proxy download timeout")));
      req.end();
    });
  }
  try {
    return await request(url);
  } finally {
    agent.destroy();
  }
}
