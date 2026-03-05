import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import type { MachineUsage } from "../types";
import styles from "./UsageChart.module.css";

interface UsageChartProps {
  data: MachineUsage[];
}

export function UsageChart({ data }: UsageChartProps) {
  return (
    <div className={styles.wrapper}>
      <h3 className={styles.heading}>Usage Telemetry (24h)</h3>
      <div className={styles.chart}>
        <ResponsiveContainer width="100%" height={280}>
          <LineChart data={data}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis dataKey="timestamp" />
            <YAxis domain={[0, 100]} unit="%" />
            <Tooltip />
            <Legend />
            <Line
              type="monotone"
              dataKey="cpu_percent"
              name="CPU"
              stroke="#2563eb"
              strokeWidth={2}
              dot={false}
            />
            <Line
              type="monotone"
              dataKey="gpu_percent"
              name="GPU"
              stroke="#16a34a"
              strokeWidth={2}
              dot={false}
            />
            <Line
              type="monotone"
              dataKey="memory_percent"
              name="Memory"
              stroke="#d97706"
              strokeWidth={2}
              dot={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
