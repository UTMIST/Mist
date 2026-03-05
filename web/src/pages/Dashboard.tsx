import { SummaryCard } from "../components/SummaryCard";
import { ContainersTable } from "../components/ContainersTable";
import { MaintenanceTable } from "../components/MaintenanceTable";
import { UsageChart } from "../components/UsageChart";
import {
  resourceSummary,
  containers,
  maintenanceEvents,
  usageHistory,
} from "../mocks/dashboard";
import styles from "./Dashboard.module.css";

export function Dashboard() {
  return (
    <div className={styles.page}>
      <h2 className={styles.title}>Dashboard</h2>

      <section className={styles.summary}>
        <SummaryCard label="Total Machines" value={resourceSummary.total_machines} />
        <SummaryCard label="Available" value={resourceSummary.available} />
        <SummaryCard label="Occupied" value={resourceSummary.occupied} />
      </section>

      <section className={styles.section}>
        <ContainersTable containers={containers} />
      </section>

      <section className={styles.section}>
        <UsageChart data={usageHistory} />
      </section>

      <section className={styles.section}>
        <MaintenanceTable events={maintenanceEvents} />
      </section>
    </div>
  );
}
