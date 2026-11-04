import { useEffect, useMemo, useRef, useState } from "react";

import {
  applyCleanup,
  CleanupApplyAPIError,
  CleanupDryRunAPIError,
  createCleanupDryRun,
  type CleanupApplyResponse,
  type CleanupDryRunAction,
  type CleanupDryRunResponse,
  type CleanupIssueSelection,
  type DataHealthIssue,
} from "../../../api/controller";
import styles from "../WebController.module.css";

type Props = {
  issues: DataHealthIssue[];
  scanRevision: number;
  scanBusy: boolean;
  scanError: string;
  onRescan: () => void;
};

type BusyStage = "dry-run" | "apply" | "";
type SavedPlan = {
  response: CleanupDryRunResponse;
  issues: CleanupIssueSelection[];
  scanRevision: number;
};

const severityLabel: Record<DataHealthIssue["severity"], string> = {
  error: "エラー",
  warning: "警告",
};

function sameIDs(left: string[], right: string[]): boolean {
  const sortedLeft = [...left].sort();
  const sortedRight = [...right].sort();
  return sortedLeft.length === sortedRight.length && sortedLeft.every((id, index) => id === sortedRight[index]);
}

function safeDataPath(path: string): boolean {
  return path.startsWith("data/") && !path.split("/").some((part) => part === ".." || part === ".");
}

function validateDryRun(response: CleanupDryRunResponse, requested: CleanupIssueSelection[]): void {
  if (
    !response.ok || !response.plan_id || !/^sha256:[a-f0-9]{64}$/i.test(response.plan_digest) ||
    !response.ready_to_apply || response.backup_requirement !== "apply" || !response.backup_planned ||
    !Array.isArray(response.actions) || response.actions.length !== requested.length
  ) {
    throw new Error("cleanup dry-runのplan/backup情報が不足または不一致のため、applyを停止しました。");
  }

  const requestedByID = new Map(requested.map((item) => [item.issue_id, item.before_hash]));
  const actionIDs = new Set<string>();
  const issueIDs = new Set<string>();
  for (const action of response.actions) {
    if (
      !action.action_id || actionIDs.has(action.action_id) ||
      !requestedByID.has(action.issue_id) || issueIDs.has(action.issue_id) ||
      action.before_hash !== requestedByID.get(action.issue_id) ||
      !action.destructive || action.expected_after_state !== "moved_to_trash" ||
      !Array.isArray(action.affected_paths) || action.affected_paths.length !== 1 ||
      !safeDataPath(action.affected_paths[0]) ||
      !Array.isArray(action.backup_sources) || action.backup_sources.length !== 1 ||
      action.backup_sources[0] !== action.affected_paths[0] ||
      !/^sha256:[a-f0-9]{64}$/i.test(action.backup_inventory_hash) ||
      !Number.isFinite(action.backup_file_count) || action.backup_file_count < 0 ||
      !Number.isFinite(action.backup_total_size) || action.backup_total_size < 0
    ) {
      throw new Error(`cleanup dry-run actionが選択issueまたはbackup inventoryと一致しません（${action.action_id || "IDなし"}）。applyを停止しました。`);
    }
    actionIDs.add(action.action_id);
    issueIDs.add(action.issue_id);
  }
  if (!sameIDs([...issueIDs], [...requestedByID.keys()])) {
    throw new Error("cleanup dry-runのissue一覧が送信した選択と一致しません。applyを停止しました。");
  }
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "cleanup APIの処理に失敗しました。";
}

function isSHA256(value: string): boolean {
  return /^[a-f0-9]{64}$/i.test(value);
}

function validateApplyResponse(response: CleanupApplyResponse, actionIDs: string[]): string {
  if (
    !response.ok || !response.operation_id || !response.apply_backup_id ||
    response.status !== "verified" || response.result !== "verified" ||
    !Array.isArray(response.actions) || !sameIDs(response.actions.map((action) => action.action_id), actionIDs) ||
    response.actions.some((action) => action.status !== "verified" || !action.trash_entry_id)
  ) {
    return "cleanup apply responseが選択actionまたは完了状態と一致しません。operationを再送せず、Backendの状態を確認してください。";
  }
  return "";
}

function selectedIssuesForActions(actions: CleanupDryRunAction[], selectedActionIDs: string[], requested: CleanupIssueSelection[]): CleanupIssueSelection[] {
  const selectedIssues = new Set(actions.filter((action) => selectedActionIDs.includes(action.action_id)).map((action) => action.issue_id));
  return requested.filter((item) => selectedIssues.has(item.issue_id));
}

export function DataCleanupSection({ issues, scanRevision, scanBusy, scanError, onRescan }: Props) {
  const [selectedIssueIDs, setSelectedIssueIDs] = useState<string[]>([]);
  const [selectionRevision, setSelectionRevision] = useState<number | null>(null);
  const [savedPlan, setSavedPlan] = useState<SavedPlan | null>(null);
  const [selectedActionIDs, setSelectedActionIDs] = useState<string[]>([]);
  const [busy, setBusy] = useState<BusyStage>("");
  const [dryRunError, setDryRunError] = useState("");
  const [dryRunNeedsRescan, setDryRunNeedsRescan] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [applyAttempted, setApplyAttempted] = useState(false);
  const [applyAttemptRevision, setApplyAttemptRevision] = useState<number | null>(null);
  const [applyResult, setApplyResult] = useState<CleanupApplyResponse | null>(null);
  const [partialOperation, setPartialOperation] = useState<{
    operationID: string;
    status?: string;
    applyBackupID?: string;
  } | null>(null);
  const inFlightRef = useRef(false);
  const scanRevisionRef = useRef(scanRevision);
  useEffect(() => {
    scanRevisionRef.current = scanRevision;
  }, [scanRevision]);

  const destructiveIssues = useMemo(() => issues.filter((issue) => issue.has_destructive_actions), [issues]);
  const selectedIssues = selectionRevision === scanRevision
    ? issues.filter((issue) => issue.has_destructive_actions && selectedIssueIDs.includes(issue.id))
    : [];
  const issueSelections = selectedIssues.map((issue) => ({ issue_id: issue.id, before_hash: issue.hash }));
  const actions = savedPlan?.response.actions ?? [];
  const selectedActions = actions.filter((action) => selectedActionIDs.includes(action.action_id));
  const requestIssues = savedPlan
    ? selectedIssuesForActions(actions, selectedActionIDs, savedPlan.issues)
    : [];
  const planMatchesCurrentScan = Boolean(
    savedPlan && !scanBusy && !scanError && savedPlan.scanRevision === scanRevision &&
    savedPlan.issues.every((planned) => issues.some((issue) => issue.id === planned.issue_id && issue.hash === planned.before_hash)),
  );
  const mustRescanAfterApply = applyAttempted && applyAttemptRevision !== null && scanRevision <= applyAttemptRevision;
  const dataScanReady = !scanBusy && !scanError;
  const canEditIssues = dataScanReady && busy === "" && !partialOperation && !mustRescanAfterApply;
  const canCreatePlan = issueSelections.length > 0 && issueSelections.every((item) => isSHA256(item.before_hash)) &&
    dataScanReady && busy === "" && !partialOperation && !mustRescanAfterApply && !dryRunNeedsRescan;
  const canApply = Boolean(
    savedPlan && planMatchesCurrentScan && savedPlan.response.ready_to_apply &&
    selectedActions.length > 0 && requestIssues.length === selectedActions.length &&
    busy === "" && !applyAttempted && !partialOperation,
  );

  function toggleIssue(issueID: string, checked: boolean) {
    const current = selectionRevision === scanRevision ? selectedIssueIDs : [];
    setSelectedIssueIDs(checked ? [...current.filter((id) => id !== issueID), issueID] : current.filter((id) => id !== issueID));
    setSelectionRevision(scanRevision);
    setSavedPlan(null);
    setSelectedActionIDs([]);
    setDryRunError("");
    setDryRunNeedsRescan(false);
  }

  async function createPlan() {
    if (!canCreatePlan || inFlightRef.current) return;
    const requested = issueSelections;
    const requestedRevision = scanRevision;
    inFlightRef.current = true;
    setBusy("dry-run");
    setDryRunError("");
    setDryRunNeedsRescan(false);
    setApplyError("");
    setSavedPlan(null);
    setSelectedActionIDs([]);
    try {
      const response = await createCleanupDryRun(requested);
      if (scanRevisionRef.current !== requestedRevision) {
        setDryRunNeedsRescan(true);
        setDryRunError("dry-run中にData Healthが再scanされました。最新issueを選び直して、新しいdry-runを作成してください。");
        return;
      }
      validateDryRun(response, requested);
      setSavedPlan({ response, issues: requested, scanRevision: requestedRevision });
      setApplyAttempted(false);
      setApplyAttemptRevision(null);
      setApplyResult(null);
      setPartialOperation(null);
    } catch (error: unknown) {
      setDryRunError(describeError(error));
      if (error instanceof CleanupDryRunAPIError && error.status === 409) {
        setDryRunNeedsRescan(true);
      }
    } finally {
      inFlightRef.current = false;
      setBusy("");
    }
  }

  async function applySelectedActions() {
    if (!savedPlan || !canApply || inFlightRef.current) return;
    const chosenIssues = requestIssues;
    const targetDetails = selectedActions.map((action) =>
      `${action.action_id}\n${action.affected_paths.join(", ")}\n  before SHA-256: ${action.before_hash}\n  expected: ${action.expected_after_state}`,
    ).join("\n\n");
    const confirmed = window.confirm(
      `選択したcleanup action ${selectedActions.length}件を実行します。\n\n` +
      `Plan: ${savedPlan.response.plan_id}\nDigest: ${savedPlan.response.plan_digest}\n` +
      `apply前に別のpoint-in-time backupを自動作成します。成功時は対象をTrashへ移動し、Trashからの復元が可能です。\n` +
      `物理的な最終削除やbackup restoreは行いません。途中失敗時も自動再試行・自動復元しません。\n\n` +
      `${targetDetails}\n\n続行しますか？`,
    );
    if (!confirmed) return;

    inFlightRef.current = true;
    setBusy("apply");
    setApplyAttempted(true);
    setApplyAttemptRevision(scanRevision);
    setApplyError("");
    try {
      const response = await applyCleanup({
        plan_id: savedPlan.response.plan_id,
        plan_digest: savedPlan.response.plan_digest,
        issues: chosenIssues,
        action_ids: selectedActions.map((action) => action.action_id),
        confirm: true,
      });
      setApplyResult(response);
      setApplyError(validateApplyResponse(response, selectedActions.map((action) => action.action_id)));
    } catch (error: unknown) {
      setApplyError(describeError(error));
      if (error instanceof CleanupApplyAPIError && error.operationID) {
        setPartialOperation({
          operationID: error.operationID,
          ...(error.operationStatus ? { status: error.operationStatus } : {}),
          ...(error.applyBackupID ? { applyBackupID: error.applyBackupID } : {}),
        });
      }
    } finally {
      inFlightRef.current = false;
      setBusy("");
    }
  }

  return (
    <section className={styles.migrationStage} aria-labelledby="data-cleanup-title">
      <div className={styles.deviceCardHeader}>
        <h4 id="data-cleanup-title" className={styles.deviceCardTitle}>Data cleanup</h4>
        <span className={styles.planPending}>選択・dry-run・確認後に実行</span>
      </div>
      <p className={styles.helperText}>
        Data Health issueを選んでdry-runし、action単位で再選択してください。runtime dataはapply時にのみ変更され、事前backupを作成してから対象をTrashへ移動します。
      </p>

      {(scanBusy || scanError) && (
        <p className={styles.info} role="status">
          {scanBusy ? "scan中のためcleanupの選択・実行を停止しています。" : "最新Data Health scanに失敗したため、cleanupの選択・実行を停止しています。"}
        </p>
      )}

      <fieldset className={styles.migrationFields} disabled={!canEditIssues}>
        <legend>cleanup対象issueを明示選択（初期状態は未選択）</legend>
        {destructiveIssues.length === 0 ? (
          <p className={styles.info}>破壊的cleanup actionを持つissueはありません。</p>
        ) : (
          <ul className={styles.migrationActionChoices}>
            {issues.map((issue) => (
              <li key={issue.id} className={styles.deviceCard}>
                <label className={styles.migrationCheckbox}>
                  <input
                    type="checkbox"
                    checked={selectionRevision === scanRevision && selectedIssueIDs.includes(issue.id)}
                    disabled={!issue.has_destructive_actions}
                    onChange={(event) => toggleIssue(issue.id, event.target.checked)}
                  />
                  <span><strong>{issue.id}</strong> — {issue.message}{issue.has_destructive_actions ? "（cleanup候補）" : "（対象外）"}</span>
                </label>
                <dl className={styles.migrationPlanMeta}>
                  <div><dt>Severity</dt><dd>{severityLabel[issue.severity]}</dd></div>
                  <div><dt>Path / before SHA-256</dt><dd><code>{issue.path}</code><br /><code>{issue.hash}</code></dd></div>
                </dl>
              </li>
            ))}
          </ul>
        )}
      </fieldset>
      <button type="button" className={styles.button} onClick={() => void createPlan()} disabled={!canCreatePlan}>
        {busy === "dry-run" ? "cleanup planを確認中..." : "選択issueでcleanup dry-run"}
      </button>
      {dryRunError && (
        <div>
          <p className={styles.error} role="alert">cleanup dry-runに失敗しました: {dryRunError}</p>
          {dryRunNeedsRescan && <button type="button" className={styles.button} onClick={onRescan} disabled={scanBusy}>Data Healthを再スキャン</button>}
        </div>
      )}

      {savedPlan && (
        <article className={styles.deviceCard} aria-labelledby="data-cleanup-plan-title">
          <div className={styles.deviceCardHeader}>
            <h5 id="data-cleanup-plan-title" className={styles.deviceCardTitle}>保存されたcleanup dry-run</h5>
            <span className={savedPlan.response.ready_to_apply && planMatchesCurrentScan ? styles.planReady : styles.planPending}>
              {planMatchesCurrentScan ? "明示選択待ち" : "stale — 再scanと新しいplanが必要"}
            </span>
          </div>
          <dl className={styles.migrationPlanMeta}>
            <div><dt>Plan / digest</dt><dd><code>{savedPlan.response.plan_id}</code> / <code>{savedPlan.response.plan_digest}</code></dd></div>
            <div><dt>Backup</dt><dd>apply直前に影響対象をpoint-in-time backup（まだ作成されていません）</dd></div>
          </dl>
          {!planMatchesCurrentScan && <p className={styles.error} role="alert">Data Healthのscan revisionまたはissue hashがplan作成後に変わりました。このplanはapplyできません。</p>}
          <fieldset className={styles.migrationFields} disabled={!planMatchesCurrentScan || busy !== "" || applyAttempted || Boolean(partialOperation)}>
            <legend>cleanup actionを明示選択（初期状態は未選択）</legend>
            <ul className={styles.migrationActionChoices}>
              {actions.map((action) => (
                <li key={action.action_id} className={styles.deviceCard}>
                  <label className={styles.migrationCheckbox}>
                    <input
                      type="checkbox"
                      checked={selectedActionIDs.includes(action.action_id)}
                      onChange={(event) => setSelectedActionIDs((current) => event.target.checked
                        ? [...current.filter((id) => id !== action.action_id), action.action_id]
                        : current.filter((id) => id !== action.action_id))}
                    />
                    <span><strong>{action.action_id}</strong> — {action.action} / destructive: {action.destructive ? "yes" : "no"}</span>
                  </label>
                  <dl className={styles.migrationPlanMeta}>
                    <div><dt>Affected paths</dt><dd><ul className={styles.migrationPathList}>{action.affected_paths.map((path) => <li key={path}><code>{path}</code></li>)}</ul></dd></div>
                    <div><dt>Before SHA-256</dt><dd><code>{action.before_hash}</code></dd></div>
                    <div><dt>Expected state</dt><dd>{action.expected_after_state}</dd></div>
                    <div><dt>Backup sources / inventory</dt><dd>{action.backup_sources.map((path) => <code key={path}>{path} </code>)}<br /><code>{action.backup_inventory_hash}</code>; {action.backup_file_count} files; {action.backup_total_size} bytes</dd></div>
                  </dl>
                </li>
              ))}
            </ul>
          </fieldset>
          {planMatchesCurrentScan && selectedActions.length === 0 && <p className={styles.info} role="status">applyするactionを1件以上選択してください。全actionは自動選択されません。</p>}
          <button type="button" className={styles.primaryButton} onClick={() => void applySelectedActions()} disabled={!canApply}>
            {busy === "apply" ? "cleanupを実行中..." : applyAttempted ? "apply要求済み（再実行不可）" : "確認してcleanup apply（Trashへ移動）"}
          </button>
          {(applyError || (applyAttempted && !applyResult && !partialOperation)) && (
            <div>
              {applyError && <p className={styles.error} role="alert">cleanup applyに失敗しました: {applyError}</p>}
              <p className={styles.info} role="status">
                同じplan/actionを再送しません。operation IDがない場合はData Healthを再scanし、issueを選び直して新しいdry-runを作成してください。
              </p>
              {!partialOperation && <button type="button" className={styles.button} onClick={onRescan} disabled={scanBusy}>Data Healthを再スキャン</button>}
            </div>
          )}
        </article>
      )}

      {(applyResult || partialOperation) && (
        <article className={styles.deviceCard} aria-labelledby="data-cleanup-result-title">
          <h5 id="data-cleanup-result-title" className={styles.deviceCardTitle}>Cleanup operation result</h5>
          {applyError && <p className={styles.error} role="alert">{applyError}</p>}
          {applyResult && (
            <>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>Operation ID</dt><dd><code>{applyResult.operation_id}</code></dd></div>
                <div><dt>Status / result</dt><dd>{applyResult.status} / {applyResult.result}</dd></div>
                <div><dt>Apply backup ID</dt><dd><code>{applyResult.apply_backup_id}</code></dd></div>
              </dl>
              <ul className={styles.migrationPathList}>
                {applyResult.actions.map((action) => <li key={action.action_id}><code>{action.action_id}</code> — {action.status}; Trash entry: <code>{action.trash_entry_id}</code></li>)}
              </ul>
              <p className={styles.info} role="status">対象はTrashから復元可能です。最終削除はこのUIでは実行できません。Data Healthを再scanして結果を確認できます。</p>
              <button type="button" className={styles.button} onClick={onRescan} disabled={scanBusy}>Data Healthを再スキャン</button>
            </>
          )}
          {partialOperation && (
            <>
              <p className={styles.info} role="status">partial / error operationを保持しました。cleanup applyを再試行せず、operation・backup・Trash状態を確認してください。</p>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>Operation ID</dt><dd><code>{partialOperation.operationID}</code></dd></div>
                <div><dt>Status</dt><dd>{partialOperation.status ?? "API responseを確認"}</dd></div>
                <div><dt>Apply backup ID</dt><dd>{partialOperation.applyBackupID ? <code>{partialOperation.applyBackupID}</code> : "responseにありません"}</dd></div>
              </dl>
            </>
          )}
        </article>
      )}
    </section>
  );
}
