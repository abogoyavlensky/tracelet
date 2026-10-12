// Design-prototype data shaped exactly like the API's responses: quickmeet,
// a video-meeting app with an embedded SFU, web-push rings, and SQLite.
// Deleted with the prototypes once the real screens are built.
import type { Log, LogsPage, Services, Session, Span, SpansPage, Trace } from '../api'

const base = Date.parse('2026-10-12T14:20:00Z')

function at(ms: number): string {
  return new Date(base + ms).toISOString()
}

function hex(seed: number, length: number): string {
  let out = ''
  let x = seed * 2654435761
  while (out.length < length) {
    x = (x * 1103515245 + 12345) >>> 0
    out += x.toString(16).padStart(8, '0')
  }
  return out.slice(0, length)
}

export const session: Session = {
  scope: 'admin',
  token_name: 'laptop',
  projects: [
    { slug: 'quickmeet', name: 'Quickmeet' },
    { slug: 'tracelet', name: 'Tracelet' },
  ],
}

export const services: Services = {
  items: [
    { service: 'quickmeet-api', logs: 18_412, spans: 9_870 },
    { service: 'quickmeet-sfu', logs: 6_204, spans: 1_212 },
    { service: 'quickmeet-worker', logs: 1_033, spans: 402 },
  ],
  range: { from: at(-3_600_000), to: at(0) },
}

const resource = (service: string) => ({
  'service.name': service,
  'service.version': '0.9.3',
  'deployment.environment': 'production',
  'host.name': 'vps-fra-1',
  'process.runtime.name': 'go',
  'process.runtime.version': 'go1.27.0',
})

export const ringTraceId = hex(7, 32)

const stack = `webpush: send to https://updates.push.services.mozilla.com/wpush/v2/gAAAAABm: 410 Gone
  subscription expired or unsubscribed
github.com/quickmeet/push.(*Sender).Send
	/src/push/sender.go:88
github.com/quickmeet/rooms.(*Service).Ring
	/src/rooms/ring.go:141
github.com/quickmeet/httpapi.(*Handler).ringMember
	/src/httpapi/rooms.go:203`

type LogSeed = [
  ms: number,
  service: string,
  severity: number,
  body: string,
  attrs?: Record<string, unknown>,
  trace?: string,
]

const logSeeds: LogSeed[] = [
  [
    -2_000,
    'quickmeet-api',
    9,
    'POST /api/rooms/r_8fk2/ring 200 in 412ms',
    { 'http.route': '/api/rooms/{id}/ring', 'http.response.status_code': 200, 'user.id': 'u_41' },
    ringTraceId,
  ],
  [
    -2_050,
    'quickmeet-api',
    17,
    'push subscription gone, removing it',
    {
      'user.id': 'u_77',
      'push.endpoint_host': 'updates.push.services.mozilla.com',
      'exception.type': 'webpush.GoneError',
      'exception.stacktrace': stack,
    },
    ringTraceId,
  ],
  [
    -2_300,
    'quickmeet-api',
    9,
    'ring sent to alice (fcm)',
    { 'user.id': 'u_12', 'push.provider': 'fcm' },
    ringTraceId,
  ],
  [
    -4_100,
    'quickmeet-sfu',
    9,
    'participant joined room r_8fk2 (2 in room)',
    { 'room.id': 'r_8fk2', 'participant.identity': 'u_41' },
  ],
  [
    -5_900,
    'quickmeet-sfu',
    5,
    'ICE candidate pair selected: udp 10.0.0.4:52114 -> 85.214.9.12:3478 (relay)',
    { 'room.id': 'r_8fk2' },
  ],
  [
    -7_400,
    'quickmeet-api',
    9,
    'GET /api/rooms/r_8fk2 200 in 6ms',
    { 'http.route': '/api/rooms/{id}', 'http.response.status_code': 200 },
  ],
  [
    -9_800,
    'quickmeet-api',
    13,
    'slow query: SELECT * FROM rings WHERE room_id = ? AND state = ? took 248ms',
    { 'db.system': 'sqlite', 'db.duration_ms': 248 },
  ],
  [
    -12_000,
    'quickmeet-worker',
    9,
    'expired 3 unanswered rings older than 45s',
    { 'job.name': 'ring.expire', 'rings.expired': 3 },
  ],
  [
    -15_500,
    'quickmeet-sfu',
    13,
    'TURN allocation refresh took 1.2s; client may see a freeze',
    { 'turn.server': 'turn.quickmeet.app:3478' },
  ],
  [
    -18_000,
    'quickmeet-api',
    9,
    'POST /api/rooms 201 in 18ms',
    { 'http.route': '/api/rooms', 'http.response.status_code': 201, 'room.id': 'r_8fk2' },
  ],
  [-21_200, 'quickmeet-api', 1, 'session cookie refreshed for u_41', { 'user.id': 'u_41' }],
  [
    -24_000,
    'quickmeet-sfu',
    9,
    'participant left room r_1qz9 (0 in room); closing room after 30s',
    { 'room.id': 'r_1qz9' },
  ],
  [
    -27_300,
    'quickmeet-api',
    17,
    'POST /api/signin 401: wrong password for anna@example.com',
    { 'http.route': '/api/signin', 'http.response.status_code': 401 },
  ],
  [
    -30_000,
    'quickmeet-worker',
    5,
    'vacuum skipped: 2% free pages under the 10% threshold',
    { 'job.name': 'sqlite.vacuum' },
  ],
  [
    -33_400,
    'quickmeet-api',
    9,
    'GET /api/history?page=2 200 in 11ms',
    { 'http.route': '/api/history', 'http.response.status_code': 200 },
  ],
  [
    -36_700,
    'quickmeet-sfu',
    21,
    'panic recovered in track forwarder: runtime error: index out of range [3] with length 3',
    { 'room.id': 'r_x81m', 'exception.type': 'runtime.Error' },
  ],
  [
    -40_100,
    'quickmeet-api',
    9,
    'blur model served from cache (immutable, 1y)',
    { 'http.route': '/static/*' },
  ],
  [
    -44_000,
    'quickmeet-api',
    13,
    'rate limit hit for 203.0.113.9 on /api/rooms (11 requests in 10s)',
    { 'client.address': '203.0.113.9' },
  ],
  [
    -47_500,
    'quickmeet-sfu',
    9,
    'room r_x81m: simulcast layer switched to 360p for u_19 (bandwidth 410 kbps)',
    { 'room.id': 'r_x81m' },
  ],
  [
    -51_000,
    'quickmeet-api',
    9,
    'GET /api/rooms/r_x81m 200 in 4ms',
    { 'http.response.status_code': 200 },
  ],
  [
    -55_200,
    'quickmeet-worker',
    9,
    'sent 2 missed-call notifications',
    { 'job.name': 'ring.missed' },
  ],
  [
    -58_900,
    'quickmeet-api',
    5,
    'livekit token issued for u_19 in room r_x81m, ttl 6h',
    { 'user.id': 'u_19', 'room.id': 'r_x81m' },
  ],
  [
    -63_000,
    'quickmeet-sfu',
    17,
    'DTLS handshake timeout for participant u_23 after 10s',
    { 'room.id': 'r_x81m', 'participant.identity': 'u_23' },
  ],
  [
    -67_400,
    'quickmeet-api',
    9,
    'POST /api/rooms/r_x81m/join 200 in 9ms',
    { 'http.response.status_code': 200 },
  ],
  [
    -71_000,
    'quickmeet-api',
    9,
    'user u_88 switched interface language to ru',
    { 'user.id': 'u_88', locale: 'ru' },
  ],
  [-75_300, 'quickmeet-sfu', 5, 'stats: 3 rooms, 7 participants, 14 tracks, egress 2.1 Mbps', {}],
  [
    -79_000,
    'quickmeet-api',
    13,
    'deprecated header X-Client-Version seen from app build 0.8.1',
    { 'app.version': '0.8.1' },
  ],
  [
    -83_600,
    'quickmeet-worker',
    9,
    'backup written: quickmeet.db (4.2 MB) to /data/backups/2026-10-12T14.db',
    { 'job.name': 'sqlite.backup' },
  ],
  [-88_000, 'quickmeet-api', 9, 'GET /healthz 200 in 0.3ms', {}],
  [
    -92_500,
    'quickmeet-sfu',
    9,
    'participant joined room r_x81m (3 in room)',
    { 'room.id': 'r_x81m' },
  ],
  [
    -97_000,
    'quickmeet-api',
    17,
    'webpush: VAPID signature rejected by fcm.googleapis.com: 403 Forbidden',
    { 'push.provider': 'fcm' },
  ],
  [
    -101_400,
    'quickmeet-api',
    9,
    'POST /api/rooms 201 in 15ms',
    { 'http.response.status_code': 201 },
  ],
  [
    -106_000,
    'quickmeet-worker',
    1,
    'tick ring.expire: nothing to do',
    { 'job.name': 'ring.expire' },
  ],
  [-110_500, 'quickmeet-api', 9, 'GET /api/rooms 200 in 7ms', { 'http.response.status_code': 200 }],
  [
    -115_000,
    'quickmeet-sfu',
    13,
    'packet loss 6.4% on uplink of u_19 over the last 10s',
    { 'room.id': 'r_x81m', 'rtp.loss_pct': 6.4 },
  ],
  [
    -119_800,
    'quickmeet-api',
    9,
    'signed in anna@example.com from Firefox 131 on Android',
    { 'user.id': 'u_41' },
  ],
  [
    -124_000,
    'quickmeet-api',
    5,
    'cache miss for room list of u_41; loaded 12 rooms',
    { 'user.id': 'u_41' },
  ],
  [-129_300, 'quickmeet-sfu', 9, 'room r_x81m created by u_19', { 'room.id': 'r_x81m' }],
  [
    -134_000,
    'quickmeet-worker',
    9,
    'started: 4 jobs scheduled (ring.expire, ring.missed, sqlite.vacuum, sqlite.backup)',
    {},
  ],
  [-139_000, 'quickmeet-api', 9, 'listening on :8080 (version 0.9.3, commit 3fa91c2)', {}],
]

function toLog([ms, service, severity, body, attrs, trace]: LogSeed, i: number): Log {
  return {
    ts: at(ms),
    observed_ts: at(ms + 3),
    service,
    environment: 'production',
    version: '0.9.3',
    severity_number: severity,
    severity_text:
      ['', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'][Math.ceil(severity / 4)] ?? '',
    body,
    trace_id: trace ?? (i % 5 === 3 ? hex(100 + i, 32) : undefined),
    span_id: trace ? hex(200 + i, 16) : undefined,
    scope: 'log/slog',
    resource: resource(service),
    attributes: attrs,
  }
}

export const logsPage: LogsPage = {
  items: logSeeds.map(toLog),
  next_cursor: 'eyJ0cyI6IjIwMjYtMTAtMTJUMTQ6MTc6NDFaIn0',
  range: { from: at(-3_600_000), to: at(0) },
  limit: 40,
  truncated: true,
}

type SpanSeed = {
  id: string
  parent?: string
  service: string
  name: string
  kind: number
  start: number
  end: number
  status?: number
  message?: string
  attrs?: Record<string, unknown>
  events?: Span['events']
}

const t0 = -2_412

function toSpan(s: SpanSeed): Span {
  return {
    ts: at(t0 + s.start),
    end_ts: at(t0 + s.end),
    duration_ns: (s.end - s.start) * 1e6,
    service: s.service,
    environment: 'production',
    version: '0.9.3',
    trace_id: ringTraceId,
    span_id: s.id,
    parent_span_id: s.parent,
    name: s.name,
    kind: s.kind,
    status_code: s.status ?? 0,
    status_message: s.message,
    scope: 'go.opentelemetry.io/otel',
    resource: resource(s.service),
    attributes: s.attrs,
    events: s.events,
  }
}

const id = (n: number) => hex(300 + n, 16)

const traceSeeds: SpanSeed[] = [
  {
    id: id(0),
    service: 'quickmeet-api',
    name: 'POST /api/rooms/{id}/ring',
    kind: 2,
    start: 0,
    end: 412,
    attrs: {
      'http.request.method': 'POST',
      'http.route': '/api/rooms/{id}/ring',
      'url.path': '/api/rooms/r_8fk2/ring',
      'http.response.status_code': 200,
      'user_agent.original': 'Mozilla/5.0 (Android 14; Mobile; rv:131.0) Gecko/131.0 Firefox/131.0',
      'client.address': '198.51.100.23',
    },
  },
  {
    id: id(1),
    parent: id(0),
    service: 'quickmeet-api',
    name: 'auth.session',
    kind: 1,
    start: 1,
    end: 6,
    attrs: { 'user.id': 'u_41' },
  },
  {
    id: id(2),
    parent: id(1),
    service: 'quickmeet-api',
    name: 'sqlite.query',
    kind: 3,
    start: 2,
    end: 5,
    attrs: {
      'db.system': 'sqlite',
      'db.statement': 'SELECT user_id, expires_at FROM sessions WHERE token_hash = ?',
    },
  },
  {
    id: id(3),
    parent: id(0),
    service: 'quickmeet-api',
    name: 'sqlite.query',
    kind: 3,
    start: 7,
    end: 11,
    attrs: {
      'db.system': 'sqlite',
      'db.statement': 'SELECT id, owner_id, name FROM rooms WHERE id = ?',
    },
  },
  {
    id: id(4),
    parent: id(0),
    service: 'quickmeet-api',
    name: 'rooms.ring',
    kind: 1,
    start: 12,
    end: 404,
    attrs: { 'room.id': 'r_8fk2', 'ring.recipients': 2 },
  },
  {
    id: id(5),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'sqlite.query',
    kind: 3,
    start: 13,
    end: 16,
    attrs: {
      'db.system': 'sqlite',
      'db.statement': 'SELECT user_id FROM members WHERE room_id = ? AND user_id != ?',
    },
  },
  {
    id: id(6),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'sqlite.query',
    kind: 3,
    start: 17,
    end: 22,
    attrs: {
      'db.system': 'sqlite',
      'db.statement': 'INSERT INTO rings (room_id, from_id, to_id, state) VALUES (?, ?, ?, ?)',
    },
  },
  {
    id: id(7),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'webpush.send',
    kind: 3,
    start: 23,
    end: 181,
    attrs: { 'user.id': 'u_12', 'push.provider': 'fcm' },
  },
  {
    id: id(8),
    parent: id(7),
    service: 'quickmeet-api',
    name: 'POST fcm.googleapis.com',
    kind: 3,
    start: 25,
    end: 178,
    attrs: {
      'http.request.method': 'POST',
      'server.address': 'fcm.googleapis.com',
      'http.response.status_code': 201,
    },
  },
  {
    id: id(9),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'webpush.send',
    kind: 3,
    start: 24,
    end: 391,
    status: 2,
    message: '410 Gone: subscription expired or unsubscribed',
    attrs: { 'user.id': 'u_77', 'push.provider': 'mozilla' },
    events: [
      {
        ts: at(t0 + 390),
        name: 'exception',
        attributes: {
          'exception.type': 'webpush.GoneError',
          'exception.message': '410 Gone: subscription expired or unsubscribed',
          'exception.stacktrace': stack,
        },
      },
    ],
  },
  {
    id: id(10),
    parent: id(9),
    service: 'quickmeet-api',
    name: 'POST updates.push.services.mozilla.com',
    kind: 3,
    start: 27,
    end: 389,
    status: 2,
    message: 'HTTP 410',
    attrs: {
      'http.request.method': 'POST',
      'server.address': 'updates.push.services.mozilla.com',
      'http.response.status_code': 410,
    },
  },
  {
    id: id(11),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'sqlite.query',
    kind: 3,
    start: 392,
    end: 397,
    attrs: {
      'db.system': 'sqlite',
      'db.statement': 'DELETE FROM push_subscriptions WHERE endpoint = ?',
    },
  },
  {
    id: id(12),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'livekit.token',
    kind: 1,
    start: 398,
    end: 402,
    attrs: { 'room.id': 'r_8fk2', 'livekit.ttl': '6h' },
  },
  {
    id: id(13),
    parent: id(4),
    service: 'quickmeet-api',
    name: 'publish ring.created',
    kind: 4,
    start: 402,
    end: 403,
    attrs: { 'messaging.destination.name': 'ring.created' },
  },
  {
    id: id(14),
    parent: id(13),
    service: 'quickmeet-worker',
    name: 'ring.created',
    kind: 5,
    start: 405,
    end: 431,
    attrs: { 'messaging.destination.name': 'ring.created', 'job.attempt': 1 },
  },
  {
    id: id(15),
    parent: id(14),
    service: 'quickmeet-worker',
    name: 'sqlite.query',
    kind: 3,
    start: 406,
    end: 409,
    attrs: {
      'db.system': 'sqlite',
      'db.statement': 'UPDATE rings SET expires_at = ? WHERE id = ?',
    },
  },
  {
    id: id(16),
    parent: id(14),
    service: 'quickmeet-worker',
    name: 'schedule ring.expire',
    kind: 1,
    start: 410,
    end: 411,
  },
  {
    id: id(17),
    parent: id(4),
    service: 'quickmeet-sfu',
    name: 'sfu.room.notify',
    kind: 2,
    start: 403,
    end: 409,
    attrs: { 'room.id': 'r_8fk2' },
  },
  {
    id: id(18),
    parent: id(17),
    service: 'quickmeet-sfu',
    name: 'signal.broadcast',
    kind: 1,
    start: 404,
    end: 408,
    attrs: { 'room.id': 'r_8fk2', participants: 1 },
  },
  {
    id: id(19),
    parent: id(0),
    service: 'quickmeet-api',
    name: 'json.encode',
    kind: 1,
    start: 405,
    end: 406,
  },
  {
    id: id(20),
    parent: id(0),
    service: 'quickmeet-api',
    name: 'access.log',
    kind: 1,
    start: 407,
    end: 408,
  },
  {
    id: id(21),
    parent: hex(999, 16),
    service: 'quickmeet-sfu',
    name: 'sfu.participant.update',
    kind: 1,
    start: 409,
    end: 411,
    attrs: { 'room.id': 'r_8fk2' },
  },
]

const traceSpans = traceSeeds.map(toSpan)

export const trace: Trace = {
  trace_id: ringTraceId,
  start: at(t0),
  end: at(t0 + 431),
  duration_ns: 431 * 1e6,
  services: ['quickmeet-api', 'quickmeet-sfu', 'quickmeet-worker'],
  spans: traceSpans,
  range: { from: at(-86_400_000), to: at(86_400_000) },
  truncated: false,
}

export const errorSpanId = id(9)

export const traceLogs: Log[] = [
  [3, id(1), 1, 'session ok for u_41'],
  [12, id(4), 9, 'ringing 2 members of room r_8fk2'],
  [181, id(7), 9, 'ring sent to alice (fcm)'],
  [390, id(9), 17, 'push subscription gone, removing it'],
  [397, id(11), 5, 'deleted 1 push subscription for u_77'],
  [402, id(12), 5, 'livekit token issued for u_41 in room r_8fk2, ttl 6h'],
  [412, id(0), 9, 'POST /api/rooms/r_8fk2/ring 200 in 412ms'],
  [420, id(14), 9, 'ring r_9921 expires at 14:20:43'],
].map(([ms, spanId, severity, body], i) => ({
  ...toLog(
    [
      t0 + (ms as number),
      i === 7 ? 'quickmeet-worker' : 'quickmeet-api',
      severity as number,
      body as string,
      { 'user.id': 'u_41' },
      ringTraceId,
    ],
    i,
  ),
  span_id: spanId as string,
}))

type RootSeed = [ms: number, service: string, name: string, durMs: number, status?: number]

const rootSeeds: RootSeed[] = [
  [-2_412, 'quickmeet-api', 'POST /api/rooms/{id}/ring', 412],
  [-7_406, 'quickmeet-api', 'GET /api/rooms/{id}', 6],
  [-9_900, 'quickmeet-api', 'GET /api/rings', 251],
  [-12_040, 'quickmeet-worker', 'ring.expire', 38],
  [-18_018, 'quickmeet-api', 'POST /api/rooms', 18],
  [-27_310, 'quickmeet-api', 'POST /api/signin', 9, 2],
  [-33_411, 'quickmeet-api', 'GET /api/history', 11],
  [-36_710, 'quickmeet-sfu', 'sfu.track.forward', 1_840, 2],
  [-51_004, 'quickmeet-api', 'GET /api/rooms/{id}', 4],
  [-55_230, 'quickmeet-worker', 'ring.missed', 120],
  [-63_000, 'quickmeet-sfu', 'sfu.participant.join', 10_020, 2],
  [-67_409, 'quickmeet-api', 'POST /api/rooms/{id}/join', 9],
  [-88_000, 'quickmeet-api', 'GET /healthz', 0.3],
  [-101_415, 'quickmeet-api', 'POST /api/rooms', 15],
  [-110_507, 'quickmeet-api', 'GET /api/rooms', 7],
]

export const spansPage: SpansPage = {
  items: rootSeeds.map(([ms, service, name, durMs, status], i) => ({
    ts: at(ms),
    end_ts: at(ms + durMs),
    duration_ns: Math.round(durMs * 1e6),
    service,
    environment: 'production',
    version: '0.9.3',
    trace_id: i === 0 ? ringTraceId : hex(500 + i, 32),
    span_id: hex(600 + i, 16),
    name,
    kind: service === 'quickmeet-worker' ? 5 : 2,
    status_code: status ?? 0,
    status_message: status === 2 ? 'error' : undefined,
    resource: resource(service),
  })),
  next_cursor: 'eyJ0cyI6IjIwMjYtMTAtMTJUMTQ6MTg6MDBaIn0',
  range: { from: at(-3_600_000), to: at(0) },
  limit: 15,
  truncated: true,
}
