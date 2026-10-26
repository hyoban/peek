// Times Codiff over the scenarios' repositories (scripts/perf/repos), as
// scripts/perf/compare.sh does, opening its window over each with
// harness.cjs, and prints a line a measure, as Peek's performance tests:
//
//   PERF codiff <scenario> <measure> <value> <unit>
//
// Run it with the Node of a checkout of Codiff, built, whose Electron it
// starts:
//
//   node scripts/perf/codiff/run.mjs path/to/codiff path/to/repos
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import process from 'node:process';

const [codiff, repos] = process.argv.slice(2).map((p) => resolve(p));
if (!codiff || !repos) {
  console.error('usage: node scripts/perf/codiff/run.mjs path/to/codiff path/to/repos');
  process.exit(2);
}
const electron = createRequire(join(codiff, 'package.json'))('electron');
const harness = join(import.meta.dirname, 'harness.cjs');
const work = await mkdtemp(join(tmpdir(), 'codiff-perf-'));

// open runs Codiff over the scenario's repository, its diffs in the
// style given, their lines wrapped as Peek's are, taking the measures.
async function open(scenario, style, measures) {
  const home = join(work, `${scenario}-${style}`);
  await mkdir(join(home, '.codiff'), { recursive: true });
  await writeFile(
    join(home, '.codiff', 'codiff.jsonc'),
    JSON.stringify({ settings: { agentBackend: 'claude', diffStyle: style, wordWrap: true } }),
  );
  await writeFile(
    join(home, '.codiff', 'window-state.json'),
    JSON.stringify({ height: 860, width: 1280, x: 100, y: 100 }),
  );
  const output = join(home, 'result.json');
  const env = {
    ...process.env,
    CODIFF_REPOSITORY_PATH: join(repos, scenario),
    ELECTRON_RENDERER_URL: '',
    PERF_CODIFF: codiff,
    PERF_HOME: home,
    PERF_MEASURES: measures.join(','),
    PERF_OUTPUT: output,
  };
  delete env.ELECTRON_RUN_AS_NODE;
  await new Promise((done, fail) => {
    const child = spawn(electron, [harness], { env, stdio: ['ignore', 'ignore', 'pipe'] });
    let stderr = '';
    child.stderr.on('data', (chunk) => (stderr += chunk));
    const timeout = setTimeout(() => {
      child.kill('SIGKILL');
      fail(new Error(`${scenario} timed out: ${stderr}`));
    }, 300_000);
    child.on('exit', (code) => {
      clearTimeout(timeout);
      code === 0 ? done() : fail(new Error(`${scenario}: Electron exited ${code}: ${stderr}`));
    });
  });
  const result = JSON.parse(await readFile(output, 'utf8'));
  await rm(home, { force: true, recursive: true });
  return result;
}

const report = (scenario, measure, value, unit) =>
  console.log(`PERF codiff ${scenario} ${measure} ${value.toFixed(1)} ${unit}`);

for (const [i, style] of ['unified', 'split'].entries()) {
  const large = await open('large', style, ['scroll', 'resize']);
  if (i === 0) report('large', 'open', large.open, 'ms');
  report('large', `scroll-${style}`, large.scroll, 'ms/frame');
  report('large', `resize-${style}`, large.resize, 'ms/frame');
}
for (const [i, style] of ['unified', 'split'].entries()) {
  const many = await open('many', style, ['scroll']);
  if (i === 0) report('many', 'open', many.open, 'ms');
  report('many', `scroll-${style}`, many.scroll, 'ms/frame');
}
const untracked = await open('untracked', 'unified', []);
report('untracked', 'open', untracked.open, 'ms');
const memory = await open('memory', 'unified', ['load-all']);
report('memory', 'open', memory.open, 'ms');
report('memory', 'load-all', memory['load-all'], 'ms');
report('memory', 'rss', memory.rss, 'MB');
await rm(work, { force: true, recursive: true });
