import type {BaseMachine} from "#/types/machine.ts";

export type BaseJob = {
  id: string
  name: string
  created: string
  accessed: string
  dockerImage: string
  usageHistory: UsageData[]
}

export type UsageData = {
  component: string // Component that we observe. E.g. GPU, CPU, etc.
  observations: number[]
}

export type Job = BaseJob & { machine: BaseMachine }