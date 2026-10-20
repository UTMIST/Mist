/**
 * Mist Fake Backend - Built-in Hypothetical Scenarios
 *
 * Provides modular scenario generators for testing various cluster topologies,
 * load conditions, failure modes, and organizational structures.
 */

export const SCENARIO_DESCRIPTIONS = {
  default: 'Standard balanced cluster with 1 GPU node, 1 Tenstorrent node, 2 teams, and 3 users.',
  'high-load': 'Resource-exhausted cluster: 100% GPU/TT saturation, long waiting queue, and 95% full storage.',
  unhealthy: 'Degraded cluster with offline nodes, failed jobs, driver error messages, and storage errors.',
  'empty-slate': 'Clean empty cluster with 0 jobs, 0 datasets, and 0 teams for onboarding / first-run testing.',
  'large-cluster': 'Multi-node enterprise setup with 5 machines (H100s, Wormholes), 5 teams, and 30+ jobs.',
}

export function createDefaultScenario() {
  const now = Date.now()
  return {
    name: 'default',
    description: SCENARIO_DESCRIPTIONS.default,
    currentUserIndex: 0,
    storageBudgetGiB: 2000,
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
        allocated: 1,
        available: 3,
        ready: true,
      },
      {
        accelerator: 'tenstorrent',
        node: 'mist-tt-worker-01',
        unit: 'board',
        total: 4,
        allocated: 0,
        available: 4,
        ready: true,
        chips_per_device: 2,
      },
    ],
    storage: {
      enabled: true,
      capacity_bytes: 1099511627776, // 1 TiB
      available_bytes: 879609302220, // 800 GiB
      model_capacity_bytes: 2199023255552, // 2 TiB
      model_available_bytes: 1759218604441, // 1.6 TiB
      dataset_allocation_gib: 500,
      model_allocation_gib: 1000,
      upload_limit_bytes: 5368709120, // 5 GiB
    },
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
    jobs: [
      {
        id: 'job-seed-001',
        name: 'ResNet50 Vision Benchmark',
        created: new Date(now - 3600000).toISOString(),
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
        created: new Date(now - 1800000).toISOString(),
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
        created: new Date(now - 600000).toISOString(),
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
        created: new Date(now - 86400000).toISOString(),
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
        created: new Date(now - 43200000).toISOString(),
        state: 'ready',
        team_id: 'team-nlp',
        team_name: 'Natural Language Processing',
        scope: 'common',
        member_name: 'Grace Hopper',
        shared: true,
      },
    ],
  }
}

export function createHighLoadScenario() {
  const now = Date.now()
  const base = createDefaultScenario()
  base.name = 'high-load'
  base.description = SCENARIO_DESCRIPTIONS['high-load']

  // Saturated hardware: 0 available
  base.pools = [
    {
      accelerator: 'nvidia',
      node: 'mist-gpu-node-01',
      unit: 'GPU',
      total: 4,
      allocated: 4,
      available: 0,
      ready: true,
    },
    {
      accelerator: 'tenstorrent',
      node: 'mist-tt-worker-01',
      unit: 'board',
      total: 4,
      allocated: 4,
      available: 0,
      ready: true,
      chips_per_device: 2,
    },
  ]

  // Storage 95% consumed
  base.storage = {
    enabled: true,
    capacity_bytes: 1073741824000, // 1 TiB
    available_bytes: 32212254720, // 30 GiB left (near capacity!)
    model_capacity_bytes: 2147483648000,
    model_available_bytes: 53687091200, // 50 GiB left
    dataset_allocation_gib: 980,
    model_allocation_gib: 1950,
    upload_limit_bytes: 2147483648,
  }

  // 4 running jobs taking all accelerators + 8 queued jobs
  base.jobs = [
    {
      id: 'job-hl-run-01',
      name: 'LLM Multi-GPU Pretraining Node 1',
      created: new Date(now - 7200000).toISOString(),
      job_state: 'InProgress',
      accelerator: 'nvidia',
      device_count: 2,
      image: 'ghcr.io/utmist/pytorch-cuda:latest',
      cpu: '16',
      memory: '64Gi',
      node: 'mist-gpu-node-01',
      checkpoint_directory: '/outputs/checkpoints/job-hl-run-01',
      output_directory: '/outputs/runs/job-hl-run-01',
      team_id: 'team-vision',
      creator_name: 'Ada Lovelace',
      can_cancel: true,
    },
    {
      id: 'job-hl-run-02',
      name: 'Diffusion Video Upscaler',
      created: new Date(now - 3600000).toISOString(),
      job_state: 'InProgress',
      accelerator: 'nvidia',
      device_count: 2,
      image: 'ghcr.io/utmist/pytorch-cuda:latest',
      cpu: '16',
      memory: '64Gi',
      node: 'mist-gpu-node-01',
      checkpoint_directory: '/outputs/checkpoints/job-hl-run-02',
      output_directory: '/outputs/runs/job-hl-run-02',
      team_id: 'team-nlp',
      creator_name: 'Grace Hopper',
      can_cancel: true,
    },
    {
      id: 'job-hl-run-03',
      name: 'TT Wormhole Full-Board Stress Test',
      created: new Date(now - 5400000).toISOString(),
      job_state: 'InProgress',
      accelerator: 'tenstorrent',
      device_count: 4,
      image: 'ghcr.io/utmist/tt-metal-runtime:latest',
      cpu: '16',
      memory: '64Gi',
      node: 'mist-tt-worker-01',
      checkpoint_directory: '/outputs/checkpoints/job-hl-run-03',
      output_directory: '/outputs/runs/job-hl-run-03',
      team_id: 'team-vision',
      creator_name: 'Alan Turing',
      can_cancel: true,
    },
    // Queued jobs
    ...Array.from({ length: 8 }, (_, i) => ({
      id: `job-hl-wait-0${i + 1}`,
      name: `Queued Experiment #${i + 1}`,
      created: new Date(now - (8 - i) * 300000).toISOString(),
      job_state: 'Scheduled',
      accelerator: i % 2 === 0 ? 'nvidia' : 'tenstorrent',
      device_count: (i % 2) + 1,
      image: i % 2 === 0 ? 'ghcr.io/utmist/pytorch-cuda:latest' : 'ghcr.io/utmist/tt-metal-runtime:latest',
      cpu: '4',
      memory: '16Gi',
      message: `Waiting for resources (Position in queue: ${i + 1})`,
      checkpoint_directory: `/outputs/checkpoints/wait-${i + 1}`,
      output_directory: `/outputs/runs/wait-${i + 1}`,
      team_id: i % 2 === 0 ? 'team-vision' : 'team-nlp',
      creator_name: i % 3 === 0 ? 'Ada Lovelace' : i % 3 === 1 ? 'Alan Turing' : 'Grace Hopper',
      can_cancel: true,
    })),
  ]

  return base
}

export function createUnhealthyScenario() {
  const now = Date.now()
  const base = createDefaultScenario()
  base.name = 'unhealthy'
  base.description = SCENARIO_DESCRIPTIONS.unhealthy

  base.machines = [
    {
      name: 'mist-gpu-node-01',
      address: '10.0.1.10',
      ready: false,
      schedulable: false,
      cpu_allocatable: '0',
      memory_allocatable: '0Gi',
    },
    {
      name: 'mist-tt-worker-01',
      address: '10.0.1.11',
      ready: true,
      schedulable: false, // Placement paused!
      cpu_allocatable: '16',
      memory_allocatable: '64Gi',
    },
  ]

  base.pools = [
    {
      accelerator: 'nvidia',
      node: 'mist-gpu-node-01',
      unit: 'GPU',
      total: 4,
      allocated: 0,
      available: null,
      ready: false,
      message: 'Node offline; NVIDIA driver communication lost (NVML error 999)',
    },
    {
      accelerator: 'tenstorrent',
      node: 'mist-tt-worker-01',
      unit: 'board',
      total: 4,
      allocated: 0,
      available: 0,
      ready: false,
      chips_per_device: 2,
      message: 'Node cordon active: scheduling paused for driver kernel patch',
    },
  ]

  base.jobs = [
    {
      id: 'job-fail-001',
      name: 'Vision Model OOM Failure',
      created: new Date(now - 7200000).toISOString(),
      job_state: 'Failure',
      accelerator: 'nvidia',
      device_count: 2,
      image: 'ghcr.io/utmist/pytorch-cuda:latest',
      cpu: '8',
      memory: '16Gi',
      node: 'mist-gpu-node-01',
      exit_code: 137,
      message: 'Command terminated by SIGKILL (Out of Memory - cgroup limit exceeded)',
      checkpoint_directory: '/outputs/checkpoints/job-fail-001',
      output_directory: '/outputs/runs/job-fail-001',
      team_id: 'team-vision',
      creator_name: 'Ada Lovelace',
      can_cancel: false,
    },
    {
      id: 'job-fail-002',
      name: 'Faulty Python Script Crash',
      created: new Date(now - 3600000).toISOString(),
      job_state: 'Failure',
      accelerator: 'cpu',
      device_count: 0,
      image: 'docker.io/library/python:3.11-slim',
      cpu: '2',
      memory: '4Gi',
      node: 'mist-tt-worker-01',
      exit_code: 1,
      message: 'RuntimeError: ModuleNotFoundError: No module named torch_geometric',
      checkpoint_directory: '/outputs/checkpoints/job-fail-002',
      output_directory: '/outputs/runs/job-fail-002',
      team_id: 'team-nlp',
      creator_name: 'Grace Hopper',
      can_cancel: false,
    },
    {
      id: 'job-stuck-003',
      name: 'Stuck Queued Job',
      created: new Date(now - 1800000).toISOString(),
      job_state: 'Scheduled',
      accelerator: 'nvidia',
      device_count: 1,
      image: 'ghcr.io/utmist/pytorch-cuda:latest',
      cpu: '4',
      memory: '16Gi',
      message: '0/2 nodes are available: 1 node is offline, 1 node has untolerated taint',
      checkpoint_directory: '/outputs/checkpoints/job-stuck-003',
      output_directory: '/outputs/runs/job-stuck-003',
      team_id: 'team-vision',
      creator_name: 'Alan Turing',
      can_cancel: true,
    },
  ]

  return base
}

export function createEmptySlateScenario() {
  return {
    name: 'empty-slate',
    description: SCENARIO_DESCRIPTIONS['empty-slate'],
    currentUserIndex: 0,
    storageBudgetGiB: 1000,
    users: [
      {
        id: 'usr_newcomer',
        name: 'New Researcher',
        email: 'researcher@utmist.gitlab.io',
        role: 'admin',
        banned: false,
      },
    ],
    machines: [
      {
        name: 'lab-cluster-master',
        address: '192.168.1.10',
        ready: true,
        schedulable: true,
        cpu_allocatable: '16',
        memory_allocatable: '64Gi',
      },
    ],
    pools: [
      {
        accelerator: 'nvidia',
        node: 'lab-cluster-master',
        unit: 'GPU',
        total: 2,
        allocated: 0,
        available: 2,
        ready: true,
      },
    ],
    storage: {
      enabled: true,
      capacity_bytes: 536870912000, // 500 GiB
      available_bytes: 536870912000,
      model_capacity_bytes: 1073741824000,
      model_available_bytes: 1073741824000,
      dataset_allocation_gib: 0,
      model_allocation_gib: 0,
      upload_limit_bytes: 5368709120,
    },
    teams: [],
    jobs: [],
    datasets: [],
  }
}

export function createLargeClusterScenario() {
  const now = Date.now()
  const base = createDefaultScenario()
  base.name = 'large-cluster'
  base.description = SCENARIO_DESCRIPTIONS['large-cluster']
  base.storageBudgetGiB = 10000

  base.users = [
    { id: 'usr_ada', name: 'Ada Lovelace', email: 'ada@utmist.gitlab.io', role: 'admin', banned: false },
    { id: 'usr_alan', name: 'Alan Turing', email: 'alan@utmist.gitlab.io', role: 'user', banned: false },
    { id: 'usr_grace', name: 'Grace Hopper', email: 'grace@utmist.gitlab.io', role: 'user', banned: false },
    { id: 'usr_curie', name: 'Marie Curie', email: 'marie@utmist.gitlab.io', role: 'admin', banned: false },
    { id: 'usr_feynman', name: 'Richard Feynman', email: 'feynman@utmist.gitlab.io', role: 'user', banned: false },
    { id: 'usr_shannon', name: 'Claude Shannon', email: 'shannon@utmist.gitlab.io', role: 'user', banned: false },
    { id: 'usr_neumann', name: 'John von Neumann', email: 'neumann@utmist.gitlab.io', role: 'user', banned: true },
  ]

  base.machines = [
    { name: 'dgx-h100-node-01', address: '10.10.1.1', ready: true, schedulable: true, cpu_allocatable: '64', memory_allocatable: '512Gi' },
    { name: 'dgx-h100-node-02', address: '10.10.1.2', ready: true, schedulable: true, cpu_allocatable: '64', memory_allocatable: '512Gi' },
    { name: 'tt-wormhole-box-01', address: '10.10.1.3', ready: true, schedulable: true, cpu_allocatable: '32', memory_allocatable: '128Gi' },
    { name: 'tt-wormhole-box-02', address: '10.10.1.4', ready: true, schedulable: true, cpu_allocatable: '32', memory_allocatable: '128Gi' },
    { name: 'control-plane-k3s', address: '10.10.1.100', ready: true, schedulable: true, cpu_allocatable: '16', memory_allocatable: '64Gi' },
  ]

  base.pools = [
    { accelerator: 'nvidia', node: 'dgx-h100-node-01', unit: 'GPU', total: 8, allocated: 4, available: 4, ready: true },
    { accelerator: 'nvidia', node: 'dgx-h100-node-02', unit: 'GPU', total: 8, allocated: 6, available: 2, ready: true },
    { accelerator: 'tenstorrent', node: 'tt-wormhole-box-01', unit: 'board', total: 4, allocated: 2, available: 2, ready: true, chips_per_device: 2 },
    { accelerator: 'tenstorrent', node: 'tt-wormhole-box-02', unit: 'board', total: 4, allocated: 1, available: 3, ready: true, chips_per_device: 2 },
  ]

  base.teams = [
    {
      id: 'team-cv',
      name: 'Computer Vision Lab',
      disabled: false,
      policy: { cpu: '32', memory: '128Gi', nvidia: 8, tenstorrent: 4, concurrent: 8, queued: 40, storage_gib: 1000, model_storage_gib: 2000, runtime_seconds: 86400, registries: ['docker.io', 'ghcr.io'] },
      members: [{ id: 'usr_ada', name: 'Ada Lovelace', email: 'ada@utmist.gitlab.io', common_writer: true }, { id: 'usr_curie', name: 'Marie Curie', email: 'marie@utmist.gitlab.io', common_writer: true }],
      grants: [],
    },
    {
      id: 'team-llm',
      name: 'Foundation Models & NLP',
      disabled: false,
      policy: { cpu: '64', memory: '256Gi', nvidia: 8, tenstorrent: 2, concurrent: 6, queued: 30, storage_gib: 2000, model_storage_gib: 4000, runtime_seconds: 86400, registries: ['docker.io', 'ghcr.io'] },
      members: [{ id: 'usr_grace', name: 'Grace Hopper', email: 'grace@utmist.gitlab.io', common_writer: true }, { id: 'usr_shannon', name: 'Claude Shannon', email: 'shannon@utmist.gitlab.io', common_writer: false }],
      grants: [],
    },
    {
      id: 'team-robotics',
      name: 'Autonomous Robotics',
      disabled: false,
      policy: { cpu: '16', memory: '64Gi', nvidia: 4, tenstorrent: 4, concurrent: 4, queued: 20, storage_gib: 800, model_storage_gib: 1200, runtime_seconds: 43200, registries: ['docker.io'] },
      members: [{ id: 'usr_alan', name: 'Alan Turing', email: 'alan@utmist.gitlab.io', common_writer: true }, { id: 'usr_feynman', name: 'Richard Feynman', email: 'feynman@utmist.gitlab.io', common_writer: false }],
      grants: [],
    },
  ]

  // Generate 25 jobs across all states
  const jobStates = ['Success', 'InProgress', 'Scheduled', 'Failure', 'Cancelled']
  base.jobs = Array.from({ length: 25 }, (_, i) => ({
    id: `job-enterprise-${String(i + 1).padStart(3, '0')}`,
    name: `Model Exploration #${i + 1}`,
    created: new Date(now - (25 - i) * 1200000).toISOString(),
    job_state: jobStates[i % jobStates.length],
    accelerator: i % 3 === 0 ? 'nvidia' : i % 3 === 1 ? 'tenstorrent' : 'cpu',
    device_count: i % 3 === 2 ? 0 : (i % 4) + 1,
    image: i % 3 === 0 ? 'ghcr.io/utmist/pytorch-cuda:latest' : i % 3 === 1 ? 'ghcr.io/utmist/tt-metal-runtime:latest' : 'docker.io/library/python:3.11-slim',
    cpu: String((i % 4 + 1) * 4),
    memory: `${(i % 4 + 1) * 16}Gi`,
    node: i % 2 === 0 ? 'dgx-h100-node-01' : 'tt-wormhole-box-01',
    exit_code: jobStates[i % jobStates.length] === 'Failure' ? 1 : jobStates[i % jobStates.length] === 'Success' ? 0 : undefined,
    message: jobStates[i % jobStates.length] === 'Failure' ? 'Killed by cluster controller' : undefined,
    checkpoint_directory: `/outputs/checkpoints/job-enterprise-${i + 1}`,
    output_directory: `/outputs/runs/job-enterprise-${i + 1}`,
    team_id: i % 2 === 0 ? 'team-cv' : 'team-llm',
    creator_name: base.users[i % base.users.length].name,
    can_cancel: jobStates[i % jobStates.length] === 'InProgress' || jobStates[i % jobStates.length] === 'Scheduled',
  }))

  return base
}

export const SCENARIO_FACTORIES = {
  default: createDefaultScenario,
  'high-load': createHighLoadScenario,
  unhealthy: createUnhealthyScenario,
  'empty-slate': createEmptySlateScenario,
  'large-cluster': createLargeClusterScenario,
}

export function getScenario(name) {
  const factory = SCENARIO_FACTORIES[name]
  if (!factory) {
    throw new Error(`Unknown scenario '${name}'. Available: ${Object.keys(SCENARIO_FACTORIES).join(', ')}`)
  }
  return factory()
}
