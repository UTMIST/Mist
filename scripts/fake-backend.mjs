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
 *   PORT=3000 node scripts/fake-backend.mjs
 *   npm --prefix web-interface run fake-backend
 */

import http from 'node:http'
import { URL } from 'node:url'
import crypto from 'node:crypto'

const PORT = Number(process.env.PORT || process.env.MIST_FAKE_PORT || 3000)
const HOST = process.env.HOST || '127.0.0.1'

// --- In-Memory State & Seed Data ---

const state = {
  users: [
    {
      id: 'usr_ada',
      name: 'Ada Lovelace',
      email: 'ada@utmist.gitlab.io',
      role: 'admin',
      banned: false,
    },
    {
      id: 'usr_alan',
      name: 'Alan Turing',
      email: 'alan@utmist.gitlab.io',
      role: 'user',
      banned: false,
    },
    {
      id: 'usr_grace',
      name: 'Grace Hopper',
      email: 'grace@utmist.gitlab.io',
      role: 'user',
      banned: false,
    },
  ],
  currentUserIndex: 0, // Ada Lovelace by default
  teams: [
    {
      id: 'team-vision',
      name: 'Computer Vision',
      disabled: false,
      policy: {
        cpu: '16',
        memory: '64Gi',
        nvidia: 2,
        tenstorrent: 4,
        concurrent: 4,
        queued: 20,
        storage_gib: 300,
        model_storage_gib: 600,
        runtime_seconds: 86400,
        registries: ['docker.io', 'ghcr.io', 'quay.io'],
      },
      members: [
        {
          id: 'usr_ada',
          name: 'Ada Lovelace',
          email: 'ada@utmist.gitlab.io',
          common_writer: true,
        },
        {
          id: 'usr_alan',
          name: 'Alan Turing',
          email: 'alan@utmist.gitlab.io',
          common_writer: false,
        },
      ],
      grants: [
        {
          id: 'grant-nlp-share',
          target_team: 'team-nlp',
          scope: 'common',
        },
      ],
    },
    {
      id: 'team-nlp',
      name: 'Natural Language Processing',
      disabled: false,
      policy: {
        cpu: '8',
        memory: '32Gi',
        nvidia: 1,
        tenstorrent: 2,
        concurrent: 2,
        queued: 10,
        storage_gib: 200,
        model_storage_gib: 400,
        runtime_seconds: 43200,
        registries: ['docker.io', 'ghcr.io'],
      },
      members: [
        {
          id: 'usr_ada',
          name: 'Ada Lovelace',
          email: 'ada@utmist.gitlab.io',
          common_writer: true,
        },
        {
          id: 'usr_grace',
          name: 'Grace Hopper',
          email: 'grace@utmist.gitlab.io',
          common_writer: false,
        },
      ],
      grants: [],
    },
  ],
  storageBudgetGiB: 2000,
  jobs: [
    {
      id: 'job-seed-001',
      name: 'ResNet50 Vision Benchmark',
      created: new Date(Date.now() - 3600000).toISOString(),
      job_state: 'Success',
      accelerator: 'nvidia',
      device_count: 2,
      image: 'ghcr.io/utmist/pytorch-cuda:latest',
      cpu: '8',
      memory: '32Gi',
      node: 'mist-gpu-node-01',
      exit_code: 0,
      message: 'Job completed successfully in 1420s',
      checkpoint_directory: '/outputs/checkpoints/job-seed-001',
      output_directory: '/outputs/runs/job-seed-001',
      timeout_seconds: 7200,
      team_id: 'team-vision',
      creator_id: 'usr_ada',
      creator_name: 'Ada Lovelace',
      storage_scope: 'common',
      can_cancel: false,
    },
    {
      id: 'job-seed-002',
      name: 'BERT Fine-tuning Test',
      created: new Date(Date.now() - 1800000).toISOString(),
      job_state: 'InProgress',
      accelerator: 'nvidia',
      device_count: 1,
      image: 'ghcr.io/utmist/pytorch-cuda:latest',
      cpu: '4',
      memory: '16Gi',
      node: 'mist-gpu-node-01',
      checkpoint_directory: '/outputs/checkpoints/job-seed-002',
      output_directory: '/outputs/runs/job-seed-002',
      timeout_seconds: 3600,
      team_id: 'team-nlp',
      creator_id: 'usr_grace',
      creator_name: 'Grace Hopper',
      storage_scope: 'usr_grace',
      can_cancel: true,
    },
    {
      id: 'job-seed-003',
      name: 'Tenstorrent Wormhole Matrix Multiplication',
      created: new Date(Date.now() - 600000).toISOString(),
      job_state: 'Scheduled',
      accelerator: 'tenstorrent',
      device_count: 2,
      image: 'ghcr.io/utmist/tt-metal-runtime:latest',
      cpu: '4',
      memory: '16Gi',
      message: 'Queued; waiting for 2 Tenstorrent n300 board allocations',
      checkpoint_directory: '/outputs/checkpoints/job-seed-003',
      output_directory: '/outputs/runs/job-seed-003',
      timeout_seconds: 1800,
      team_id: 'team-vision',
      creator_id: 'usr_alan',
      creator_name: 'Alan Turing',
      storage_scope: 'usr_alan',
      can_cancel: true,
    },
  ],
  datasets: [
    {
      id: 'ds-cifar100',
      name: 'CIFAR-100 Benchmark Dataset',
      filename: 'cifar100.zip',
      size: 169000000,
      files: 100,
      sha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
      created: new Date(Date.now() - 86400000).toISOString(),
      state: 'ready',
      team_id: 'team-vision',
      team_name: 'Computer Vision',
      scope: 'common',
      member_name: 'Ada Lovelace',
      shared: false,
    },
    {
      id: 'ds-wikitext',
      name: 'Wikitext-103 Subset',
      filename: 'wikitext.tar.gz',
      size: 512000000,
      files: 1,
      sha256: 'ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb',
      created: new Date(Date.now() - 43200000).toISOString(),
      state: 'ready',
      team_id: 'team-nlp',
      team_name: 'Natural Language Processing',
      scope: 'common',
      member_name: 'Grace Hopper',
      shared: true,
    },
  ],
  jobLogs: new Map([
    [
      'job-seed-001',
      `[2026-10-16 20:00:01] [INFO] Initializing PyTorch runtime on NVIDIA node 'mist-gpu-node-01'
[2026-10-16 20:00:03] [INFO] Allocated 2x NVIDIA RTX A6000 GPUs (CUDA devices 0, 1)
[2026-10-16 20:00:05] [INFO] Loading CIFAR-100 dataset from /inputs/cifar100
[2026-10-16 20:00:12] [INFO] Epoch 1/10: train_loss=2.31, val_acc=68.4%
[2026-10-16 20:10:45] [INFO] Epoch 10/10: train_loss=0.42, val_acc=84.2%
[2026-10-16 20:23:41] [INFO] Checkpoint saved to /outputs/runs/job-seed-001/checkpoint_final.pt
[2026-10-16 20:23:42] [INFO] Training complete. Process exited with code 0.`,
    ],
    [
      'job-seed-002',
      `[2026-10-16 20:30:00] [INFO] BERT fine-tuning initialized.
[2026-10-16 20:30:02] [INFO] Using GPU 0 (Allocated 1x GPU)
[2026-10-16 20:30:15] [INFO] Warmup steps complete. Training batch 450/2000...
[2026-10-16 20:30:30] [INFO] Current loss: 1.284`,
    ],
    [
      'job-seed-003',
      `[2026-10-16 20:50:00] [INFO] Job queued in team 'team-vision'. Awaiting Tenstorrent admission controller.`,
    ],
  ]),
  jobFiles: new Map([
    [
      'job-seed-001',
      [
        {
          path: 'checkpoint_final.pt',
          size: 104857600,
          modified: new Date(Date.now() - 3600000).toISOString(),
        },
        {
          path: 'training_metrics.json',
          size: 4096,
          modified: new Date(Date.now() - 3600000).toISOString(),
        },
        {
          path: 'run_summary.txt',
          size: 512,
          modified: new Date(Date.now() - 3600000).toISOString(),
        },
      ],
    ],
  ]),
}

// Automatically progress queued jobs to running and completed
setInterval(() => {
  for (const job of state.jobs) {
    if (job.job_state === 'Scheduled') {
      job.job_state = 'InProgress'
      job.node =
        job.accelerator === 'tenstorrent'
          ? 'mist-tt-worker-01'
          : 'mist-gpu-node-01'
      job.message = 'Pod running on ' + job.node
      const logs =
        state.jobLogs.get(job.id) ||
        `[${new Date().toISOString()}] Job submitted.\n`
      state.jobLogs.set(
        job.id,
        logs +
          `[${new Date().toISOString()}] Admitted to cluster. Initializing pod on ${job.node}...\n`,
      )
    } else if (job.job_state === 'InProgress') {
      const elapsed = Date.now() - new Date(job.created).getTime()
      if (elapsed > 20000) {
        // Complete job after 20s
        job.job_state = 'Success'
        job.exit_code = 0
        job.message = 'Job completed successfully'
        job.can_cancel = false
        const logs = state.jobLogs.get(job.id) || ''
        state.jobLogs.set(
          job.id,
          logs +
            `[${new Date().toISOString()}] Execution finished with code 0.\nOutputs written to /outputs.\n`,
        )
        if (!state.jobFiles.has(job.id)) {
          state.jobFiles.set(job.id, [
            {
              path: 'output.log',
              size: 2048,
              modified: new Date().toISOString(),
            },
            {
              path: 'model_weights.bin',
              size: 52428800,
              modified: new Date().toISOString(),
            },
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
      'Access-Control-Allow-Headers':
        'Content-Type, X-Mist-Team, Authorization',
      'Access-Control-Max-Age': '86400',
    })
    res.end()
    return
  }

  const parsedUrl = new URL(req.url, `http://${req.headers.host || 'localhost'}`)
  let pathname = parsedUrl.pathname
  const query = Object.fromEntries(parsedUrl.searchParams.entries())
  const activeTeam = req.headers['x-mist-team'] || query.team_id || ''

  // Normalize path by stripping optional '/api' prefix (Vite proxy rewrites /api, direct hits do not)
  if (pathname.startsWith('/api/')) {
    pathname = pathname.slice(4)
  } else if (pathname === '/api') {
    pathname = '/'
  }

  const currentUser = state.users[state.currentUserIndex] || null

  try {
    // ----------------------------------------------------
    // AUTH & SESSION ENDPOINTS
    // ----------------------------------------------------

    // Session check used by AccountGate
    if (pathname === '/session' && req.method === 'GET') {
      sendJSON(res, 200, {
        enabled: true,
        user: currentUser,
      })
      return
    }

    // Better Auth: Session retrieval
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

    // Better Auth: Email Sign-in
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

    // Better Auth: Sign-out
    if (pathname === '/auth/sign-out' && req.method === 'POST') {
      state.currentUserIndex = -1 // signed out
      sendJSON(res, 200, { success: true })
      return
    }

    // Better Auth: Change Password
    if (pathname === '/auth/change-password' && req.method === 'POST') {
      await parseJSONBody(req)
      sendJSON(res, 200, { success: true })
      return
    }

    // Better Auth Admin: List Users
    if (
      (pathname === '/auth/admin/list-users' ||
        pathname === '/auth/admin/listUsers') &&
      (req.method === 'GET' || req.method === 'POST')
    ) {
      sendJSON(res, 200, {
        users: state.users,
        total: state.users.length,
      })
      return
    }

    // Better Auth Admin: Create User
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

    // Better Auth Admin: Set User Password
    if (pathname === '/auth/admin/set-user-password' && req.method === 'POST') {
      await parseJSONBody(req)
      sendJSON(res, 200, { success: true })
      return
    }

    // Better Auth Admin: Revoke User Sessions
    if (
      pathname === '/auth/admin/revoke-user-sessions' &&
      req.method === 'POST'
    ) {
      await parseJSONBody(req)
      sendJSON(res, 200, { success: true })
      return
    }

    // Better Auth Admin: Ban / Unban User
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
        machines: [
          {
            name: 'mist-gpu-node-01',
            address: '10.0.1.10',
            ready: true,
            schedulable: true,
            cpu_allocatable: '32',
            memory_allocatable: '128Gi',
          },
          {
            name: 'mist-tt-worker-01',
            address: '10.0.1.11',
            ready: true,
            schedulable: true,
            cpu_allocatable: '16',
            memory_allocatable: '64Gi',
          },
        ],
        pools: [
          {
            accelerator: 'nvidia',
            node: 'mist-gpu-node-01',
            unit: 'GPU',
            total: 4,
            allocated: state.jobs
              .filter(
                (j) =>
                  j.accelerator === 'nvidia' && j.job_state === 'InProgress',
              )
              .reduce((sum, j) => sum + (j.device_count || 1), 0),
            available: Math.max(
              0,
              4 -
                state.jobs
                  .filter(
                    (j) =>
                      j.accelerator === 'nvidia' &&
                      j.job_state === 'InProgress',
                  )
                  .reduce((sum, j) => sum + (j.device_count || 1), 0),
            ),
            ready: true,
          },
          {
            accelerator: 'tenstorrent',
            node: 'mist-tt-worker-01',
            unit: 'board',
            total: 4,
            allocated: state.jobs
              .filter(
                (j) =>
                  j.accelerator === 'tenstorrent' &&
                  j.job_state === 'InProgress',
              )
              .reduce((sum, j) => sum + (j.device_count || 1), 0),
            available: Math.max(
              0,
              4 -
                state.jobs
                  .filter(
                    (j) =>
                      j.accelerator === 'tenstorrent' &&
                      j.job_state === 'InProgress',
                  )
                  .reduce((sum, j) => sum + (j.device_count || 1), 0),
            ),
            ready: true,
            chips_per_device: 2,
          },
        ],
      })
      return
    }

    if (pathname === '/images' && req.method === 'GET') {
      sendJSON(res, 200, {
        self_service: true,
        teams_enabled: true,
        registries: ['docker.io', 'ghcr.io', 'quay.io', 'localhost:5000'],
        images: [
          {
            reference: 'docker.io/library/python:3.11-slim',
            accelerators: ['cpu'],
          },
          {
            reference: 'docker.io/library/pytorch-cpu:2.2.0',
            accelerators: ['cpu'],
          },
          {
            reference: 'ghcr.io/utmist/pytorch-cuda:latest',
            accelerators: ['nvidia'],
          },
          {
            reference: 'ghcr.io/utmist/tt-metal-runtime:latest',
            accelerators: ['tenstorrent'],
          },
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
      })
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
      sendJSON(res, 200, {
        enabled: true,
        capacity_bytes: 1099511627776, // 1 TiB
        available_bytes: 879609302220, // 800 GiB
        model_capacity_bytes: 2199023255552, // 2 TiB
        model_available_bytes: 1759218604441, // 1.6 TiB
        dataset_allocation_gib: 500,
        model_allocation_gib: 1000,
        upload_limit_bytes: 5368709120, // 5 GiB
      })
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
        upload_limit_bytes: 5368709120,
      })
      return
    }

    // Dataset upload (streaming / raw binary or multipart)
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

    // Health check
    if (pathname === '/healthz' && req.method === 'GET') {
      sendJSON(res, 200, { status: 'ok', executor: 'fake-backend' })
      return
    }

    // 404 Fallback
    sendError(res, 404, `Endpoint not found: ${req.method} ${pathname}`)
  } catch (err) {
    console.error('[Fake Backend Error]', err)
    sendError(res, 500, 'Internal Server Error: ' + err.message)
  }
})

server.listen(PORT, HOST, () => {
  console.log(`======================================================`)
  console.log(`Mist Fake Backend running at http://${HOST}:${PORT}`)
  console.log(`Ready for web-interface testing!`)
  console.log(`- Simulated User: ${state.users[state.currentUserIndex]?.name} (${state.users[state.currentUserIndex]?.role})`)
  console.log(`- Teams: ${state.teams.map((t) => t.name).join(', ')}`)
  console.log(`- Jobs: ${state.jobs.length} pre-populated`)
  console.log(`======================================================`)
})

process.on('SIGINT', () => {
  server.close(() => {
    console.log('\nFake backend stopped.')
    process.exit(0)
  })
})
