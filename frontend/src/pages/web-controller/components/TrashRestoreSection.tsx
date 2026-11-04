import { useRef, useState } from "react";

import {
  previewTrashRestore,
  restoreTrashEntry,
  TrashRestoreAPIError,
  type TrashRestorePreview as TrashRestorePreviewData,
  type TrashRestoreResult,
} from "../../../api/controller";
import styles from "../WebController.module.css";

type Props = {
  entryID: string;
  originalID: string;
  originalRelativePath: string;
  beforeInventorySHA256: string;
  onRestored: (result: TrashRestoreResult) => void;
};

function restoreReasonLabel(reason: string | undefined): string {
  const labels: Record<string, string> = {
    entry_not_sealed: "Trash entryが復元可能な保持状態ではありません。",
    unsafe_payload: "Trash payloadにsymlink等があり、安全に読み取れません。",
    payload_unavailable: "Trash payloadが見つからないか、読み取れません。",
    inventory_changed: "削除時点からTrash payloadの内容またはinventoryが変化しています。",
    unsafe_target: "復元先の親pathにsymlink等があり、安全に確認できません。",
    target_parent_unavailable: "復元先の親directoryを利用できません。",
    path_collision: "復元先pathが既に使用されています。既存dataは上書きしません。",
    target_unavailable: "復元先pathの状態を安全に確認できません。",
    id_check_unavailable: "同一IDの有無を安全に確認できません。",
    id_collision: "同一collection内に同じIDが既にあります。既存dataは上書きしません。",
    cross_device: "Trash payloadと復元先がatomic rename可能な同一filesystem上にありません。",
  };
  return reason ? (labels[reason] ?? `復元を停止しました（${reason}）。`) : "復元できない理由を確認できません。";
}

function validatePreview(
  preview: TrashRestorePreviewData,
  expected: Pick<Props, "entryID" | "originalID" | "originalRelativePath">,
): void {
  if (
    preview.entry_id !== expected.entryID ||
    preview.original_id !== expected.originalID ||
    preview.original_relative_path !== expected.originalRelativePath ||
    typeof preview.path_available !== "boolean" ||
    typeof preview.id_available !== "boolean" ||
    typeof preview.restore_available !== "boolean" ||
    typeof preview.expected_inventory_hash !== "string"
  ) {
    throw new Error("restore dry-runのentry/path/availabilityが選択中のTrash entryと一致しません。復元を停止しました。");
  }
  if (preview.restore_available && (
    !preview.path_available || !preview.id_available || preview.reason ||
    !/^sha256:[a-f0-9]{64}$/.test(preview.expected_inventory_hash)
  )) {
    throw new Error("restore dry-runの復元可否または確認hashが不整合です。復元を停止しました。");
  }
}

function validateRestoreResult(
  result: TrashRestoreResult,
  expected: Pick<Props, "entryID" | "originalID" | "originalRelativePath">,
): void {
  if (
    result.entry_id !== expected.entryID ||
    result.original_id !== expected.originalID ||
    result.original_relative_path !== expected.originalRelativePath ||
    !result.operation_id
  ) {
    throw new Error("restore responseがdry-run対象と一致しません。operation履歴とruntime dataを確認してください。再実行は禁止です。");
  }
}

function describeError(error: unknown): string {
  if (error instanceof TrashRestoreAPIError && error.status === 409) {
    return `${error.message} dry-run後にentryまたは復元先が変化した可能性があります。画面を更新して再確認してください。`;
  }
  if (error instanceof TrashRestoreAPIError && error.status >= 500) {
    return `${error.message} 復元が部分的に進んだ可能性があります。operation履歴とTrash/runtime dataを確認するまで再実行しないでください。`;
  }
  return error instanceof Error ? error.message : "Trash restore APIの処理に失敗しました。";
}

export function TrashRestoreSection({
  entryID,
  originalID,
  originalRelativePath,
  beforeInventorySHA256,
  onRestored,
}: Props) {
  const [preview, setPreview] = useState<TrashRestorePreviewData | null>(null);
  const [dryRunError, setDryRunError] = useState("");
  const [restoreError, setRestoreError] = useState("");
  const [result, setResult] = useState<TrashRestoreResult | null>(null);
  const [busy, setBusy] = useState<"dry-run" | "restore" | "">("");
  const [restoreAttempted, setRestoreAttempted] = useState(false);
  const inFlightRef = useRef(false);

  const canRunDryRun = !busy && !restoreAttempted && !result;
  const canRestore = Boolean(preview?.restore_available && canRunDryRun);

  async function runDryRun() {
    if (!canRunDryRun || inFlightRef.current) return;
    inFlightRef.current = true;
    setBusy("dry-run");
    setDryRunError("");
    setRestoreError("");
    setPreview(null);
    try {
      const response = await previewTrashRestore(entryID);
      validatePreview(response, { entryID, originalID, originalRelativePath });
      setPreview(response);
    } catch (error: unknown) {
      setDryRunError(describeError(error));
    } finally {
      inFlightRef.current = false;
      setBusy("");
    }
  }

  async function restore() {
    if (!preview || !canRestore || inFlightRef.current) return;
    const confirmed = window.confirm(
      `Trash entryを元の場所へ復元します。既存dataは上書きしません。\n\n` +
      `Entry: ${entryID}\n元識別子: ${originalID}\n復元先: ${originalRelativePath}\n` +
      `dry-run inventory hash: ${preview.expected_inventory_hash}\n\n` +
      `Backendは実行直前にhash・path・ID衝突を再検証します。競合時は停止します。\n` +
      `失敗時に自動再試行・自動rollbackはしません。部分実行の可能性がある場合はoperation履歴を確認してください。\n\n` +
      `このentryを復元しますか？`,
    );
    if (!confirmed) return;

    inFlightRef.current = true;
    setBusy("restore");
    setRestoreAttempted(true);
    setRestoreError("");
    try {
      const response = await restoreTrashEntry(entryID, {
        expected_inventory_hash: preview.expected_inventory_hash,
        confirm: true,
      });
      validateRestoreResult(response, { entryID, originalID, originalRelativePath });
      setResult(response);
      onRestored(response);
    } catch (error: unknown) {
      setRestoreError(describeError(error));
    } finally {
      inFlightRef.current = false;
      setBusy("");
    }
  }

  return (
    <section className={styles.migrationStage} aria-labelledby="trash-restore-title">
      <h4 id="trash-restore-title" className={styles.deviceCardTitle}>Trash restore</h4>
      <p className={styles.helperText}>
        dry-runで元path、ID衝突、payload inventoryを再確認します。復元先に既存dataがある場合は上書きせず停止します。
      </p>
      <button
        type="button"
        className={styles.button}
        onClick={() => void runDryRun()}
        disabled={!canRunDryRun}
      >
        {busy === "dry-run" ? "復元可否を確認中..." : "復元可否を再確認（dry-run）"}
      </button>

      {dryRunError && <p className={styles.error} role="alert">復元dry-runに失敗しました: {dryRunError}</p>}
      {busy === "dry-run" && <p className={styles.info} role="status">runtime dataとTrashを変更せず、復元条件を確認しています...</p>}

      {preview && (
        <>
          <dl className={styles.migrationPlanMeta}>
            <div><dt>Entry / 元識別子</dt><dd><code>{preview.entry_id}</code> / <code>{preview.original_id}</code></dd></div>
            <div><dt>復元予定path</dt><dd><code>{preview.original_relative_path}</code></dd></div>
            <div><dt>path衝突確認</dt><dd>{preview.path_available ? "空き" : "利用不可・衝突"}</dd></div>
            <div><dt>ID衝突確認</dt><dd>{preview.id_available ? "空き" : "利用不可・衝突"}</dd></div>
            <div><dt>削除前inventory SHA-256</dt><dd><code>{beforeInventorySHA256 || "記録なし"}</code></dd></div>
            {preview.expected_inventory_hash && (
              <div>
                <dt>今回のdry-run確認hash</dt>
                <dd><code>{preview.expected_inventory_hash}</code>（削除前inventory SHA-256とは別の確認値）</dd>
              </div>
            )}
          </dl>
          {preview.restore_available ? (
            <p className={styles.planReady} role="status">復元可能です。確認後に限り、このentryだけを復元できます。</p>
          ) : (
            <p className={styles.planPending} role="alert">{restoreReasonLabel(preview.reason)}</p>
          )}
          {preview.reason === "inventory_changed" && (
            <p className={styles.error} role="alert">
              削除前inventory SHA-256（表示値）と現在のTrash payloadが一致しないため、復元を停止しました。
            </p>
          )}
          <button
            type="button"
            className={styles.button}
            onClick={() => void restore()}
            disabled={!canRestore}
          >
            {busy === "restore" ? "復元中..." : "このentryを復元"}
          </button>
        </>
      )}

      {restoreError && (
        <div>
          <p className={styles.error} role="alert">復元結果を確認できません: {restoreError}</p>
          {restoreAttempted && <p className={styles.error}>同じentryの再実行はブロックされています。operation履歴とruntime dataを確認してください。</p>}
        </div>
      )}
      {result && (
        <p className={styles.info} role="status">
          復元しました: <code>{result.original_relative_path}</code>（operation <code>{result.operation_id}</code>）。Trash entryは復元済みとして表示します。
        </p>
      )}
    </section>
  );
}
