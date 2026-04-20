export type UsageData = {
  component: string // Component that we observe. E.g. GPU, CPU, etc.
  observations: number[]
}