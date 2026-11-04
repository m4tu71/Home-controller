import { useMemo, useRef, useState } from "react";

import {
  applyMigration,
  createMigrationRehearsalBackup,
  fetchBackupDetail,
  MigrationApplyAPIError,
  previewBackupRehearsal,
  runBackupRehearsal,
  verifyMigration,
  type BackupCreateResponse,
  type BackupDetailResponse,
  type BackupRestorePreview,
  type MigrationApplyResult,
  type MigrationDryRunPlan,
  type MigrationVerifyResult,
} from "../../../api/controller";
import { MigrationRollbackSection } from "./MigrationRollbackSection";
import styles from "../WebController.module.css";

type Props = { plan: MigrationDryRunPlan };
type BusyStage = "backup" | "preview" | "rehearsal" | "apply" | "verify" | "";

const bedroomPresetActionIDs = [
  "roomlight_on_ch3",
  "roomlight_full_ch3",
  "roomlight_off_ch3",
  "roomlight_night_ch3",
  "roomlight_bright_up_ch3",
  "roomlight_bright_down_ch3",
];

function sameIDs(left: string[], right: string[]): boolean {
  const sortedLeft = [...left].sort();
  const sortedRight = [...right].sort();
  return sortedLeft.length === sortedRight.length && sortedLeft.every((id, index) => id === sortedRight[index]);
}

function formatBytes(value: number): string {
  return `${new Intl.NumberFormat("ja-JP").format(value)} bytes`;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "Migration maintenance APIの処理に失敗しました。";
}

export function MigrationApplyVerifySection({ plan }: Props) {
  const payloadByID = useMemo(
    () => new Map(plan.payload.actions.map((action) => [action.action_id, action])),
    [plan.payload.actions],
  );
  const selectableActions = useMemo(
    () => plan.actions.filter((action) => action.selectable && payloadByID.has(action.action_id)),
    [payloadByID, plan.actions],
  );
  const isBedroomPresetPlan = plan.payload_schema_id === "migration.v2";
  const bedroomPresetContractValid = !isBedroomPresetPlan || (
    plan.actions.length === bedroomPresetActionIDs.length &&
    plan.payload.actions.length === bedroomPresetActionIDs.length &&
    plan.actions.every((action) => bedroomPresetActionIDs.includes(action.action_id) && action.selectable) &&
    plan.payload.actions.every((action) => bedroomPresetActionIDs.includes(action.action_id)) &&
    bedroomPresetActionIDs.every((id) => plan.actions.some((action) => action.action_id === id) && payloadByID.has(id))
  );
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [busy, setBusy] = useState<BusyStage>("");
  const requestInFlightRef = useRef(false);
  const [error, setError] = useState("");
  const [backupSummary, setBackupSummary] = useState<BackupCreateResponse | null>(null);
  const [backupDetail, setBackupDetail] = useState<BackupDetailResponse | null>(null);
  const [preview, setPreview] = useState<BackupRestorePreview | null>(null);
  const [applyAttempted, setApplyAttempted] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [applyResult, setApplyResult] = useState<MigrationApplyResult | null>(null);
  const [partialApply, setPartialApply] = useState<{
    operationID: string;
    status?: string;
    applyBackupID?: string;
  } | null>(null);
  const [verifyResult, setVerifyResult] = useState<MigrationVerifyResult | null>(null);
  const [verifyError, setVerifyError] = useState("");

  const selectedActions = selectedIDs
    .map((id) => plan.actions.find((action) => action.action_id === id))
    .filter((action): action is NonNullable<typeof action> => Boolean(action));
  const affectedPaths = [...new Set(selectedIDs.flatMap((id) => {
    const action = payloadByID.get(id);
    return action?.planned_writes.map((write) => write.path) ?? [];
  }))];
  const selectedBackupSources = [...new Set(selectedActions.flatMap((action) => action.backup_sources ?? []))].sort();
  const selectedActionsHaveBackupSources = selectedActions.length > 0 && selectedActions.every(
    (action) => Boolean(action.backup_sources?.length),
  );
  const hasValidSelection = selectedIDs.length > 0 && selectedIDs.length === selectedActions.length;
  const selectionComplete = isBedroomPresetPlan
    ? bedroomPresetContractValid && sameIDs(selectedIDs, bedroomPresetActionIDs)
    : hasValidSelection;
  const canCreateBackup = plan.ready_to_apply && Boolean(plan.plan_digest) && selectionComplete && selectedActionsHaveBackupSources;
  const selectionLocked = backupSummary !== null;
  const applyOperationID = applyResult?.operation_id ?? partialApply?.operationID ?? "";
  const rehearsalVerified = backupDetail?.role === "rehearsal" && backupDetail.state === "rehearsal_verified";

  function validateBackup(
    backup: BackupDetailResponse,
    expectedState: "snapshot_verified" | "rehearsal_verified" | "either",
    createdSummary: BackupCreateResponse | null = backupSummary,
  ): void {
    if (backup.backup_id !== createdSummary?.backup_id) throw new Error("backup detailのIDが作成結果と一致しません。");
    const stateMatches = expectedState === "either"
      ? backup.state === "snapshot_verified" || backup.state === "rehearsal_verified"
      : backup.state === expectedState;
    if (backup.role !== "rehearsal" || !stateMatches) {
      throw new Error(`backup role/stateが不正です（${backup.role}/${backup.state}）。`);
    }
    if (backup.plan_id !== plan.plan_id || !sameIDs(backup.action_ids, selectedIDs)) {
      throw new Error("backupのplan/action選択が現在のplanと一致しません。applyは停止しました。");
    }
    if (!sameIDs(backup.sources, selectedBackupSources)) {
      throw new Error("backup source一覧が選択actionのsourceと一致しません。applyは停止しました。");
    }
    if (
      backup.inventory_hash !== createdSummary?.inventory_hash ||
      backup.file_count !== createdSummary.file_count ||
      backup.total_size !== createdSummary.total_size
    ) {
      throw new Error("backup inventoryが作成結果から変化しています。applyは停止しました。");
    }
  }

  async function loadBackupDetail(
    backupID: string,
    expectedState: "snapshot_verified" | "rehearsal_verified" | "either",
    createdSummary: BackupCreateResponse | null = backupSummary,
  ) {
    const detail = await fetchBackupDetail(backupID);
    validateBackup(detail, expectedState, createdSummary);
    setBackupDetail(detail);
    return detail;
  }

  async function createBackup() {
    if (!canCreateBackup || requestInFlightRef.current) return;
    requestInFlightRef.current = true;
    setBusy("backup");
    setError("");
    setBackupSummary(null);
    setBackupDetail(null);
    setPreview(null);
    try {
      const created = await createMigrationRehearsalBackup(
        plan.plan_id,
        plan.plan_digest!,
        selectedIDs,
      );
      setBackupSummary(created);
      if (
        created.role !== "rehearsal" || created.state !== "snapshot_verified" ||
        created.plan_id !== plan.plan_id || !sameIDs(created.action_ids, selectedIDs)
      ) {
        throw new Error("作成されたbackupのrole/state/plan/actionが要求と一致しません。applyは停止しました。");
      }
      await loadBackupDetail(created.backup_id, "snapshot_verified", created);
    } catch (stageError: unknown) {
      setError(describeError(stageError));
    } finally {
      requestInFlightRef.current = false;
      setBusy("");
    }
  }

  async function reloadBackupDetail() {
    if (!backupSummary || requestInFlightRef.current) return;
    requestInFlightRef.current = true;
    setBusy("backup");
    setError("");
    try {
      await loadBackupDetail(backupSummary.backup_id, "either");
    } catch (stageError: unknown) {
      setError(describeError(stageError));
    } finally {
      requestInFlightRef.current = false;
      setBusy("");
    }
  }

  async function runPreview() {
    if (!backupSummary || !backupDetail || backupDetail.state !== "snapshot_verified" || requestInFlightRef.current) return;
    requestInFlightRef.current = true;
    setBusy("preview");
    setError("");
    setPreview(null);
    try {
      const result = await previewBackupRehearsal(backupSummary.backup_id);
      if (
        result.backup_id !== backupSummary.backup_id || result.role !== "rehearsal" ||
        result.state !== "snapshot_verified" || result.source_inventory_hash !== backupSummary.inventory_hash
      ) {
        throw new Error("隔離rehearsal previewがbackup inventoryと一致しません。");
      }
      setPreview(result);
    } catch (stageError: unknown) {
      setError(describeError(stageError));
    } finally {
      requestInFlightRef.current = false;
      setBusy("");
    }
  }

  async function rehearseBackup() {
    if (!backupSummary || !preview?.can_rehearse || requestInFlightRef.current) return;
    requestInFlightRef.current = true;
    setBusy("rehearsal");
    setError("");
    setBackupDetail(null);
    setPreview(null);
    try {
      const result = await runBackupRehearsal(backupSummary.backup_id);
      if (
        result.backup_id !== backupSummary.backup_id || result.role !== "rehearsal" ||
        result.state !== "rehearsal_verified" || result.inventory_hash !== backupSummary.inventory_hash
      ) {
        throw new Error("隔離rehearsalのreadback結果がbackup inventoryと一致しません。");
      }
      await loadBackupDetail(backupSummary.backup_id, "rehearsal_verified");
    } catch (stageError: unknown) {
      setError(describeError(stageError));
    } finally {
      requestInFlightRef.current = false;
      setBusy("");
    }
  }

  async function applySelectedActions() {
    if (!backupSummary || !rehearsalVerified || !canCreateBackup || applyAttempted || requestInFlightRef.current) return;
    const pathPreview = affectedPaths.length ? affectedPaths.join("\n") : "（対象pathなし）";
    const confirmed = window.confirm(
      `Migration applyを実行します。\n\n` +
      `Plan: ${plan.plan_id}\n選択action: ${selectedIDs.length}件\n` +
      `事前rehearsal backup: ${backupSummary.backup_id}\n` +
      `apply直前のpoint-in-time backupはBackendが別途作成します。\n\n` +
      `対象path:\n${pathPreview}\n\n` +
      `失敗時もpartial resultとbackupを保持し、自動再試行・自動rollbackはしません。続行しますか？`,
    );
    if (!confirmed) return;

    requestInFlightRef.current = true;
    setBusy("apply");
    setApplyAttempted(true);
    setApplyError("");
    setVerifyResult(null);
    setVerifyError("");
    try {
      const result = await applyMigration({
        plan_id: plan.plan_id,
        plan_digest: plan.plan_digest!,
        action_ids: selectedIDs,
        rehearsal_backup_id: backupSummary.backup_id,
        confirm: true,
      });
      setApplyResult(result);
    } catch (applyStageError: unknown) {
      setApplyError(describeError(applyStageError));
      if (applyStageError instanceof MigrationApplyAPIError && applyStageError.operationID) {
        setPartialApply({
          operationID: applyStageError.operationID,
          ...(applyStageError.operationStatus ? { status: applyStageError.operationStatus } : {}),
          ...(applyStageError.applyBackupID ? { applyBackupID: applyStageError.applyBackupID } : {}),
        });
      }
    } finally {
      requestInFlightRef.current = false;
      setBusy("");
    }
  }

  async function verifyOperation() {
    if (!applyOperationID || requestInFlightRef.current) return;
    requestInFlightRef.current = true;
    setBusy("verify");
    setVerifyError("");
    setVerifyResult(null);
    try {
      setVerifyResult(await verifyMigration(applyOperationID));
    } catch (verifyStageError: unknown) {
      setVerifyError(describeError(verifyStageError));
    } finally {
      requestInFlightRef.current = false;
      setBusy("");
    }
  }

  return (
    <section className={styles.migrationStage} aria-labelledby="migration-apply-verify-title">
      <div className={styles.deviceCardHeader}>
        <h4 id="migration-apply-verify-title" className={styles.deviceCardTitle}>Apply / verify</h4>
        <span className={plan.ready_to_apply ? styles.planReady : styles.planPending}>
          {plan.ready_to_apply ? "段階確認が必要" : "apply不可"}
        </span>
      </div>
      {!plan.ready_to_apply && (
        <p className={styles.info} role="status">planがready_to_applyではないため、backup / applyは停止しています。</p>
      )}
      {!plan.plan_digest && (
        <p className={styles.info} role="status">plan digestがないため、backup / applyは停止しています。</p>
      )}

      {isBedroomPresetPlan && (
        <p className={bedroomPresetContractValid ? styles.info : styles.error} role={bedroomPresetContractValid ? "status" : "alert"}>
          {bedroomPresetContractValid
            ? `migration.v2は6 action全件が必須です（現在 ${selectedIDs.length}/6 件選択）。`
            : "migration.v2のplan action一覧が規定の6件と一致しないため、backup / applyを停止しています。"}
        </p>
      )}

      <fieldset className={styles.migrationFields} disabled={selectionLocked || busy !== "" || !plan.ready_to_apply || !bedroomPresetContractValid}>
        <legend>apply対象を明示選択（初期状態は未選択）</legend>
        {selectableActions.length === 0 ? (
          <p className={styles.info}>このplanに選択可能なactionはありません。</p>
        ) : (
          <ul className={styles.migrationActionChoices}>
            {selectableActions.map((action) => {
              const payloadAction = payloadByID.get(action.action_id)!;
              const checked = selectedIDs.includes(action.action_id);
              return (
                <li key={action.action_id} className={styles.deviceCard}>
                  <label className={styles.migrationCheckbox}>
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(event) => setSelectedIDs((current) => event.target.checked
                        ? [...current, action.action_id]
                        : current.filter((id) => id !== action.action_id))}
                    />
                    <span><strong>{action.action_id}</strong> — {action.action}{action.destructive ? "（destructive）" : ""}</span>
                  </label>
                  <dl className={styles.migrationPlanMeta}>
                    <div><dt>Affected paths</dt><dd><ul className={styles.migrationPathList}>{payloadAction.planned_writes.map((write) => <li key={write.path}><code>{write.path}</code></li>)}</ul></dd></div>
                    <div><dt>Backup sources</dt><dd>{action.backup_sources?.length ? action.backup_sources.map((source) => <code key={source}>{source} </code>) : "未定義"}</dd></div>
                    <div><dt>Planned source inventory</dt><dd><code>{action.backup_inventory_hash ?? "なし"}</code>; {action.backup_file_count ?? "?"} files; {action.backup_total_size !== undefined ? formatBytes(action.backup_total_size) : "size unavailable"}</dd></div>
                  </dl>
                </li>
              );
            })}
          </ul>
        )}
      </fieldset>

      <div className={styles.actions}>
        <button
          type="button"
          className={styles.primaryButton}
          onClick={() => void createBackup()}
          disabled={!canCreateBackup || selectionLocked || busy !== ""}
        >
          {busy === "backup" ? "rehearsal backup作成中..." : isBedroomPresetPlan ? "6件すべて選択してrehearsal backupを作成" : "選択actionのrehearsal backupを作成"}
        </button>
      </div>
      {!isBedroomPresetPlan && !hasValidSelection && plan.ready_to_apply && <p className={styles.info}>apply前に1件以上の選択可能actionを選んでください。</p>}
      {isBedroomPresetPlan && bedroomPresetContractValid && !selectionComplete && <p className={styles.info}>6件すべてを選択するまでrehearsal backup・rehearsal・applyへ進めません。</p>}
      {hasValidSelection && !selectedActionsHaveBackupSources && <p className={styles.info}>選択actionにbackup sourceがないため実行できません。</p>}
      {error && <p className={styles.error} role="alert">{error}</p>}

      {backupSummary && (
        <article className={styles.deviceCard} aria-labelledby="migration-rehearsal-backup-title">
          <h5 id="migration-rehearsal-backup-title" className={styles.deviceCardTitle}>事前rehearsal backup</h5>
          <dl className={styles.migrationPlanMeta}>
            <div><dt>Backup ID / role</dt><dd><code>{backupSummary.backup_id}</code> / {backupDetail?.role ?? backupSummary.role}</dd></div>
            <div><dt>State</dt><dd>{backupDetail?.state ?? backupSummary.state}</dd></div>
            <div><dt>Plan / actions</dt><dd><code>{backupSummary.plan_id}</code> / {backupSummary.action_ids.join(", ")}</dd></div>
            <div><dt>Inventory</dt><dd><code>{backupDetail?.inventory_hash ?? backupSummary.inventory_hash}</code>; {backupDetail?.file_count ?? backupSummary.file_count} files; {formatBytes(backupDetail?.total_size ?? backupSummary.total_size)}</dd></div>
            <div><dt>Backup sources</dt><dd>{backupDetail?.sources?.join(", ") ?? "詳細を照合中"}</dd></div>
            {backupDetail && <div><dt>Backup files</dt><dd><ul className={styles.migrationPathList}>{backupDetail.files.map((file) => <li key={file.path}><code>{file.path}</code> — {file.size} bytes / <code>{file.sha256}</code>{file.json_status ? ` / JSON ${file.json_status}` : ""}</li>)}</ul></dd></div>}
          </dl>
          {!backupDetail && (
            <button type="button" className={styles.button} onClick={() => void reloadBackupDetail()} disabled={busy !== ""}>
              backup detailを再読込
            </button>
          )}
          {backupDetail?.state === "snapshot_verified" && (
            <div className={styles.actions}>
              <button type="button" className={styles.button} onClick={() => void runPreview()} disabled={busy !== ""}>
                {busy === "preview" ? "隔離先を確認中..." : "隔離rehearsalのdry-run"}
              </button>
            </div>
          )}
          {preview && (
            <div className={styles.deviceCard}>
              <h5 className={styles.deviceCardTitle}>隔離rehearsal preview</h5>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>隔離target</dt><dd><code>{preview.target}</code></dd></div>
                <div><dt>Capacity</dt><dd>required {formatBytes(preview.required_bytes)} / available {formatBytes(preview.available_bytes)}</dd></div>
                <div><dt>Target collision</dt><dd>{preview.target_exists ? `あり (${preview.target_inventory_hash ?? "hash unavailable"})` : "なし"}</dd></div>
                <div><dt>Rehearsal</dt><dd>{preview.can_rehearse ? "可能" : `不可${preview.reason ? `: ${preview.reason}` : ""}`}</dd></div>
              </dl>
              <button type="button" className={styles.button} onClick={() => void rehearseBackup()} disabled={!preview.can_rehearse || busy !== ""}>
                {busy === "rehearsal" ? "隔離readback中..." : "隔離rehearsalを実行"}
              </button>
            </div>
          )}
          {rehearsalVerified && <p className={styles.info} role="status">repository/byte readback済み。実runtime dataへのrestoreではありません。</p>}
        </article>
      )}

      {rehearsalVerified && (
        <article className={styles.deviceCard} aria-labelledby="migration-apply-title">
          <h5 id="migration-apply-title" className={styles.deviceCardTitle}>Migration apply</h5>
          <p className={styles.helperText}>選択した{selectedIDs.length} actionを適用します。Backendはmutation直前に別のpoint-in-time apply backupを作成します。失敗や部分適用を自動再試行・自動rollbackしません。</p>
          <p className={styles.helperText}>対象path: {affectedPaths.map((path) => <code key={path}>{path} </code>)}</p>
          <button type="button" className={styles.primaryButton} onClick={() => void applySelectedActions()} disabled={applyAttempted || busy !== ""}>
            {busy === "apply" ? "apply中..." : applyAttempted ? "apply要求済み（再実行不可）" : "確認してapply"}
          </button>
        </article>
      )}

      {(applyResult || partialApply || applyError) && (
        <article className={styles.deviceCard} aria-labelledby="migration-operation-result-title">
          <h5 id="migration-operation-result-title" className={styles.deviceCardTitle}>Apply operation result</h5>
          {applyError && <p className={styles.error} role="alert">{applyError}</p>}
          {applyResult && (
            <>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>Operation ID</dt><dd><code>{applyResult.operation_id}</code></dd></div>
                <div><dt>Status / result</dt><dd>{applyResult.status} / {applyResult.result ?? "なし"}</dd></div>
                <div><dt>Rehearsal backup</dt><dd><code>{applyResult.rehearsal_backup_id}</code></dd></div>
                <div><dt>Apply backup</dt><dd><code>{applyResult.apply_backup_id}</code>（自動作成、point-in-time）</dd></div>
              </dl>
              <ul className={styles.migrationPathList}>{applyResult.actions.map((action) => <li key={action.id}><code>{action.id}</code> — {action.action}: {action.status}{action.error ? ` — ${action.error}` : ""}</li>)}</ul>
            </>
          )}
          {partialApply && (
            <>
              <p className={styles.info} role="status">APIはpartial / error resultを返しました。状態を確認し、applyを再試行しないでください。</p>
              <dl className={styles.migrationPlanMeta}>
                <div><dt>Operation ID</dt><dd><code>{partialApply.operationID}</code></dd></div>
                <div><dt>Operation status</dt><dd>{partialApply.status ?? "API responseを確認"}</dd></div>
                <div><dt>Apply backup</dt><dd>{partialApply.applyBackupID ? <code>{partialApply.applyBackupID}</code> : "responseにありません"}</dd></div>
              </dl>
            </>
          )}
          {applyOperationID && (
            <button type="button" className={styles.button} onClick={() => void verifyOperation()} disabled={busy !== ""}>
              {busy === "verify" ? "verify中..." : "operationをverify（read-only）"}
            </button>
          )}
          {!applyOperationID && applyError && <p className={styles.info}>Operation IDがありません。新しいapplyを送る前にBackend maintenance stateを運用者が確認してください。</p>}
        </article>
      )}

      {applyOperationID && <MigrationRollbackSection operationID={applyOperationID} />}

      {verifyError && <p className={styles.error} role="alert">verifyに失敗しました: {verifyError}</p>}
      {verifyResult && (
        <article className={styles.deviceCard} aria-labelledby="migration-verify-result-title">
          <h5 id="migration-verify-result-title" className={styles.deviceCardTitle}>Repository verify: {verifyResult.status}</h5>
          <p className={styles.helperText}>Operation <code>{verifyResult.operation_id}</code>; plan <code>{verifyResult.plan_id}</code>。verifyはtargetを修復・変更しません。</p>
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
    </section>
  );
}
