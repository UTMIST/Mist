import { createFileRoute } from '@tanstack/react-router'
import { TeamsPage } from '#/components/TeamsPage.tsx'

export const Route = createFileRoute('/teams')({ component: TeamsPage })
