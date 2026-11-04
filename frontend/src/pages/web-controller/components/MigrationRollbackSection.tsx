import { useMemo, useRef, useState } from "react";

import {
  applyMigrationRollback,
  createMigrationRollbackDryRun,
  MigrationRollbackApplyAPIError,
  type MigrationRollbackApplyResult,
  type MigrationRollbackDryRunResult,
} from "../../../api/controller";
import styles from "../WebController.module.css";

type Props = { operationID: string };
type BusyStage = "dry-run" | "apply" | "";
const EMPTY_ROLLBACK_TARGETS: MigrationRollbackDryRunResult["plan"]["payload"]["targets"] = [];

function sameIDs(left: string[], right: string[]): boolean {
  const sortedLeft = [...left].sort();
  const sortedRight = [...right].sort();
  return sortedLeft.length === sortedRight.length && sortedLeft.every((id, index) => id === sortedRight[index]);
}

function reasonLabel(reason: string): string {
  const labels: Record<string, string> = {
    eligible: "rollback候補（apply後hashと一致）",
    provenance_unconfirmed: "変更完了の証拠なし",
    unsafe_or_unreadable_target: "安全に読めない、またはsymlink等",
    missing_target: "targetが見つからない",
    invalid_target_type: "targetが通常fileではない",
    modified_target: "apply後に内容が変更された",
    apply_backup_missing: "元migrationのapply backupがない",
  };
  return labels[reason] ?? reason;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "Migration rollback APIの処理に失敗しました。";
}

function validateDryRun(
  result: MigrationRollbackDryRunResult,
  operationID: string,
): MigrationRollbackDryRunResult["plan"] {
  const { plan } = result;
  const payloadTargets = plan.payload.targets;
  if (
    plan.operation_type !== "migration_rollback" ||
    plan.payload_schema_id !== "migration_rollback.v1" ||
    plan.backup_requirement !== "apply" ||
    plan.payload.migration_operation_id !== operationID ||
    !plan.plan_id || !plan.plan_digest ||
    result.targets.length !== payloadTargets.length ||
    plan.actions.length !== payloadTargets.length
  ) {
    throw new Error("rollback dry-runのplan envelopeがoperationまたはtarget一覧と一致しません。applyは停止しました。");
  }

  const responseByID = new Map(result.targets.map((target) => [target.action_id, target]));
  const actionByID = new Map(plan.actions.map((action) => [action.action_id, action]));
  const targetIDs = payloadTargets.map((target) => target.action_id);
  if (
    responseByID.size !== payloadTargets.length ||
    actionByID.size !== payloadTargets.length ||
    !sameIDs(result.targets.map((target) => target.action_id), targetIDs)
  ) {
    throw new Error("rollback dry-runのaction IDが重複または不一致です。applyは停止しました。");
  }

  let eligibleCount = 0;
  for (const target of payloadTargets) {
    const responseTarget = responseByID.get(target.action_id);
    const action = actionByID.get(target.action_id);
    if (
      !responseTarget || !action ||
      responseTarget.target_path !== target.target_path ||
      responseTarget.apply_sha256 !== target.apply_sha256 ||
      responseTarget.current_sha256 !== target.current_sha256 ||
      responseTarget.current_exists !== target.current_exists ||
      responseTarget.current_kind !== target.current_kind ||
      responseTarget.eligible !== target.eligible ||
      responseTarget.reason !== target.reason ||
      responseTarget.expected_after_state !== target.expected_after_state ||
      JSON.stringify(responseTarget.dependencies) !== JSON.stringify(target.dependencies) ||
      action.action !== "rollback_target" ||
      action.selectable !== target.eligible ||
      action.destructive !== target.eligible ||
      (target.eligible && (
        action.backup_sources?.length !== 1 ||
        action.backup_sources[0] !== target.target_path ||
        action.backup_file_count !== 1
      )) ||
      (!target.eligible && Boolean(action.backup_sources?.length))
    ) {
      throw new Error(`rollback dry-runのtarget/action内容が一致しません（${target.action_id}）。applyは停止しました。`);
    }
    if (target.eligible) {
      eligibleCount += 1;
      if (
        target.reason !== "eligible" || !target.current_exists || target.current_kind !== "file" ||
        !target.apply_sha256 || target.current_sha256 !== target.apply_sha256 ||
        target.expected_after_state !== "target_moved_to_trash"
      ) {
        throw new Error(`eligible targetのhash/stateが不正です（${target.action_id}）。applyは停止しました。`);
      }
    } else if (target.reason === "eligible" || target.expected_after_state !== "target_unchanged") {
      throw new Error(`除外targetのstateが不正です（${target.action_id}）。applyは停止しました。`);
    }
  }
  if (plan.ready_to_apply !== (eligibleCount > 0)) {
    throw new Error("rollback dry-runのready_to_applyとeligible targetが一致しません。applyは停止しました。");
  }
  return plan;
}

export function MigrationRollbackSection({ operationID }: Props) {
  const [dryRun, setDryRun] = useState<MigrationRollbackDryRunResult["plan"] | null>(null);
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [busy, setBusy] = useState<BusyStage>("");
  const [error, setError] = useState("");
  const [applyAttempted, setApplyAttempted] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [applyResult, setApplyResult] = useState<MigrationRollbackApplyResult | null>(null);
  const [partialOperation, setPartialOperation] = useState<{
    operationID: string;
    status?: string;
    applyBackupID?: string;
  } | null>(null);
  const inFlightRef = useRef(false);

  const targets = dryRun?.payload.targets ?? EMPTY_ROLLBACK_TARGETS;
  const eligibleTargets = useMemo(() => targets.filter((target) => target.eligible), [targets]);
  const actionByID = useMemo(
    () => new Map((dryRun?.actions ?? []).map((action) => [action.action_id, action])),
    [dryRun?.actions],
  );
  const selectedTargets = targets.filter((target) => selectedIDs.includes(target.action_id) && target.eligible);
  const selectedActionIDs = selectedTargets.map((target) => target.action_id);
  const canApply = Boolean(dryRun?.ready_to_apply && dryRun.plan_digest && selectedActionIDs.length > 0 && !applyAttempted && !applyResult && !partialOperation);

  async function runDryRun() {
    if (inFlightRef.current || applyResult || partialOperation) return;
    inFlightRef.current = true;
    setBusy("dry-run");
    setError("");
    setDryRun(null);
    setSelectedIDs([]);
    setApplyAttempted(false);
    setApplyError("");
    try {
      const result = await createMigrationRollbackDryRun(operationID);
      validateDryRun(result, operationID);
      setDryRun(result.plan);
    } catch (dryRunError: unknown) {
      setError(describeError(dryRunError));
    } finally {
      inFlightRef.current = false;
      setBusy("");
    }
  }

  async function applyRollback() {
    if (!dryRun || !canApply || inFlightRef.current) return;
    const targetsText = selectedTargets.map((target) =>
      `${target.target_path}\n  apply SHA-256: ${target.apply_sha256}\n  current SHA-256: ${target.current_sha256}`,
    ).join("\n");
    const confirmed = window.confirm(
      `Migration rollbackを実行します。\n\n` +
      `Rollback plan: ${dryRun.plan_id}\n元migration operation: ${operationID}\n` +
      `選択target: ${selectedTargets.length}件\n元のapply backup: ${dryRun.payload.apply_backup_id || "なし"}\n\n` +
      `選択targetは削除せずTrashへ移動し、rollback直前に別のpoint-in-time backupを作成します。\n` +
      `Room / Device / IR scaffoldと旧sourceはrollback対象外です。backupの自動restoreや再試行は行いません。\n\n` +
      `${targetsText}\n\n続行しますか？`,
    );
    if (!confirmed) return;

    inFlightRef.current = true;
    setBusy("apply");
    setApplyAttempted(true);
    setApplyError("");
    try {
      const result = await applyMigrationRollback({
        plan_id: dryRun.plan_id,
        plan_digest: dryRun.plan_digest!,
        action_ids: selectedActionIDs,
        confirm: true,
      });
      setApplyResult(result);
      if (!sameIDs(result.actions.map((action) => action.action_id), selectedActionIDs)) {
        setApplyError("rollback responseのaction一覧が選択内容と一致しません。operationを再実行せず手動確認してください。");
      }
    } catch (rollbackError: unknown) {
      setApplyError(describeError(rollbackError));
      if (rollbackError instanceof MigrationRollbackApplyAPIError && rollbackError.operationID) {
        setPartialOperation({
          operationID: rollbackError.operationID,
          ...(rollbackError.operationStatus ? { status: rollbackError.operationStatus } : {}),
          ...(rollbackError.applyBackupID ? { applyBackupID: rollbackError.applyBackupID } : {}),
        });
      }
    } finally {
      inFlightRef.current = false;
      setBusy("");
    }
  }

  return (
    <section className={styles.migrationStage} aria-labelledby="migration-rollback-title">
      <div className={styles.deviceCardHeader}>
        <h4 id="migration-rollback-title" className={styles.deviceCardTitle}>Migration rollback</h4>
        <span className={styles.planPending}>dry-run後に個別確認</span>
      </div>
      <p className={styles.helperText}>
        rollback dry-runは候補を保存しますがruntime dataは変更しません。applyでは人手変更のないeligible targetだけをTrashへ移動します。Room / Device / IR scaffoldと旧sourceは残り、backupのrestoreは自動実行しません。
      </p>
      <p className={styles.helperText}>Migration operation: <code>{operationID}</code></p>
      <button
        type="button"
        className={styles.button}
        onClick={() => void runDryRun()}
        disabled={busy !== "" || Boolean(applyResult) || Boolean(partialOperation)}
      >
        {busy === "dry-run" ? "rollback候補を確認中..." : dryRun ? "再dry-run（新しい固定plan）" : "rollback dry-run"}
      </button>
      {error && <p className={styles.error} role="alert">rollback dry-runに失敗しました: {error}</p>}

      {dryRun && (
        <article className={styles.deviceCard} aria-labelledby="migration-rollback-plan-title">
          <div className={styles.deviceCardHeader}>
            <h5 id="migration-rollback-plan-title" className={styles.deviceCardTitle}>保存されたrollback plan</h5>
            <span className={dryRun.ready_to_apply ? styles.planReady : styles.planPending}>
              {dryRun.ready_to_apply ? `${eligibleTargets.length}件がrollback候補` : "安全なrollback候補なし"}
            </span>
          </div>
          <dl className={styles.migrationPlanMeta}>
            <div><dt>Rollback plan / digest</dt><dd><code>{dryRun.plan_id}</code> / <code>{dryRun.plan_digest}</code></dd></div>
            <div><dt>Migration plan</dt><dd><code>{dryRun.payload.migration_plan_id}</code> / <code>{dryRun.payload.migration_plan_digest}</code></dd></div>
            <div><dt>Original apply backup</dt><dd>{dryRun.payload.apply_backup_id ? <><code>{dryRun.payload.apply_backup_id}</code> — role apply / point_in_time_verified (Backend-validated)</> : "なし（eligible候補なし）"}</dd></div>
            <div><dt>Rollback backup</dt><dd>選択後、rollback apply内でpoint-in-time backupを自動作成</dd></div>
          </dl>

          <fieldset className={styles.migrationFields} disabled={busy !== "" || applyAttempted || !dryRun.ready_to_apply}>
            <legend>未変更targetだけを明示選択（初期状態は未選択）</legend>
            <ul className={styles.migrationActionChoices}>
              {targets.map((target) => {
                const checked = selectedIDs.includes(target.action_id) && target.eligible;
                const rollbackAction = actionByID.get(target.action_id);
                return (
                  <li key={target.action_id} className={styles.deviceCard}>
                    <label className={styles.migrationCheckbox}>
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={!target.eligible}
                        onChange={(event) => setSelectedIDs((current) => event.target.checked
                          ? [...current, target.action_id]
                          : current.filter((id) => id !== target.action_id))}
                      />
                      <span><strong>{target.action_id}</strong> — {target.eligible ? "eligible" : "除外: " + reasonLabel(target.reason)}</span>
                    </label>
                    <dl className={styles.migrationPlanMeta}>
                      <div><dt>Target</dt><dd><code>{target.target_path}</code></dd></div>
                      <div><dt>Apply SHA-256</dt><dd><code>{target.apply_sha256 || "なし（mutation未確定）"}</code></dd></div>
                      <div><dt>Current observation</dt><dd>{target.current_exists ? `${target.current_kind ?? "unknown"} — ` : "missing — "}<code>{target.current_sha256 || "hashなし"}</code></dd></div>
                      <div><dt>Rollback result</dt><dd>{target.expected_after_state}</dd></div>
                      {target.eligible && rollbackAction && (
                        <div><dt>Pre-rollback backup inventory</dt><dd><code>{rollbackAction.backup_inventory_hash ?? "hashなし"}</code>; {rollbackAction.backup_file_count ?? "?"} files; {rollbackAction.backup_total_size ?? "?"} bytes</dd></div>
                      )}
                      {target.dependencies.length > 0 && (
                        <div>
                          <dt>Dependencies (not removed)</dt>
                          <dd>
                            <ul className={styles.migrationPathList}>
                              {target.dependencies.map((dependency) => (
                                <li key={dependency.path}>
                                  <code>{dependency.path}</code> — {dependency.role}/{dependency.kind}; created_by_apply: {dependency.created_by_apply ? "yes" : "no"}; shared by: {dependency.shared_by_action_ids.join(", ") || "なし"}
                                </li>
                              ))}
                            </ul>
                          </dd>
                        </div>
                      )}
                    </dl>
                  </li>
                );
              })}
            </ul>
          </fieldset>

          {dryRun.ready_to_apply && selectedActionIDs.length === 0 && (
            <p className={styles.info} role="status">eligible targetから1件以上を選んでください。除外targetは選択できません。</p>
          )}
          {!dryRun.ready_to_apply && (
            <p className={styles.info} role="status">人手変更、欠落、未確認などで安全な候補がありません。rollback applyはありません。</p>
          )}
          <button type="button" className={styles.primaryButton} onClick={() => void applyRollback()} disabled={!canApply || busy !== ""}>
            {busy === "apply" ? "rollback中..." : applyAttempted ? "rollback要求済み（再実行不可）" : "確認してrollback（Trashへ移動）"}
          </button>
        </article>
      )}

      {applyAttempted && !applyResult && !partialOperation && applyError && (
        <div className={styles.actions}>
          <p className={styles.info}>同じrollback applyは再送しません。operation IDが応答にないため、対象状態を確認してから新しいdry-runを作成できます。</p>
          <button type="button" className={styles.button} onClick={() => void runDryRun()} disabled={busy !== ""}>
            新しいrollback dry-run
          </button>
        </div>
      )}

      {(applyResult || partialOperation || applyError) && (
        <article className={styles.deviceCard} aria-labelledby="migration-rollback-result-title">
          <h5 id="migration-rollback-result-title" className={styles.deviceCardTitle}>Rollback operation result</h5>
          {applyError && <p className={styles.error} role="alert">{applyError}</p>}
          {applyResult && (
            <>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>Operation ID</dt><dd><code>{applyResult.operation_id}</code></dd></div>
                <div><dt>Status / result</dt><dd>{applyResult.status} / {applyResult.result ?? "なし"}</dd></div>
                <div><dt>Rollback apply backup</dt><dd><code>{applyResult.apply_backup_id}</code>（mutation直前）</dd></div>
              </dl>
              <ul className={styles.migrationPathList}>
                {applyResult.actions.map((action) => (
                  <li key={action.action_id}><code>{action.action_id}</code> — {action.status}; Trash entry: {action.trash_entry_id ? <code>{action.trash_entry_id}</code> : "なし"}</li>
                ))}
              </ul>
              <p className={styles.info} role="status">targetはTrashに保持されています。Trash restoreまたはbackup restoreは別操作であり、この画面では自動復元しません。</p>
            </>
          )}
          {partialOperation && (
            <>
              <p className={styles.info} role="status">partial / error operationを保持しました。rollbackを再試行せず、Trash・backup・operation状態を手動確認してください。</p>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>Operation ID</dt><dd><code>{partialOperation.operationID}</code></dd></div>
                <div><dt>Status</dt><dd>{partialOperation.status ?? "API responseを確認"}</dd></div>
                <div><dt>Apply backup</dt><dd>{partialOperation.applyBackupID ? <code>{partialOperation.applyBackupID}</code> : "responseにありません"}</dd></div>
              </dl>
            </>
          )}
        </article>
      )}
    </section>
  );
}
