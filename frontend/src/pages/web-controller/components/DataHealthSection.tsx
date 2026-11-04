import { useCallback, useEffect, useState } from "react";

import { fetchDataHealth, type DataHealthIssue } from "../../../api/controller";
import { DataCleanupSection } from "./DataCleanupSection";
import styles from "../WebController.module.css";

const severityLabel: Record<DataHealthIssue["severity"], string> = {
  error: "エラー",
  warning: "警告",
};

export function DataHealthSection() {
  const [issues, setIssues] = useState<DataHealthIssue[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [scanRevision, setScanRevision] = useState(0);

  const scan = useCallback(async () => {
    setScanRevision((revision) => revision + 1);
    setLoading(true);
    setError("");

    try {
      const response = await fetchDataHealth();
      setIssues(response.issues);
    } catch (scanError) {
      setError(
        scanError instanceof Error
          ? scanError.message
          : "Data Healthの取得に失敗しました",
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void scan();
    }, 0);

    return () => window.clearTimeout(timer);
  }, [scan]);

  return (
    <section className={styles.dataHealthSection} aria-labelledby="data-health-title">
      <div className={styles.deviceHeader}>
        <h3 id="data-health-title" className={styles.deviceTitle}>
          Data Health
        </h3>
        <button
          type="button"
          className={styles.statusButton}
          onClick={() => void scan()}
          disabled={loading}
        >
          {loading ? "確認中..." : error ? "再試行" : "再スキャン"}
        </button>
      </div>

      <p className={styles.helperText}>
        data配下を読み取り専用で確認します。cleanupはissue/actionを個別選択し、別の確認後にTrashへ移動します。
      </p>

      {loading && (
        <p className={styles.info} role="status">
          {issues === null ? "Data Healthを確認しています..." : "Data Healthを再確認しています。表示中のissueは前回scanの結果です。cleanup操作はscan完了まで停止します。"}
        </p>
      )}

      {!loading && error && (
        <p className={styles.error} role="alert">
          Data Healthの取得に失敗しました: {error}
        </p>
      )}

      {!loading && !error && issues?.length === 0 && (
        <p className={styles.info} role="status">
          問題は検出されませんでした。
        </p>
      )}

      {error && issues && (
        <p className={styles.info} role="status">
          cleanup候補の表示は前回scanに基づき、最新scanが成功するまで選択・実行できません。
        </p>
      )}

      {issues && issues.length > 0 && (
        <>
          {!loading && !error && <p className={styles.info} role="status">{issues.length}件の問題が見つかりました。</p>}
          <ul className={styles.dataHealthList}>
            {issues.map((issue) => (
              <li key={issue.id}>
                <article className={styles.deviceCard}>
                  <div className={styles.deviceCardHeader}>
                    <h4 className={styles.deviceCardTitle}>{issue.message}</h4>
                    <span className={styles.badge}>{severityLabel[issue.severity]}</span>
                  </div>
                  <dl className={styles.dataHealthMeta}>
                    <div>
                      <dt>Path</dt>
                      <dd><code>{issue.path}</code></dd>
                    </div>
                    <div>
                      <dt>Issue ID</dt>
                      <dd><code>{issue.id}</code></dd>
                    </div>
                    <div>
                      <dt>SHA-256</dt>
                      <dd><code>{issue.hash}</code></dd>
                    </div>
                    <div>
                      <dt>Cleanup</dt>
                      <dd>
                        {issue.has_destructive_actions
                          ? "cleanup候補（この画面では変更されません）"
                          : "破壊的なcleanup操作なし"}
                      </dd>
                    </div>
                  </dl>
                </article>
              </li>
            ))}
          </ul>
        </>
      )}

      {issues && (
        <DataCleanupSection
          issues={issues}
          scanRevision={scanRevision}
          scanBusy={loading}
          scanError={error}
          onRescan={() => void scan()}
        />
      )}
    </section>
  );
}
