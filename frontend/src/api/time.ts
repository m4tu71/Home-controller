export type TimeData = {
  date: string;
  time: string;
  datetime:string;
};

export async function fetchTime(): Promise<TimeData> {
  const res = await fetch("/api/time");

  if (!res.ok) {
    throw new Error("時刻APIの取得に失敗しました");
  }

  return res.json();
}