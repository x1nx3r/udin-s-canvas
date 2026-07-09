import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE = 'https://canvas.x1nx3r.dev';
const SESSION = __ENV.SESSION_COOKIE;

const headers = SESSION ? { Cookie: 'session=' + SESSION } : {};
const errorRate = new Rate('errors');
const saveTrend = new Trend('save_duration');
const loadTrend = new Trend('load_duration');

export const options = {
  stages: [
    { duration: '15s', target: 100 },
    { duration: '30s', target: 300 },
    { duration: '30s', target: 500 },
    { duration: '15s', target: 0 },
  ],
  thresholds: { errors: ['rate<0.05'], http_req_duration: ['p(95)<3000'] },
};

const mockElements = JSON.stringify({
  elements: [{ type: 'rectangle', x: 10, y: 10, width: 100, height: 50, strokeColor: '#000', backgroundColor: 'transparent', id: 'a', version: 1 }],
  appState: { viewBackgroundColor: '#fff', collaborators: [] },
});

export default function () {
  group('public pages', function () {
    let r = http.get(BASE + '/');
    check(r, { 'landing 200': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);

    r = http.get(BASE + '/globals.css');
    check(r, { 'css 200': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);
  });

  if (!SESSION) {
    sleep(1);
    return;
  }

  group('authenticated', function () {
    let r = http.get(BASE + '/drawings', { headers });
    check(r, { 'dashboard 200': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);
  });

  group('create + save + load + delete', function () {
    // Create a drawing
    let r = http.post(BASE + '/draw/new', {}, { headers, redirects: 0 });
    check(r, { 'create redirects': (res) => res.status === 302 });
    errorRate.add(r.status !== 302);

    let loc = r.headers['Location'] || '';
    let id = loc.replace('/draw/', '');
    if (!id) {
      errorRate.add(1);
      sleep(1);
      return;
    }

    // Save drawing data
    r = http.post(BASE + '/api/draw/' + id + '/save', mockElements, {
      headers: Object.assign(headers, { 'Content-Type': 'application/json' }),
    });
    check(r, { 'save success': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);
    saveTrend.add(r.timings.duration);

    // Load drawing data
    r = http.get(BASE + '/api/draw/' + id + '/data', { headers });
    check(r, { 'load success': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);
    loadTrend.add(r.timings.duration);

    // Rename
    r = http.put(BASE + '/api/draw/' + id + '/rename', JSON.stringify({ title: 'load test ' + Date.now() }), {
      headers: Object.assign(headers, { 'Content-Type': 'application/json' }),
    });
    check(r, { 'rename success': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);

    // Share
    r = http.post(BASE + '/api/draw/' + id + '/share', {}, { headers });
    check(r, { 'share success': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);

    // Delete
    r = http.del(BASE + '/api/draw/' + id, {}, { headers });
    check(r, { 'delete success': (res) => res.status === 200 });
    errorRate.add(r.status !== 200);
  });

  sleep(1);
}
