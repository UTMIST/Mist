Job Lifecycle in MIST.
This document explains how jobs are handled in the scheduler, from creation to completion.

1. Job Creation

Jobs are created by the Scheduler.
Each job has:
job_id – unique identifier
job_type – category of work
gpu_type – optional GPU requirement
payload – arbitrary data for processing
Jobs are stored in Redis for persistence and event tracking.

2. Enqueue

The Scheduler enqueues jobs in Redis streams.
Each job enters the Scheduled state.
Jobs can be filtered by GPU type, priority, or type if needed.

3. Assignment to Supervisor

Supervisors are worker processes that consume jobs from the Scheduler.
A Supervisor subscribes to jobs that match its GPU type and other requirements.
Once picked up, a job state changes to InProgress.

4. Job Processing

The Supervisor executes the job logic using the provided payload.
Supervisors track progress and can emit intermediate events (optional).
The Scheduler monitors state changes and logs job activity.

5. Completion

When a job finishes successfully, the Supervisor marks it as Completed.
Failed jobs can be marked Failed or re-enqueued based on the retry policy.
All state changes are persisted in Redis for observability.

6. Redis Storage

Jobs are stored as hashes keyed by job:<job_id>:
job_type, job_state, assigned_supervisor, timestamps, and payload.
Job events are emitted to a Redis stream (job_events) to allow real-time tracking.
