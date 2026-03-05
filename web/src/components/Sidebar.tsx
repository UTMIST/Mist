import { NavLink } from "react-router-dom";
import styles from "./Sidebar.module.css";

const navItems = [
  { to: "/", label: "Dashboard" },
  { to: "/machines", label: "Machines" },
  { to: "/settings", label: "Settings" },
];

export function Sidebar() {
  return (
    <nav className={styles.sidebar}>
      <div className={styles.logo}>Mist</div>
      <ul className={styles.navList}>
        {navItems.map((item) => (
          <li key={item.to}>
            <NavLink
              to={item.to}
              className={({ isActive }) =>
                isActive ? `${styles.link} ${styles.active}` : styles.link
              }
            >
              {item.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
