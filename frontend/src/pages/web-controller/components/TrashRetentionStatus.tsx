import { useEffect, useState } from "react";

import styles from "../WebController.module.css";

type Props = {
  deletedAt: string | null;
  eligibleAfter: string | null;
  lastCheckedAt: string | null;
  restoreAvailable: boolean;
};

type ParsedTimestamp = Date | null;

function parseTimestamp(value: string | null): ParsedTimestamp {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function displayTimestamp(value: ParsedTimestamp): string {
  return value ? `${value.toISOString().replace(".000Z", "Z")} (UTC)` : "未設定または日時形式不正";
}

function displayClockCheckpoint(raw: string | null, parsed: ParsedTimestamp): string {
  if (!raw) return "未確認";
  return parsed ? displayTimestamp(parsed) : "日時形式不正";
}

export function TrashRetentionStatus({ deletedAt, eligibleAfter, lastCheckedAt, restoreAvailable }: Props) {
  const [browserNow, setBrowserNow] = useState(() => new Date());

  useEffect(() => {
    const refreshClock = () => setBrowserNow(new Date());
    const intervalID = window.setInterval(refreshClock, 60_000);
    window.addEventListener("focus", refreshClock);
    return () => {
      window.clearInterval(intervalID);
      window.removeEventListener("focus", refreshClock);
    };
  }, []);

  const deleted = parseTimestamp(deletedAt);
  const eligible = parseTimestamp(eligibleAfter);
  const checked = parseTimestamp(lastCheckedAt);
  const lastCheckInvalid = Boolean(lastCheckedAt && !checked);
  const clockWarning = lastCheckInvalid || Boolean(
    (deleted && browserNow < deleted) ||
    (checked && browserNow < checked) ||
    (deleted && checked && checked < deleted),
  );
  const retentionUnknown = !deleted || !eligible || Boolean(deleted && eligible && eligible < deleted);
  const state = retentionUnknown
    ? "保持期限不明"
    : clockWarning
      ? "時計要確認・期限判定保留"
      : browserNow >= eligible
        ? "保持期限経過（参考表示）"
        : "保持中（参考表示）";

  return (
    <section className={styles.migrationStage} aria-labelledby="trash-retention-title">
      <h4 id="trash-retention-title" className={styles.deviceCardTitle}>Trash retention status</h4>
      <dl className={styles.migrationPlanMeta}>
        <div><dt>削除日時</dt><dd>{displayTimestamp(deleted)}</dd></div>
        <div><dt>保持期限</dt><dd>{displayTimestamp(eligible)}</dd></div>
        <div><dt>Backend最終clock確認</dt><dd>{displayClockCheckpoint(lastCheckedAt, checked)}</dd></div>
        <div>
          <dt>記録上の復元可否</dt>
          <dd>{restoreAvailable ? "可能（restore dry-runで再確認）" : "不可（現在のentry記録）"}</dd>
        </div>
      </dl>

      {retentionUnknown ? (
        <p className={styles.planPending} role="alert">
          保持期限が未設定または日時形式不正のため、経過状態を判定できません。Backend hostのretention設定とTrash metadataを確認してください。
        </p>
      ) : clockWarning ? (
        <p className={styles.error} role="alert">
          clock warning: この端末の時刻が削除日時またはBackend最終確認時刻より前か、最終確認時刻を読み取れません。保持期限の経過状態は判定保留です。
        </p>
      ) : (
        <p className={eligible && browserNow >= eligible ? styles.planPending : styles.planReady} role="status">
          {state}。判定時刻: {browserNow.toISOString().replace(".000Z", "Z")} (browser UTC)
        </p>
      )}

      <p className={styles.helperText}>
        期限経過は削除の承認ではありません。この表示はbrowser時刻による参考情報です。最終削除はBackend host上で運用者がManual CLIから実行し、保持期間・clock・operation/backupを再確認します。このFrontendに最終削除の実行機能はありません。
      </p>
    </section>
  );
}
