import { Link, NavLink } from "react-router-dom";
import { HeaderTime } from "./HeaderTime";
import styles from "./Header.module.css";

export default function Header() {
  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <Link to="/" className={styles.brand}>
          Home Controller
        </Link>

        <nav className={styles.nav} aria-label="メインナビゲーション">
          <NavLink
            to="/"
            end
            className={({ isActive }) => isActive ? `${styles.link} ${styles.active}` : styles.link}
          >
            Home
          </NavLink>
          <NavLink
            to="/maintenance"
            className={({ isActive }) => isActive ? `${styles.link} ${styles.active}` : styles.link}
          >
            Maintenance
          </NavLink>
          <NavLink
            to="/trash"
            className={({ isActive }) => isActive ? `${styles.link} ${styles.active}` : styles.link}
          >
            Trash
          </NavLink>
        </nav>

        <HeaderTime />
      </div>
    </header>
  );
}
