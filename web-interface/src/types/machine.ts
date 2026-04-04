import type {BaseJob} from "#/types/job.ts";

export type BaseMachine = {
  id: string
  gpu: string
  cpu: string
  jobs: string[]
  diskUsage: string
  ramUsage: string
  cpuUsage: string
  network: { down: string, up: string }
  ip: string
}

export type Machine = BaseMachine & { jobs: BaseJob[] }