import type { MaintenanceEvent } from "../types";
import styles from "./MaintenanceTable.module.css";

interface MaintenanceTableProps {
  events: MaintenanceEvent[];
}

export function MaintenanceTable({ events }: MaintenanceTableProps) {
  return (
    <div className={styles.wrapper}>
      <h3 className={styles.heading}>Upcoming Maintenance</h3>
      <table className={styles.table}>
        <thead>
          <tr>
            <th>Date</th>
            <th>Affected Nodes</th>
            <th>Downtime</th>
            <th>Description</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => (
            <tr key={e.id}>
              <td>{e.date}</td>
              <td>{e.affected_nodes.join(", ")}</td>
              <td>{e.expected_downtime}</td>
              <td>{e.description}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
