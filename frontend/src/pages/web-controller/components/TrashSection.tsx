import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  fetchTrashDetail,
  fetchTrashEntries,
  type TrashDetailResponse,
  type TrashEntrySummary,
  type TrashListResponse,
  type TrashRestoreResult,
} from "../../../api/controller";
import { TrashRestoreSection } from "./TrashRestoreSection";
import { TrashRetentionStatus } from "./TrashRetentionStatus";
import styles from "../WebController.module.css";

const PAGE_SIZE = 25;
const KNOWN_TRASH_TYPES = [
  "room",
  "device",
  "ir_command",
  "ir_state",
  "legacy_ir_signal",
  "trash_file",
  "trash_directory",
];

const trashTypeLabels: Record<string, string> = {
  room: "Room",
  device: "Device",
  ir_command: "IR Command",
  ir_state: "IR State",
  legacy_ir_signal: "学習済みIR Signal",
  trash_file: "cleanup file",
  trash_directory: "cleanup directory",
};

const trashStatusLabels: Record<string, string> = {
  prepared: "準備中",
  sealed: "Trash保持中",
  restoring: "復元処理中",
  restored: "復元済み",
  final_deleted: "最終削除済み",
  recovery_required: "要確認",
};

function displayType(type: string): string {
  return (trashTypeLabels[type] ?? type) || "種別不明";
}

function displayStatus(status: string): string {
  return (trashStatusLabels[status] ?? status) || "状態不明";
}

function displayUTC(value: string | null | undefined): string {
  if (!value) return "記録なし";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "日時を読み取れません" : `${date.toISOString().replace(".000Z", "Z")} (UTC)`;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "Trash情報を取得できませんでした。";
}

export function TrashSection() {
  const [filterType, setFilterType] = useState("");
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<TrashListResponse | null>(null);
  const [listLoading, setListLoading] = useState(true);
  const [listError, setListError] = useState("");
  const [selectedEntryID, setSelectedEntryID] = useState("");
  const [detail, setDetail] = useState<TrashDetailResponse | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const listRequestSequence = useRef(0);
  const detailRequestSequence = useRef(0);

  const loadList = useCallback(async () => {
    const requestSequence = ++listRequestSequence.current;
    setListLoading(true);
    setListError("");
    setPage(null);
    setSelectedEntryID("");
    setDetail(null);
    setDetailError("");
    setDetailLoading(false);
    detailRequestSequence.current += 1;
    try {
      const response = await fetchTrashEntries({ type: filterType, offset, limit: PAGE_SIZE });
      if (requestSequence === listRequestSequence.current) setPage(response);
    } catch (error: unknown) {
      if (requestSequence === listRequestSequence.current) setListError(describeError(error));
    } finally {
      if (requestSequence === listRequestSequence.current) setListLoading(false);
    }
  }, [filterType, offset]);

  useEffect(() => {
    const timer = window.setTimeout(() => void loadList(), 0);
    return () => {
      window.clearTimeout(timer);
      listRequestSequence.current += 1;
    };
  }, [loadList]);

  async function loadDetail(entryID: string) {
    const requestSequence = ++detailRequestSequence.current;
    setSelectedEntryID(entryID);
    setDetail(null);
    setDetailError("");
    setDetailLoading(true);
    try {
      const response = await fetchTrashDetail(entryID);
      if (requestSequence === detailRequestSequence.current) setDetail(response);
    } catch (error: unknown) {
      if (requestSequence === detailRequestSequence.current) setDetailError(describeError(error));
    } finally {
      if (requestSequence === detailRequestSequence.current) setDetailLoading(false);
    }
  }

  const typeOptions = useMemo(() => {
    const types = new Set(KNOWN_TRASH_TYPES);
    page?.entries.forEach((entry) => types.add(entry.target_type));
    return [...types].sort((left, right) => displayType(left).localeCompare(displayType(right), "ja"));
  }, [page]);

  const selectedSummary = page?.entries.find((entry) => entry.entry_id === selectedEntryID) ?? null;
  const canGoBack = offset > 0 && !listLoading;
  const canGoForward = Boolean(page && offset + page.entries.length < page.total && !listLoading);

  function clearSelectedDetail() {
    setSelectedEntryID("");
    setDetail(null);
    setDetailError("");
    setDetailLoading(false);
    detailRequestSequence.current += 1;
  }

  function renderSummaryFields(entry: TrashEntrySummary) {
    return (
      <dl className={styles.migrationPlanMeta}>
        <div><dt>種別 / 状態</dt><dd>{displayType(entry.target_type)} / {displayStatus(entry.status)}</dd></div>
        <div><dt>元識別子</dt><dd><code>{entry.original_id || "不明"}</code></dd></div>
        <div><dt>元path (data root相対)</dt><dd><code>{entry.original_relative_path || "安全なpathを取得できません"}</code></dd></div>
        <div><dt>削除日時</dt><dd>{displayUTC(entry.deleted_at)}</dd></div>
        <div><dt>理由</dt><dd>{entry.reason || "記録なし"}</dd></div>
        <div><dt>関連operation</dt><dd>{entry.operation_type || "不明"} / <code>{entry.operation_id || "不明"}</code></dd></div>
        <div><dt>記録上の復元可否</dt><dd>{entry.restore_available ? "可能（復元時に再検証）" : "不可"}</dd></div>
      </dl>
    );
  }

  return (
    <section className={styles.migrationStage} aria-labelledby="trash-title">
      <div className={styles.deviceCardHeader}>
        <h3 id="trash-title" className={styles.deviceCardTitle}>Trash</h3>
        <button type="button" className={styles.statusButton} onClick={() => void loadList()} disabled={listLoading}>
          {listLoading ? "読み込み中..." : "Trashを再読み込み"}
        </button>
      </div>
      <p className={styles.helperText}>
        削除済みRoom / Device / IR Command / IR State / 学習済みIR dataを通常一覧と分けて確認します。復元はdetailのdry-runと明示確認が必要です。最終削除はこの画面から実行できません。
      </p>

      <div className={styles.formGroup}>
        <label className={styles.label} htmlFor="trash-type-filter">種別filter</label>
        <select
          id="trash-type-filter"
          className={styles.select}
          value={filterType}
          onChange={(event) => {
            setFilterType(event.target.value);
            setOffset(0);
            clearSelectedDetail();
          }}
        >
          <option value="">すべての種別</option>
          {typeOptions.map((type) => <option key={type} value={type}>{displayType(type)}</option>)}
        </select>
      </div>

      {listLoading && <p className={styles.info} role="status">Trash一覧を読み込んでいます...</p>}
      {!listLoading && listError && (
        <div>
          <p className={styles.error} role="alert">Trash一覧を取得できませんでした: {listError}</p>
          <button type="button" className={styles.button} onClick={() => void loadList()}>再試行</button>
        </div>
      )}
      {!listLoading && !listError && page?.total === 0 && (
        <p className={styles.info} role="status">Trash entryはありません。</p>
      )}
      {!listLoading && !listError && page && page.total > 0 && (
        <>
          <p className={styles.info} role="status">
            {page.offset + 1}–{page.offset + page.entries.length} / {page.total}件
          </p>
          <ul className={styles.migrationActionChoices}>
            {page.entries.map((entry) => (
              <li key={entry.entry_id}>
                <article className={styles.deviceCard}>
                  <div className={styles.deviceCardHeader}>
                    <h4 className={styles.deviceCardTitle}>{displayType(entry.target_type)} — {entry.original_id || "識別子不明"}</h4>
                    <span className={entry.restore_available ? styles.planReady : styles.planPending}>
                      {entry.restore_available ? "記録上 restore可能" : "restore不可"}
                    </span>
                  </div>
                  <dl className={styles.migrationPlanMeta}>
                    <div><dt>Entry / status</dt><dd><code>{entry.entry_id}</code> / {displayStatus(entry.status)}</dd></div>
                    <div><dt>元path</dt><dd><code>{entry.original_relative_path || "取得不可"}</code></dd></div>
                    <div><dt>削除日時</dt><dd>{displayUTC(entry.deleted_at)}</dd></div>
                    <div><dt>理由 / operation</dt><dd>{entry.reason || "記録なし"} / {entry.operation_type || "不明"} / <code>{entry.operation_id || "不明"}</code></dd></div>
                  </dl>
                  <button
                    type="button"
                    className={styles.button}
                    onClick={() => void loadDetail(entry.entry_id)}
                    disabled={detailLoading}
                    aria-expanded={selectedEntryID === entry.entry_id}
                  >
                    {selectedEntryID === entry.entry_id
                      ? (detailLoading ? "詳細を読み込み中..." : "詳細を再読み込み")
                      : `詳細を表示: ${entry.entry_id}`}
                  </button>
                </article>
              </li>
            ))}
          </ul>
          <div className={styles.actions}>
            <button type="button" className={styles.button} onClick={() => { clearSelectedDetail(); setOffset(Math.max(0, offset - PAGE_SIZE)); }} disabled={!canGoBack}>前のページ</button>
            <button type="button" className={styles.button} onClick={() => { clearSelectedDetail(); setOffset(offset + PAGE_SIZE); }} disabled={!canGoForward}>次のページ</button>
          </div>
        </>
      )}

      {selectedEntryID && (
        <article className={styles.deviceCard} aria-labelledby="trash-detail-title">
          <h4 id="trash-detail-title" className={styles.deviceCardTitle}>Trash entry detail</h4>
          {detailLoading && <p className={styles.info} role="status">entry detailを読み込んでいます...</p>}
          {detailError && (
            <p className={styles.error} role="alert">
              詳細は利用できません（破損metadataまたはBackend unavailableの可能性）: {detailError}
            </p>
          )}
          {detail && (
            <>
              {renderSummaryFields(detail)}
              <dl className={styles.migrationPlanMeta}>
                <div><dt>削除前inventory</dt><dd>{detail.inventory.kind}; {detail.inventory.file_count} files; {detail.inventory.total_size} bytes</dd></div>
                <div><dt>削除前 SHA-256</dt><dd><code>{detail.inventory.sha256 || "hashなし"}</code></dd></div>
              </dl>
              <TrashRetentionStatus
                deletedAt={detail.deleted_at}
                eligibleAfter={detail.eligible_after}
                lastCheckedAt={detail.last_checked_at ?? null}
                restoreAvailable={detail.restore_available}
              />
              <p className={styles.info} role="status">
                restore可否は保存時の記録です。実際のrestore直前にはpath・ID衝突とinventoryをBackendが再検証します。
              </p>
              <TrashRestoreSection
                key={detail.entry_id}
                entryID={detail.entry_id}
                originalID={detail.original_id}
                originalRelativePath={detail.original_relative_path}
                beforeInventorySHA256={detail.inventory.sha256}
                onRestored={(result: TrashRestoreResult) => {
                  setPage((current) => current ? {
                    ...current,
                    entries: current.entries.map((entry) => entry.entry_id === result.entry_id
                      ? { ...entry, status: "restored", restore_available: false }
                      : entry),
                  } : current);
                  setDetail((current) => current?.entry_id === result.entry_id
                    ? { ...current, status: "restored", restore_available: false }
                    : current);
                }}
              />
            </>
          )}
          {detailError && selectedSummary && renderSummaryFields(selectedSummary)}
        </article>
      )}
    </section>
  );
}
