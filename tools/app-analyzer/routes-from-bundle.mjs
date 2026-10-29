// Extract the SPA's routes (including nested React-Router style children) from the JS bundle, as seeds for analyzer.config.json.
// Usage: node routes-from-bundle.mjs <baseURL> [--ip <server ip>]   (--ip = reach the host without /etc/hosts; TLS errors ignored)
// Output: JSON array of paths; params are kept as :param so you can substitute real ids (e.g. /watch/:id -> /watch/1).
import https from "node:https";
import http from "node:http";

const args = process.argv.slice(2);
const baseURL = args.find((a) => !a.startsWith("--"));
const ip = args.includes("--ip") ? args[args.indexOf("--ip") + 1] : null;
if (!baseURL) { console.error("usage: node routes-from-bundle.mjs <baseURL> [--ip <ip>]"); process.exit(1); }
const base = new URL(baseURL);

function get(u) {
  return new Promise((resolve, reject) => {
    const url = new URL(u, base);
    const mod = url.protocol === "https:" ? https : http;
    const opts = { host: ip ?? url.hostname, port: url.port || (url.protocol === "https:" ? 443 : 80), path: url.pathname + url.search, headers: { Host: url.hostname }, servername: url.hostname, rejectUnauthorized: false, timeout: 15000 };
    const req = mod.get(opts, (res) => { const chunks = []; res.on("data", (c) => chunks.push(c)); res.on("end", () => resolve({ status: res.statusCode, body: Buffer.concat(chunks).toString("utf8") })); });
    req.on("error", reject); req.on("timeout", () => req.destroy(new Error("timeout")));
  });
}

const html = (await get("/")).body;
const scripts = [...html.matchAll(/<script[^>]+src="([^"]+\.js)"/g)].map((m) => m[1]);
if (!scripts.length) { console.error("no <script src=*.js> in /; not an SPA? (seeds: use the manual's page list)"); process.exit(2); }
const routes = new Set();
for (const src of scripts) {
  const js = (await get(src)).body;
  // walk the text: `path:"x"` sets the current segment; `children:[` opens a nested level under the last path; `]` closes it.
  // Good enough for React Router / Vue Router style route objects in a minified bundle.
  const stack = [""];
  let depth = 0;
  const open = [];   // bracket depth at which each stack level was pushed
  const re = /path:\s*"([^"]*)"|children:\s*\[|[\[\]]/g;
  let m, last = null;
  while ((m = re.exec(js))) {
    const tok = m[0];
    if (tok.startsWith("path:")) {
      const seg = m[1];
      const full = seg.startsWith("/") ? seg : (stack[stack.length - 1].replace(/\/$/, "") + "/" + seg).replace(/\/+/g, "/");
      last = full || "/";
      routes.add(last.replace(/\/$/, "") || "/");
    } else if (tok.startsWith("children")) {
      stack.push(last ?? stack[stack.length - 1]); open.push(depth); depth++;
    } else if (tok === "[") depth++;
    else if (tok === "]") { depth--; if (open.length && open[open.length - 1] === depth) { open.pop(); stack.pop(); } }
  }
}
const out = [...routes].filter((r) => !/[*{]/.test(r)).sort();
console.log(JSON.stringify(out, null, 2));
console.error(`${out.length} routes from ${scripts.length} bundle(s). Replace :params with real ids in seeds; drop auth/login pages for logged-in personas if they redirect.`);
