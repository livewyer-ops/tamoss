const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const { mkdtempSync, readdirSync, readFileSync, mkdirSync, writeFileSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { join } = require('node:path');
const { chromium, firefox } = require('playwright');

const base = process.env.IBC_UI_URL || 'http://127.0.0.1:5180';
const output = process.env.IBC_EVIDENCE_DIR || '.local/ibc/sync';
const dir = mkdtempSync(join(tmpdir(), 'ibc-sync-'));
const fragmented = process.env.IBC_CONTAINER === 'fmp4';
const muxed = process.env.IBC_CONTAINER === 'muxed-ts';
mkdirSync(output, { recursive: true });
const root = '00000000-0000-4000-8000-000000000001';
const ids = { video: '00000000-0000-4000-8000-000000000002', audio: '00000000-0000-4000-8000-000000000003' };
execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i', 'color=black:size=320x180:rate=25:duration=12', '-vf', "drawbox=x=0:y=0:w=iw:h=ih:color=white:t=fill:enable='lt(mod(t,1),0.08)'", '-c:v', 'libx264', '-preset', 'ultrafast', '-tune', 'zerolatency', '-g', '50', '-sc_threshold', '0', '-f', 'segment', '-segment_time', '2', '-segment_format', 'mpegts', join(dir, 'video-%02d.ts')]);
execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i', "aevalsrc='if(lt(mod(t,1),0.08),0.5*sin(2*PI*1000*t),0)':s=48000:d=12", '-ac', '2', '-c:a', 'aac', '-b:a', '128k', '-f', 'segment', '-segment_time', '2', '-segment_format', 'mpegts', join(dir, 'audio-%02d.ts')]);
if (muxed) {
  const input = kind => 'concat:' + readdirSync(dir).filter(f => f.startsWith(kind)).sort().map(f => join(dir, f)).join('|');
  execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-i', input('video'), '-i', input('audio'), '-map', '0:v', '-map', '1:a', '-c', 'copy', '-f', 'segment', '-segment_time', '2', '-segment_format', 'mpegts', join(dir, 'muxed-%02d.ts')]);
}
if (fragmented) for (const kind of Object.keys(ids)) {
  const input = readdirSync(dir).filter(f => f.startsWith(kind)).sort().map(f => join(dir, f)).join('|');
  execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-i', `concat:${input}`, '-c', 'copy', ...(kind === 'audio' ? ['-bsf:a', 'aac_adtstoasc'] : []), '-f', 'hls', '-hls_time', '2', '-hls_playlist_type', 'vod', '-hls_segment_type', 'fmp4', '-hls_fmp4_init_filename', `${kind}-init.mp4`, '-hls_segment_filename', join(dir, `${kind}-%02d.m4s`), join(dir, `${kind}.m3u8`)]);
}
const timestamp = (ticks, timebase) => {
  const [numerator, denominator] = timebase.split('/').map(BigInt);
  const nanos = (fragmented ? 100000000000n : 98600000000n) + BigInt(ticks) * numerator * 1000000000n / denominator;
  return `${nanos / 1000000000n}:${nanos % 1000000000n}`;
};
const tracks = Object.fromEntries(Object.entries(ids).map(([kind, id]) => {
  const segments = readdirSync(dir).filter(f => f.startsWith(muxed && kind === 'video' ? 'muxed' : kind) && f.endsWith(fragmented ? '.m4s' : '.ts')).sort().map((file, index) => {
    const probe = fragmented ? `concat:${join(dir, `${kind}-init.mp4`)}|${join(dir, file)}` : join(dir, file);
    const metadata = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-select_streams', kind === 'video' ? 'v:0' : 'a:0', '-show_packets', '-show_streams', '-show_entries', 'packet=pts,duration:stream=time_base', '-of', 'json', probe]));
    const packets = metadata.packets;
    const first = packets[0].pts;
    const last = packets.at(-1);
    const end = last.pts + (last.duration ?? (kind === 'audio' ? 1024 : packets[1].pts - packets[0].pts));
    const timebase = metadata.streams[0].time_base;
    return { object_id: `${id.slice(0, -4)}${kind === 'video' ? '10' : '20'}${String(index + 10).padStart(2, '0')}`, timerange: `[${timestamp(first, timebase)}_${timestamp(end, timebase)})`, ts_offset: fragmented ? '100:0' : '98:600000000', get_urls: [{ url: `https://media.fixture.test/${file}`, presigned: true }], ...(fragmented ? { init_object: { object_id: `${id.slice(0, -2)}99`, get_urls: [{ url: `https://media.fixture.test/${kind}-init.mp4`, presigned: true }] } } : {}) };
  });
  const flow = { id, source_id: root, label: `Sync ${kind}`, format: `urn:x-nmos:format:${kind}`, codec: kind === 'video' ? 'video/h264' : 'audio/aac', container: `${kind}/${fragmented ? 'mp4' : 'mp2t'}`, timerange: '[100:0_112:0)', essence_parameters: { ...(kind === 'video' ? { frame_rate: { numerator: 25, denominator: 1 }, frame_width: 320, frame_height: 180 } : { sample_rate: 48000, channels: 2 }), ...(fragmented ? { init_segments: true } : {}) } };
  return [id, { flow, segments }];
}));

(async () => {
  for (const [name, launcher] of Object.entries({ chromium, firefox })) {
    const browser = await launcher.launch();
    try {
      const context = await browser.newContext();
      context.setDefaultTimeout(15000);
      const page = await context.newPage();
      await page.route('**/api/flows/00000000-0000-4000-8000-**', async route => {
        const path = new URL(route.request().url()).pathname.split('/');
        const id = path[3];
        const collection = Object.entries(ids).map(([role, id]) => ({ role, id }));
        const data = id === root ? (muxed ? (path[4] === 'segments' ? tracks[ids.video].segments : { ...tracks[ids.video].flow, id: root }) : path[4] === 'flow_collection' ? collection : { id: root, source_id: root, label: 'A/V synchronisation fixture', format: 'urn:x-nmos:format:multi', timerange: '[100:0_112:0)', flow_collection: collection }) : path[4] === 'segments' ? tracks[id].segments : tracks[id].flow;
        await route.fulfill({ json: data });
      });
      await page.route('https://media.fixture.test/**', route => route.fulfill({ body: readFileSync(join(dir, new URL(route.request().url()).pathname.slice(1))), contentType: fragmented ? 'video/mp4' : 'video/mp2t', headers: { 'Access-Control-Allow-Origin': '*' } }));
      await page.addInitScript(() => {
        window.syncEvidence = { video: [], audio: [], stalls: [] };
        const analysers = [];
        const createSource = AudioContext.prototype.createMediaElementSource;
        AudioContext.prototype.createMediaElementSource = function(media) {
          const source = createSource.call(this, media);
          const analyser = this.createAnalyser();
          analyser.fftSize = 256;
          source.connect(analyser);
          analysers.push(analyser);
          return source;
        };
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 1;
        const ctx = canvas.getContext('2d', { willReadFrequently: true });
        let wasLight = false;
        let wasLoud = false;
        setInterval(() => {
          const video = document.querySelector('video');
          if (!video || video.paused || video.readyState < 2 || !analysers.length) return;
          ctx.drawImage(video, 0, 0, 1, 1);
          const light = ctx.getImageData(0, 0, 1, 1).data[0] > 180;
          const loud = analysers.some(analyser => {
            const pcm = new Float32Array(256);
            analyser.getFloatTimeDomainData(pcm);
            return Math.sqrt(pcm.reduce((sum, x) => sum + x * x, 0) / pcm.length) > 0.05;
          });
          const point = { at: performance.now(), time: video.currentTime };
          if (light && !wasLight) window.syncEvidence.video.push(point);
          if (loud && !wasLoud) window.syncEvidence.audio.push(point);
          wasLight = light;
          wasLoud = loud;
        }, 5);
      });
      await page.goto(`${base}/playback?flow=${root}`);
      await page.waitForFunction(() => [...document.querySelectorAll('[role=status]')].some(n => n.textContent === 'Ready')).catch(async error => {
        console.log((await page.locator('body').innerText()).slice(-1200));
        throw error;
      });
      await page.locator('omakase-play-button').first().click();
      await page.waitForFunction(() => document.querySelector('video')?.ended, null, { timeout: 30000 });
      const evidence = await page.evaluate(() => window.syncEvidence);
      evidence.driftMs = evidence.video.filter(v => v.time > 0.5 && v.time < 11.5).map(v => Math.min(...evidence.audio.map(a => Math.abs(a.at - v.at))));
      writeFileSync(join(output, `${name}.json`), JSON.stringify(evidence, null, 2), { mode: 0o600 });
      console.log(JSON.stringify({ browser: name, markers: evidence.driftMs.length, maxDriftMs: Math.max(...evidence.driftMs) }));
      assert.ok(evidence.driftMs.length >= 9, 'Flash and tone markers throughout the recording');
      assert.ok(evidence.driftMs.every(ms => ms <= 100), 'A/V alignment within 100 ms');
    } finally { await browser.close(); }
  }
})().catch(error => { console.error(error.name + ': ' + error.message.split('\n')[0]); process.exitCode = 1; });
