import type { BaseJob } from '#/types/job.ts'
import type {UsageData} from "#/types/usageData.ts";

export type BaseMachine = {
  id: string
  gpu: string
  cpu: string
  diskUsage: string
  ramUsage: string
  cpuUsage: string
  network: { down: string; up: string }
  ip: string
  usageHistory: UsageData // the usage of the total machine resources
}

export type Machine = BaseMachine & { jobs: BaseJob[] }
