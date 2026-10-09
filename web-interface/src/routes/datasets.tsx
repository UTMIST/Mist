import { createFileRoute } from '@tanstack/react-router'
import { DatasetsPage } from '#/components/DatasetsPage.tsx'

export const Route = createFileRoute('/datasets')({ component: DatasetsPage })
