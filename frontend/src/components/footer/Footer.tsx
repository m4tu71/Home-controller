import { Link } from "react-router-dom";
import styles from "./Footer.module.css";

export default function Footer() {
  return (
    <footer className={styles.footer}>
      <div className={styles.inner}>
        <div className={styles.version}>
          <span className={styles.versionLabel}>Version</span>
          <span className={styles.versionValue}>{__APP_VERSION__}</span>
        </div>

        <Link
          to="/design-system"
          className={styles.link}
        >
          Design System
        </Link>
      </div>
    </footer>
  );
}
