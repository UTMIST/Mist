import type { Alert } from '#/types/alert.ts'
import type { Job } from '#/types/job.ts'
import type { Machine } from '#/types/machine.ts'

export type User = {
  username: string
  role: string
  profilePicture: string
  email: string
}

export function getUser(): User {
  // Return user information

  // TODO: Call API - We use sample data for now
  const data = generateSampleData()

  return data.user
}

export function logout() {
  // TODO: Call API - we only log a message for now
  console.log('logout called')
}

function generateSampleUsageData(): number[] {
  return Array.from({ length: 25 }, (_, i) => {
    if (i < 6) return 5 + Math.random() * 10
    if (i < 10) return 10 + Math.random() * 20
    if (i < 14) return 50 + Math.random() * 45
    if (i < 18) return 70 + Math.random() * 25
    return 30 + Math.random() * 30
  })
}

type SampleData = {
  jobs: Job[]
  machines: Machine[]
  alerts: Alert[]
  user: User
}

export function generateSampleData(): SampleData {
  const machines: Machine[] = [
    {
      id: 'tenstorrent_1',
      isAvailable: true,
      purpose: 'Inference',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'tenstorrent_2',
      isAvailable: true,
      purpose: 'Inference',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '23GB/128GB (55%)',
      cpuUsage: '5%',
      ramUsage: '6.7GB/32GB (52%)',
      network: {
        down: '5 GB/s',
        up: '32 MB/s',
      },
      ip: '11.22.33.45',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'tenstorrent_3',
      isAvailable: false,
      purpose: 'Inference',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'tenstorrent_4',
      isAvailable: false,
      purpose: 'Training',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'tenstorrent_5',
      isAvailable: true,
      purpose: 'Training',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'tenstorrent_6',
      isAvailable: false,
      purpose: 'Inference',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
  ]

  const jobs: Job[] = [
    {
      id: 'f3xkcd',
      created: new Date('2026-03-01T22:32:00'),
      accessed: new Date('2026-03-05T11:01:00'),
      machine: machines[0],
      dockerImage: 'utmist/mpt-3.5-turbo',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'a1b2c3',
      created: new Date('2026-03-01T22:32:00'),
      accessed: new Date('2026-03-05T11:01:00'),
      machine: machines[0],
      dockerImage: 'utmist/mpt-3.5-turbo',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'x9y8z7',
      created: new Date('2026-03-01T22:32:00'),
      accessed: new Date('2026-03-05T11:01:00'),
      machine: machines[2],
      dockerImage: 'utmist/mpt-3.5-turbo',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
    {
      id: 'm4n5p6',
      created: new Date('2026-03-01T22:32:00'),
      accessed: new Date('2026-03-05T11:01:00'),
      machine: machines[2],
      dockerImage: 'utmist/mpt-3.5-turbo',
      usageHistory: {
        gpu: generateSampleUsageData(),
        cpu: generateSampleUsageData(),
        ram: generateSampleUsageData(),
      },
    },
  ]

  machines[0].jobs = [jobs[0], jobs[1]]
  machines[2].jobs = [jobs[2], jobs[3]]

  const user = {
    username: 'TheArchons',
    profilePicture: '/sample-avatar.png', // real avatars should probably be stored in a bucket
    role: 'Software Developer',
    email: 'thearchons@utmist.ca',
  }

  const alerts: Alert[] = [
    {
      message:
        'A4000_3 is currently down for maintenance. Expect maintenance until 2026-05-06 11:00pm',
      severity: 'high',
    },
    {
      message:
        'A4000_4 scheduled for downtime from 2026-03-15 11:00pm to 2026-03-16 6:00am',
      severity: 'medium',
    },
    {
      message: "Tenstorrent_1's maintenance has been completed",
      severity: 'low',
    },
  ]

  return {
    jobs: jobs,
    machines: machines,
    user: user,
    alerts: alerts,
  }
}
