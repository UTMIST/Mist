import { createFileRoute } from '@tanstack/react-router'
import { DashboardPage } from '#/components/DashboardPage.tsx'

export const Route = createFileRoute('/dashboard')({ component: DashboardPage })
