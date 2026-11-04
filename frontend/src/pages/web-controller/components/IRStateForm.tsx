import type { FormEvent } from "react";

import type { IRCommandFormat, IRState, IRStateSchema } from "../../../api/controller";

import styles from "../WebController.module.css";
import { parseStateDraft, registeredState, stateOperationReason } from "./irStateDraft";
import { IROperationFields } from "./IROperationFields";

type IRStateFormProps = {
  schema?: IRStateSchema;
  selectedState?: IRState;
  loading: boolean;
  createId: string;
  createName: string;
  createValues: Record<string, string>;
  createFormat: IRCommandFormat;
  createRawTimings: string;
  createProtocol: string;
  createCode: string;
  createPreset: string;
  editName: string;
  editValues: Record<string, string>;
  editFormat: IRCommandFormat;
  editRawTimings: string;
  editProtocol: string;
  editCode: string;
  editPreset: string;
  fieldErrors: Record<string, string>;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onUpdate: (event: FormEvent<HTMLFormElement>) => void;
  onCreateIdChange: (value: string) => void;
  onCreateNameChange: (value: string) => void;
  onCreateValueChange: (key: string, value: string | undefined) => void;
  onCreateFormatChange: (value: IRCommandFormat) => void;
  onCreateRawTimingsChange: (value: string) => void;
  onCreateProtocolChange: (value: string) => void;
  onCreateCodeChange: (value: string) => void;
  onCreatePresetChange: (value: string) => void;
  onEditNameChange: (value: string) => void;
  onEditValueChange: (key: string, value: string | undefined) => void;
  onEditFormatChange: (value: IRCommandFormat) => void;
  onEditRawTimingsChange: (value: string) => void;
  onEditProtocolChange: (value: string) => void;
  onEditCodeChange: (value: string) => void;
  onEditPresetChange: (value: string) => void;
};

type StateFieldsProps = {
  prefix: "create" | "edit";
  schema: IRStateSchema;
  values: Record<string, string>;
  errors: Record<string, string>;
  disabled: boolean;
  onChange: (key: string, value: string | undefined) => void;
};

function StateFields({ prefix, schema, values, errors, disabled, onChange }: StateFieldsProps) {
  const draft = parseStateDraft(schema, values);
  return <>
    {Object.entries(schema).map(([key, base]) => {
      const id = `ir-state-${prefix}-${key}`;
      if (draft.pending.has(key)) return <p className={styles.helperText} key={key}>{base.label || key}: selectorの入力待ちです</p>;
      const field = draft.resolved[key] ?? base;
      if (field.presence === "absent") return null;
      const optional = field.presence === "optional";
      const included = Object.hasOwn(values, key);
      let unsetValue = "";
      while (field.values?.includes(unsetValue)) unsetValue += "_";
      const label = `${field.label || key} (${field.type})`;
      return <div className={styles.formGroup} key={key}>
        {optional && <label className={styles.label}><input type="checkbox" aria-label={`${prefix} ${key}を入力`} checked={included} disabled={disabled} onChange={(e) => onChange(key, e.target.checked ? "" : undefined)} />入力する（省略可）</label>}
        <label className={styles.label} htmlFor={id}>{label}</label>
        <p className={styles.helperText}>field ID: {key} · {optional ? "任意" : "必須"}{field.unit ? ` · 単位: ${field.unit}` : ""}</p>
        {field.description && <p className={styles.helperText}>{field.description}</p>}
        {(!optional || included) && (field.type === "enum" ? <select id={id} className={styles.select} value={included ? values[key] : unsetValue} disabled={disabled} onChange={(e) => onChange(key, e.target.value === unsetValue ? undefined : e.target.value)}>
          <option value={unsetValue}>選択してください</option>{field.values?.map((v, i) => <option key={i} value={v}>{v === "" ? "空文字" : v}</option>)}
        </select> : field.type === "boolean" ? <select id={id} className={styles.select} value={values[key] ?? ""} disabled={disabled} onChange={(e) => onChange(key, e.target.value || undefined)}>
          <option value="">選択してください</option><option value="true">true</option><option value="false">false</option>
        </select> : <input id={id} className={styles.input} type={field.type === "number" ? "number" : "text"} min={field.type === "number" ? field.min : undefined} max={field.type === "number" ? field.max : undefined} step="any" value={values[key] ?? ""} disabled={disabled} onChange={(e) => onChange(key, e.target.value)} />)}
        {field.type === "number" && <p className={styles.helperText}>範囲: {field.min ?? "-"} ～ {field.max ?? "-"} / 刻み: {field.step ?? "制約なし"}</p>}
        {(errors[key] || (included && draft.errors[key])) && <p className={styles.schemaFieldError}>{errors[key] || draft.errors[key]}</p>}
      </div>;
    })}
    {Object.keys(draft.discarded).length > 0 && <p className={styles.warningText}>保存時に除外するdraft値: {Object.entries(draft.discarded).map(([key, v]) => `${key}=${JSON.stringify(v)}`).join(" / ")}。画面内では保持し、条件を戻すと復元します。</p>}
    {draft.errors.schema && <p className={styles.schemaFieldError}>{draft.errors.schema}</p>}
  </>;
}

export function IRStateForm(props: IRStateFormProps) {
  const { schema, selectedState, loading, fieldErrors } = props;
  if (!schema) {
    return (
      <div className={styles.irStateGuidance}>
        <p className={styles.warningText}>State Schemaを保存すると、raw_samples Stateを作成・編集できます。</p>
        <p className={styles.helperText}>既存Stateは一覧から確認し、不整合なStateは削除できます。</p>
      </div>
    );
  }

  const createDisabled = loading;
  const registered = registeredState(selectedState);
  const savedReason = selectedState ? stateOperationReason(schema, selectedState) : "";

  return (
    <div className={styles.deviceForm}>
      <form className={styles.deviceSubForm} onSubmit={props.onCreate} noValidate>
        <h4 className={styles.formTitle}>IR State作成</h4>
        <div className={styles.formGroup}>
          <label className={styles.label} htmlFor="ir-state-create-id">IR State ID</label>
          <input id="ir-state-create-id" className={styles.input} value={props.createId} onChange={(event) => props.onCreateIdChange(event.target.value)} placeholder="cool_24" disabled={createDisabled} />
          <p className={styles.helperText}>小文字のsnake_caseで入力します。</p>
        </div>
        <div className={styles.formGroup}>
          <label className={styles.label} htmlFor="ir-state-create-name">表示名</label>
          <input id="ir-state-create-name" className={styles.input} value={props.createName} onChange={(event) => props.onCreateNameChange(event.target.value)} disabled={createDisabled} />
        </div>
        <StateFields prefix="create" schema={schema} values={props.createValues} errors={fieldErrors} disabled={createDisabled} onChange={props.onCreateValueChange} />
        <IROperationFields prefix="ir-state-create" format={props.createFormat} rawTimings={props.createRawTimings} protocol={props.createProtocol} code={props.createCode} preset={props.createPreset} disabled={createDisabled} onFormatChange={props.onCreateFormatChange} onRawTimingsChange={props.onCreateRawTimingsChange} onProtocolChange={props.onCreateProtocolChange} onCodeChange={props.onCreateCodeChange} onPresetChange={props.onCreatePresetChange} />
        <div className={styles.roomFormActions}><button type="submit" className={styles.primaryButton} disabled={createDisabled}>IR State作成</button></div>
      </form>

      <form className={styles.deviceSubForm} onSubmit={props.onUpdate} noValidate>
        <h4 className={styles.formTitle}>IR State編集</h4>
        <div className={styles.formGroup}><label className={styles.label}>IR State ID</label><div className={styles.readOnlyValue}>{selectedState?.id ?? "-"}</div></div>
        {!selectedState ? (
          <p className={styles.helperText}>編集するIR Stateを一覧から選択してください。</p>
        ) : (
          <>
            <div className={styles.formGroup}><label className={styles.label} htmlFor="ir-state-edit-name">表示名</label><input id="ir-state-edit-name" className={styles.input} value={props.editName} onChange={(event) => props.onEditNameChange(event.target.value)} disabled={loading} /></div>
            <StateFields prefix="edit" schema={schema} values={props.editValues} errors={fieldErrors} disabled={loading || registered} onChange={props.onEditValueChange} />
            <IROperationFields prefix="ir-state-edit" format={props.editFormat} originalFormat={selectedState.format} rawTimings={props.editRawTimings} protocol={props.editProtocol} code={props.editCode} preset={props.editPreset} disabled={loading || registered} formatDisabled={registered} formatGuidance={registered ? "登録済みStateの値・format・信号内容は変更できません。変更には新Stateを作成してください。名前・repeat編集と同じ値の再学習は可能です。" : undefined} onFormatChange={props.onEditFormatChange} onRawTimingsChange={props.onEditRawTimingsChange} onProtocolChange={props.onEditProtocolChange} onCodeChange={props.onEditCodeChange} onPresetChange={props.onEditPresetChange} />
            {savedReason && <p className={styles.warningText}>{savedReason}</p>}
            <label className={styles.label} htmlFor="ir-state-edit-repeat">保存repeat（0は既定値）</label>
            <input id="ir-state-edit-repeat" name="repeat" className={styles.input} type="number" min="0" max="5" defaultValue={selectedState.repeat ?? 0} key={`${selectedState.id}:${selectedState.repeat}`} disabled={loading} />
            <div className={styles.roomFormActions}><button type="submit" className={styles.primaryButton} disabled={loading}>IR State更新</button></div>
          </>
        )}
      </form>
    </div>
  );
}
