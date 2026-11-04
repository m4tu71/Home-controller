import styles from "./DesignSystem.module.css";

export default function Home() {
  return (
    <main className={styles.page}>
      <h1 className={styles.title}>Design Preview</h1>

      {/* Colors */}
      <section className={styles.section}>
        <h2>Colors</h2>

        <div className={styles.colorGrid}>
          <div>
            <div
              className={styles.colorBox}
              style={{ background: "var(--color-bg)" }}
            />
            <p>--color-bg</p>
          </div>

          <div>
            <div
              className={styles.colorBox}
              style={{ background: "var(--color-surface)" }}
            />
            <p>--color-surface</p>
          </div>

          <div>
            <div
              className={styles.colorBox}
              style={{ background: "var(--color-primary)" }}
            />
            <p>--color-primary</p>
          </div>
        </div>
      </section>

      {/* Radius */}
      <section className={styles.section}>
        <h2>Radius</h2>

        <div className={styles.radiusGrid}>
          <div className={styles.radiusSm}>
            radius-sm
          </div>

          <div className={styles.radiusMd}>
            radius-md
          </div>
        </div>
      </section>

      {/* Shadow */}
      <section className={styles.section}>
        <h2>Shadow</h2>

        <div className={styles.shadowCard}>
          shadow-sm
        </div>
      </section>

      {/* Font */}
      <section className={styles.section}>
        <h2>Typography</h2>

        <p className={styles.fontSm}>
          font-size-sm
        </p>

        <p className={styles.fontMd}>
          font-size-md
        </p>

        <p className={styles.fontLg}>
          font-size-lg
        </p>

        <p className={styles.fontXl}>
          font-size-xl
        </p>
      </section>
    </main>
  );
}