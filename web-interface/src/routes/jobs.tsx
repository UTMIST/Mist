import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/jobs')({
  component: RouteComponent,
})

function RouteComponent() {
  return <div>Hello "/jobs"!</div>
}
