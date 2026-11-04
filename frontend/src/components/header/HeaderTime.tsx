import { useEffect, useState } from "react";

import { fetchTime } from "../../api/time";
import styles from "./Header.module.css";

type TimeState =
  | { status: "loading" }
  | { status: "ready"; serverTimeMs: number; syncedAtMs: number }
  | { status: "error" };

function parseServerTime(date: string, time: string): number {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const clock = /^(\d{2}):(\d{2}):(\d{2})$/.exec(time);
  if (!match || !clock) throw new Error("invalid server time");
  const value = Date.UTC(
    Number(match[1]), Number(match[2]) - 1, Number(match[3]),
    Number(clock[1]), Number(clock[2]), Number(clock[3]),
  );
  if (new Date(value).toISOString().slice(0, 19) !== `${date}T${time}`) {
    throw new Error("invalid server time");
  }
  return value;
}

export function HeaderTime() {
  const [time, setTime] = useState<TimeState>({ status: "loading" });
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    let active = true;

    const refresh = async () => {
      try {
        const data = await fetchTime();
        if (active) {
          const serverTimeMs = parseServerTime(data.date, data.time);
          const syncedAtMs = Date.now();
          setNowMs(syncedAtMs);
          setTime({ status: "ready", serverTimeMs, syncedAtMs });
        }
      } catch {
        if (active) setTime((previous) => previous.status === "ready" ? previous : { status: "error" });
      }
    };

    void refresh();
    const clockTimer = window.setInterval(() => setNowMs(Date.now()), 1_000);
    const syncTimer = window.setInterval(() => void refresh(), 60_000);
    return () => {
      active = false;
      window.clearInterval(clockTimer);
      window.clearInterval(syncTimer);
    };
  }, []);

  const displayed = time.status === "ready"
    ? new Date(time.serverTimeMs + Math.max(0, nowMs - time.syncedAtMs)).toISOString().slice(0, 19)
    : "";

  return (
    <div className={styles.timeDisplay} aria-label="現在時刻">
      {time.status === "ready" ? (
        <time dateTime={displayed} title={displayed.replace("T", " ")}>
          {displayed.slice(11)}
        </time>
      ) : time.status === "loading" ? (
        <span role="status">読み込み中...</span>
      ) : (
        <span role="status">時刻を取得できません</span>
      )}
    </div>
  );
}
