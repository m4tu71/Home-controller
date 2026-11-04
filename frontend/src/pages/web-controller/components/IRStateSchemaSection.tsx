import type { FormEvent } from "react";

import type { Device, Room } from "../../../api/controller";
import {
  IRStateSchemaForm,
  type IRStateSchemaFieldDraft,
  type IRStateSchemaTemplate,
} from "./IRStateSchemaForm";
import styles from "../WebController.module.css";

type IRStateSchemaSectionProps = {
  selectedRoom?: Room;
  selectedDevice?: Device;
  irRemoteType: "command" | "stateful";
  fields: IRStateSchemaFieldDraft[];
  fieldErrors: Record<number, string>;
  schemaExists: boolean;
  existingStateIDs: string[];
  invalidStateIDs: string[];
  message: string;
  error: string;
  loading: boolean;
  loaded: boolean;
  templates: IRStateSchemaTemplate[];
  selectedTemplateId: string;
  onRefresh: () => void;
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

export function IRStateSchemaSection({
  selectedRoom,
  selectedDevice,
  irRemoteType,
  fields,
  fieldErrors,
  schemaExists,
  existingStateIDs,
  invalidStateIDs,
  message,
  error,
  loading,
  loaded,
  templates,
  selectedTemplateId,
  onRefresh,
  onSubmit,
  onTemplateChange,
  onApplyTemplate,
  onAddField,
  onUpdateField,
  onRemoveField,
}: IRStateSchemaSectionProps) {
  if (!selectedDevice || selectedDevice.primary_adapter !== "ir" || irRemoteType !== "stateful") {
    return null;
  }

  const disabled = loading || !loaded || !selectedRoom;

  return (
    <section className={styles.deviceBlock} aria-busy={loading || !loaded}>
      <div className={styles.deviceHeader}>
        <h3 className={styles.deviceTitle}>リモコンの項目設定（Schema）</h3>
        <button
          type="button"
          className={styles.statusButton}
          onClick={onRefresh}
          disabled={disabled}
        >
          設定を再読込
        </button>
      </div>

      <div className={styles.roomStatusGrid}>
        <div className={styles.statusLabel}>対象の部屋</div>
        <div className={styles.statusValue}>
          {selectedRoom ? `${selectedRoom.name} (${selectedRoom.id})` : "-"}
        </div>
        <div className={styles.statusLabel}>対象の機器</div>
        <div className={styles.statusValue}>
          {selectedDevice.name} ({selectedDevice.id})
        </div>
        <div className={styles.statusLabel}>Schema</div>
        <div className={styles.statusValue}>
          {schemaExists ? `${fields.length} 項目` : "未作成"}
        </div>
      </div>

      {message && <p className={styles.info} role="status">{message}</p>}
      {error && <p className={styles.error} role="alert">{error}</p>}
      {!loaded && !message && <p className={styles.info} role="status">IR State Schemaを読み込んでいます...</p>}

      {loaded && !loading && !error && !schemaExists && (
        <div className={styles.irStateSchemaGuidance}>
          <p className={styles.warningText} role="status">State Schemaはまだ作成されていません。</p>
          {existingStateIDs.length > 0 ? (
            <p className={styles.helperText}>
              既存State（{existingStateIDs.join(", ")}）を確認し、すべてに互換なSchemaを保存してください。不整合なStateは次のState管理画面で確認・削除できます。
            </p>
          ) : (
            <p className={styles.helperText}>
              既存Stateはありません。標準テンプレートまたは項目追加からSchemaを作成してください。
            </p>
          )}
        </div>
      )}

      {invalidStateIDs.length > 0 && (
        <div className={styles.irStateSchemaGuidance}>
          <p className={styles.warningText}>Schemaと整合しないStateがあります。</p>
          <p className={styles.helperText}>
            要確認State: {invalidStateIDs.join(", ")}。理由と既存の学習情報を確認してください。学習済みStateの値は変更できません。旧Deviceを保全し、別Deviceへの設定を検討してください。
          </p>
        </div>
      )}

      <IRStateSchemaForm
        fields={fields}
        fieldErrors={fieldErrors}
        loading={loading}
        disabled={disabled}
        templates={templates}
        selectedTemplateId={selectedTemplateId}
        onSubmit={onSubmit}
        onTemplateChange={onTemplateChange}
        onApplyTemplate={onApplyTemplate}
        onAddField={onAddField}
        onUpdateField={onUpdateField}
        onRemoveField={onRemoveField}
      />
    </section>
  );
}
