import { Link } from "react-router-dom";

import { TrashSection } from "./components/TrashSection";
import styles from "./WebController.module.css";

export function TrashPage() {
  return (
    <main className={styles.page}>
      <div className={styles.container}>
        <header className={styles.header}>
          <p className={styles.label}>Trash</p>
          <h1 className={styles.title}>Trash</h1>
          <p className={styles.description}>
            削除済みdataの一覧・詳細・保持状態と、明示確認を伴う復元手順を確認します。物理的な最終削除はこの画面から実行できません。
          </p>
          <div className={styles.pageLinks}>
            <Link className={styles.button} to="/">日常操作へ戻る</Link>
            <Link className={styles.button} to="/settings">Room / Device設定</Link>
            <Link className={styles.button} to="/maintenance">Maintenanceへ戻る</Link>
          </div>
        </header>

        <section className={styles.pageGroup} aria-labelledby="trash-entries-group">
          <h2 id="trash-entries-group" className={styles.deviceTitle}>Trash entries</h2>
          <TrashSection />
        </section>
      </div>
    </main>
  );
}

export default TrashPage;
