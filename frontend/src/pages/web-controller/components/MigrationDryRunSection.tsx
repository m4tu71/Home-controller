import { useRef, useState, type FormEvent } from "react";

import {
  createMigrationDryRun,
  MigrationDryRunAPIError,
  type Device,
  type IRConfig,
  type IRStateSchema,
  type MigrationDryRunMapping,
  type MigrationDryRunPlan,
  type Room,
} from "../../../api/controller";
import { MigrationApplyVerifySection } from "./MigrationApplyVerifySection";
import styles from "../WebController.module.css";

type MappingDraft = {
  sourceName: string;
  targetKind: "command" | "state";
  roomID: string;
  deviceID: string;
  targetID: string;
  stateJSON: string;
  approveRawMedian: boolean;
  picoCommand: string;
  createRoom: boolean;
  roomName: string;
  roomSortOrder: string;
  createDevice: boolean;
  deviceName: string;
  deviceType: string;
  deviceCapabilities: string;
  createIRConfig: boolean;
  carrierKHz: string;
  defaultRepeat: string;
  createStateSchema: boolean;
  stateSchemaJSON: string;
};

const presetCommands = [
  "LIGHT_ON_CH3",
  "LIGHT_FULL_CH3",
  "LIGHT_OFF_CH3",
  "LIGHT_NIGHT_CH3",
  "LIGHT_BRIGHT_UP_CH3",
  "LIGHT_BRIGHT_DOWN_CH3",
];

function emptyMapping(): MappingDraft {
  return {
    sourceName: "",
    targetKind: "command",
    roomID: "",
    deviceID: "",
    targetID: "",
    stateJSON: "",
    approveRawMedian: false,
    picoCommand: "",
    createRoom: false,
    roomName: "",
    roomSortOrder: "",
    createDevice: false,
    deviceName: "",
    deviceType: "",
    deviceCapabilities: "",
    createIRConfig: false,
    carrierKHz: "",
    defaultRepeat: "",
    createStateSchema: false,
    stateSchemaJSON: "",
  };
}

function parseJSONObject(value: string, label: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(value);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`${label}はJSON objectで指定してください。`);
  }
  return parsed as Record<string, unknown>;
}

function describeDryRunError(error: unknown): string {
  if (error instanceof MigrationDryRunAPIError) {
    if (error.status === 400 || error.status === 422) {
      return `入力または移行元データを確認してください。${error.message}`;
    }
    if (error.status === 409) {
      return `移行元または移行先に競合・変更があります。planは作成されていません。${error.message}`;
    }
  }
  return error instanceof Error ? error.message : "Migration planの作成に失敗しました";
}

export function MigrationDryRunSection() {
  const [mappings, setMappings] = useState<MappingDraft[]>([emptyMapping()]);
  const [plan, setPlan] = useState<MigrationDryRunPlan | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const mappingRevisionRef = useRef(0);
  const requestSequenceRef = useRef(0);
  const requestInFlightRef = useRef(false);

  const updateMapping = (index: number, patch: Partial<MappingDraft>) => {
    if (requestInFlightRef.current) return;
    mappingRevisionRef.current += 1;
    setMappings((current) =>
      current.map((mapping, currentIndex) =>
        currentIndex === index ? { ...mapping, ...patch } : mapping,
      ),
    );
    setPlan(null);
    setError("");
  };

  const addMapping = () => {
    if (requestInFlightRef.current) return;
    mappingRevisionRef.current += 1;
    setMappings((current) => [...current, emptyMapping()]);
    setPlan(null);
    setError("");
  };

  const removeMapping = (index: number) => {
    if (requestInFlightRef.current) return;
    mappingRevisionRef.current += 1;
    setMappings((current) => current.filter((_, currentIndex) => currentIndex !== index));
    setPlan(null);
    setError("");
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (requestInFlightRef.current) return;
    setError("");
    setPlan(null);
    const requestSequence = requestSequenceRef.current + 1;
    const submittedRevision = mappingRevisionRef.current;
    let requestStarted = false;

    try {
      const requestMappings: MigrationDryRunMapping[] = mappings.map((mapping, index) => {
        const sourceName = mapping.sourceName.trim();
        if (!sourceName || !mapping.roomID.trim() || !mapping.deviceID.trim() || !mapping.targetID.trim()) {
          throw new Error(`mapping ${index + 1}: source、Room ID、Device ID、target IDは必須です。`);
        }

        if (!/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(sourceName) || sourceName.includes("..")) {
          throw new Error(`mapping ${index + 1}: 移行元ファイル名には英数字、_、-、.のみ使用できます。`);
        }

        let state: Record<string, unknown> | undefined;
        if (mapping.targetKind === "state" && mapping.stateJSON.trim()) {
          state = parseJSONObject(mapping.stateJSON, `mapping ${index + 1}: State payload`);
        }

        let room: Room | undefined;
        if (mapping.createRoom) {
          const name = mapping.roomName.trim();
          if (!name) throw new Error(`mapping ${index + 1}: 新規Roomの表示名は必須です。`);
          const sortOrder = mapping.roomSortOrder.trim();
          const parsedSortOrder = sortOrder ? Number(sortOrder) : undefined;
          if (parsedSortOrder !== undefined && !Number.isInteger(parsedSortOrder)) {
            throw new Error(`mapping ${index + 1}: Room sort_orderは整数で指定してください。`);
          }
          room = {
            id: mapping.roomID.trim(),
            name,
            ...(parsedSortOrder !== undefined ? { sort_order: parsedSortOrder } : {}),
          };
        }

        let device: Device | undefined;
        if (mapping.createDevice) {
          const name = mapping.deviceName.trim();
          const deviceType = mapping.deviceType.trim();
          if (!name || !deviceType) {
            throw new Error(`mapping ${index + 1}: 新規Deviceの表示名とdevice_typeは必須です。`);
          }
          const capabilities = mapping.deviceCapabilities
            .split(",")
            .map((capability) => capability.trim())
            .filter(Boolean);
          device = {
            id: mapping.deviceID.trim(),
            name,
            room_id: mapping.roomID.trim(),
            device_type: deviceType,
            primary_adapter: "ir",
            ...(capabilities.length ? { capabilities } : {}),
          };
        }

        let irConfig: IRConfig | undefined;
        if (mapping.createIRConfig) {
          const carrierKHz = Number(mapping.carrierKHz);
          const repeatText = mapping.defaultRepeat.trim();
          const defaultRepeat = repeatText ? Number(repeatText) : undefined;
          if (!Number.isInteger(carrierKHz) || carrierKHz < 1) {
            throw new Error(`mapping ${index + 1}: IR configのcarrier_khzは正の整数で指定してください。`);
          }
          if (defaultRepeat !== undefined && (!Number.isInteger(defaultRepeat) || defaultRepeat < 0)) {
            throw new Error(`mapping ${index + 1}: default_repeatは0以上の整数で指定してください。`);
          }
          irConfig = {
            ir_remote_type: mapping.targetKind === "command" ? "command" : "stateful",
            carrier_khz: carrierKHz,
            ...(defaultRepeat !== undefined ? { default_repeat: defaultRepeat } : {}),
          };
        }

        let stateSchema: IRStateSchema | undefined;
        if (mapping.targetKind === "state" && mapping.createStateSchema) {
          const parsed = parseJSONObject(mapping.stateSchemaJSON, `mapping ${index + 1}: State schema`);
          if (Object.keys(parsed).length === 0) {
            throw new Error(`mapping ${index + 1}: State schemaには1つ以上のfieldが必要です。`);
          }
          stateSchema = parsed as IRStateSchema;
        }

        return {
          action_id: `migrate_${index + 1}`,
          source_path: `ir-signals/${sourceName}.json`,
          target_kind: mapping.targetKind,
          room_id: mapping.roomID.trim(),
          device_id: mapping.deviceID.trim(),
          target_id: mapping.targetID.trim(),
          decision: {
            room_id: mapping.roomID.trim(),
            device_id: mapping.deviceID.trim(),
            target_id: mapping.targetID.trim(),
            ...(state ? { state } : {}),
            ...(mapping.approveRawMedian ? { approve_raw_median: true } : {}),
            ...(mapping.picoCommand ? { pico_command: mapping.picoCommand } : {}),
          },
          ...(room ? { room } : {}),
          ...(device ? { device } : {}),
          ...(irConfig ? { ir_config: irConfig } : {}),
          ...(stateSchema ? { state_schema: stateSchema } : {}),
        };
      });

      requestInFlightRef.current = true;
      requestSequenceRef.current = requestSequence;
      requestStarted = true;
      setSubmitting(true);
      const response = await createMigrationDryRun(requestMappings);
      if (
        requestSequenceRef.current === requestSequence &&
        mappingRevisionRef.current === submittedRevision
      ) {
        setPlan(response.plan);
      }
    } catch (submitError: unknown) {
      if (
        (!requestStarted || requestSequenceRef.current === requestSequence) &&
        mappingRevisionRef.current === submittedRevision
      ) {
        setError(describeDryRunError(submitError));
      }
    } finally {
      if (!requestStarted || requestSequenceRef.current === requestSequence) {
        requestInFlightRef.current = false;
        setSubmitting(false);
      }
    }
  };

  return (
    <section className={styles.migrationSection} aria-labelledby="migration-dry-run-title">
      <div className={styles.deviceHeader}>
        <h3 id="migration-dry-run-title" className={styles.deviceTitle}>
          Migration plan / dry-run
        </h3>
      </div>
      <p className={styles.helperText}>
        旧IR Signalのファイル名を確認して移行先を指定し、filesystem変更前にplanを確認します。旧一覧APIは使用しません。Backendがsource内容と移行先を検証します。Applyとverifyは下の独立した段階で操作し、IR送信は行いません。
      </p>

      <form className={styles.migrationForm} onSubmit={(event) => void submit(event)} aria-busy={submitting}>
          <fieldset className={styles.migrationFields} disabled={submitting}>
            <legend>Migration mappings</legend>
            <ol className={styles.migrationMappingList}>
            {mappings.map((mapping, index) => {
              return (
                <li key={index}>
                  <fieldset className={styles.migrationMapping}>
                    <legend>Mapping {index + 1}</legend>
                    <div className={styles.formRow}>
                      <label className={styles.formGroup}>
                        <span className={styles.label}>移行元ファイル名（.jsonを除く）</span>
                        <input
                          className={styles.input}
                          value={mapping.sourceName}
                          onChange={(event) => updateMapping(index, { sourceName: event.target.value })}
                          placeholder="bedroom_ac_power_off"
                          required
                        />
                      </label>
                      <label className={styles.formGroup}>
                        <span className={styles.label}>移行先種別</span>
                        <select
                          className={styles.select}
                          value={mapping.targetKind}
                          onChange={(event) => updateMapping(index, { targetKind: event.target.value as MappingDraft["targetKind"] })}
                        >
                          <option value="command">Command</option>
                          <option value="state">State</option>
                        </select>
                      </label>
                    </div>
                    {mapping.sourceName.trim() && (
                      <p className={styles.helperText}>
                        Source: <code>ir-signals/{mapping.sourceName.trim()}.json</code>（形式・carrierはdry-runで検証）
                      </p>
                    )}
                    <div className={styles.formRow}>
                      <label className={styles.formGroup}>
                        <span className={styles.label}>Room ID</span>
                        <input className={styles.input} value={mapping.roomID} onChange={(event) => updateMapping(index, { roomID: event.target.value })} required />
                      </label>
                      <label className={styles.formGroup}>
                        <span className={styles.label}>Device ID</span>
                        <input className={styles.input} value={mapping.deviceID} onChange={(event) => updateMapping(index, { deviceID: event.target.value })} required />
                      </label>
                    </div>
                    <label className={styles.formGroup}>
                      <span className={styles.label}>{mapping.targetKind === "command" ? "Command ID" : "State ID"}</span>
                      <input className={styles.input} value={mapping.targetID} onChange={(event) => updateMapping(index, { targetID: event.target.value })} required />
                    </label>

                    <fieldset className={styles.migrationProvisioning}>
                      <legend>新規metadata（未作成の場合のみ）</legend>
                      <p className={styles.helperText}>
                        選択したmetadataは作成せずplan内のplanned writeに含めます。既存metadataにはチェックを付けないでください。既存pathに指定するとBackendが競合として拒否します。同じmetadataを複数mappingで新規作成する場合は、該当mappingすべてに同じ内容を指定してください。
                      </p>
                      <label className={styles.migrationCheckbox}>
                        <input type="checkbox" checked={mapping.createRoom} onChange={(event) => updateMapping(index, { createRoom: event.target.checked })} />
                        Room metadataを新規作成
                      </label>
                      {mapping.createRoom && (
                        <div className={styles.formRow}>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>Room表示名</span>
                            <input className={styles.input} value={mapping.roomName} onChange={(event) => updateMapping(index, { roomName: event.target.value })} required />
                          </label>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>sort_order（任意の整数）</span>
                            <input className={styles.input} type="number" step="1" value={mapping.roomSortOrder} onChange={(event) => updateMapping(index, { roomSortOrder: event.target.value })} />
                          </label>
                        </div>
                      )}

                      <label className={styles.migrationCheckbox}>
                        <input type="checkbox" checked={mapping.createDevice} onChange={(event) => updateMapping(index, { createDevice: event.target.checked })} />
                        Device metadataを新規作成（primary_adapterはir）
                      </label>
                      {mapping.createDevice && (
                        <div className={styles.formRow}>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>Device表示名</span>
                            <input className={styles.input} value={mapping.deviceName} onChange={(event) => updateMapping(index, { deviceName: event.target.value })} required />
                          </label>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>device_type</span>
                            <input className={styles.input} value={mapping.deviceType} onChange={(event) => updateMapping(index, { deviceType: event.target.value })} required />
                          </label>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>capabilities（任意、カンマ区切り）</span>
                            <input className={styles.input} value={mapping.deviceCapabilities} onChange={(event) => updateMapping(index, { deviceCapabilities: event.target.value })} />
                          </label>
                        </div>
                      )}

                      <label className={styles.migrationCheckbox}>
                        <input type="checkbox" checked={mapping.createIRConfig} onChange={(event) => updateMapping(index, { createIRConfig: event.target.checked })} />
                        IR configを新規作成
                      </label>
                      {mapping.createIRConfig && (
                        <div className={styles.formRow}>
                          <div className={styles.formGroup}>
                            <span className={styles.label}>ir_remote_type</span>
                            <div className={styles.readOnlyValue}>
                              {mapping.targetKind === "command" ? "command" : "stateful"}
                            </div>
                          </div>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>carrier_khz（移行元と一致必須）</span>
                            <input className={styles.input} type="number" min="1" step="1" value={mapping.carrierKHz} onChange={(event) => updateMapping(index, { carrierKHz: event.target.value })} required />
                          </label>
                          <label className={styles.formGroup}>
                            <span className={styles.label}>default_repeat（任意、0以上）</span>
                            <input className={styles.input} type="number" min="0" step="1" value={mapping.defaultRepeat} onChange={(event) => updateMapping(index, { defaultRepeat: event.target.value })} />
                          </label>
                        </div>
                      )}

                      {mapping.targetKind === "state" && (
                        <>
                          <label className={styles.migrationCheckbox}>
                            <input type="checkbox" checked={mapping.createStateSchema} onChange={(event) => updateMapping(index, { createStateSchema: event.target.checked })} />
                            State schemaを新規作成
                          </label>
                          {mapping.createStateSchema && (
                            <label className={styles.formGroup}>
                              <span className={styles.label}>State schema（typed JSON object）</span>
                              <textarea
                                className={styles.textarea}
                                value={mapping.stateSchemaJSON}
                                onChange={(event) => updateMapping(index, { stateSchemaJSON: event.target.value })}
                                placeholder={'{"power":{"type":"enum","values":["off","on"]}}'}
                                required
                              />
                              <span className={styles.helperText}>
                                fieldごとにenum / number / boolean / stringを指定します。Schemaの型とState payloadが一致する必要があります。
                              </span>
                            </label>
                          )}
                        </>
                      )}
                    </fieldset>

                    {mapping.targetKind === "state" && (
                      <label className={styles.formGroup}>
                        <span className={styles.label}>State payload (JSON object)</span>
                        <textarea
                          className={styles.textarea}
                          value={mapping.stateJSON}
                          onChange={(event) => updateMapping(index, { stateJSON: event.target.value })}
                          placeholder={'{"power":"on"}'}
                          aria-describedby={`migration-state-help-${index}`}
                        />
                        <span id={`migration-state-help-${index}`} className={styles.helperText}>
                          空欄のままでもplanを作成できますが、State内容が未決定のplanはapply不可になります。
                        </span>
                      </label>
                    )}

                    <label className={styles.migrationCheckbox}>
                      <input
                        type="checkbox"
                        checked={mapping.approveRawMedian}
                        onChange={(event) => updateMapping(index, { approveRawMedian: event.target.checked })}
                      />
                      raw_medianからRAWへの変換を確認（該当sourceのみ）
                    </label>
                    {mapping.targetKind === "command" && (
                      <label className={styles.formGroup}>
                        <span className={styles.label}>Pico command（preset_command sourceのみ）</span>
                        <select
                          className={styles.select}
                          value={mapping.picoCommand}
                          onChange={(event) => updateMapping(index, { picoCommand: event.target.value })}
                        >
                          <option value="">未決定</option>
                          {presetCommands.map((command) => <option key={command} value={command}>{command}</option>)}
                        </select>
                      </label>
                    )}

                    {mappings.length > 1 && (
                      <button
                        type="button"
                        className={styles.statusButton}
                        onClick={() => removeMapping(index)}
                        disabled={submitting}
                      >
                        Mappingを削除
                      </button>
                    )}
                  </fieldset>
                </li>
              );
            })}
            </ol>
          </fieldset>

          <div className={styles.actions}>
            <button type="button" className={styles.button} onClick={addMapping} disabled={submitting || mappings.length >= 100}>
              Mappingを追加
            </button>
            <button type="submit" className={styles.primaryButton} disabled={submitting}>
              {submitting ? "plan作成中..." : "dry-run planを作成"}
            </button>
          </div>
      </form>

      {error && <p className={styles.error} role="alert">{error}</p>}

      {plan && (
        <article className={styles.migrationPlan} aria-labelledby="migration-plan-result-title">
          <div className={styles.deviceCardHeader}>
            <h4 id="migration-plan-result-title" className={styles.deviceCardTitle}>保存されたdry-run plan</h4>
            <span className={plan.ready_to_apply ? styles.planReady : styles.planPending}>
              {plan.ready_to_apply ? "apply準備完了（ここでは実行しません）" : "人の判断または追加情報が必要"}
            </span>
          </div>
          <dl className={styles.migrationPlanMeta}>
            <div><dt>Plan ID</dt><dd><code>{plan.plan_id}</code></dd></div>
            <div><dt>Plan digest</dt><dd><code>{plan.plan_digest ?? "なし"}</code></dd></div>
            <div><dt>Backup requirement</dt><dd>{plan.backup_requirement}</dd></div>
          </dl>
          <ol className={styles.migrationPlanActions}>
            {plan.payload.actions.map((action) => {
              const summary = plan.actions.find((item) => item.action_id === action.action_id);
              const isStateTarget = action.target_before.path.includes("/states/");
              return (
                <li key={action.action_id}>
                  <article className={styles.deviceCard}>
                    <h5 className={styles.deviceCardTitle}>{action.action_id} — {action.action}</h5>
                    <dl className={styles.migrationPlanMeta}>
                      <div><dt>Source</dt><dd><code>{action.source.path}</code></dd></div>
                      <div><dt>Source format</dt><dd>{action.source_format}</dd></div>
                      <div><dt>Source size</dt><dd>{action.source.size} bytes</dd></div>
                      <div><dt>Source SHA-256</dt><dd><code>{action.source.sha256 ?? "なし"}</code></dd></div>
                      <div><dt>Target</dt><dd><code>{action.target_before.path}</code> — {action.target_before.exists ? "既存target（競合）" : "未作成"}</dd></div>
                      <div><dt>Target format</dt><dd>{action.target_format} ({action.transform})</dd></div>
                      {action.decision && (
                        <div>
                          <dt>Human decision</dt>
                          <dd>
                            <ul className={styles.migrationDecisionList}>
                              <li>Room / Device / target: <code>{action.decision.room_id} / {action.decision.device_id} / {action.decision.target_id}</code></li>
                              {isStateTarget && (
                                <li>State payload: {action.decision.state ? <code>{JSON.stringify(action.decision.state)}</code> : "未決定"}</li>
                              )}
                              {action.source_format === "raw_median" && (
                                <li>raw_median変換: {action.decision.approve_raw_median ? "確認済み" : "未確認"}</li>
                              )}
                              {action.source_format === "preset_command" && (
                                <li>Pico command: {action.decision.pico_command || "未決定"}</li>
                              )}
                            </ul>
                          </dd>
                        </div>
                      )}
                      <div><dt>Selection</dt><dd>{summary?.selectable ? "選択可能" : "選択不可"}</dd></div>
                      <div>
                        <dt>Planned paths</dt>
                        <dd>
                          <ul className={styles.migrationPathList}>
                            {action.planned_writes.map((write) => (
                              <li key={write.path}>
                                <code>{write.path}</code> — {write.role} ({write.expected_after_state})
                                {write.content && (
                                  <pre className={styles.migrationWriteContent}>
                                    <code>{JSON.stringify(write.content, null, 2)}</code>
                                  </pre>
                                )}
                              </li>
                            ))}
                          </ul>
                        </dd>
                      </div>
                    </dl>
                  </article>
                </li>
              );
            })}
          </ol>
          <p className={styles.helperText}>
            applyとverifyは以下で別々に確認します。rollbackとIR送信はこの画面から実行できません。
          </p>
          <MigrationApplyVerifySection key={plan.plan_id} plan={plan} />
        </article>
      )}
    </section>
  );
}
