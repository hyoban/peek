// Codiff's side of the comparison scripts/perf/compare.sh makes, run as
// Electron's main script by run.mjs: it starts Codiff's own main script
// with settings and a home of its own, as Codiff's startup benchmark
// (electron/benchmarks/startup.cjs) does, and measures, in its window, the
// measures of Peek's performance tests, written to PERF_OUTPUT as JSON:
//
//   - open: from the window made to the diffs in view drawn;
//   - scroll: a frame, scrolling down 120 points a frame;
//   - resize: a frame, the window widening 4 points a frame;
//   - load-all: scrolling to the end, 700 points every two frames;
//   - rss: the memory all of Electron's processes hold after.
//
// Chromium draws frames as fast as it can here, not at the display's
// rate, so that a frame's time is the work it took.
const { app, ipcMain } = require('electron');
const { writeFileSync } = require('node:fs');
const { join } = require('node:path');
const os = require('node:os');

app.commandLine.appendSwitch('disable-frame-rate-limit');
app.commandLine.appendSwitch('disable-gpu-vsync');
os.homedir = () => process.env.PERF_HOME;
app.setPath('userData', join(os.homedir(), 'chromium'));

const results = {};
let window;
let windowCreated;

const preload = join(os.homedir(), 'perf-preload.cjs');
writeFileSync(
  preload,
  `const { ipcRenderer } = require('electron');
globalThis.perfSend = (name, payload) => ipcRenderer.send('perf', name, payload);
globalThis.perfInvoke = (name, payload) => ipcRenderer.invoke('perf', name, payload);
(${measure.toString()})(${JSON.stringify(process.env.PERF_MEASURES || '')});`,
);

app.on('browser-window-created', (_event, w) => {
  window = w;
  windowCreated = performance.timeOrigin + performance.now();
  w.webContents.session.registerPreloadScript({ filePath: preload, type: 'frame' });
});

ipcMain.on('perf', (_event, name, payload) => {
  switch (name) {
    case 'open':
      results.open = payload - windowCreated;
      break;
    case 'result':
      Object.assign(results, payload);
      break;
    case 'done': {
      let kb = 0;
      for (const metric of app.getAppMetrics()) {
        kb += metric.memory.workingSetSize;
      }
      results.rss = kb / 1024;
      writeFileSync(process.env.PERF_OUTPUT, JSON.stringify(results));
      app.exit();
      break;
    }
  }
});
ipcMain.handle('perf', (_event, name, payload) => {
  if (name === 'resize') {
    const [, height] = window.getSize();
    window.setSize(payload, height);
  }
});

// measure runs in Codiff's window, before its own scripts.
function measure(measures) {
  const frame = () => new Promise((resolve) => requestAnimationFrame(() => resolve(performance.now())));
  // The diffs in view drawn, their lines in them, as Codiff's startup
  // benchmark waits for, or folded under their headers, as the files it
  // takes for generated are.
  const drawn = () => {
    const shown = [...document.querySelectorAll('.review diffs-container')].filter((node) => {
      const bounds = node.getBoundingClientRect();
      return bounds.height > 0 && bounds.bottom > 0 && bounds.top < innerHeight;
    });
    return (
      shown.length > 0 &&
      shown.every(
        (node) =>
          node.shadowRoot?.querySelector('[data-code] [data-line]') ||
          node.querySelector('.codiff-file-header.collapsed'),
      ) &&
      document.fonts.status === 'loaded'
    );
  };
  // The element that scrolls the diffs.
  const scroller = () => {
    for (let node = document.querySelector('.review diffs-container'); node; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (/(auto|scroll)/.test(style.overflowY) && node.scrollHeight > node.clientHeight) {
        return node;
      }
    }
    return document.scrollingElement;
  };
  const average = async (n, step) => {
    let last = await frame();
    const start = last;
    for (let i = 0; i < n; i++) {
      await step(i);
      last = await frame();
    }
    return (last - start) / n;
  };
  const run = async () => {
    while (!drawn()) {
      await frame();
    }
    perfSend('open', performance.timeOrigin + performance.now());
    for (let i = 0; i < 10; i++) {
      await frame();
    }
    const result = {};
    const el = scroller();
    if (measures.includes('scroll')) {
      result.scroll = await average(150, () => {
        el.scrollTop += 120;
      });
    }
    if (measures.includes('resize')) {
      el.scrollTop = 0;
      await frame();
      const width = outerWidth;
      result.resize = await average(30, (i) => perfInvoke('resize', width + i * 4));
    }
    if (measures.includes('load-all')) {
      const start = performance.now();
      el.scrollTop = 0;
      while (el.scrollTop + el.clientHeight < el.scrollHeight - 1) {
        el.scrollTop += 700; // as Peek's and Godiff's tests
        await frame();
        await frame();
      }
      result['load-all'] = performance.now() - start;
    }
    perfSend('result', result);
    perfSend('done');
  };
  addEventListener('DOMContentLoaded', () => run());
}

require(join(process.env.PERF_CODIFF, 'electron/main.cjs'));
