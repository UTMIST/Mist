import { createServer } from 'node:http'
import { mkdirSync } from 'node:fs'
import { dirname } from 'node:path'
import { timingSafeEqual } from 'node:crypto'
import Database from 'better-sqlite3'
import { betterAuth } from 'better-auth'
import { getMigrations } from 'better-auth/db/migration'
import { toNodeHandler } from 'better-auth/node'
import { admin } from 'better-auth/plugins/admin'
import { APIError, createAuthMiddleware } from 'better-auth/api'

const dbPath = process.env.MIST_AUTH_DATABASE ?? './data/auth.sqlite'
const origin = process.env.MIST_SITE_ORIGIN ?? 'http://127.0.0.1:3001'
const secret = process.env.BETTER_AUTH_SECRET
if (!secret || secret.length < 32) throw new Error('BETTER_AUTH_SECRET must contain at least 32 characters')
mkdirSync(dirname(dbPath), { recursive: true, mode: 0o700 })
const database = new Database(dbPath)
database.pragma('journal_mode = WAL')
database.pragma('busy_timeout = 5000')
const options = {
  appName: 'Mist', database, secret, baseURL: origin, basePath: '/auth',
  trustedOrigins: (process.env.MIST_ALLOWED_ORIGINS ?? origin).split(',').filter(Boolean),
  emailAndPassword: { enabled: true, disableSignUp: true, minPasswordLength: 12, maxPasswordLength: 128 },
  session: { expiresIn: 60 * 60 * 24 * 7, updateAge: 60 * 60 * 24, cookieCache: { enabled: false } },
  advanced: { useSecureCookies: origin.startsWith('https:'), ipAddress: { ipAddressHeaders: ['x-real-ip'] } },
  rateLimit: { enabled: true, storage: 'database', window: 60, max: 120,
    customRules: { '/get-session': false, '/sign-in/email': { window: 60, max: 8 } } },
  plugins: [admin()],
  hooks: { before: createAuthMiddleware(async (ctx) => {
    if (ctx.path === '/admin/create-user' || ctx.path === '/admin/set-user-password') {
      const password = ctx.body?.password ?? ctx.body?.newPassword
      if (typeof password !== 'string' || password.length < 12 || password.length > 128)
        throw new APIError('BAD_REQUEST', { message: 'Password must contain 12–128 characters' })
    }
  }) },
}
await (await getMigrations(options)).runMigrations()
const auth = betterAuth(options)
if (!database.prepare('SELECT id FROM user LIMIT 1').get()) {
  const email = process.env.MIST_ADMIN_EMAIL
  const password = process.env.MIST_ADMIN_PASSWORD
  if (!email || !password || password.length < 12) throw new Error('Initial admin credentials are required for an empty database')
  // A server-only SDK call, before the network listener exists. Public signup stays disabled.
  await auth.api.createUser({ body: { name: 'Mist Administrator', email, password, role: 'admin' } })
  console.log('Initial Mist administrator created; credentials are not logged')
}
const handler = toNodeHandler(auth)
const server = createServer((req, res) => {
  if (req.url === '/internal/members' && req.method === 'GET') {
    const configured = process.env.MIST_INTERNAL_TOKEN ?? ''
    const presented = req.headers.authorization?.replace(/^Bearer /, '') ?? ''
    if (configured.length < 32 || Buffer.byteLength(configured) !== Buffer.byteLength(presented) ||
        !timingSafeEqual(Buffer.from(configured), Buffer.from(presented))) {
      res.writeHead(403); res.end(); return
    }
    const members = database.prepare('SELECT id, banned, role FROM user').all()
    res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
    res.end(JSON.stringify({ members: members.map(({ id, banned, role }) => ({ id, active: !banned, role })) }))
    return
  }
  if (req.url === '/healthz') {
    try { database.prepare('SELECT 1').get(); res.writeHead(200); res.end('ok') }
    catch { res.writeHead(503); res.end('database unavailable') }
    return
  }
  if (!req.url?.startsWith('/auth/')) { res.writeHead(404); res.end(); return }
  // Only the same-origin Go proxy is exposed to clients. It replaces this header.
  void handler(req, res)
})
server.requestTimeout = 30000
server.headersTimeout = 10000
server.listen(Number(process.env.PORT ?? 3002), process.env.HOST ?? '127.0.0.1', () => console.log('Mist auth service ready'))
for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => server.close(() => { database.close(); process.exit(0) }))
