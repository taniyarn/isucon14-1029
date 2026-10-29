#!/usr/bin/env node
// app-size: the code's scale in <= 25 lines of Markdown, before app-analysis (no network, no DB, no deps).
// usage: node app-size.mjs [DIR=webapp] [--out <file>]
import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';
import readline from 'node:readline';

const args = process.argv.slice(2);
let out = '';
let dir = 'webapp';
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--out') out = args[++i] || '';
  else dir = args[i];
}
if (!fs.existsSync(dir) || !fs.statSync(dir).isDirectory()) {
  console.error(`app-size: ${dir} is not a directory`);
  process.exit(1);
}

const SKIP = new Set(['node_modules', 'vendor', 'target', '.git', 'dist', 'build', 'public', 'static', '.venv', 'venv', '__pycache__', 'frontend']);
function walk(d, acc = [], depth = 0) {
  let ents;
  try { ents = fs.readdirSync(d, { withFileTypes: true }); } catch { return acc; }
  for (const e of ents) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) { if (!SKIP.has(e.name) && depth < 8) walk(p, acc, depth + 1); }
    else if (e.isFile()) acc.push(p);
  }
  return acc;
}
const files = walk(dir);
const rel = (p) => path.relative(dir, p);
const read = (p) => { try { return fs.readFileSync(p, 'utf8'); } catch { return ''; } };
const nlines = (s) => (s ? s.split('\n').length - (s.endsWith('\n') ? 1 : 0) : 0);
const fmt = (n) => n.toLocaleString('en-US');

// ---- language dirs (Go first) ----
const LANGS = [
  { name: 'Go', marker: 'go.mod', ext: ['.go'] },
  { name: 'Rust', marker: 'Cargo.toml', ext: ['.rs'] },
  { name: 'Node', marker: 'package.json', ext: ['.js', '.ts', '.mjs', '.cjs'] },
  { name: 'Python', marker: 'pyproject.toml', ext: ['.py'] },
  { name: 'Python', marker: 'requirements.txt', ext: ['.py'] },
  { name: 'Ruby', marker: 'Gemfile', ext: ['.rb'] },
  { name: 'PHP', marker: 'composer.json', ext: ['.php'] },
  { name: 'Perl', marker: 'cpanfile', ext: ['.pm', '.pl'] },
];
const langDirs = [];
for (const L of LANGS) {
  for (const f of files) {
    if (path.basename(f) !== L.marker) continue;
    const d = path.dirname(f);
    if (langDirs.some((x) => x.dir === d)) continue;
    langDirs.push({ ...L, dir: d });
  }
}
const main = langDirs[0];
const lines = [];
lines.push(`## App size: \`${dir}\``);
if (!main) {
  lines.push(`- no language dir found (no go.mod / Cargo.toml / package.json / … under ${dir})`);
}
const srcOf = (L) => files.filter((f) => f.startsWith(L.dir + path.sep) && L.ext.includes(path.extname(f)) && !/_test\.go$/.test(f) && !f.includes(`${path.sep}node_modules${path.sep}`));
let src = [];
if (main) {
  src = srcOf(main).map((f) => { const s = read(f); return { f, s, n: nlines(s) }; });
  src.sort((a, b) => b.n - a.n);
  const total = src.reduce((a, x) => a + x.n, 0);
  const others = langDirs.slice(1).map((L) => `${rel(L.dir)} (${L.name})`);
  lines.push(`- Language: **${main.name}** \`${path.join(dir, rel(main.dir))}\` (${main.marker}) · ${src.length} files · **${fmt(total)} lines**${others.length ? ` · other dirs: ${others.slice(0, 6).join(', ')}${others.length > 6 ? ', …' : ''}` : ''}`);
  lines.push(`- Largest files: ${src.slice(0, 10).map((x) => `${path.relative(main.dir, x.f)} ${fmt(x.n)}`).join(' · ')}`);
}

// ---- routes and handlers (Go; simple greps for the others) ----
if (main) {
  const all = src.map((x) => x.s).join('\n');
  const methods = {};
  const perFile = {};
  let nroutes = 0;
  let ndebug = 0;
  const reMethodPath = /\.(?:HandleFunc|Handle|MethodFunc|Method)\(\s*"(?:(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS) )?\/[^"]*"/g; // net/http 1.22 mux, gorilla
  const reVerb = /\.(Get|Post|Put|Delete|Patch|Head|Options|GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any|get|post|put|delete|patch)\(\s*["'`]\/[^"'`]*["'`]/g; // chi / echo / gin / fiber / express / sinatra-ish
  const reRuby = /^\s*(get|post|put|delete|patch)\s+['"]\/[^'"]*['"]/gm;
  for (const x of src) {
    let c = 0;
    for (const m of x.s.matchAll(reMethodPath)) { if (m[0].includes('"/debug/')) { ndebug++; continue; } c++; const k = m[1] || 'ANY'; methods[k] = (methods[k] || 0) + 1; }
    for (const m of x.s.matchAll(reVerb)) { c++; const k = m[1].toUpperCase(); methods[k] = (methods[k] || 0) + 1; }
    for (const m of x.s.matchAll(reRuby)) { c++; const k = m[1].toUpperCase(); methods[k] = (methods[k] || 0) + 1; }
    if (c) perFile[path.relative(main.dir, x.f)] = c;
    nroutes += c;
  }
  let handlers = '';
  if (main.name === 'Go') {
    const reH = /^func\s+(?:\([^)]*\)\s*)?\w+\(\s*\w+\s+(?:http\.ResponseWriter|echo\.Context|\*gin\.Context|\*fiber\.Ctx)/gm;
    handlers = ` · handler funcs: ${(all.match(reH) || []).length}`;
  }
  const mod = read(path.join(main.dir, main.marker));
  const fw = [['go-chi/chi', 'chi'], ['labstack/echo', 'echo'], ['gin-gonic/gin', 'gin'], ['gorilla/mux', 'gorilla/mux'], ['gofiber/fiber', 'fiber'], ['"express"', 'express'], ['"fastify"', 'fastify'], ['"hono"', 'hono'], ['actix-web', 'actix-web'], ['axum', 'axum']]
    .filter(([k]) => mod.includes(k)).map(([, v]) => v);
  if (main.name === 'Go' && /\bHandleFunc\("(GET|POST|PUT|DELETE|PATCH) /.test(all)) fw.push('net/http patterns');
  const mstr = Object.entries(methods).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k} ${v}`).join(', ');
  lines.push(`- Routes: **${nroutes}**${mstr ? ` (${mstr})` : ''}${ndebug ? ` (+${ndebug} /debug/)` : ''}${handlers}${fw.length ? ` · router: ${fw.join(' + ')}` : ''}`);
  const pf = Object.entries(perFile).sort((a, b) => b[1] - a[1]);
  if (pf.length) lines.push(`- Routes registered in: ${pf.slice(0, 6).map(([f, n]) => `${f} ${n}`).join(' · ')}`);
}
const openapi = files.find((f) => /(^|\/)(openapi|swagger)\.(ya?ml|json)$/.test(f) && !f.includes('node_modules'));
if (openapi) {
  const s = read(openapi);
  const n = (s.match(/^ {2}\/[^\s:]*:\s*$/gm) || []).length;
  lines.push(`- OpenAPI: \`${path.join(dir, rel(openapi))}\` (${n} paths)`);
}

// ---- schema ----
const sqlFiles = files.filter((f) => /\.sql$/.test(f)).sort();
const tables = new Map(); // name -> {cols, idx, rows, file}
const tname = (s) => s.replace(/[`"]/g, '').split('.').pop().toLowerCase();
function splitTop(body) {
  const parts = []; let depth = 0; let cur = ''; let q = '';
  for (const ch of body) {
    if (q) { cur += ch; if (ch === q) q = ''; continue; }
    if (ch === "'" || ch === '"' || ch === '`') { q = ch; cur += ch; continue; }
    if (ch === '(') depth++;
    if (ch === ')') depth--;
    if (ch === ',' && depth === 0) { parts.push(cur.trim()); cur = ''; continue; }
    cur += ch;
  }
  if (cur.trim()) parts.push(cur.trim());
  return parts;
}
for (const f of sqlFiles) {
  const s = read(f).replace(/--[^\n]*/g, '');
  const reCT = /CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([`"\w.]+)\s*\(/gi;
  let m;
  while ((m = reCT.exec(s))) {
    let i = reCT.lastIndex; let depth = 1; let q = '';
    for (; i < s.length && depth > 0; i++) {
      const ch = s[i];
      if (q) { if (ch === q) q = ''; continue; }
      if (ch === "'" || ch === '"' || ch === '`') q = ch;
      else if (ch === '(') depth++;
      else if (ch === ')') depth--;
    }
    const parts = splitTop(s.slice(reCT.lastIndex, i - 1));
    let cols = 0; let idx = 0;
    for (const p of parts) {
      if (/^(PRIMARY\s+KEY|UNIQUE|KEY|INDEX|FULLTEXT|SPATIAL)\b/i.test(p)) idx++;
      else if (/^(CONSTRAINT|FOREIGN\s+KEY|CHECK)\b/i.test(p)) { if (/\bUNIQUE\b/i.test(p)) idx++; }
      else { cols++; if (/\bPRIMARY\s+KEY\b|\bUNIQUE\b/i.test(p)) idx++; }
    }
    tables.set(tname(m[1]), { cols, idx, extra: 0, rows: 0, file: rel(f) });
  }
}
let extraIdx = 0;
for (const f of sqlFiles) {
  const s = read(f).replace(/--[^\n]*/g, '');
  for (const m of s.matchAll(/CREATE\s+(?:UNIQUE\s+|FULLTEXT\s+|SPATIAL\s+)?INDEX\s+[`\w]+\s+ON\s+([`"\w.]+)/gi)) {
    const t = tables.get(tname(m[1])); if (t) { t.extra++; extraIdx++; }
  }
  for (const m of s.matchAll(/ALTER\s+TABLE\s+([`"\w.]+)([^;]*)/gi)) {
    const t = tables.get(tname(m[1])); if (!t) continue;
    const n = (m[2].match(/\bADD\s+(?:UNIQUE\s+|FULLTEXT\s+|SPATIAL\s+|PRIMARY\s+)?(?:INDEX|KEY)\b/gi) || []).length;
    t.extra += n; extraIdx += n;
  }
}
// INSERT tuples in *.sql / *.sql.gz (estimate: tuples per INSERT statement, one statement per line or VALUES lines)
async function countInserts(f) {
  let stream = fs.createReadStream(f);
  if (f.endsWith('.gz')) stream = stream.pipe(zlib.createGunzip());
  const rl = readline.createInterface({ input: stream, crlfDelay: Infinity });
  let cur = null;
  for await (const line of rl) {
    const m = /^\s*(?:INSERT|REPLACE)\s+(?:IGNORE\s+)?INTO\s+([`"\w.]+)/i.exec(line);
    if (m) {
      cur = tname(m[1]);
      const v = line.slice(m[0].length);
      const n = /VALUES?\s*\(/i.test(v) ? (v.match(/\)\s*,\s*\(/g) || []).length + 1 : 0;
      if (tables.has(cur)) tables.get(cur).rows += n;
      if (/;\s*$/.test(line)) cur = null;
      continue;
    }
    if (cur && /^\s*(?:VALUES?\s*)?\(/i.test(line) && tables.has(cur)) tables.get(cur).rows += (line.match(/\)\s*,\s*\(/g) || []).length + 1;
    if (cur && /;\s*$/.test(line)) cur = null;
  }
}
const dataFiles = files.filter((f) => /\.sql(\.gz)?$/.test(f));
for (const f of dataFiles) { try { await countInserts(f); } catch { /* unreadable gz: skip */ } }
const missing = [];
for (const f of files.filter((x) => /(^|\/)init[^/]*\.sh$/.test(x))) {
  const code = read(f).split('\n').filter((l) => !/^\s*#/.test(l)).join('\n');
  for (const m of code.matchAll(/(?<![\w.$*{-])([\w-][\w.-]*\.sql(?:\.gz)?)/g)) {
    if (!fs.existsSync(path.join(path.dirname(f), m[1])) && !missing.includes(m[1])) missing.push(m[1]);
  }
}
if (tables.size) {
  const schemaFiles = [...new Set([...tables.values()].map((t) => t.file))];
  const totalRows = [...tables.values()].reduce((a, t) => a + t.rows, 0);
  lines.push(`- Tables: **${tables.size}** in ${schemaFiles.map((x) => `\`${x}\``).join(', ')} · initial rows ≈ ${fmt(totalRows)} (INSERT tuples in ${dataFiles.length} .sql/.sql.gz)${extraIdx ? ` · +${extraIdx} idx from CREATE INDEX/ALTER` : ''}${missing.length ? ` · **not in repo** (referenced by init*.sh): ${missing.slice(0, 3).join(', ')}` : ''}`);
  lines.push('');
  lines.push('| table | cols | idx (PK+keys) | initial rows |');
  lines.push('|---|---:|---:|---:|');
  const rows = [...tables.entries()].sort((a, b) => b[1].rows - a[1].rows || a[0].localeCompare(b[0]));
  const MAX = 12;
  for (const [n, t] of rows.slice(0, MAX)) lines.push(`| ${n} | ${t.cols} | ${t.idx}${t.extra ? `+${t.extra}` : ''} | ${t.rows ? fmt(t.rows) : '-'} |`);
  if (rows.length > MAX) lines.push(`| … ${rows.length - MAX} more: ${rows.slice(MAX).map(([n]) => n).join(', ')} | | | |`);
  lines.push('');
} else {
  lines.push(`- Tables: no CREATE TABLE found in ${sqlFiles.length} .sql files under ${dir}`);
}

// ---- notable components ----
const notes = [];
const docker = files.filter((f) => /(^|\/)(Dockerfile[^/]*|(docker-)?compose[^/]*\.ya?ml)$/.test(f)).map(rel);
if (docker.length) notes.push(`docker: ${docker.slice(0, 4).join(', ')}${docker.length > 4 ? ', …' : ''}`);
const codeFiles = langDirs.flatMap((L) => srcOf(L));
const sqlite = files.filter((f) => /\.(db|sqlite3?)$/.test(f)).map(rel);
const sqliteCode = codeFiles.filter((f) => /sqlite/i.test(read(f))).map(rel);
if (sqlite.length || sqliteCode.length) notes.push(`sqlite: ${[...sqlite, ...sqliteCode].slice(0, 4).join(', ')}`);
const urls = new Map(); const execs = [];
for (const f of codeFiles) {
  const s = read(f).split('\n');
  s.forEach((l, i) => {
    if (/^\s*(\/\/|#|\*|\/\*)/.test(l)) return;
    for (const m of l.matchAll(/https?:\/\/[A-Za-z0-9_.\-]+(?::\d+)?/g)) {
      const u = m[0];
      if (/\/\/(github\.com|golang\.org|pkg\.go\.dev|www\.w3\.org|json-schema\.org|example\.(com|org))/.test(u)) continue;
      if (!urls.has(u)) urls.set(u, `${rel(f)}:${i + 1}`);
    }
    if (/exec\.Command|child_process|subprocess\.|\bsystem\(/.test(l)) execs.push(`${rel(f)}:${i + 1}`);
  });
}
if (urls.size) notes.push(`URLs in code: ${[...urls].slice(0, 4).map(([u, w]) => `${u} (${w})`).join(', ')}${urls.size > 4 ? ` +${urls.size - 4}` : ''}`);
if (execs.length) notes.push(`**exec**: ${execs.slice(0, 4).join(', ')}${execs.length > 4 ? ` +${execs.length - 4}` : ''}`);
const others = [];
for (const [re, n] of [[/redis/i, 'redis'], [/memcache/i, 'memcached'], [/amqp|rabbitmq/i, 'amqp'], [/pdns|powerdns/i, 'powerdns']]) {
  if (files.some((f) => re.test(rel(f))) || codeFiles.some((f) => re.test(read(f)))) others.push(n);
}
if (others.length) notes.push(`mentions: ${others.join(', ')}`);
lines.push(`- Components: ${notes.length ? notes.join(' · ') : 'none found (no Dockerfile/compose, sqlite, URLs, exec in code)'}`);

const text = lines.join('\n') + '\n';
process.stdout.write(text);
if (out) { fs.mkdirSync(path.dirname(out), { recursive: true }); fs.writeFileSync(out, text); console.error(`app-size: wrote ${out}`); }
