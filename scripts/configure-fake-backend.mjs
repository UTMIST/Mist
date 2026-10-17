#!/usr/bin/env node
/**
 * Mist Fake Backend Configuration CLI
 *
 * Configures hypothetical scenarios, fake users, machines, and hardware pools
 * for local testing of the web interface.
 *
 * Usage:
 *   node scripts/configure-fake-backend.mjs                     # Interactive mode
 *   node scripts/configure-fake-backend.mjs --scenario high-load # Switch scenario
 *   node scripts/configure-fake-backend.mjs --status            # Show current topology
 *   node scripts/configure-fake-backend.mjs --list-scenarios    # List all presets
 *   node scripts/configure-fake-backend.mjs --reset             # Reset to default
 */

import fs from 'node:fs'
import path from 'node:path'
import http from 'node:http'
import readline from 'node:readline/promises'
import { stdin as input, stdout as output } from 'node:process'
import { fileURLToPath } from 'node:url'
import {
  getScenario,
  SCENARIO_DESCRIPTIONS,
  SCENARIO_FACTORIES,
} from './fake-backend-scenarios.mjs'

const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const CONFIG_FILE = path.resolve(ROOT_DIR, '.fake-backend-config.json')

// Parse CLI flags
const argv = process.argv.slice(2)
function getFlag(name) {
  const idx = argv.indexOf(name)
  return idx !== -1 && argv[idx + 1] ? argv[idx + 1] : null
}
function hasFlag(name) {
  return argv.includes(name)
}

const PORT = Number(
  getFlag('--port') || process.env.PORT || process.env.MIST_FAKE_PORT || 3000,
)
const HOST = process.env.HOST || '127.0.0.1'

// --- Server Communication Helpers ---

function requestJSON(method, endpointPath, body = null) {
  return new Promise((resolve, reject) => {
    const data = body ? JSON.stringify(body) : null
    const req = http.request(
      {
        hostname: HOST,
        port: PORT,
        path: endpointPath,
        method,
        headers: {
          'Content-Type': 'application/json',
          ...(data ? { 'Content-Length': Buffer.byteLength(data) } : {}),
        },
        timeout: 1500,
      },
      (res) => {
        let resData = ''
        res.on('data', (chunk) => (resData += chunk))
        res.on('end', () => {
          try {
            resolve({
              status: res.statusCode,
              data: resData ? JSON.parse(resData) : null,
            })
          } catch (e) {
            resolve({ status: res.statusCode, raw: resData })
          }
        })
      },
    )
    req.on('timeout', () => {
      req.destroy()
      reject(new Error('Connection timed out'))
    })
    req.on('error', (err) => reject(err))
    if (data) req.write(data)
    req.end()
  })
}

async function checkLiveServer() {
  try {
    const res = await requestJSON('GET', '/_fake/state')
    if (res.status === 200 && res.data) {
      return res.data
    }
  } catch {}
  return null
}

function loadLocalConfigFile() {
  if (fs.existsSync(CONFIG_FILE)) {
    try {
      return JSON.parse(fs.readFileSync(CONFIG_FILE, 'utf-8'))
    } catch {}
  }
  return null
}

function saveLocalConfigFile(state) {
  fs.writeFileSync(CONFIG_FILE, JSON.stringify(state, null, 2), 'utf-8')
}

async function applyState(state) {
  // 1. Save to local config file
  saveLocalConfigFile(state)

  // 2. If live server running, update it dynamically
  const live = await checkLiveServer()
  if (live) {
    try {
      await requestJSON('POST', '/_fake/state', state)
      console.log(`\n✓ Live fake backend at http://${HOST}:${PORT} updated instantly!`)
      console.log(`✓ Saved configuration to .fake-backend-config.json`)
      return
    } catch (err) {
      console.warn(`\n(Live update failed: ${err.message})`)
    }
  }
  console.log(`\n✓ Saved configuration to .fake-backend-config.json`)
  console.log(`  (The fake backend is offline. Changes will take effect when it starts.)`)
}

function parseKeyValue(inputStr) {
  const result = {}
  const pairs = inputStr.split(',')
  for (const pair of pairs) {
    const [k, v] = pair.split('=').map((s) => s.trim())
    if (k && v !== undefined) {
      result[k] = v.replace(/^["']|["']$/g, '')
    }
  }
  return result
}

function printStatus(state) {
  console.log('\n======================================================')
  console.log(`Scenario:     ${state.activeScenario || state.name || 'custom'}`)
  const activeUser =
    state.users?.[state.currentUserIndex] || state.currentUser || null
  console.log(
    `Active User:  ${activeUser ? `${activeUser.name} <${activeUser.email}> [${activeUser.role}]` : 'None'}`,
  )
  console.log(`Total Users:  ${state.users?.length ?? 0}`)
  console.log('------------------------------------------------------')
  console.log('Machines & Nodes:')
  for (const m of state.machines || []) {
    const status = m.ready ? (m.schedulable ? 'Ready' : 'Placement Paused') : 'Offline'
    console.log(`  • ${m.name.padEnd(22)} ${m.address.padEnd(14)} [${status.padEnd(16)}] ${m.cpu_allocatable} CPU · ${m.memory_allocatable} RAM`)
  }
  console.log('Hardware Accelerator Pools:')
  for (const p of state.pools || []) {
    const avail = p.available === null ? 'Unknown' : p.available
    const msg = p.message ? ` (${p.message})` : ''
    console.log(`  • ${p.accelerator.toUpperCase().padEnd(12)} Node: ${p.node.padEnd(20)} Total: ${p.total}  Allocated: ${p.allocated}  Available: ${avail}${msg}`)
  }
  console.log('Teams:')
  for (const t of state.teams || []) {
    console.log(`  • ${t.name.padEnd(26)} Members: ${t.members?.length ?? 0}  Policy: ${t.policy?.storage_gib ?? 0} GiB Datasets / ${t.policy?.model_storage_gib ?? 0} GiB Models`)
  }
  console.log('Jobs in History:')
  const states = {}
  for (const j of state.jobs || []) {
    states[j.job_state] = (states[j.job_state] || 0) + 1
  }
  console.log(`  Total: ${state.jobs?.length ?? 0} (${Object.entries(states).map(([k, v]) => `${k}: ${v}`).join(', ') || '0'})`)
  console.log('======================================================\n')
}

// --- CLI Commands ---

async function main() {
  if (hasFlag('--help') || hasFlag('-h')) {
    console.log(`
Mist Fake Backend Configurator

Commands & Options:
  --scenario <name>       Load a preset scenario (default, high-load, unhealthy, empty-slate, large-cluster)
  --list-scenarios        List all available built-in scenarios
  --status                Display current cluster topology, machines, users, and jobs
  --set-user <id|email>   Set which user is active in the web session
  --add-user <kv>         Add a user: name="...",email="...",role=admin|user
  --add-machine <kv>      Add a machine: name="...",ip="...",cpu=32,ram=128Gi,gpu=4,tt=0
  --add-team <kv>         Add a team: name="...",storage=300,models=600
  --config <path>         Load configuration from a JSON file
  --export <path>         Export active configuration to a JSON file
  --reset                 Reset back to the default scenario
  --port <number>         Target port (default: 3000)

Run with no flags for the interactive menu.
`)
    return
  }

  if (hasFlag('--list-scenarios')) {
    console.log('\nAvailable Hypothetical Scenarios:')
    for (const [name, desc] of Object.entries(SCENARIO_DESCRIPTIONS)) {
      console.log(`  • ${name.padEnd(16)} : ${desc}`)
    }
    console.log('')
    return
  }

  // Current state resolution
  let currentState = (await checkLiveServer()) || loadLocalConfigFile() || getScenario('default')

  if (hasFlag('--status')) {
    const live = await checkLiveServer()
    console.log(live ? `[Backend Server: ONLINE at http://${HOST}:${PORT}]` : `[Backend Server: OFFLINE (reading config file)]`)
    printStatus(currentState)
    return
  }

  if (hasFlag('--scenario')) {
    const name = getFlag('--scenario')
    try {
      const scenario = getScenario(name)
      await applyState(scenario)
      printStatus(scenario)
    } catch (err) {
      console.error(`Error: ${err.message}`)
      process.exit(1)
    }
    return
  }

  if (hasFlag('--reset')) {
    const defaultScenario = getScenario('default')
    await applyState(defaultScenario)
    console.log('Reset to default scenario successfully.')
    return
  }

  if (hasFlag('--config')) {
    const filePath = getFlag('--config')
    if (!fs.existsSync(filePath)) {
      console.error(`File not found: ${filePath}`)
      process.exit(1)
    }
    const data = JSON.parse(fs.readFileSync(filePath, 'utf-8'))
    await applyState(data)
    console.log(`Loaded configuration from ${filePath}.`)
    return
  }

  if (hasFlag('--export')) {
    const outPath = getFlag('--export')
    fs.writeFileSync(outPath, JSON.stringify(currentState, null, 2), 'utf-8')
    console.log(`Exported configuration to ${outPath}.`)
    return
  }

  if (hasFlag('--set-user')) {
    const target = getFlag('--set-user')
    const idx = (currentState.users || []).findIndex(
      (u) => u.id === target || u.email.toLowerCase() === target.toLowerCase(),
    )
    if (idx === -1) {
      console.error(`User '${target}' not found. Available: ${currentState.users.map((u) => u.email).join(', ')}`)
      process.exit(1)
    }
    currentState.currentUserIndex = idx
    await applyState(currentState)
    console.log(`Active user set to: ${currentState.users[idx].name} (${currentState.users[idx].email})`)
    return
  }

  if (hasFlag('--add-user')) {
    const kv = parseKeyValue(getFlag('--add-user'))
    if (!kv.name || !kv.email) {
      console.error('Error: name and email are required (e.g. name="Marie Curie",email="curie@rad.edu",role=admin)')
      process.exit(1)
    }
    const newUser = {
      id: 'usr_' + Date.now().toString(36),
      name: kv.name,
      email: kv.email,
      role: kv.role || 'user',
      banned: false,
    }
    currentState.users = currentState.users || []
    currentState.users.push(newUser)
    await applyState(currentState)
    console.log(`Added user: ${newUser.name} <${newUser.email}> (${newUser.role})`)
    return
  }

  if (hasFlag('--add-machine')) {
    const kv = parseKeyValue(getFlag('--add-machine'))
    if (!kv.name) {
      console.error('Error: machine name is required (e.g. name=dgx-a100,ip=10.0.1.50,cpu=64,ram=256Gi,gpu=8)')
      process.exit(1)
    }
    const newMachine = {
      name: kv.name,
      address: kv.ip || kv.address || '10.0.1.' + Math.floor(Math.random() * 200 + 20),
      ready: kv.ready !== 'false',
      schedulable: kv.schedulable !== 'false',
      cpu_allocatable: kv.cpu || '32',
      memory_allocatable: kv.ram || '128Gi',
    }
    currentState.machines = currentState.machines || []
    currentState.machines.push(newMachine)

    // Add GPU or TT pools if requested
    currentState.pools = currentState.pools || []
    if (Number(kv.gpu) > 0) {
      currentState.pools.push({
        accelerator: 'nvidia',
        node: newMachine.name,
        unit: 'GPU',
        total: Number(kv.gpu),
        allocated: 0,
        available: Number(kv.gpu),
        ready: true,
      })
    }
    if (Number(kv.tt) > 0) {
      currentState.pools.push({
        accelerator: 'tenstorrent',
        node: newMachine.name,
        unit: 'board',
        total: Number(kv.tt),
        allocated: 0,
        available: Number(kv.tt),
        ready: true,
        chips_per_device: 2,
      })
    }
    await applyState(currentState)
    console.log(`Added machine: ${newMachine.name} with ${kv.gpu || 0} GPUs and ${kv.tt || 0} Tenstorrent boards.`)
    return
  }

  if (hasFlag('--add-team')) {
    const kv = parseKeyValue(getFlag('--add-team'))
    if (!kv.name) {
      console.error('Error: team name is required (e.g. name="Quantum ML",storage=500,models=1000)')
      process.exit(1)
    }
    const activeUser = currentState.users?.[currentState.currentUserIndex]
    const newTeam = {
      id: 'team-' + kv.name.toLowerCase().replace(/\W+/g, '-'),
      name: kv.name,
      disabled: false,
      policy: {
        cpu: kv.cpu || '16',
        memory: kv.memory || '64Gi',
        nvidia: Number(kv.gpu ?? 2),
        tenstorrent: Number(kv.tt ?? 2),
        concurrent: 4,
        queued: 20,
        storage_gib: Number(kv.storage || 300),
        model_storage_gib: Number(kv.models || 600),
        runtime_seconds: 86400,
        registries: ['docker.io', 'ghcr.io'],
      },
      members: activeUser
        ? [{ id: activeUser.id, name: activeUser.name, email: activeUser.email, common_writer: true }]
        : [],
      grants: [],
    }
    currentState.teams = currentState.teams || []
    currentState.teams.push(newTeam)
    await applyState(currentState)
    console.log(`Added team: ${newTeam.name}`)
    return
  }

  // --- Interactive Terminal Menu ---
  const rl = readline.createInterface({ input, output })
  try {
    while (true) {
      const live = await checkLiveServer()
      console.clear()
      console.log('======================================================')
      console.log('           MIST FAKE BACKEND CONFIGURATOR')
      console.log('======================================================')
      console.log(`Backend Server: ${live ? `ONLINE (http://${HOST}:${PORT})` : 'OFFLINE (saving to config file)'}`)
      console.log(`Active Scenario: ${currentState.activeScenario || currentState.name || 'custom'}`)
      const u = currentState.users?.[currentState.currentUserIndex]
      console.log(`Active User:     ${u ? `${u.name} <${u.email}> (${u.role})` : 'None'}`)
      console.log(`Machines:        ${currentState.machines?.map((m) => m.name).join(', ') || 'None'}`)
      console.log(`Teams:           ${currentState.teams?.map((t) => t.name).join(', ') || 'None'}`)
      console.log('------------------------------------------------------')
      console.log('Select an action:')
      console.log('  1) Switch to a hypothetical scenario preset')
      console.log('  2) Switch active logged-in user')
      console.log('  3) Add a fake machine / node')
      console.log('  4) Add a fake user')
      console.log('  5) Add a fake team')
      console.log('  6) View full cluster status')
      console.log('  7) Reset to default scenario')
      console.log('  8) Exit')
      console.log('------------------------------------------------------')

      const choice = (await rl.question('Enter choice [1-8]: ')).trim()

      if (choice === '1') {
        console.log('\nAvailable Hypothetical Scenarios:')
        const keys = Object.keys(SCENARIO_DESCRIPTIONS)
        keys.forEach((key, i) => {
          console.log(`  ${i + 1}) ${key.padEnd(16)} : ${SCENARIO_DESCRIPTIONS[key]}`)
        })
        const sel = (await rl.question('\nSelect scenario [1-5]: ')).trim()
        const idx = parseInt(sel, 10) - 1
        if (keys[idx]) {
          currentState = getScenario(keys[idx])
          await applyState(currentState)
          await rl.question('\nPress Enter to continue...')
        }
      } else if (choice === '2') {
        console.log('\nAvailable Users:')
        currentState.users.forEach((user, i) => {
          const mark = i === currentState.currentUserIndex ? ' (ACTIVE)' : ''
          console.log(`  ${i + 1}) ${user.name} <${user.email}> [${user.role}]${mark}`)
        })
        const sel = (await rl.question('\nSelect user number: ')).trim()
        const idx = parseInt(sel, 10) - 1
        if (currentState.users[idx]) {
          currentState.currentUserIndex = idx
          await applyState(currentState)
        }
      } else if (choice === '3') {
        console.log('\n--- Add Fake Machine ---')
        const name = (await rl.question('Machine name (e.g. dgx-h100-01): ')).trim()
        if (!name) continue
        const ip = (await rl.question('IP Address (default 10.0.1.25): ')).trim() || '10.0.1.25'
        const cpu = (await rl.question('Allocatable CPU cores (default 32): ')).trim() || '32'
        const ram = (await rl.question('Allocatable RAM (default 128Gi): ')).trim() || '128Gi'
        const gpu = parseInt((await rl.question('NVIDIA GPUs count (default 0): ')).trim() || '0', 10)
        const tt = parseInt((await rl.question('Tenstorrent boards count (default 0): ')).trim() || '0', 10)

        const machine = {
          name,
          address: ip,
          ready: true,
          schedulable: true,
          cpu_allocatable: cpu,
          memory_allocatable: ram,
        }
        currentState.machines = currentState.machines || []
        currentState.machines.push(machine)

        currentState.pools = currentState.pools || []
        if (gpu > 0) {
          currentState.pools.push({
            accelerator: 'nvidia',
            node: name,
            unit: 'GPU',
            total: gpu,
            allocated: 0,
            available: gpu,
            ready: true,
          })
        }
        if (tt > 0) {
          currentState.pools.push({
            accelerator: 'tenstorrent',
            node: name,
            unit: 'board',
            total: tt,
            allocated: 0,
            available: tt,
            ready: true,
            chips_per_device: 2,
          })
        }
        await applyState(currentState)
        await rl.question('\nMachine added! Press Enter to continue...')
      } else if (choice === '4') {
        console.log('\n--- Add Fake User ---')
        const name = (await rl.question('Full name (e.g. Marie Curie): ')).trim()
        if (!name) continue
        const email = (await rl.question('Email (e.g. marie@utmist.gitlab.io): ')).trim()
        if (!email) continue
        const role = (await rl.question('Role (admin / user, default user): ')).trim() || 'user'
        currentState.users = currentState.users || []
        currentState.users.push({
          id: 'usr_' + Date.now().toString(36),
          name,
          email,
          role,
          banned: false,
        })
        await applyState(currentState)
        await rl.question('\nUser added! Press Enter to continue...')
      } else if (choice === '5') {
        console.log('\n--- Add Fake Team ---')
        const name = (await rl.question('Team name (e.g. Quantum Computing Lab): ')).trim()
        if (!name) continue
        const storage = parseInt((await rl.question('Dataset storage in GiB (default 300): ')).trim() || '300', 10)
        const models = parseInt((await rl.question('Model storage in GiB (default 600): ')).trim() || '600', 10)
        const activeUser = currentState.users?.[currentState.currentUserIndex]

        const newTeam = {
          id: 'team-' + name.toLowerCase().replace(/\W+/g, '-'),
          name,
          disabled: false,
          policy: {
            cpu: '16',
            memory: '64Gi',
            nvidia: 2,
            tenstorrent: 2,
            concurrent: 4,
            queued: 20,
            storage_gib: storage,
            model_storage_gib: models,
            runtime_seconds: 86400,
            registries: ['docker.io', 'ghcr.io'],
          },
          members: activeUser
            ? [{ id: activeUser.id, name: activeUser.name, email: activeUser.email, common_writer: true }]
            : [],
          grants: [],
        }
        currentState.teams = currentState.teams || []
        currentState.teams.push(newTeam)
        await applyState(currentState)
        await rl.question('\nTeam added! Press Enter to continue...')
      } else if (choice === '6') {
        printStatus(currentState)
        await rl.question('\nPress Enter to return to menu...')
      } else if (choice === '7') {
        currentState = getScenario('default')
        await applyState(currentState)
        await rl.question('\nReset to default scenario! Press Enter to continue...')
      } else if (choice === '8') {
        break
      }
    }
  } finally {
    rl.close()
  }
}

main().catch((err) => {
  console.error('Fatal error:', err)
  process.exit(1)
})
