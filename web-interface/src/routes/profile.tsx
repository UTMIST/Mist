import { createFileRoute } from '@tanstack/react-router'
import { AccountPage } from '#/components/AccountPage.tsx'

export const Route = createFileRoute('/profile')({ component: AccountPage })
