import type { BaseMachine } from '#/types/machine.ts'
import type {UsageData} from "#/types/usageData.ts";

export type BaseJob = {
  id: string
  created: Date
  accessed: Date
  dockerImage: string
  usageHistory: UsageData[] // the usage of the resources of the machine allocated to this specific job
}

export type Job = BaseJob & { machine: BaseMachine }
