const assert = require('node:assert/strict');
const { mkdirSync, writeFileSync } = require('node:fs');
const { join, basename } = require('node:path');
const { chromium, firefox } = require('playwright');

const base = process.env.IBC_UI_URL || 'http://127.0.0.1:5180';
const output = process.env.IBC_EVIDENCE_DIR || '.local/ibc/continuity';
const repeats = Number(process.env.IBC_REPEATS || 5);
const profile = process.env.IBC_PROFILE || 'natural';
const browsers = (process.env.IBC_BROWSERS || 'chromium,firefox').split(',');
const fixtures = [
  { name: 'portrait', flow: '9b669909-8c56-5fe5-9be6-6fdd0cde2067', duration: 49.666666, frames: 1490, width: 1080, height: 1920, objects: 50, audio: 'efdd1c79-180b-514b-b38a-023a1f0cf9b5', video: 'd55b9c81-f7fd-598e-bdb3-082c806e6fff' },
  { name: 'reporter', flow: 'b3bb7172-79e0-4094-9f8e-b329cd63a842', duration: 10.078333, frames: 251, width: 1920, height: 1080, objects: 6 },
].filter(f => !process.env.IBC_FIXTURE || f.name === process.env.IBC_FIXTURE);
assert.ok(fixtures.length);
mkdirSync(output, { recursive: true });

function instrument() {
  window.playbackEvidence = { samples: [], events: [], graphs: [] };
  const evidence = window.playbackEvidence;
  const createSource = AudioContext.prototype.createMediaElementSource;
  AudioContext.prototype.createMediaElementSource = function(media) {
    const source = createSource.call(this, media);
    const analyser = this.createAnalyser();
    analyser.fftSize = 2048;
    source.connect(analyser);
    evidence.graphs.push({ context: this, analyser });
    return source;
  };
  const attached = new WeakSet();
  setInterval(() => {
    const video = document.querySelector('video');
    if (!video) return;
    if (!attached.has(video)) {
      attached.add(video);
      for (const name of ['play', 'playing', 'pause', 'waiting', 'seeking', 'seeked', 'ended', 'error']) {
        video.addEventListener(name, () => evidence.events.push({ name, at: performance.now(), time: video.currentTime }));
      }
    }
    const audio = evidence.graphs.map(({ context, analyser }) => {
      const pcm = new Float32Array(analyser.fftSize);
      analyser.getFloatTimeDomainData(pcm);
      return { state: context.state, rms: Math.sqrt(pcm.reduce((sum, n) => sum + n * n, 0) / pcm.length) };
    });
    evidence.samples.push({ at: performance.now(), time: video.currentTime, paused: video.paused,
      readyState: video.readyState, status: [...document.querySelectorAll('[role=status]')].map(n => n.textContent),
      buffered: Array.from({ length: video.buffered.length }, (_, i) => [video.buffered.start(i), video.buffered.end(i)]), audio });
  }, 100);
}

(async () => {
  const results = [];
  for (const name of browsers) {
    const browser = await ({ chromium, firefox })[name].launch();
    try {
      for (const fixture of fixtures) for (let run = 0; run < repeats; run++) {
        const viewport = run % 2 ? { width: 390, height: 844 } : { width: 1440, height: 1000 };
        const id = `${name}-${fixture.name}-${profile}-${run + 1}`;
        const context = await browser.newContext({ viewport });
        context.setDefaultTimeout(15000);
        const page = await context.newPage();
        const requests = [];
        const tracked = new Map();
        const pageErrors = [];
        let summary;
        let failure;
        try {
          if (process.env.IBC_DIST) {
            await page.route(`${base}/assets/**`, route => route.fulfill({ path: join(process.env.IBC_DIST, 'assets', basename(new URL(route.request().url()).pathname)) }));
            await page.route(`${base}/playback?**`, async route => {
              const response = await route.fetch();
              await route.fulfill({ response, body: require('node:fs').readFileSync(join(process.env.IBC_DIST, 'index.html')) });
            });
          }
          if (profile === '8mbps') {
            assert.equal(name, 'chromium', 'CDP throttling requires Chromium');
            const cdp = await context.newCDPSession(page);
            await cdp.send('Network.enable');
            await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 120, downloadThroughput: 1000000, uploadThroughput: 1000000 });
          }
          if (profile.startsWith('delay-')) {
            const track = profile.includes('audio') ? fixture.audio : fixture.video;
            assert.ok(track, 'Delay cases require the portrait fixture');
            const response = await fetch(`https://api.tamoss.live/flows/${track}/segments?limit=300`);
            assert.equal(response.status, 200);
            const segments = await response.json();
            const object = segments[profile.includes('first') ? 0 : 5].object_id;
            let delayed = false;
            await page.route('https://*.backblazeb2.com/**', async route => {
              if (!delayed && new URL(route.request().url()).pathname.endsWith('/' + object)) {
                delayed = true;
                await new Promise(resolve => setTimeout(resolve, profile.includes('first') ? 8000 : 12000));
              }
              await route.continue().catch(() => undefined);
            });
          }
          page.on('pageerror', error => pageErrors.push(error.message.replace(/https?:\/\/\S+/g, '[URL]')));
          page.on('request', request => {
            const url = new URL(request.url());
            if (!url.hostname.endsWith('.backblazeb2.com')) return;
            const row = { path: url.pathname, start: Date.now() };
            tracked.set(request, row);
            requests.push(row);
          });
          page.on('response', response => {
            const row = tracked.get(response.request());
            if (row) row.status = response.status();
          });
          page.on('requestfinished', request => {
            const row = tracked.get(request);
            if (row) { row.finished = true; row.timing = request.timing(); }
          });
          page.on('requestfailed', request => {
            const row = tracked.get(request);
            if (row) row.failed = request.failure()?.errorText;
          });
          await page.addInitScript(instrument);
          const start = Date.now();
          await page.goto(`${base}/playback?flow=${fixture.flow}`, { waitUntil: 'domcontentloaded' });
          if (profile.includes('first')) await page.locator('omakase-play-button').first().click();
          else {
            await page.waitForFunction(() => [...document.querySelectorAll('[role=status]')].some(n => n.textContent === 'Ready'), null, { timeout: 40000 });
            await page.locator('omakase-play-button').first().click();
          }
          await page.waitForFunction(() => document.querySelector('video')?.currentTime > 0.2, null, { timeout: 40000 });
          const startupMs = Date.now() - start;
          if (profile.endsWith('-pause')) {
            await page.waitForFunction(() => document.querySelector('video')?.currentTime > 1 && [...document.querySelectorAll('[role=status]')].some(n => n.textContent === 'Buffering'), null, { timeout: 20000 });
            await page.locator('omakase-play-button').first().click();
            const pausedAt = await page.locator('video').evaluate(video => video.currentTime);
            await page.waitForTimeout(12000);
            assert.ok(await page.locator('video').evaluate((video, time) => video.paused && Math.abs(video.currentTime - time) < 0.05, pausedAt), 'User pause survives buffer recovery');
            await page.locator('omakase-play-button').first().click();
          }
          await page.waitForFunction(() => document.querySelector('video')?.ended, null, { timeout: (fixture.duration + 65) * 1000 });
          summary = await page.evaluate(() => {
            const video = document.querySelector('video');
            const quality = video.getVideoPlaybackQuality();
            const evidence = window.playbackEvidence;
            return { duration: video.duration, width: video.videoWidth, height: video.videoHeight,
              frames: quality.totalVideoFrames, dropped: quality.droppedVideoFrames, hiddenAudio: document.querySelectorAll('audio').length,
              samples: evidence.samples, events: evidence.events };
          });
          summary.startupMs = startupMs;
          summary.completedObjects = new Set(requests.filter(r => r.finished && r.status === 200).map(r => r.path)).size;
          summary.maxAudioRms = Math.max(0, ...summary.samples.flatMap(s => s.audio.map(a => a.rms)));
          summary.stallSamples = summary.samples.filter(s => s.time > 0.3 && s.time < fixture.duration - 0.3 && (s.paused || s.status.includes('Buffering'))).length;
          assert.equal(summary.hiddenAudio, 0);
          assert.equal(summary.width, fixture.width);
          assert.equal(summary.height, fixture.height);
          assert.ok(Math.abs(summary.duration - fixture.duration) < 0.1);
          assert.ok(summary.frames >= fixture.frames - 3);
          assert.equal(summary.completedObjects, fixture.objects);
          assert.ok(summary.maxAudioRms > 0.001, 'Decoded audio must be non-silent through Web Audio');
          const roundingSeeks = summary.events.filter(e => e.name === 'seeking' && e.time > 0.3);
          const pauses = summary.events.filter(e => e.name === 'pause' && e.time < fixture.duration - 0.3);
          // Omakase rounds a pause to a video frame, which can re-decode a GOP.
          // Its frame callback may retry the same target once, but cannot advance a loop.
          assert.ok(roundingSeeks.length <= pauses.length * 2, 'At most two native frame-alignment seeks per pause');
          assert.ok(roundingSeeks.every(seek => pauses.some(pause => seek.at >= pause.at && seek.at - pause.at < 250 && Math.abs(seek.time - pause.time) <= fixture.duration / fixture.frames + 0.005)), 'Only native sub-frame pause rounding is permitted');
          for (const pause of pauses) {
            const seeks = roundingSeeks.filter(seek => seek.at >= pause.at && seek.at - pause.at < 250);
            assert.ok(seeks.length <= 2, 'Native frame alignment must be bounded');
            assert.ok(seeks.length < 2 || Math.abs(seeks[0].time - seeks[1].time) < 0.001, 'A native retry must retain the same frame target');
          }
          if (!pauses.length) assert.ok(Math.abs(summary.frames - fixture.frames) <= 3);
          assert.ok(summary.dropped / summary.frames < 0.01, 'Dropped frames below 1%');
          assert.deepEqual(pageErrors, []);
          if (process.env.IBC_REQUIRE_NO_STALLS === '1') assert.equal(summary.stallSamples, 0);
          const paused = summary.samples.filter(s => s.paused && s.time > 0.3 && s.time < fixture.duration - 0.3);
          assert.ok(paused.every(s => !s.status.includes('Playing')), 'Paused buffering cannot report Playing');
          await page.screenshot({ path: `${output}/${id}.png`, fullPage: true });
          // Real keyboard controls, buffered seek, user pause, then route cleanup.
          const button = page.locator('omakase-play-button').first();
          await button.focus();
          await page.keyboard.press('Space');
          await page.waitForFunction(() => document.querySelector('video')?.currentTime > 0.3 && !document.querySelector('video').paused);
          await button.click();
          await page.waitForFunction(() => document.querySelector('video')?.paused);
          await page.locator('video').evaluate(video => { video.currentTime = Math.min(4, video.duration / 2); });
          await page.waitForTimeout(500);
          assert.equal(await page.locator('video').evaluate(video => video.paused), true, 'Seek while paused must not resume');
          if (viewport.width < 600) await page.getByRole('button', { name: 'Open navigation' }).click();
          await page.getByRole('link', { name: 'Flows', exact: true }).first().click();
          await page.waitForFunction(() => document.querySelectorAll('video,audio').length === 0);
        } catch (error) {
          failure = error.message.split('\n')[0].replace(/https?:\/\/\S+/g, '[URL]');
        } finally {
          const evidence = summary || await page.evaluate(() => window.playbackEvidence && ({ samples: window.playbackEvidence.samples, events: window.playbackEvidence.events })).catch(() => null);
          writeFileSync(`${output}/${id}.json`, JSON.stringify({ id, fixture, viewport, failure, evidence, requests, pageErrors }, null, 2), { mode: 0o600 });
          const { samples, events, ...metrics } = summary || {};
          const result = { id, ok: !failure, failure, ...metrics };
          results.push(result);
          console.log(JSON.stringify(result));
          await context.close();
        }
      }
    } finally { await browser.close(); }
  }
  writeFileSync(`${output}/summary-${profile}.json`, JSON.stringify(results, null, 2), { mode: 0o600 });
  if (results.some(r => !r.ok)) process.exitCode = 1;
})().catch(error => { console.error(error.name + ': ' + error.message.split('\n')[0]); process.exitCode = 1; });
