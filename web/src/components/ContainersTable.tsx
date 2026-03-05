import type { Container } from "../types";
import styles from "./ContainersTable.module.css";

interface ContainersTableProps {
  containers: Container[];
}

const statusLabel: Record<string, string> = {
  running: "Running",
  stopped: "Stopped",
  pending: "Pending",
};

export function ContainersTable({ containers }: ContainersTableProps) {
  return (
    <div className={styles.wrapper}>
      <h3 className={styles.heading}>Provisioned Containers</h3>
      <table className={styles.table}>
        <thead>
          <tr>
            <th>Name</th>
            <th>Image</th>
            <th>Node</th>
            <th>GPU</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {containers.map((c) => (
            <tr key={c.id}>
              <td>{c.name}</td>
              <td>{c.image}</td>
              <td>{c.node}</td>
              <td>{c.gpu_type}</td>
              <td>
                <span className={`${styles.status} ${styles[c.status]}`}>
                  {statusLabel[c.status]}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
