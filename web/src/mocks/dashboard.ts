import type {
  Container,
  MaintenanceEvent,
  MachineUsage,
  ResourceSummary,
} from "../types";

export const resourceSummary: ResourceSummary = {
  total_machines: 12,
  available: 7,
  occupied: 5,
};

export const containers: Container[] = [
  {
    id: "ctr-001",
    name: "training-resnet50",
    image: "pytorch-cuda:2.1",
    status: "running",
    gpu_type: "AMD",
    created: "2026-03-01T10:00:00Z",
    node: "node-gpu-01",
  },
  {
    id: "ctr-002",
    name: "inference-bert",
    image: "pytorch-cpu:2.1",
    status: "running",
    gpu_type: "CPU",
    created: "2026-03-02T14:30:00Z",
    node: "node-cpu-03",
  },
  {
    id: "ctr-003",
    name: "finetune-llama",
    image: "pytorch-cuda:2.1",
    status: "pending",
    gpu_type: "AMD",
    created: "2026-03-03T09:15:00Z",
    node: "node-gpu-02",
  },
  {
    id: "ctr-004",
    name: "eval-vit",
    image: "pytorch-rocm:2.1",
    status: "stopped",
    gpu_type: "AMD",
    created: "2026-02-28T16:45:00Z",
    node: "node-gpu-03",
  },
  {
    id: "ctr-005",
    name: "preprocess-data",
    image: "pytorch-cpu:2.1",
    status: "running",
    gpu_type: "CPU",
    created: "2026-03-03T11:00:00Z",
    node: "node-cpu-01",
  },
  {
    id: "ctr-006",
    name: "training-gpt-small",
    image: "pytorch-cuda:2.1",
    status: "running",
    gpu_type: "TT",
    created: "2026-03-04T08:20:00Z",
    node: "node-tt-01",
  },
];

export const maintenanceEvents: MaintenanceEvent[] = [
  {
    id: "maint-001",
    date: "2026-03-10",
    affected_nodes: ["node-gpu-01", "node-gpu-02"],
    expected_downtime: "4 hours",
    description: "GPU driver update and firmware patch",
  },
  {
    id: "maint-002",
    date: "2026-03-15",
    affected_nodes: ["node-cpu-01", "node-cpu-02", "node-cpu-03"],
    expected_downtime: "2 hours",
    description: "Kernel security update",
  },
  {
    id: "maint-003",
    date: "2026-03-22",
    affected_nodes: ["node-tt-01"],
    expected_downtime: "6 hours",
    description: "Tenstorrent firmware upgrade",
  },
];

export const usageHistory: MachineUsage[] = [
  { timestamp: "00:00", cpu_percent: 32, gpu_percent: 45, memory_percent: 58 },
  { timestamp: "04:00", cpu_percent: 28, gpu_percent: 38, memory_percent: 55 },
  { timestamp: "08:00", cpu_percent: 65, gpu_percent: 72, memory_percent: 70 },
  { timestamp: "12:00", cpu_percent: 78, gpu_percent: 85, memory_percent: 76 },
  { timestamp: "16:00", cpu_percent: 82, gpu_percent: 90, memory_percent: 80 },
  { timestamp: "20:00", cpu_percent: 55, gpu_percent: 60, memory_percent: 65 },
  { timestamp: "24:00", cpu_percent: 30, gpu_percent: 40, memory_percent: 55 },
];
