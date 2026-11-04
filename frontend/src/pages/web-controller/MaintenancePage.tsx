import { Link } from "react-router-dom";

import { DataHealthSection } from "./components/DataHealthSection";
import { MigrationDryRunSection } from "./components/MigrationDryRunSection";
import { OperationHistorySection } from "./components/OperationHistorySection";
import styles from "./WebController.module.css";

export function MaintenancePage() {
  return (
    <main className={styles.page}>
      <div className={styles.container}>
        <header className={styles.header}>
          <p className={styles.label}>Maintenance</p>
          <h1 className={styles.title}>Maintenance</h1>
          <p className={styles.description}>
            data health、migration、cleanup、operation履歴を確認します。実dataを変更する操作は手順ごとの明示確認が必要です。
          </p>
          <div className={styles.pageLinks}>
            <Link className={styles.button} to="/">日常操作へ戻る</Link>
            <Link className={styles.button} to="/settings">Room / Device設定</Link>
            <Link className={styles.button} to="/trash">Trashを確認</Link>
          </div>
        </header>

        <section className={styles.pageGroup} aria-labelledby="maintenance-data-health-group">
          <h2 id="maintenance-data-health-group" className={styles.deviceTitle}>Data Health / cleanup</h2>
          <DataHealthSection />
        </section>

        <section className={styles.pageGroup} aria-labelledby="maintenance-migration-group">
          <h2 id="maintenance-migration-group" className={styles.deviceTitle}>Migration</h2>
          <MigrationDryRunSection />
        </section>

        <section className={styles.pageGroup} aria-labelledby="maintenance-operation-history-group">
          <h2 id="maintenance-operation-history-group" className={styles.deviceTitle}>Operation履歴 / recovery</h2>
          <OperationHistorySection />
        </section>
      </div>
    </main>
  );
}

export default MaintenancePage;
