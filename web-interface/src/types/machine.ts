import type { BaseJob, UsageData } from '#/types/job.ts'

export type BaseMachine = {
  id: string
  isAvailable: boolean
  purpose: string
  gpu: string
  cpu: string
  diskUsage: string
  ramUsage: string
  cpuUsage: string
  network: { down: string; up: string }
  ip: string
  usageHistory: UsageData[] // the usage of the total machine resources
}

export type Machine = BaseMachine & { jobs: BaseJob[] }
