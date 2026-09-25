import type { BaseMachine } from '#/types/machine.ts'

export type BaseJob = {
  id: string
  created: Date
  accessed: Date
  dockerImage: string
  usageHistory: UsageData[] // the usage of the resources of the machine allocated to this specific job
}

export type UsageData = {
  component: string // Component that we observe. E.g. GPU, CPU, etc.
  observations: number[]
}

export type Job = BaseJob & { machine: BaseMachine }
