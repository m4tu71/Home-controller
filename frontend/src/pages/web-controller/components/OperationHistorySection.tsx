import { useCallback, useEffect, useRef, useState } from "react";

import { Link } from "react-router-dom";

import {
  fetchBackupDetail,
  fetchMaintenanceOperationDetail,
  fetchMaintenanceOperations,
  verifyMigration,
  type BackupDetailResponse,
  type MaintenanceOperationDetail,
  type MaintenanceOperationSummary,
  type MigrationVerifyResult,
} from "../../../api/controller";
import styles from "../WebController.module.css";

type BackupCheck = {
  backupID: string;
  role: "rehearsal" | "apply";
  expectedState: string;
  detail?: BackupDetailResponse;
  error?: string;
  loading: boolean;
};

const operationTypeLabels: Record<string, string> = {
  migration: "Migration",
  migration_rollback: "Migration rollback",
  cleanup: "Cleanup",
  trash_restore: "Trash restore",
  trash_final_delete: "Trash final delete",
};

const statusLabels: Record<string, string> = {
  prepared: "準備済み",
  applying: "適用中",
  applied: "適用済み・未検証",
  verifying: "検証中",
  verified: "検証済み",
  partial_failure: "部分失敗",
  verification_failed: "検証失敗",
  recovery_required: "要手動確認",
  restoring: "復元中",
  restored: "復元済み",
  restore_partial: "復元部分失敗",
  final_deleted: "最終削除済み",
  failed: "失敗",
  pending: "未開始",
  unchanged: "変更なし",
  applied_unconfirmed: "適用済み・未確認",
  trashed_unconfirmed: "Trash移動済み・未確認",
  ambiguous: "結果不明",
};

const classificationLabels: Record<string, string> = {
  unchanged: "変更なし",
  applied_unconfirmed: "適用された可能性・未確認",
  trashed_unconfirmed: "Trashへ移動した可能性・未確認",
  ambiguous: "状態を特定できない",
};

const reasonLabels: Record<string, string> = {
  missing_or_invalid_snapshot: "比較用snapshotがない、または無効",
  invalid_snapshot: "snapshotが無効",
  runtime_read_failed: "runtime dataを読み取れない",
  trash_read_failed: "Trashを読み取れない",
  filesystem_mismatch: "filesystem状態が記録と一致しない",
  migration_multi_path_manual_review: "複数pathを含むため手動確認が必要",
};

function label(value: string | undefined, labels: Record<string, string>, fallback: string): string {
  if (!value) return fallback;
  return labels[value] ?? value;
}

function displayTime(value: string | null | undefined): string {
  if (!value) return "記録なし（未完了の可能性）";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "日時を読み取れません"
    : `${date.toISOString().replace(".000Z", "Z")} (UTC)`;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "Maintenance履歴を取得できませんでした。";
}

function backupIssues(
  backup: BackupDetailResponse,
  check: BackupCheck,
  operation: MaintenanceOperationDetail,
): string[] {
  const issues: string[] = [];
  if (backup.backup_id !== check.backupID) issues.push("backup IDが要求と一致しません");
  if (backup.role !== check.role) issues.push(`roleが${check.role}ではありません`);
  if (backup.state !== check.expectedState) issues.push(`stateが${check.expectedState}ではありません`);
  if (operation.plan_id && backup.plan_id !== operation.plan_id) issues.push("plan IDがoperationと一致しません");
  const operationActionIDs = operation.actions.map((action) => action.action_id).filter(Boolean).sort();
  const backupActionIDs = [...backup.action_ids].sort();
  if (
    operationActionIDs.length > 0 &&
    (operationActionIDs.length !== backupActionIDs.length || operationActionIDs.some((id, index) => id !== backupActionIDs[index]))
  ) {
    issues.push("action ID一覧がoperationと一致しません");
  }
  return issues;
}

function nextActionLabel(action: string): string {
  const labels: Record<string, string> = {
    verify: "read-only verify候補",
    restore: "backup / Trashからの復元を人が確認する候補",
    new_dry_run: "最新状態からdry-runを作り直す候補",
    manual_inspection: "Backend hostでの手動調査が必要",
  };
  return labels[action] ?? action;
}

function expectedBackups(operation: MaintenanceOperationDetail): BackupCheck[] {
  const checks: BackupCheck[] = [];
  if (operation.rehearsal_backup_id) {
    checks.push({
      backupID: operation.rehearsal_backup_id,
      role: "rehearsal",
      expectedState: "rehearsal_verified",
      loading: true,
    });
  }
  if (operation.apply_backup_id) {
    checks.push({
      backupID: operation.apply_backup_id,
      role: "apply",
      expectedState: "point_in_time_verified",
      loading: true,
    });
  }
  return checks;
}

export function OperationHistorySection() {
  const [operations, setOperations] = useState<MaintenanceOperationSummary[] | null>(null);
  const [listLoading, setListLoading] = useState(true);
  const [listError, setListError] = useState("");
  const [selectedOperationID, setSelectedOperationID] = useState("");
  const [detail, setDetail] = useState<MaintenanceOperationDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [backupChecks, setBackupChecks] = useState<BackupCheck[]>([]);
  const [verifyResult, setVerifyResult] = useState<MigrationVerifyResult | null>(null);
  const [verifyLoading, setVerifyLoading] = useState(false);
  const [verifyError, setVerifyError] = useState("");
  const listSequence = useRef(0);
  const detailSequence = useRef(0);
  const verifySequence = useRef(0);

  const loadList = useCallback(async () => {
    const sequence = ++listSequence.current;
    setListLoading(true);
    setListError("");
    setOperations(null);
    setSelectedOperationID("");
    setDetail(null);
    setDetailLoading(false);
    setDetailError("");
    setBackupChecks([]);
    setVerifyResult(null);
    setVerifyLoading(false);
    setVerifyError("");
    detailSequence.current += 1;
    verifySequence.current += 1;
    try {
      const response = await fetchMaintenanceOperations();
      if (sequence === listSequence.current) setOperations(response.operations);
    } catch (error: unknown) {
      if (sequence === listSequence.current) setListError(describeError(error));
    } finally {
      if (sequence === listSequence.current) setListLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => void loadList(), 0);
    return () => {
      window.clearTimeout(timer);
      listSequence.current += 1;
      detailSequence.current += 1;
      verifySequence.current += 1;
    };
  }, [loadList]);

  async function loadDetail(operationID: string) {
    const sequence = ++detailSequence.current;
    setSelectedOperationID(operationID);
    setDetail(null);
    setDetailError("");
    setDetailLoading(true);
    setBackupChecks([]);
    setVerifyLoading(false);
    setVerifyResult(null);
    setVerifyError("");
    verifySequence.current += 1;
    try {
      const response = await fetchMaintenanceOperationDetail(operationID);
      if (response.operation_id !== operationID) throw new Error("operation detailのIDが選択内容と一致しません。");
      if (sequence !== detailSequence.current) return;
      setDetail(response);
      const checks = expectedBackups(response);
      setBackupChecks(checks);
      const resolvedChecks = await Promise.all(checks.map(async (check): Promise<BackupCheck> => {
        try {
          const backup = await fetchBackupDetail(check.backupID);
          const issues = backupIssues(backup, check, response);
          return {
            ...check,
            loading: false,
            detail: backup,
            error: issues.length > 0 ? `関連backupの照合に失敗: ${issues.join("、")}` : "",
          };
        } catch (error: unknown) {
          return { ...check, loading: false, error: describeError(error) };
        }
      }));
      if (sequence === detailSequence.current) setBackupChecks(resolvedChecks);
    } catch (error: unknown) {
      if (sequence === detailSequence.current) setDetailError(describeError(error));
    } finally {
      if (sequence === detailSequence.current) setDetailLoading(false);
    }
  }

  async function runReadOnlyVerify() {
    if (!detail || detail.type !== "migration" || !detail.available_next_actions.includes("verify") || verifyLoading) return;
    const sequence = ++verifySequence.current;
    setVerifyLoading(true);
    setVerifyError("");
    setVerifyResult(null);
    try {
      const response = await verifyMigration(detail.operation_id);
      if (response.operation_id !== detail.operation_id || (detail.plan_id && response.plan_id !== detail.plan_id)) {
        throw new Error("verify結果のoperation/plan IDが選択中の履歴と一致しません。");
      }
      if (sequence === verifySequence.current) setVerifyResult(response);
    } catch (error: unknown) {
      if (sequence === verifySequence.current) setVerifyError(describeError(error));
    } finally {
      if (sequence === verifySequence.current) setVerifyLoading(false);
    }
  }

  function renderSummary(operation: MaintenanceOperationSummary) {
    return (
      <dl className={styles.migrationPlanMeta}>
        <div><dt>Operation ID</dt><dd><code>{operation.operation_id}</code></dd></div>
        <div><dt>種別 / 状態</dt><dd>{label(operation.type, operationTypeLabels, "種別不明")} / {label(operation.status, statusLabels, "状態不明")}</dd></div>
        <div><dt>対象識別子</dt><dd><code>{operation.target || "安全な識別子なし"}</code></dd></div>
        <div><dt>Plan ID</dt><dd><code>{operation.plan_id || "記録なし"}</code></dd></div>
        <div><dt>開始 / 完了 (UTC)</dt><dd>{displayTime(operation.started_at)} / {displayTime(operation.completed_at)}</dd></div>
        <div><dt>Rehearsal backup</dt><dd><code>{operation.rehearsal_backup_id || "なし"}</code></dd></div>
        <div><dt>Apply backup</dt><dd><code>{operation.apply_backup_id || "なし"}</code></dd></div>
      </dl>
    );
  }

  const canVerify = Boolean(
    detail?.type === "migration" && detail.available_next_actions.includes("verify"),
  );

  return (
    <section className={styles.migrationStage} aria-labelledby="operation-history-title">
      <div className={styles.deviceCardHeader}>
        <h3 id="operation-history-title" className={styles.deviceCardTitle}>Operation履歴 / Recovery</h3>
        <button type="button" className={styles.statusButton} onClick={() => void loadList()} disabled={listLoading}>
          {listLoading ? "読み込み中..." : "履歴を再読み込み"}
        </button>
      </div>
      <p className={styles.helperText}>
        Migration / cleanup / delete / restoreの記録を読み取り専用で確認します。履歴の対象path、内部snapshotは非表示です。自動再開・自動rollbackは行いません。
      </p>

      {listLoading && <p className={styles.info} role="status">Operation履歴を読み込んでいます...</p>}
      {!listLoading && listError && (
        <div>
          <p className={styles.error} role="alert">履歴を取得できませんでした: {listError}</p>
          <button type="button" className={styles.button} onClick={() => void loadList()}>再試行</button>
        </div>
      )}
      {!listLoading && !listError && operations?.length === 0 && (
        <p className={styles.info} role="status">Maintenance operation履歴はありません。</p>
      )}
      {!listLoading && !listError && operations && operations.length > 0 && (
        <ul className={styles.migrationActionChoices}>
          {operations.map((operation) => (
            <li key={operation.operation_id}>
              <article className={styles.deviceCard}>
                <div className={styles.deviceCardHeader}>
                  <h4 className={styles.deviceCardTitle}>{label(operation.type, operationTypeLabels, "Maintenance operation")}</h4>
                  <span className={operation.status === "verified" || operation.status === "restored" ? styles.planReady : styles.planPending}>
                    {label(operation.status, statusLabels, "状態不明")}
                  </span>
                </div>
                {renderSummary(operation)}
                <button
                  type="button"
                  className={styles.button}
                  onClick={() => void loadDetail(operation.operation_id)}
                  disabled={detailLoading}
                  aria-expanded={selectedOperationID === operation.operation_id}
                >
                  {selectedOperationID === operation.operation_id
                    ? detailLoading ? "詳細を読み込み中..." : "詳細を再読み込み"
                    : `詳細を表示: ${operation.operation_id}`}
                </button>
              </article>
            </li>
          ))}
        </ul>
      )}

      {selectedOperationID && (
        <article className={styles.deviceCard} aria-labelledby="operation-history-detail-title">
          <h4 id="operation-history-detail-title" className={styles.deviceCardTitle}>Operation detail</h4>
          {detailLoading && <p className={styles.info} role="status">operation detailと関連backupを照合しています...</p>}
          {detailError && <p className={styles.error} role="alert">詳細を取得できませんでした: {detailError}</p>}
          {detail && (
            <>
              {renderSummary(detail)}
              {detail.previous_status && <p className={styles.info}>Recovery検出前のstatus: {label(detail.previous_status, statusLabels, "状態不明")}</p>}
              {detail.error && <p className={styles.error} role="alert">記録されたoperation error: {detail.error}</p>}
              <p className={styles.helperText}>targetのpathと生snapshotはAPIで伏せられます。安全な対象識別子・action IDのみ表示しています。</p>
              {detail.actions.length === 0 ? <p className={styles.info}>action記録はありません。</p> : (
                <ul className={styles.migrationActionChoices}>
                  {detail.actions.map((action) => (
                    <li key={action.action_id}>
                      <article className={styles.deviceCard}>
                        <h5 className={styles.deviceCardTitle}>{action.action_id}</h5>
                        <dl className={styles.migrationPlanMeta}>
                          <div><dt>Action / status</dt><dd>{action.action || "種別不明"} / {label(action.status, statusLabels, "状態不明")}</dd></div>
                          <div><dt>Recovery分類</dt><dd>{label(action.classification, classificationLabels, "分類なし")}</dd></div>
                          {action.reason && <div><dt>理由</dt><dd>{label(action.reason, reasonLabels, "理由不明")}</dd></div>}
                        </dl>
                        {action.error && <p className={styles.error} role="alert">Action error: {action.error}</p>}
                      </article>
                    </li>
                  ))}
                </ul>
              )}

              <h5 className={styles.deviceCardTitle}>関連backup</h5>
              {backupChecks.length === 0 ? <p className={styles.info}>operationに関連backup IDは記録されていません。</p> : (
                <ul className={styles.migrationActionChoices}>
                  {backupChecks.map((check) => (
                    <li key={`${check.role}:${check.backupID}`}>
                      <article className={styles.deviceCard}>
                        <h6 className={styles.deviceCardTitle}>{check.role === "rehearsal" ? "Rehearsal backup" : "Apply backup"}</h6>
                        <p><code>{check.backupID}</code></p>
                        {check.loading && <p className={styles.info} role="status">backup detailを確認しています...</p>}
                        {check.error && <p className={styles.error} role="alert">backup detailを確認できません: {check.error}。復元可能とは判断しないでください。</p>}
                        {check.detail && (
                          <>
                            <p>Role: {check.detail.role}; state: {check.detail.state}</p>
                            <p>Files: {check.detail.file_count}; size: {check.detail.total_size} bytes; inventory SHA-256: <code>{check.detail.inventory_hash}</code></p>
                            {!check.error && <p className={styles.info} role="status">role / state / plan / action IDをoperationと照合しました。これは実復元の許可ではありません。</p>}
                          </>
                        )}
                      </article>
                    </li>
                  ))}
                </ul>
              )}

              <h5 className={styles.deviceCardTitle}>安全な次の確認候補</h5>
              {detail.available_next_actions.length === 0
                ? <p className={styles.info}>APIは個別の次操作候補を返していません。状態を確認し、必要なら最新状態からdry-runしてください。</p>
                : (
                  <>
                    <ul className={styles.migrationDecisionList}>
                      {detail.available_next_actions.map((action) => <li key={action}>{nextActionLabel(action)}</li>)}
                    </ul>
                    <p className={styles.helperText}>これはBackendの候補表示であり、操作可能性や復元の安全性を保証しません。この画面からrestore、再apply、resumeは実行しません。</p>
                    {detail.available_next_actions.includes("verify") && detail.type !== "migration" && (
                      <p className={styles.info}>このoperation typeにはFrontendから呼べるmigration verify routeがありません。Backend hostで記録と実dataを確認してください。</p>
                    )}
                    {detail.available_next_actions.includes("restore") && (
                      <p className={styles.info}>Restore候補: apply backupのrole/stateを上で確認し、Trash entryは <Link to="/trash">Trash一覧</Link> で別途確認してください。実dataへの復元は人の判断後、別の確認手順で行います。</p>
                    )}
                    {detail.available_next_actions.includes("new_dry_run") && (
                      <p className={styles.info}>新しいdry-run候補: <a href="#data-health-title">Data Health</a>で再scanし、必要な場合は <a href="#migration-dry-run-title">Migration dry-run</a>から現在状態を再評価してください。</p>
                    )}
                    {detail.available_next_actions.includes("manual_inspection") && (
                      <p className={styles.error} role="alert">状態が曖昧です。再試行や復元をせず、Backend host上でruntime data・Trash・backupをoperation IDと照合してください。</p>
                    )}
                    {canVerify && (
                      <div>
                        <button type="button" className={styles.button} onClick={() => void runReadOnlyVerify()} disabled={verifyLoading}>
                          {verifyLoading ? "read-only verify中..." : "Migrationをread-only verify"}
                        </button>
                        <p className={styles.helperText}>verifyはmigration targetを読戻すだけです。自動修復・rollbackは行いません。</p>
                      </div>
                    )}
                  </>
                )}
              {verifyError && <p className={styles.error} role="alert">read-only verifyに失敗しました: {verifyError}</p>}
              {verifyResult && (
                <article className={styles.deviceCard} aria-labelledby="operation-history-verify-title">
                  <h5 id="operation-history-verify-title" className={styles.deviceCardTitle}>Repository verify: {label(verifyResult.status, statusLabels, verifyResult.status)}</h5>
                  {verifyResult.targets.length === 0 ? <p className={styles.info}>verify targetはありません。</p> : (
                    <ul className={styles.migrationPathList}>
                      {verifyResult.targets.map((target) => (
                        <li key={`${target.action_id}:${target.target_path}`}>
                          <strong>{target.action_id}: {target.status}</strong> — <code>{target.target_path}</code>
                          {target.reason && <div>差異: {target.reason}</div>}
                          {target.expected_sha256 && <div>期待SHA-256: <code>{target.expected_sha256}</code></div>}
                          {target.actual_sha256 && <div>実測SHA-256: <code>{target.actual_sha256}</code></div>}
                        </li>
                      ))}
                    </ul>
                  )}
                </article>
              )}
            </>
          )}
        </article>
      )}
    </section>
  );
}
