#!/usr/bin/env node
/**
 * Mist Fake Backend Server
 *
 * A standalone, zero-dependency Node.js HTTP server simulating the Mist Go API
 * and Better Auth service. Designed for local frontend development, testing,
 * and rapid prototyping without requiring k3s, GPUs, or Better Auth SQLite.
 *
 * Usage:
 *   node scripts/fake-backend.mjs
 *   node scripts/fake-backend.mjs --scenario high-load
 *   node scripts/fake-backend.mjs --config ./custom-config.json
 *   PORT=3000 node scripts/fake-backend.mjs
 *   npm --prefix web-interface run fake-backend
 */

import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import { URL, fileURLToPath } from 'node:url'
import crypto from 'node:crypto'
import {
  getScenario,
  SCENARIO_DESCRIPTIONS,
  SCENARIO_FACTORIES,
} from './fake-backend-scenarios.mjs'

// Parse command line arguments
const args = process.argv.slice(2)
function getArg(flag) {
  const idx = args.indexOf(flag)
  return idx !== -1 && args[idx + 1] ? args[idx + 1] : null
}

const PORT = Number(
  getArg('--port') || process.env.PORT || process.env.MIST_FAKE_PORT || 3000,
)
const HOST = process.env.HOST || '127.0.0.1'
const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const DEFAULT_CONFIG_FILE = path.resolve(ROOT_DIR, '.fake-backend-config.json')

// --- Initial State Management ---

let state = {
  activeScenario: 'default',
  users: [],
  currentUserIndex: 0,
  machines: [],
  pools: [],
  storageBudgetGiB: 2000,
  storage: {},
  teams: [],
  jobs: [],
  datasets: [],
  images: {
    self_service: true,
    teams_enabled: true,
    registries: ['docker.io', 'ghcr.io', 'quay.io', 'localhost:5000'],
    images: [
      { reference: 'docker.io/library/python:3.11-slim', accelerators: ['cpu'] },
      { reference: 'docker.io/library/pytorch-cpu:2.2.0', accelerators: ['cpu'] },
      { reference: 'ghcr.io/utmist/pytorch-cuda:latest', accelerators: ['nvidia'] },
      { reference: 'ghcr.io/utmist/tt-metal-runtime:latest', accelerators: ['tenstorrent'] },
    ],
    profiles: [
      {
        accelerator: 'cpu',
        default_image: 'docker.io/library/python:3.11-slim',
        device_unit: 'core',
        max_devices: 16,
        note: 'Standard CPU runtime',
      },
      {
        accelerator: 'nvidia',
        default_image: 'ghcr.io/utmist/pytorch-cuda:latest',
        device_unit: 'GPU',
        max_devices: 4,
        note: 'NVIDIA CUDA 12.4 + PyTorch runtime',
      },
      {
        accelerator: 'tenstorrent',
        default_image: 'ghcr.io/utmist/tt-metal-runtime:latest',
        device_unit: 'board',
        max_devices: 4,
        note: 'Tenstorrent Wormhole n300 PCIe runtime',
      },
    ],
  },
  jobLogs: new Map(),
  jobFiles: new Map(),
}

export function applyConfiguration(config) {
  if (config.users) state.users = JSON.parse(JSON.stringify(config.users))
  if (config.currentUserIndex !== undefined) state.currentUserIndex = config.currentUserIndex
  if (config.activeUserId) {
    const idx = state.users.findIndex(
      (u) => u.id === config.activeUserId || u.email.toLowerCase() === config.activeUserId.toLowerCase(),
    )
    if (idx >= 0) state.currentUserIndex = idx
  }
  if (config.machines) state.machines = JSON.parse(JSON.stringify(config.machines))
  if (config.pools) state.pools = JSON.parse(JSON.stringify(config.pools))
  if (config.storage) state.storage = JSON.parse(JSON.stringify(config.storage))
  if (config.storageBudgetGiB !== undefined) state.storageBudgetGiB = config.storageBudgetGiB
  if (config.teams) state.teams = JSON.parse(JSON.stringify(config.teams))
  if (config.jobs) {
    state.jobs = JSON.parse(JSON.stringify(config.jobs))
    for (const job of state.jobs) {
      if (!state.jobLogs.has(job.id)) {
        state.jobLogs.set(
          job.id,
          `[${job.created}] Job ${job.name} initialized in state ${job.job_state}\n`,
        )
      }
    }
  }
  if (config.datasets) state.datasets = JSON.parse(JSON.stringify(config.datasets))
  if (config.images) state.images = JSON.parse(JSON.stringify(config.images))
  if (config.name) state.activeScenario = config.name
}

// Determine initial scenario / config
function initializeState() {
  const cliScenario = getArg('--scenario') || process.env.MIST_FAKE_SCENARIO
  const cliConfig = getArg('--config') || process.env.MIST_FAKE_CONFIG

  if (cliScenario) {
    try {
      console.log(`[Config] Loading requested scenario: '${cliScenario}'`)
      applyConfiguration(getScenario(cliScenario))
      return
    } catch (err) {
      console.warn(`[Config] Warning: ${err.message}. Falling back to default.`)
    }
  }

  if (cliConfig && fs.existsSync(cliConfig)) {
    try {
      console.log(`[Config] Loading configuration from: ${cliConfig}`)
      const raw = fs.readFileSync(cliConfig, 'utf-8')
      applyConfiguration(JSON.parse(raw))
      return
    } catch (err) {
      console.warn(`[Config] Failed to load config from ${cliConfig}: ${err.message}`)
    }
  }

  if (fs.existsSync(DEFAULT_CONFIG_FILE)) {
    try {
      console.log(`[Config] Found local ${DEFAULT_CONFIG_FILE}, loading...`)
      const raw = fs.readFileSync(DEFAULT_CONFIG_FILE, 'utf-8')
      applyConfiguration(JSON.parse(raw))
      return
    } catch (err) {
      console.warn(`[Config] Failed to parse ${DEFAULT_CONFIG_FILE}: ${err.message}`)
    }
  }

  // Fallback to default scenario
  applyConfiguration(getScenario('default'))
}

initializeState()

// Automatically progress queued jobs over time
setInterval(() => {
  for (const job of state.jobs) {
    if (job.job_state === 'Scheduled') {
      // Find suitable node if available
      const pool = state.pools.find(
        (p) => p.accelerator === job.accelerator && p.ready && (p.available === null || p.available > 0),
      )
      if (pool) {
        job.job_state = 'InProgress'
        job.node = pool.node
        job.message = `Pod running on ${job.node}`
        const logs = state.jobLogs.get(job.id) || `[${new Date().toISOString()}] Job submitted.\n`
        state.jobLogs.set(
          job.id,
          logs + `[${new Date().toISOString()}] Admitted to cluster. Initializing pod on ${job.node}...\n`,
        )
      }
    } else if (job.job_state === 'InProgress') {
      const elapsed = Date.now() - new Date(job.created).getTime()
      if (elapsed > 20000 && !job.id.startsWith('job-hl-run')) {
        job.job_state = 'Success'
        job.exit_code = 0
        job.message = 'Job completed successfully'
        job.can_cancel = false
        const logs = state.jobLogs.get(job.id) || ''
        state.jobLogs.set(
          job.id,
          logs + `[${new Date().toISOString()}] Execution finished with code 0.\nOutputs saved.\n`,
        )
        if (!state.jobFiles.has(job.id)) {
          state.jobFiles.set(job.id, [
            { path: 'output.log', size: 2048, modified: new Date().toISOString() },
            { path: 'model_weights.bin', size: 52428800, modified: new Date().toISOString() },
          ])
        }
      }
    }
  }
}, 5000)

// --- Helper Functions ---

function parseJSONBody(req) {
  return new Promise((resolve, reject) => {
    let data = ''
    req.on('data', (chunk) => {
      data += chunk
      if (data.length > 50 * 1024 * 1024) {
        req.destroy()
        reject(new Error('Request payload too large'))
      }
    })
    req.on('end', () => {
      if (!data.trim()) return resolve({})
      try {
        resolve(JSON.parse(data))
      } catch (err) {
        reject(new Error('Malformed JSON: ' + err.message))
      }
    })
    req.on('error', reject)
  })
}

function sendJSON(res, statusCode, payload) {
  const body = JSON.stringify(payload)
  res.writeHead(statusCode, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(body),
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET, POST, PATCH, DELETE, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type, X-Mist-Team, Authorization',
  })
  res.end(body)
}

function sendError(res, statusCode, message) {
  sendJSON(res, statusCode, { error: message })
}

// --- Request Router ---

const server = http.createServer(async (req, res) => {
  // CORS Preflight
  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PATCH, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, X-Mist-Team, Authorization',
      'Access-Control-Max-Age': '86400',
    })
    res.end()
    return
  }

  const parsedUrl = new URL(req.url, `http://${req.headers.host || 'localhost'}`)
  let pathname = parsedUrl.pathname
  const query = Object.fromEntries(parsedUrl.searchParams.entries())
  const activeTeam = req.headers['x-mist-team'] || query.team_id || ''

  // Normalize path by stripping optional '/api' prefix
  if (pathname.startsWith('/api/')) {
    pathname = pathname.slice(4)
  } else if (pathname === '/api') {
    pathname = '/'
  }

  const currentUser = state.users[state.currentUserIndex] || null

  try {
    // ----------------------------------------------------
    // FAKE BACKEND DYNAMIC CONFIGURATION & SCENARIO CONTROL
    // ----------------------------------------------------

    if (pathname === '/_fake/state' && req.method === 'GET') {
      sendJSON(res, 200, {
        activeScenario: state.activeScenario,
        currentUserIndex: state.currentUserIndex,
        currentUser,
        users: state.users,
        machines: state.machines,
        pools: state.pools,
        storage: state.storage,
        storageBudgetGiB: state.storageBudgetGiB,
        teams: state.teams,
        jobs: state.jobs,
        datasets: state.datasets,
      })
      return
    }

    if ((pathname === '/_fake/state' || pathname === '/_fake/configure') && req.method === 'POST') {
      const body = await parseJSONBody(req)
      applyConfiguration(body)
      sendJSON(res, 200, {
        success: true,
        message: 'Configuration applied successfully',
        activeScenario: state.activeScenario,
      })
      return
    }

    if (pathname === '/_fake/scenarios' && req.method === 'GET') {
      sendJSON(res, 200, {
        scenarios: Object.entries(SCENARIO_DESCRIPTIONS).map(([name, description]) => ({
          name,
          description,
          active: state.activeScenario === name,
        })),
      })
      return
    }

    const scenarioMatch = pathname.match(/^\/_fake\/scenario\/([^/]+)$/)
    if (scenarioMatch && req.method === 'POST') {
      const name = scenarioMatch[1]
      try {
        const scenarioData = getScenario(name)
        applyConfiguration(scenarioData)
        sendJSON(res, 200, {
          success: true,
          scenario: name,
          message: `Switched to hypothetical scenario: '${name}'`,
        })
      } catch (err) {
        sendError(res, 400, err.message)
      }
      return
    }

    if (pathname === '/_fake/reset' && req.method === 'POST') {
      const defaultData = getScenario('default')
      applyConfiguration(defaultData)
      sendJSON(res, 200, { success: true, message: 'Reset to default scenario' })
      return
    }

    // ----------------------------------------------------
    // AUTH & SESSION ENDPOINTS
    // ----------------------------------------------------

    if (pathname === '/session' && req.method === 'GET') {
      sendJSON(res, 200, {
        enabled: true,
        user: currentUser,
      })
      return
    }

    if (pathname === '/auth/get-session' && req.method === 'GET') {
      if (!currentUser) {
        sendJSON(res, 200, null)
      } else {
        sendJSON(res, 200, {
          user: currentUser,
          session: {
            id: 'sess_' + currentUser.id,
            userId: currentUser.id,
            expiresAt: new Date(Date.now() + 86400000).toISOString(),
          },
        })
      }
      return
    }

    if (pathname === '/auth/sign-in/email' && req.method === 'POST') {
      const body = await parseJSONBody(req)
      const user = state.users.find(
        (u) => u.email.toLowerCase() === (body.email || '').toLowerCase(),
      )
      if (!user) {
        sendJSON(res, 401, {
          error: { message: 'Invalid credentials or user not found' },
        })
        return
      }
      if (user.banned) {
        sendJSON(res, 403, {
          error: { message: 'Account is deactivated' },
        })
        return
      }
      state.currentUserIndex = state.users.findIndex((u) => u.id === user.id)
      sendJSON(res, 200, {
        user,
        session: {
          id: 'sess_' + user.id,
          userId: user.id,
          expiresAt: new Date(Date.now() + 86400000).toISOString(),
        },
      })
      return
    }

    if (pathname === '/auth/sign-out' && req.method === 'POST') {
      state.currentUserIndex = -1 // signed out
      sendJSON(res, 200, { success: true })
      return
    }

    if (pathname === '/auth/change-password' && req.method === 'POST') {
      await parseJSONBody(req)
      sendJSON(res, 200, { success: true })
      return
    }

    if (
      (pathname === '/auth/admin/list-users' || pathname === '/auth/admin/listUsers') &&
      (req.method === 'GET' || req.method === 'POST')
    ) {
      sendJSON(res, 200, {
        users: state.users,
        total: state.users.length,
      })
      return
    }

    if (pathname === '/auth/admin/create-user' && req.method === 'POST') {
      const body = await parseJSONBody(req)
      if (!body.name || !body.email) {
        sendError(res, 400, 'Name and email are required')
        return
      }
      const newUser = {
        id: 'usr_' + crypto.randomBytes(4).toString('hex'),
        name: body.name,
        email: body.email,
        role: body.role || 'user',
        banned: false,
      }
      state.users.push(newUser)
      sendJSON(res, 200, { user: newUser })
      return
    }

    if (pathname === '/auth/admin/set-user-password' && req.method === 'POST') {
      await parseJSONBody(req)
      sendJSON(res, 200, { success: true })
      return
    }

    if (pathname === '/auth/admin/revoke-user-sessions' && req.method === 'POST') {
      await parseJSONBody(req)
      sendJSON(res, 200, { success: true })
      return
    }

    if (pathname === '/auth/admin/ban-user' && req.method === 'POST') {
      const body = await parseJSONBody(req)
      const target = state.users.find((u) => u.id === body.userId)
      if (target) target.banned = true
      sendJSON(res, 200, { success: true })
      return
    }

    if (pathname === '/auth/admin/unban-user' && req.method === 'POST') {
      const body = await parseJSONBody(req)
      const target = state.users.find((u) => u.id === body.userId)
      if (target) target.banned = false
      sendJSON(res, 200, { success: true })
      return
    }

    // ----------------------------------------------------
    // HARDWARE & IMAGES ENDPOINTS
    // ----------------------------------------------------

    if (pathname === '/hardware' && req.method === 'GET') {
      sendJSON(res, 200, {
        observed_at: new Date().toISOString(),
        machines: state.machines,
        pools: state.pools,
      })
      return
    }

    if (pathname === '/images' && req.method === 'GET') {
      sendJSON(res, 200, state.images)
      return
    }

    // ----------------------------------------------------
    // TEAMS ENDPOINTS
    // ----------------------------------------------------

    if (pathname === '/teams' && req.method === 'GET') {
      sendJSON(res, 200, {
        teams: state.teams,
        storage_budget_gib: state.storageBudgetGiB,
      })
      return
    }

    if (pathname === '/teams' && req.method === 'POST') {
      const body = await parseJSONBody(req)
      const newTeam = {
        id: 'team-' + (body.name || 'unnamed').toLowerCase().replace(/\W+/g, '-'),
        name: body.name || 'New Team',
        disabled: false,
        policy: {
          cpu: '8',
          memory: '32Gi',
          nvidia: 1,
          tenstorrent: 2,
          concurrent: 2,
          queued: 10,
          storage_gib: Number(body.storage_gib || 200),
          model_storage_gib: Number(body.model_storage_gib || 500),
          runtime_seconds: 86400,
          registries: ['docker.io', 'ghcr.io'],
        },
        members: currentUser
          ? [
              {
                id: currentUser.id,
                name: currentUser.name,
                email: currentUser.email,
                common_writer: true,
              },
            ]
          : [],
        grants: [],
      }
      state.teams.push(newTeam)
      sendJSON(res, 200, newTeam)
      return
    }

    const teamMatch = pathname.match(/^\/teams\/([^/]+)$/)
    if (teamMatch && req.method === 'PATCH') {
      const teamId = teamMatch[1]
      const team = state.teams.find((t) => t.id === teamId)
      if (!team) return sendError(res, 404, 'Team not found')
      const body = await parseJSONBody(req)
      if (body.name !== undefined) team.name = body.name
      if (body.disabled !== undefined) team.disabled = body.disabled
      if (body.policy !== undefined) team.policy = { ...team.policy, ...body.policy }
      sendJSON(res, 200, team)
      return
    }

    const teamMembersMatch = pathname.match(/^\/teams\/([^/]+)\/members$/)
    if (teamMembersMatch && req.method === 'POST') {
      const teamId = teamMembersMatch[1]
      const team = state.teams.find((t) => t.id === teamId)
      if (!team) return sendError(res, 404, 'Team not found')
      const body = await parseJSONBody(req)
      const user = state.users.find((u) => u.id === body.user_id)
      if (!user) return sendError(res, 404, 'User not found')
      const existing = team.members.find((m) => m.id === user.id)
      if (existing) {
        existing.common_writer = !!body.common_writer
      } else {
        team.members.push({
          id: user.id,
          name: user.name,
          email: user.email,
          common_writer: !!body.common_writer,
        })
      }
      sendJSON(res, 200, team)
      return
    }

    const teamMemberDeleteMatch = pathname.match(/^\/teams\/([^/]+)\/members\/([^/]+)$/)
    if (teamMemberDeleteMatch && req.method === 'DELETE') {
      const teamId = teamMemberDeleteMatch[1]
      const userId = decodeURIComponent(teamMemberDeleteMatch[2])
      const team = state.teams.find((t) => t.id === teamId)
      if (!team) return sendError(res, 404, 'Team not found')
      team.members = team.members.filter((m) => m.id !== userId)
      sendJSON(res, 200, team)
      return
    }

    const teamGrantsMatch = pathname.match(/^\/teams\/([^/]+)\/grants$/)
    if (teamGrantsMatch && req.method === 'POST') {
      const teamId = teamGrantsMatch[1]
      const team = state.teams.find((t) => t.id === teamId)
      if (!team) return sendError(res, 404, 'Team not found')
      const body = await parseJSONBody(req)
      const newGrant = {
        id: 'grant-' + crypto.randomBytes(3).toString('hex'),
        target_team: body.target_team,
        scope: body.scope || 'common',
      }
      team.grants.push(newGrant)
      sendJSON(res, 200, team)
      return
    }

    const teamGrantDeleteMatch = pathname.match(/^\/teams\/([^/]+)\/grants\/([^/]+)$/)
    if (teamGrantDeleteMatch && req.method === 'DELETE') {
      const teamId = teamGrantDeleteMatch[1]
      const grantId = teamGrantDeleteMatch[2]
      const team = state.teams.find((t) => t.id === teamId)
      if (!team) return sendError(res, 404, 'Team not found')
      team.grants = team.grants.filter((g) => g.id !== grantId)
      sendJSON(res, 200, team)
      return
    }

    // ----------------------------------------------------
    // JOBS ENDPOINTS
    // ----------------------------------------------------

    if (pathname === '/jobs' && req.method === 'GET') {
      let filtered = state.jobs
      if (activeTeam) {
        filtered = filtered.filter((j) => j.team_id === activeTeam)
      }
      sendJSON(res, 200, {
        jobs: filtered,
        count: filtered.length,
      })
      return
    }

    if (pathname === '/jobs' && req.method === 'POST') {
      const body = await parseJSONBody(req)
      const jobId = 'job-' + crypto.randomBytes(4).toString('hex')
      const newJob = {
        id: jobId,
        name: body.name || `Experiment ${jobId}`,
        created: new Date().toISOString(),
        job_state: 'Scheduled',
        accelerator: body.accelerator || 'cpu',
        device_count: body.device_count || (body.accelerator === 'cpu' ? 0 : 1),
        image: body.image || 'docker.io/library/python:3.11-slim',
        cpu: body.cpu || '2',
        memory: body.memory || '4Gi',
        message: 'Job submitted; awaiting placement',
        checkpoint_directory: `/outputs/checkpoints/${jobId}`,
        output_directory: `/outputs/runs/${jobId}`,
        working_directory: body.working_directory || '/workspace',
        timeout_seconds: body.timeout_seconds || 3600,
        team_id: activeTeam || 'legacy',
        creator_id: currentUser?.id || 'usr_anonymous',
        creator_name: currentUser?.name || 'Local Developer',
        storage_scope: body.storage_scope || currentUser?.id || 'common',
        can_cancel: true,
      }
      state.jobs.unshift(newJob)
      state.jobLogs.set(
        jobId,
        `[${newJob.created}] Job created: ${newJob.name}\n[${newJob.created}] Image: ${newJob.image}\n[${newJob.created}] Queueing job on Kubernetes admission controller...\n`,
      )
      sendJSON(res, 200, {
        job_id: jobId,
        job: newJob,
      })
      return
    }

    const jobCancelMatch = pathname.match(/^\/jobs\/([^/]+)\/cancel$/)
    if (jobCancelMatch && req.method === 'POST') {
      const jobId = decodeURIComponent(jobCancelMatch[1])
      const job = state.jobs.find((j) => j.id === jobId)
      if (!job) return sendError(res, 404, 'Job not found')
      job.job_state = 'Cancelled'
      job.message = 'Cancelled by user'
      job.can_cancel = false
      const logs = state.jobLogs.get(jobId) || ''
      state.jobLogs.set(
        jobId,
        logs + `[${new Date().toISOString()}] Job cancelled by user.\n`,
      )
      sendJSON(res, 200, job)
      return
    }

    const jobLogsMatch = pathname.match(/^\/jobs\/([^/]+)\/logs$/)
    if (jobLogsMatch && req.method === 'GET') {
      const jobId = decodeURIComponent(jobLogsMatch[1])
      const logs = state.jobLogs.get(jobId) || 'No logs recorded for this job yet.'
      sendJSON(res, 200, { logs })
      return
    }

    const jobFilesMatch = pathname.match(/^\/jobs\/([^/]+)\/files$/)
    if (jobFilesMatch && req.method === 'GET') {
      const jobId = decodeURIComponent(jobFilesMatch[1])
      const files = state.jobFiles.get(jobId) || []
      sendJSON(res, 200, { files })
      return
    }

    const jobDownloadMatch = pathname.match(/^\/jobs\/([^/]+)\/files\/download$/)
    if (jobDownloadMatch && req.method === 'GET') {
      const filePath = query.path || 'output.txt'
      res.writeHead(200, {
        'Content-Type': 'text/plain; charset=utf-8',
        'Content-Disposition': `attachment; filename="${filePath.split('/').pop()}"`,
        'Access-Control-Allow-Origin': '*',
      })
      res.end(`[Mock file content for ${filePath} from job]`)
      return
    }

    // ----------------------------------------------------
    // STORAGE & DATASETS ENDPOINTS
    // ----------------------------------------------------

    if (pathname === '/storage' && req.method === 'GET') {
      sendJSON(res, 200, state.storage)
      return
    }

    if (pathname === '/storage/folders' && req.method === 'GET') {
      const folders = []
      for (const t of state.teams) {
        folders.push({
          team_id: t.id,
          team_name: t.name,
          scope: 'common',
          name: 'Common Space',
          shared: false,
          writable: true,
        })
        for (const m of t.members) {
          folders.push({
            team_id: t.id,
            team_name: t.name,
            scope: m.id,
            name: `${m.name}'s Folder`,
            shared: false,
            writable: m.id === currentUser?.id,
          })
        }
      }
      sendJSON(res, 200, { folders })
      return
    }

    if (pathname === '/storage/files' && req.method === 'GET') {
      if (query.download === 'true') {
        const filePath = query.path || 'downloaded-file.dat'
        res.writeHead(200, {
          'Content-Type': 'application/octet-stream',
          'Content-Disposition': `attachment; filename="${filePath.split('/').pop()}"`,
          'Access-Control-Allow-Origin': '*',
        })
        res.end(`[Mock content of ${filePath} in team ${query.source_team}]`)
        return
      }
      sendJSON(res, 200, {
        files: [
          {
            path: 'data/train_manifest.json',
            size: 15360,
            modified: new Date(Date.now() - 7200000).toISOString(),
          },
          {
            path: 'models/model_final.pt',
            size: 89128960,
            modified: new Date(Date.now() - 3600000).toISOString(),
          },
        ],
      })
      return
    }

    if (pathname === '/datasets' && req.method === 'GET') {
      let filtered = state.datasets
      if (activeTeam) {
        filtered = filtered.filter((d) => !d.team_id || d.team_id === activeTeam)
      }
      sendJSON(res, 200, {
        datasets: filtered,
        upload_limit_bytes: state.storage.upload_limit_bytes || 5368709120,
      })
      return
    }

    if (pathname === '/datasets' && req.method === 'POST') {
      const filename = query.filename || 'dataset.bin'
      const name = query.name || filename
      const scope = query.scope || currentUser?.id || 'common'
      const activeTeamObj = state.teams.find((t) => t.id === activeTeam)

      let totalBytes = 0
      req.on('data', (chunk) => {
        totalBytes += chunk.length
      })

      req.on('end', () => {
        const newDs = {
          id: 'ds-' + crypto.randomBytes(4).toString('hex'),
          name,
          filename,
          size: totalBytes || 1048576,
          files: query.format === 'zip' ? 12 : 1,
          sha256: crypto.randomBytes(32).toString('hex'),
          created: new Date().toISOString(),
          state: 'ready',
          team_id: activeTeam || 'legacy',
          team_name: activeTeamObj?.name || 'Legacy Workspace',
          scope,
          member_name: currentUser?.name || 'Local Pilot',
          shared: false,
        }
        state.datasets.unshift(newDs)
        sendJSON(res, 200, newDs)
      })
      return
    }

    const datasetDeleteMatch = pathname.match(/^\/datasets\/([^/]+)$/)
    if (datasetDeleteMatch && req.method === 'DELETE') {
      const datasetId = datasetDeleteMatch[1]
      state.datasets = state.datasets.filter((d) => d.id !== datasetId)
      sendJSON(res, 200, { success: true })
      return
    }

    const datasetFilesMatch = pathname.match(/^\/datasets\/([^/]+)\/files$/)
    if (datasetFilesMatch && req.method === 'GET') {
      sendJSON(res, 200, {
        files: [
          {
            path: 'metadata.json',
            size: 2048,
            modified: new Date().toISOString(),
          },
          {
            path: 'samples/sample_001.png',
            size: 45056,
            modified: new Date().toISOString(),
          },
        ],
      })
      return
    }

    const datasetDownloadMatch = pathname.match(/^\/datasets\/([^/]+)\/files\/download$/)
    if (datasetDownloadMatch && req.method === 'GET') {
      const filePath = query.path || 'dataset-file.dat'
      res.writeHead(200, {
        'Content-Type': 'application/octet-stream',
        'Content-Disposition': `attachment; filename="${filePath.split('/').pop()}"`,
        'Access-Control-Allow-Origin': '*',
      })
      res.end(`[Mock content of dataset file ${filePath}]`)
      return
    }

    if (pathname === '/healthz' && req.method === 'GET') {
      sendJSON(res, 200, { status: 'ok', executor: 'fake-backend' })
      return
    }

    sendError(res, 404, `Endpoint not found: ${req.method} ${pathname}`)
  } catch (err) {
    console.error('[Fake Backend Error]', err)
    sendError(res, 500, 'Internal Server Error: ' + err.message)
  }
})

server.listen(PORT, HOST, () => {
  console.log(`======================================================`)
  console.log(`Mist Fake Backend running at http://${HOST}:${PORT}`)
  console.log(`Active Scenario: '${state.activeScenario}'`)
  console.log(`Simulated User:  ${state.users[state.currentUserIndex]?.name} (${state.users[state.currentUserIndex]?.role})`)
  console.log(`Machines:        ${state.machines.map((m) => m.name).join(', ')}`)
  console.log(`Teams:           ${state.teams.map((t) => t.name).join(', ')}`)
  console.log(`Jobs:            ${state.jobs.length} in history`)
  console.log(`Run 'node scripts/configure-fake-backend.mjs' to modify`)
  console.log(`======================================================`)
})

process.on('SIGINT', () => {
  server.close(() => {
    console.log('\nFake backend stopped.')
    process.exit(0)
  })
})
