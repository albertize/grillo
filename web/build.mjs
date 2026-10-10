// SPDX-License-Identifier: Apache-2.0
// Locked, local-only build. Dependency provisioning is a separate explicit step.
import { build } from 'esbuild';
import { readFile, mkdir, writeFile, readdir, rm } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, dirname, basename } from 'node:path';
import { createHash } from 'node:crypto';

const root = dirname(fileURLToPath(import.meta.url));
const output = resolve(root, '../internal/ui/assets/generated');
const result = await build({
  absWorkingDir: root,
  entryPoints: { app: 'src/app.jsx', 'theme-init': 'src/theme-init.mjs' },
  bundle: true,
  outdir: output,
  entryNames: '[name]',
  assetNames: 'fonts/[name]-[hash]',
  format: 'esm',
  target: ['es2022'],
  minify: true,
  jsx: 'automatic',
  define: { 'process.env.NODE_ENV': '"production"' },
  loader: { '.woff2': 'file', '.woff': 'file', '.ttf': 'file', '.svg': 'file', '.eot': 'file' },
  legalComments: 'eof',
  write: false,
});

// Ship upstream notices for every locked package, including CSS/font licenses.
const lock = JSON.parse(await readFile(resolve(root, 'package-lock.json'), 'utf8'));
const notices = ['Grillo frontend third-party notices. Original Grillo source: Apache-2.0.\n'];
for (const [path, entry] of Object.entries(lock.packages).sort()) {
  if (!path) continue;
  const location = resolve(root, path);
  notices.push(`\n--- ${path} ${entry.version} (${entry.license ?? 'see notices'}) ---\n`);
  async function collect(dir) {
    for (const item of (await readdir(dir, { withFileTypes: true })).sort((a,b) => a.name.localeCompare(b.name, 'en'))) {
      if (item.isDirectory() && item.name !== 'node_modules') await collect(resolve(dir, item.name));
      else if (item.isFile() && /^(license|licence|notice|copying|copyright|ofl)/i.test(item.name)) notices.push(await readFile(resolve(dir, item.name), 'utf8'));
    }
  }
  try { await collect(location); }
  catch (error) { if (!(entry.optional && error.code === 'ENOENT')) throw error; }
}
for (const file of (await readdir(resolve(root, 'licenses'))).sort()) notices.push(await readFile(resolve(root, 'licenses', file), 'utf8'));
const files = new Map(result.outputFiles.map(f => [f.path, f.contents]));
files.set(resolve(output, 'licenses.txt'), Buffer.from(notices.join('\n')));
// Explicit user-supplied branding assets only; never embed the whole media tree.
const iconFiles = ['apple-touch-icon.png', 'favicon.ico', 'favicon.svg', 'g-foglia-32.png', 'g-foglia-64.png', 'icon-192.png', 'icon-512.png'];
for (const name of iconFiles) files.set(resolve(output, 'icons', name), await readFile(resolve(root, '../media/g-icon', name)));
files.set(resolve(output, 'site.webmanifest'), Buffer.from(JSON.stringify({
  name: 'Grillo console', short_name: 'Grillo', start_url: '/', display: 'standalone',
  theme_color: '#102b35', background_color: '#f0f4f4',
  icons: [192, 512].map(size => ({ src: `/assets/generated/icons/icon-${size}.png`, sizes: `${size}x${size}`, type: 'image/png', purpose: 'any' })),
}) + '\n'));
// Fingerprint the lock and source inputs so Go tests can reject absent assets.
const digest = createHash('sha256');
for (const file of ['package.json', 'package-lock.json', 'build.mjs', '.npmrc', ...((await readdir(resolve(root, 'licenses'))).sort().map(n => 'licenses/' + n)), ...((await readdir(resolve(root, 'src'))).filter(n => !n.includes('.test.')).sort().map(n => 'src/' + n)), '../internal/ui/assets/index.html', ...iconFiles.map(n => '../media/g-icon/' + n)]) {
  digest.update(file); digest.update(await readFile(resolve(root, file)));
}
files.set(resolve(output, 'manifest.json'), Buffer.from(JSON.stringify({ sourceHash: digest.digest('hex'), react: '19.3.0', patternfly: '6.6.1' }) + '\n'));

if (process.argv.includes('--check')) {
  for (const [file, data] of files) {
    const existing = await readFile(file).catch(() => null);
    if (!existing || !Buffer.from(data).equals(existing)) throw new Error(`Missing/stale frontend asset: ${basename(file)}; run make ui-build`);
  }
  console.log('PASS: frontend assets match locked sources');
} else {
  // Remove only this build's dedicated generated subtree, never the source assets.
  await rm(output, { recursive: true, force: true });
  for (const [file, data] of files) { await mkdir(dirname(file), { recursive: true }); await writeFile(file, data); }
  console.log(`Built ${files.size} embedded frontend assets (React 19.3.0, PatternFly 6.6.1)`);
}
