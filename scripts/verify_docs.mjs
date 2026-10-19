// Usage: OPENAPI_VALIDATOR_ROOT=/path/to/temp/node_modules node scripts/verify_docs.mjs
// The validator is an optional documentation dependency, not an application dependency.
import { createRequire } from 'node:module';
import { readFile, readdir, access } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const backend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const workspace = path.dirname(backend);
const moduleRoot = process.env.OPENAPI_VALIDATOR_ROOT;
if (!moduleRoot) throw new Error('Set OPENAPI_VALIDATOR_ROOT to node_modules containing @apidevtools/swagger-parser');
const require = createRequire(path.join(path.resolve(moduleRoot), '__validator__.cjs'));
const SwaggerParser = require('@apidevtools/swagger-parser');
const spec = await SwaggerParser.validate(path.join(backend, 'docs/openapi.yaml'));
const source = await readFile(path.join(backend, 'internal/httpapi/router.go'), 'utf8');
const prefixes = {api: '', protected: '', admin: '', pic: '/pic', panelist: '/panelist'};
const declared = new Set();
for (const match of source.matchAll(/\b(api|protected|admin|pic|panelist)\.(Get|Post|Put|Delete|Patch)\("([^"]+)"/g)) {
  declared.add(`${match[2].toLowerCase()} ${prefixes[match[1]]}${match[3].replace(/:([\w]+)/g, '{$1}')}`);
}
const documented = new Set();
for (const [route, item] of Object.entries(spec.paths)) {
  for (const method of ['get', 'post', 'put', 'delete', 'patch']) {
    if (item[method]) documented.add(`${method} ${route}`);
  }
}
const missing = [...declared].filter(x => !documented.has(x));
const extra = [...documented].filter(x => !declared.has(x));
if (missing.length || extra.length) throw new Error(JSON.stringify({missing, extra}));

async function markdownFiles(dir) {
  const files = [];
  for (const item of await readdir(dir, {withFileTypes: true})) {
    const full = path.join(dir, item.name);
    if (item.isDirectory()) files.push(...await markdownFiles(full));
    else if (item.name.endsWith('.md')) files.push(full);
  }
  return files;
}
const docs = [path.join(workspace, 'README.md'), path.join(backend, 'README.md'), path.join(workspace, 'frontend-ss/README.md'),
  ...await markdownFiles(path.join(backend, 'docs')), ...await markdownFiles(path.join(workspace, 'frontend-ss/docs'))];
let links = 0;
const broken = [];
for (const filename of docs) {
  const content = await readFile(filename, 'utf8');
  for (const match of content.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)) {
    const target = match[1].replace(/^<|>$/g, '').split('#')[0];
    if (!target || /^\w+:\/\//.test(target)) continue;
    links += 1;
    try { await access(path.resolve(path.dirname(filename), decodeURIComponent(target))); }
    catch { broken.push({filename: path.relative(workspace, filename), target}); }
  }
}
if (broken.length) throw new Error(JSON.stringify({broken}, null, 2));
console.log(`OpenAPI valid: ${documented.size} operations match router; ${docs.length} Markdown files and ${links} local links verified.`);
