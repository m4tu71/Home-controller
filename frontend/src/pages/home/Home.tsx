import { Link } from "react-router-dom";
import styles from "./Home.module.css";

export default function Home() {
  return (
    <main className={styles.page}>
      <section className={styles.hero}>
        <p className={styles.label}>Raspberry Pi Web App</p>
        <h1 className={styles.title}>Go API+Reactの学習用</h1>
        <div className={styles.actions}>
          <Link className={styles.secondaryLink} to="/design-system">
            Design System
          </Link>
        </div>
      </section>

    </main>
  );
}
