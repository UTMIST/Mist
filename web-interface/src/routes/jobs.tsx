import { createFileRoute } from '@tanstack/react-router'
import { JobsPage } from '#/components/JobsPage.tsx'

export const Route = createFileRoute('/jobs')({ component: JobsPage })
export type UsageData = { component: string; observations: number[] }
