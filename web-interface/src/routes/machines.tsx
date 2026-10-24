import { createFileRoute, Navigate } from '@tanstack/react-router'

export const Route = createFileRoute('/machines')({
  component: () => <Navigate to="/dashboard" replace />,
})
