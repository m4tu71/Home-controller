import type { FormEvent } from "react";

import type { IRStateFieldType, IRStateFieldSchema, IRStateFieldCase } from "../../../api/controller";

import { IRStateSchemaAttributes } from "./IRStateSchemaAttributes";
import { fieldFromDraft, extendField, incompatibleConstraintKeys, isExtendedField } from "./irStateSchemaEditor";

import styles from "../WebController.module.css";

export type IRStateSchemaFieldDraft = {
  key: string;
  type: IRStateFieldType;
  values: string;
  min: string;
  max: string;
  definition?: string;
};

export type IRStateSchemaTemplate = {
  id: string;
  label: string;
  description: string;
};

type IRStateSchemaFormProps = {
  fields: IRStateSchemaFieldDraft[];
  fieldErrors: Record<number, string>;
  loading: boolean;
  disabled: boolean;
  templates: IRStateSchemaTemplate[];
  selectedTemplateId: string;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onTemplateChange: (templateId: string) => void;
  onApplyTemplate: () => void;
  onAddField: () => void;
  onUpdateField: (
    index: number,
    field: keyof IRStateSchemaFieldDraft,
    value: string
  ) => void;
  onRemoveField: (index: number) => void;
};

export function IRStateSchemaForm({
  fields,
  fieldErrors,
  loading,
  disabled,
  templates,
  selectedTemplateId,
  onSubmit,
  onTemplateChange,
  onApplyTemplate,
  onAddField,
  onUpdateField,
  onRemoveField,
}: IRStateSchemaFormProps) {
  return (
    <form className={styles.irStateSchemaForm} onSubmit={onSubmit}>
      <div className={styles.schemaFormHeader}>
        <div>
          <h4 className={styles.formTitle}>基本設定と条件</h4>
          <p className={styles.helperText}>
            リモコンで選べる項目・値・数値範囲を登録します。まず基本設定を合わせ、モードによって違う項目だけ条件を追加してください。Schemaを設定しても信号は生成されません。送信するStateごとの学習が必要です。
          </p>
        </div>
        <div className={styles.schemaTemplateActions}>
          <label className={styles.label} htmlFor="state-schema-template">
            標準テンプレート
          </label>
          <select
            id="state-schema-template"
            className={styles.select}
            value={selectedTemplateId}
            onChange={(event) => onTemplateChange(event.target.value)}
            disabled={disabled}
          >
            {templates.map((template) => (
              <option key={template.id} value={template.id}>
                {template.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            className={styles.statusButton}
            onClick={onApplyTemplate}
            disabled={disabled || templates.length === 0}
          >
            テンプレートを適用
          </button>
        </div>
      </div>

      <ol className={styles.schemaSetupSteps}>
        <li>テンプレートを出発点に、リモコンで使う項目を確認します。</li>
        <li>各項目の型、選択肢、最小値・最大値を実リモコンに合わせます。</li>
        <li>送風では温度を使わない、などの違いがある項目だけ入力ルールと条件を追加します。</li>
        <li>保存前の確認で、既存Stateへの影響を確認します。</li>
      </ol>
      {templates.find((template) => template.id === selectedTemplateId) && (
        <p className={styles.helperText}>
          {templates.find((template) => template.id === selectedTemplateId)?.description}
        </p>
      )}

      {fields.length === 0 ? (
        <p className={styles.roomEmptyState}>
          項目がありません。標準テンプレートまたは項目追加から始めてください。
        </p>
      ) : (
        <div className={styles.schemaFieldList}>
          {fields.map((field, index) => {
            const definition = fieldFromDraft(field);
            const change = (next: IRStateFieldSchema) => onUpdateField(index, "definition", JSON.stringify(next));
            const changeCase = (caseIndex: number, next: IRStateFieldCase) => change({ ...definition, cases: definition.cases!.map((c, i) => i === caseIndex ? next : c) });
            return (
            <div className={styles.schemaField} key={index}>
              <div className={styles.schemaFieldHeader}>
                <h5 className={styles.formTitle}>項目 {index + 1}：{definition.label || field.key || "未設定"}</h5>
                <button
                  type="button"
                  className={styles.dangerButton}
                  onClick={() => onRemoveField(index)}
                  disabled={disabled}
                >
                  項目削除
                </button>
              </div>

              {fieldErrors[index] && (
                <p className={styles.schemaFieldError}>{fieldErrors[index]}</p>
              )}

              <div className={styles.formRow}>
                <div className={styles.formGroup}>
                  <label className={styles.label} htmlFor={`state-schema-key-${index}`}>
                    項目ID
                  </label>
                  <input
                    id={`state-schema-key-${index}`}
                    className={styles.input}
                    value={field.key}
                    onChange={(event) => onUpdateField(index, "key", event.target.value)}
                    placeholder="target_temperature"
                    aria-describedby={`state-schema-key-help-${index}`}
                    disabled={disabled}
                  />
                  <p id={`state-schema-key-help-${index}`} className={styles.helperText}>保存用の識別子です。例：mode、target_temperature。画面に出す名前は「表示名」で設定できます。</p>
                </div>
                <div className={styles.formGroup}>
                  <label className={styles.label} htmlFor={`state-schema-type-${index}`}>
                    型
                  </label>
                  <select
                    id={`state-schema-type-${index}`}
                    className={styles.select}
                    value={field.type}
                    onChange={(event) =>
                      onUpdateField(
                        index,
                        "type",
                        event.target.value as IRStateFieldType
                      )
                    }
                    disabled={disabled}
                  >
                    <option value="enum">選択肢（enum）</option>
                    <option value="number">数値（number）</option>
                    <option value="boolean">はい／いいえ（boolean）</option>
                    <option value="string">文字列（string）</option>
                  </select>
                </div>
              </div>

              {!isExtendedField(definition) && <div className={styles.irStateSchemaGuidance}>
                <p className={styles.helperText}>基本設定では、この項目は常に必須です。任意入力・適用外・数値の刻み・モード別の条件が必要な場合だけ、入力ルールを追加してください。</p>
                <button type="button" className={styles.statusButton} disabled={disabled} onClick={() => {
                  const removed = incompatibleConstraintKeys(definition);
                  const next = extendField(definition);
                  if (removed.length && !window.confirm(`入力ルールの追加に伴い、この型では使わない既存設定を取り除きます: ${removed.map((key) => `${key}=${JSON.stringify(definition[key as keyof IRStateFieldSchema])}`).join(", ")}。変更後: ${JSON.stringify(next)}。よろしいですか？`)) return;
                  change(next);
                }}>入力ルールを追加</button>
                {incompatibleConstraintKeys(definition).length > 0 && <p>保持している追加設定（この型では使いません）: {incompatibleConstraintKeys(definition).map((key) => `${key}=${JSON.stringify(definition[key as keyof IRStateFieldSchema])}`).join(", ")}</p>}
              </div>}
              <p className={styles.helperText}>基本設定：条件別の設定がない場合、またはどの条件にも一致しない場合に使います。</p>
              <IRStateSchemaAttributes id={`field ${index + 1} base`} type={definition.type} value={definition} extended={isExtendedField(definition)} disabled={disabled} onChange={(attributes) => change({ ...attributes, type: definition.type, ...(definition.cases !== undefined ? { cases: definition.cases } : {}) })} />
              {isExtendedField(definition) && <>
                <div className={styles.irStateSchemaGuidance}>
                  <p className={styles.helperText}>モードなどによる条件分岐：基本設定と違う場合だけ追加します。</p>
                  <ol className={styles.schemaSetupSteps}>
                    <li>「条件別の設定を追加」→「条件行を追加」を押します。</li>
                    <li>「条件に使う項目」でmodeなどを選び、一致させる値にチェックします。</li>
                    <li>その条件の「入力の扱い」と候補・数値範囲を設定します。例：modeがfanなら、温度は「使わない（適用外）」。</li>
                  </ol>
                  <p className={styles.helperText}>複数の条件行はすべて一致する必要があります。同じ条件項目のチェックはどれか1つに一致すれば有効です。条件別の候補・範囲・刻みは基本設定から引き継がないため、必要な値を指定してください。複数の設定が同時に一致しないようにします。</p>
                </div>
                {definition.cases?.map((c, ci) => <fieldset key={ci} disabled={disabled}>
                  <legend>項目 {index + 1}：条件別の設定 {ci + 1}</legend>
                  <button type="button" className={styles.statusButton} onClick={() => change({ ...definition, cases: definition.cases!.filter((_, i) => i !== ci) })}>条件別の設定を削除</button>
                  {c.when.all.map((selector, si) => {
                    const source = fields.find((f) => f.key === selector.field);
                    const sourceField = source ? fieldFromDraft(source) : undefined;
                    const candidates = sourceField?.type === "boolean" ? [true, false] : sourceField?.values ?? [];
                    const updateSelector = (next: typeof selector) => changeCase(ci, { ...c, when: { all: c.when.all.map((s, i) => i === si ? next : s) } });
                    return <div className={styles.schemaField} key={si}>
                      <label className={styles.label}>条件に使う項目
                        <select className={styles.select} aria-label={`field ${index + 1} case ${ci + 1} selector ${si + 1}`} value={selector.field} onChange={(e) => updateSelector({ field: e.target.value, in: [] })}>
                          <option value="">選択してください</option>
                          {selector.field && !fields.some((f) => f.key === selector.field && f.key !== field.key && ["enum", "boolean"].includes(f.type) && !fieldFromDraft(f).cases && (fieldFromDraft(f).presence ?? "required") === "required") && <option value={selector.field}>{selector.field}（参照不正）</option>}
                          {fields.filter((f) => f.key && f.key !== field.key && ["enum", "boolean"].includes(f.type) && fieldFromDraft(f).cases === undefined && (fieldFromDraft(f).presence ?? "required") === "required").map((f) => <option key={f.key} value={f.key}>{f.key}</option>)}
                        </select>
                      </label>
                      {candidates.map((v, vi) => <label className={styles.label} key={vi}>
                        <input type="checkbox" aria-label={`field ${index + 1} case ${ci + 1} condition ${si + 1} ${typeof v}:${v === "" ? "空文字" : String(v)}`} checked={selector.in.includes(v)} onChange={(e) => updateSelector({ ...selector, in: e.target.checked ? [...selector.in, v] : selector.in.filter((x) => x !== v) })} />{v === "" ? "空文字" : String(v)}
                      </label>)}
                      {selector.in.filter((v) => !candidates.some((candidate) => candidate === v)).map((v, vi) => <p key={vi}>候補外条件値: {JSON.stringify(v)}</p>)}
                      <button type="button" className={styles.statusButton} onClick={() => changeCase(ci, { ...c, when: { all: c.when.all.filter((_, i) => i !== si) } })}>条件行を削除</button>
                    </div>;
                  })}
                  <button type="button" className={styles.statusButton} onClick={() => changeCase(ci, { ...c, when: { all: [...c.when.all, { field: "", in: [] }] } })}>条件行を追加</button>
                  <IRStateSchemaAttributes id={`field ${index + 1} case ${ci + 1}`} type={definition.type} value={c} extended disabled={disabled} onChange={(attrs) => changeCase(ci, { ...attrs, presence: attrs.presence ?? "required", when: c.when })} />
                </fieldset>)}
                {definition.cases?.length === 0 && <button type="button" className={styles.statusButton} disabled={disabled} onClick={() => {
                  const next = { ...definition }; delete next.cases; change(next);
                }}>空の条件設定を解除</button>}
                <button type="button" className={styles.statusButton} disabled={disabled} onClick={() => change({ ...definition, cases: [...(definition.cases ?? []), { when: { all: [] }, presence: "required" }] })}>条件別の設定を追加</button>
              </>}

            </div>
          ); })}
        </div>
      )}

      <div className={styles.schemaFormActions}>
        <button
          type="button"
          className={styles.statusButton}
          onClick={onAddField}
          disabled={disabled}
        >
          項目追加
        </button>
        <button type="submit" className={styles.primaryButton} disabled={disabled || loading}>
          項目設定を保存
        </button>
      </div>
    </form>
  );
}
