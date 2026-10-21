// Usage: OPENAPI_VALIDATOR_ROOT=/path/to/temp/node_modules node scripts/verify_docs.mjs
// The validator is an optional documentation dependency, not an application dependency.
import { createRequire } from 'node:module';
import { readFile, readdir, access } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// This intentionally reads only literal Fiber registrations, matching the repository's route style.
// Router helpers use the same router parameter names as the assembly in router.go.
export function discoverRoutes(sources) {
  const production = [...sources].filter(([filename]) => filename.endsWith('.go') && !filename.endsWith('_test.go'));
  const prefixes = new Map([['app', '']]);
  const groups = production.flatMap(([, source]) => [...source.matchAll(/^\s*(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"/gm)]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const [, name, parent, suffix] of groups) {
      if (!prefixes.has(parent)) continue;
      const prefix = prefixes.get(parent) + suffix;
      if (prefixes.has(name) && prefixes.get(name) !== prefix) throw new Error(`Ambiguous router alias ${name}`);
      if (!prefixes.has(name)) { prefixes.set(name, prefix); changed = true; }
    }
  }
  const declared = new Set();
  for (const [, source] of production) {
    for (const [, router, method, route] of source.matchAll(/^\s*(\w+)\.(Get|Post|Put|Delete|Patch|Head|Options)\("([^"]+)"/gm)) {
      if (!prefixes.has(router)) continue;
      const full = prefixes.get(router) + route;
      if (!full.startsWith('/api/v1/')) continue;
      declared.add(`${method.toLowerCase()} ${full.slice('/api/v1'.length).replace(/:([\w]+)/g, '{$1}')}`);
    }
  }
  return declared;
}

async function main() {
  const backend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
  const workspace = path.dirname(backend);
  const moduleRoot = process.env.OPENAPI_VALIDATOR_ROOT;
  if (!moduleRoot) throw new Error('Set OPENAPI_VALIDATOR_ROOT to node_modules containing @apidevtools/swagger-parser');
  const require = createRequire(path.join(path.resolve(moduleRoot), '__validator__.cjs'));
  const SwaggerParser = require('@apidevtools/swagger-parser');
  const spec = await SwaggerParser.validate(path.join(backend, 'docs/openapi.yaml'));
  const httpDirectory = path.join(backend, 'internal/httpapi');
  const sourceFiles = (await readdir(httpDirectory)).filter(name => name.endsWith('.go') && !name.endsWith('_test.go')).sort();
  const sources = new Map(await Promise.all(sourceFiles.map(async name => [name, await readFile(path.join(httpDirectory, name), 'utf8')])));
  const declared = discoverRoutes(sources);
  const documented = new Set();
  for (const [route, item] of Object.entries(spec.paths)) {
    for (const method of ['get', 'post', 'put', 'delete', 'patch', 'head', 'options']) {
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
    ...await markdownFiles(path.join(backend, 'docs')), ...await markdownFiles(path.join(workspace, 'frontend-ss/docs')), ...await markdownFiles(path.join(workspace, 'deployment')), ...await markdownFiles(path.join(workspace, 'docs/features'))];
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
  console.log(`OpenAPI valid: ${documented.size} operations match ${sourceFiles.length} production HTTP files; ${docs.length} Markdown files and ${links} local links verified.`);
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await main();
