export type JobState =
  | "Scheduled"
  | "InProgress"
  | "Success"
  | "Error"
  | "Failure";

export interface Job {
  id: string;
  type: string;
  payload: Record<string, unknown>;
  retries: number;
  created: string;
  required_gpu?: string;
  job_state: JobState;
  consumer_id?: string;
  time_assigned?: string;
  time_started?: string;
  time_completed?: string;
  result?: Record<string, unknown>;
  error?: string;
}

export type SupervisorState = "active" | "inactive" | "failed";

export interface SupervisorStatus {
  consumer_id: string;
  gpu_type: string;
  status: SupervisorState;
  last_seen: string;
  started_at: string;
}

export type ContainerStatus = "running" | "stopped" | "pending";

export interface Container {
  id: string;
  name: string;
  image: string;
  status: ContainerStatus;
  gpu_type: string;
  created: string;
  node: string;
}

export interface MaintenanceEvent {
  id: string;
  date: string;
  affected_nodes: string[];
  expected_downtime: string;
  description: string;
}

export interface MachineUsage {
  timestamp: string;
  cpu_percent: number;
  gpu_percent: number;
  memory_percent: number;
}

export interface ResourceSummary {
  total_machines: number;
  available: number;
  occupied: number;
}
