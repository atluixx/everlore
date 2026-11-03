// Work around Vercel's local Go builder pointing filePathMap at /tmp binaries
// that its prebuilt uploader cannot resolve. Run after `vercel build`.
import { copyFile, readFile, writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';

const configPath = '.vercel/output/functions/api/index.func/.vc-config.json';
const config = JSON.parse(await readFile(configPath, 'utf8'));
const source = config.filePathMap?.bootstrap;
if (source) {
  const resolved = path.resolve(source);
  const tempRoot = path.resolve(process.env.TMPDIR || '/tmp') + path.sep;
  if (!resolved.startsWith(tempRoot) || path.basename(resolved) !== 'bootstrap') {
    throw new Error('Unexpected Vercel bootstrap path; refusing to copy it');
  }
  const directory = path.dirname(configPath);
  await mkdir(directory, { recursive: true });
  await copyFile(resolved, path.join(directory, 'bootstrap'));
  delete config.filePathMap.bootstrap;
  if (Object.keys(config.filePathMap).length === 0) delete config.filePathMap;
  await writeFile(configPath, JSON.stringify(config, null, 2) + '\n');
}
