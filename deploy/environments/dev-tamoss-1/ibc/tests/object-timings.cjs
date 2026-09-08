const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const { mkdirSync, writeFileSync } = require('node:fs');
const output = process.env.IBC_EVIDENCE_DIR || '.local/ibc/object-timings';
const kubeconfig = process.env.KUBECONFIG;
mkdirSync(output, { recursive: true });

(async () => {
  const response = await fetch('https://api.tamoss.live/flows/efdd1c79-180b-514b-b38a-023a1f0cf9b5/segments?limit=8&presigned=true');
  assert.equal(response.status, 200);
  const objects = (await response.json()).slice(0, 8).map(s => ({ id: s.object_id, url: s.get_urls[0].url }));
  const local = [];
  for (let run = 0; run < 3; run++) for (const object of objects) {
    const start = performance.now();
    const r = await fetch(object.url);
    const headers = performance.now();
    const bytes = (await r.arrayBuffer()).byteLength;
    local.push({ run, id: object.id, status: r.status, bytes, firstByteMs: headers - start, totalMs: performance.now() - start });
  }
  const results = { local };
  if (kubeconfig) {
    const script = `import json,sys,time,urllib.request
objects=json.load(sys.stdin)
rows=[]
for run in range(3):
 for obj in objects:
  start=time.monotonic()
  with urllib.request.urlopen(obj['url'],timeout=30) as response:
   headers=time.monotonic()
   body=response.read()
   rows.append(dict(run=run,id=obj['id'],status=response.status,bytes=len(body),firstByteMs=(headers-start)*1000,totalMs=(time.monotonic()-start)*1000))
print(json.dumps(rows))`;
    results.gke = JSON.parse(execFileSync('kubectl', ['--kubeconfig', kubeconfig, '-n', 'ibc-tamoss-public', 'exec', '-i', 'deploy/ibc-tamoss-public-api', '--', 'python', '-c', script], { input: JSON.stringify(objects), timeout: 180000, maxBuffer: 1024 * 1024 }));
  }
  writeFileSync(`${output}/timings.json`, JSON.stringify(results, null, 2), { mode: 0o600 });
  for (const [location, rows] of Object.entries(results)) {
    const sorted = rows.map(r => r.firstByteMs).sort((a, b) => a - b);
    console.log(JSON.stringify({ location, requests: rows.length, failures: rows.filter(r => r.status !== 200).length, firstByteP50Ms: sorted[Math.ceil(sorted.length * 0.5) - 1], firstByteP95Ms: sorted[Math.ceil(sorted.length * 0.95) - 1], slowerThanTwoSeconds: sorted.filter(n => n > 2000).length }));
  }
})().catch(error => { console.error(error.name + ': timing collection failed'); process.exitCode = 1; });
