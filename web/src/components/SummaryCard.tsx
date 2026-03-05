import styles from "./SummaryCard.module.css";

interface SummaryCardProps {
  label: string;
  value: number | string;
}

export function SummaryCard({ label, value }: SummaryCardProps) {
  return (
    <div className={styles.card}>
      <span className={styles.value}>{value}</span>
      <span className={styles.label}>{label}</span>
    </div>
  );
}
