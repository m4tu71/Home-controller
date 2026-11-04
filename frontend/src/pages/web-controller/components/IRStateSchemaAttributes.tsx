import type { IRStateConstraints, IRStateFieldType, IRStatePresence } from "../../../api/controller";
import styles from "../WebController.module.css";

type Attributes = IRStateConstraints & { presence?: IRStatePresence };
type Props = { id: string; type: IRStateFieldType; value: Attributes; extended: boolean; disabled: boolean; onChange: (value: Attributes) => void };
export function IRStateSchemaAttributes({ id, type, value, extended, disabled, onChange }: Props) {
  const inputId = id.replaceAll(" ", "-");
  const set = (key: keyof Attributes, next: unknown) => {
    const copy = { ...value };
    if (next === undefined) delete copy[key];
    else Object.assign(copy, { [key]: next });
    onChange(copy);
  };
  return <div className={styles.schemaFieldList}>
    {extended && <label className={styles.label}>入力の扱い
      <select className={styles.select} aria-label={`${id} presence`} value={value.presence ?? ""} disabled={disabled} onChange={(e) => set("presence", e.target.value || undefined)}>
        <option value="">指定なし（必須）</option><option value="required">必須</option><option value="optional">任意</option><option value="absent">使わない（適用外）</option>
      </select>
    </label>}
    {value.presence === "absent" && ["values", "min", "max", "step", "allow_empty"].some((key) => Object.hasOwn(value, key)) && <div>
      <p>使わない項目には候補・範囲などを指定できません。除去対象: {Object.entries(value).filter(([key]) => ["values", "min", "max", "step", "allow_empty"].includes(key)).map(([key, v]) => `${key}=${JSON.stringify(v)}`).join(", ")}</p>
      <button type="button" className={styles.statusButton} disabled={disabled} onClick={() => {
        const next = { ...value };
        for (const key of ["values", "min", "max", "step", "allow_empty"] as const) delete next[key];
        if (window.confirm(`適用外の制約を除去します。変更後: ${JSON.stringify(next)}。よろしいですか？`)) onChange(next);
      }}>適用外の制約を除去</button>
    </div>}
    {type === "enum" && value.presence !== "absent" && <fieldset disabled={disabled}>
      <legend>選択肢</legend>
      <p className={styles.helperText}>リモコンで選べる値を1つずつ登録します。前後の空白も保存されます。空欄は空文字の候補になります。</p>
      {(value.values ?? []).map((candidate, index) => <div className={styles.schemaEnumRow} key={index}>
        <label className={styles.label} htmlFor={`${inputId}-value-${index}`}>候補 {index + 1}{candidate === "" ? "（空文字）" : ""}</label>
        <input id={`${inputId}-value-${index}`} className={styles.input} value={candidate} onChange={(e) => set("values", value.values!.map((v, i) => i === index ? e.target.value : v))} />
        <button type="button" className={styles.statusButton} onClick={() => set("values", value.values!.filter((_, i) => i !== index))} aria-label={`${id} 候補 ${index + 1}を削除`}>削除</button>
      </div>)}
      <button type="button" className={styles.statusButton} onClick={() => set("values", [...(value.values ?? []), ""])}>候補追加</button>
    </fieldset>}
    {type === "number" && value.presence !== "absent" && <div className={styles.formRow}>
      {(extended ? ["min", "max", "step"] as const : ["min", "max"] as const).map((key) => <label className={styles.label} key={key}>{{ min: "最小値", max: "最大値", step: "刻み" }[key]}（省略可）
        <input className={styles.input} aria-label={`${id} ${key}`} type="number" step="any" value={value[key] ?? ""} disabled={disabled} onChange={(e) => set(key, e.target.value === "" ? undefined : Number(e.target.value))} />
      </label>)}
    </div>}
    {extended && type === "string" && value.presence !== "absent" && <label className={styles.label}>空文字
      <select className={styles.select} aria-label={`${id} allow_empty`} value={value.allow_empty === undefined ? "" : String(value.allow_empty)} disabled={disabled} onChange={(e) => set("allow_empty", e.target.value === "" ? undefined : e.target.value === "true")}>
        <option value="">省略（許可）</option><option value="true">許可</option><option value="false">拒否</option>
      </select>
    </label>}
    <details className={styles.schemaDisplayDetails} open={["label", "description", "unit"].some((key) => Object.hasOwn(value, key))}>
      <summary>表示名・説明・単位（任意）</summary>
      <p className={styles.helperText}>保存する項目IDや値を変えずに、操作画面で分かりやすく表示するための設定です。</p>
      {(["label", "description", "unit"] as const).map((key) => <div className={styles.formGroup} key={key}>
        <label className={styles.label}><input type="checkbox" aria-label={`${id} ${key}を指定`} checked={value[key] !== undefined} disabled={disabled} onChange={(e) => set(key, e.target.checked ? "" : undefined)} />{{ label: "表示名を設定", description: "説明・リモコンのメモを設定", unit: "単位を設定" }[key]}</label>
        <p className={styles.helperText}>{{ label: "入力欄・日常画面・送信確認の項目名です。例：設定温度。空なら項目IDを表示します。", description: "State入力欄に表示する説明です。リモコンの表記・使えるモード・仕様の確認元などをメモできます。", unit: "数値の横に表示します。例：℃。値の変換や許容範囲の変更は行いません。" }[key]}</p>
        {value[key] !== undefined && (key === "description"
          ? <textarea className={styles.textarea} aria-label={`${id} ${key}`} value={value[key]} disabled={disabled} onChange={(e) => set(key, e.target.value)} rows={3} />
          : <input className={styles.input} aria-label={`${id} ${key}`} value={value[key]} disabled={disabled} onChange={(e) => set(key, e.target.value)} />)}
      </div>)}
    </details>
  </div>;
}
