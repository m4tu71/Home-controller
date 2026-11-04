import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  captureIRCommand,
  captureIRState,
  createIRCommand,
  createIRState,
  deleteIRCommand,
  deleteIRState,
  fetchIRCommand,
  fetchIRCommands,
  fetchIRConfig,
  fetchIRExecutionOptions,
  fetchIRState,
  fetchIRStates,
  fetchIRStateSchema,
  sendIRCommand,
  sendIRState,
  updateIRCommand,
  updateIRConfig,
  updateIRState,
  updateIRStateSchema,
  IRSendAPIError,
  IRStateAPIError,
  isIRStateCaptureErrorResponse,
  type Device,
  type IRCommand,
  type IRCommandFormat,
  type IRCommandWriteRequest,
  type IRConfig,
  type IRExecutionMode,
  type IRExecutionOptionsResponse,
  type IRState,
  type IRStateSchema,
  type IRStateWriteRequest,
  type Room,
} from "../../../api/controller";
import {
  operationFieldsForRequest,
  PANASONIC_40,
  timingsToFormValue,
} from "../components/irOperationForm";
import { parseStateDraft, registeredState, stateOperationReason, stateSummary } from "../components/irStateDraft";
import { schemaFieldsFromSchema, schemaFromDrafts, fieldFromDraft, planFieldTypeChange, stateSchemaIssues } from "../components/irStateSchemaEditor";
import {
  type IRStateSchemaFieldDraft,
  type IRStateSchemaTemplate,
} from "../components/IRStateSchemaForm";

type IRStateSchemaTemplateDefinition = IRStateSchemaTemplate & {
  fields: IRStateSchemaFieldDraft[];
};

const irStateSchemaTemplates: IRStateSchemaTemplateDefinition[] = [
  {
    id: "air_conditioner",
    label: "エアコン標準",
    description: "従来の初期例です。全field必須・値域共通。対象リモコンの仕様を確認して調整してください。既存Deviceへ自動適用しません。",
    fields: [
      { key: "power", type: "enum", values: "on, off", min: "", max: "" },
      {
        key: "mode",
        type: "enum",
        values: "cool, heat, dry, fan, auto",
        min: "",
        max: "",
      },
      {
        key: "target_temperature",
        type: "number",
        values: "",
        min: "16",
        max: "30",
      },
      {
        key: "fan",
        type: "enum",
        values: "auto, low, medium, high",
        min: "",
        max: "",
      },
      {
        key: "swing",
        type: "enum",
        values: "auto, fixed",
        min: "",
        max: "",
      },
    ],
  },
  {
    id: "conditional_example",
    label: "汎用・条件付き設定例",
    description: "合成例です。enabled/profileでlevelを切り替えます。候補・範囲・刻み・表示情報は機器の仕様に合わせて確認してください。保存済みDeviceへ自動適用しません。",
    fields: schemaFieldsFromSchema({
      enabled: { type: "boolean", label: "動作" },
      profile: { type: "enum", values: ["normal", "boost"] },
      level: { type: "number", presence: "absent", cases: [
        { when: { all: [{ field: "enabled", in: [true] }, { field: "profile", in: ["normal"] }] }, presence: "required", min: 0, max: 5, step: 1 },
        { when: { all: [{ field: "enabled", in: [true] }, { field: "profile", in: ["boost"] }] }, presence: "required", min: 6, max: 10, step: 1 },
      ] },
      eco: { type: "boolean", presence: "optional" },
      note: { type: "string", presence: "optional", allow_empty: false },
    }),
  },
];

type IRConfigScope = {
  roomId: string;
  deviceId: string;
  remoteType: IRConfig["ir_remote_type"];
};


type IRControllerSelection = {
  selectedRoomId: string;
  selectedDeviceId: string;
  selectedRoom?: Room;
  selectedDevice?: Device;
};

export function useIRController(selection: IRControllerSelection) {
  const { selectedRoomId, selectedDeviceId, selectedRoom, selectedDevice } = selection;

  const [irConfigRemoteType, setIRConfigRemoteType] =
    useState<IRConfig["ir_remote_type"]>("command");

  const [irConfigScope, setIRConfigScope] = useState<IRConfigScope | null>(
    null
  );

  const [irConfigCarrierKHz, setIRConfigCarrierKHz] = useState("38");

  const [irConfigDefaultRepeat, setIRConfigDefaultRepeat] = useState("1");

  const [irConfigMessage, setIRConfigMessage] = useState("");

  const [irConfigError, setIRConfigError] = useState("");

  const [irConfigLoading, setIRConfigLoading] = useState(false);

  const irConfigRequestVersionRef = useRef(0);

  const irStateSchemaRequestVersionRef = useRef(0);

  const [irStateSchemaFields, setIRStateSchemaFields] = useState<
    IRStateSchemaFieldDraft[]
  >([]);

  const [irStateSchema, setIRStateSchema] = useState<IRStateSchema | null>(null);

  const [irStateSchemaFieldErrors, setIRStateSchemaFieldErrors] = useState<
    Record<number, string>
  >({});

  const [irStateSchemaExists, setIRStateSchemaExists] = useState(false);

  const [irStateSchemaLoaded, setIRStateSchemaLoaded] = useState(false);

  const [irStateSchemaExistingStateIDs, setIRStateSchemaExistingStateIDs] =
    useState<string[]>([]);

  const [irStateSchemaInvalidStateIDs, setIRStateSchemaInvalidStateIDs] =
    useState<string[]>([]);

  const [irStateSchemaMessage, setIRStateSchemaMessage] = useState("");

  const [irStateSchemaError, setIRStateSchemaError] = useState("");

  const [irStateSchemaLoading, setIRStateSchemaLoading] = useState(false);

  const [irStateSchemaTemplateId, setIRStateSchemaTemplateId] = useState(
    irStateSchemaTemplates[0]?.id ?? ""
  );

  const [irStates, setIRStates] = useState<IRState[]>([]);

  const [irStateListLoaded, setIRStateListLoaded] = useState(false);

  const [selectedIRStateId, setSelectedIRStateId] = useState("");

  const [irStateMessage, setIRStateMessage] = useState("");

  const [irStateError, setIRStateError] = useState("");

  const [irStateLoading, setIRStateLoading] = useState(false);

  const [irStateCreateId, setIRStateCreateId] = useState("");

  const [irStateCreateName, setIRStateCreateName] = useState("");

  const [irStateCreateValues, setIRStateCreateValues] = useState<Record<string, string>>({});

  const [irStateCreateFormat, setIRStateCreateFormat] =
    useState<IRCommandFormat>("raw_samples");

  const [irStateCreateRawTimings, setIRStateCreateRawTimings] = useState("");

  const [irStateCreateProtocol, setIRStateCreateProtocol] = useState(PANASONIC_40);

  const [irStateCreateCode, setIRStateCreateCode] = useState("");

  const [irStateCreatePreset, setIRStateCreatePreset] = useState("");

  const [irStateEditName, setIRStateEditName] = useState("");

  const [irStateEditValues, setIRStateEditValues] = useState<Record<string, string>>({});

  const [irStateEditFormat, setIRStateEditFormat] =
    useState<IRCommandFormat>("raw_samples");

  const [irStateEditRawTimings, setIRStateEditRawTimings] = useState("");

  const [irStateEditProtocol, setIRStateEditProtocol] = useState(PANASONIC_40);

  const [irStateEditCode, setIRStateEditCode] = useState("");

  const [irStateEditPreset, setIRStateEditPreset] = useState("");

  const [irStateFieldErrors, setIRStateFieldErrors] = useState<Record<string, string>>({});

  const [irStateCaptureSamples, setIRStateCaptureSamples] = useState("3");

  const [irStateCaptureMaxAttempts, setIRStateCaptureMaxAttempts] = useState("9");

  const [irStateSendRepeat, setIRStateSendRepeat] = useState("");

  const [irStateExecutionMode, setIRStateExecutionMode] = useState<IRExecutionMode | "">("");

  const [irStateExecutionOptions, setIRStateExecutionOptions] = useState<IRExecutionOptionsResponse | null>(null);

  const [irStateExecutionOptionsError, setIRStateExecutionOptionsError] = useState("");

  const [irStateCaptureFieldErrors, setIRStateCaptureFieldErrors] = useState<
    Record<string, string>
  >({});

  const [irStateSendRepeatError, setIRStateSendRepeatError] = useState("");

  const irStateRequestVersionRef = useRef(0);

  const irStateDetailRequestVersionRef = useRef(0);

  const irStateSelectionRef = useRef({ roomId: "", deviceId: "", stateId: "" });

  const [irCommands, setIRCommands] = useState<IRCommand[]>([]);

  const [selectedIRCommandId, setSelectedIRCommandId] = useState("");

  const [irCommandMessage, setIRCommandMessage] = useState("");

  const [irCommandError, setIRCommandError] = useState("");

  const [irCommandLoading, setIRCommandLoading] = useState(false);

  const [irCommandListLoaded, setIRCommandListLoaded] = useState(false);

  const irCommandRequestVersionRef = useRef(0);

  const [irCommandCreateId, setIRCommandCreateId] = useState("");

  const [irCommandCreateName, setIRCommandCreateName] = useState("");

  const [irCommandCreateFormat, setIRCommandCreateFormat] =
    useState<IRCommandFormat>("raw_samples");

  const [irCommandCreatePreset, setIRCommandCreatePreset] = useState("");

  const [irCommandCreateRawTimings, setIRCommandCreateRawTimings] = useState("");

  const [irCommandCreateProtocol, setIRCommandCreateProtocol] =
    useState(PANASONIC_40);

  const [irCommandCreateCode, setIRCommandCreateCode] = useState("");

  const [irCommandCreateRepeat, setIRCommandCreateRepeat] = useState("");

  const [irCommandEditName, setIRCommandEditName] = useState("");

  const [irCommandEditFormat, setIRCommandEditFormat] =
    useState<IRCommandFormat>("raw_samples");

  const [irCommandEditPreset, setIRCommandEditPreset] = useState("");

  const [irCommandEditRawTimings, setIRCommandEditRawTimings] = useState("");

  const [irCommandEditProtocol, setIRCommandEditProtocol] =
    useState(PANASONIC_40);

  const [irCommandEditCode, setIRCommandEditCode] = useState("");

  const [irCommandEditRepeat, setIRCommandEditRepeat] = useState("");

  const [irCommandCaptureSamples, setIRCommandCaptureSamples] = useState("3");

  const [irCommandCaptureMaxAttempts, setIRCommandCaptureMaxAttempts] =
    useState("9");

  const [irCommandSendRepeat, setIRCommandSendRepeat] = useState("");

  const [irCommandExecutionMode, setIRCommandExecutionMode] = useState<IRExecutionMode | "">("");

  const [irCommandExecutionOptions, setIRCommandExecutionOptions] = useState<IRExecutionOptionsResponse | null>(null);

  const [irCommandExecutionOptionsError, setIRCommandExecutionOptionsError] = useState("");

  const syncIRConfigForm = (config?: IRConfig) => {
    const nextConfig = config ?? {
      ir_remote_type: "command" as const,
      carrier_khz: 38,
      default_repeat: 1,
    };

    setIRConfigRemoteType(nextConfig.ir_remote_type);
    setIRConfigCarrierKHz(String(nextConfig.carrier_khz));
    setIRConfigDefaultRepeat(
      nextConfig.default_repeat === undefined
        ? ""
        : String(nextConfig.default_repeat)
    );
  };

  const syncIRStateSchemaForm = (schema?: IRStateSchema) => {
    setIRStateSchema(schema ?? null);
    setIRStateSchemaFields(schema ? schemaFieldsFromSchema(schema) : []);
    setIRStateSchemaFieldErrors({});
  };

  const stateValuesToForm = (state: Record<string, unknown>): Record<string, string> =>
    Object.fromEntries(
      Object.entries(state).map(([key, value]) => [key, String(value)])
    );

  const syncIRStateEditForm = (state?: IRState) => {
    if (!state) {
      setIRStateEditName("");
      setIRStateEditValues({});
      setIRStateEditFormat("raw_samples");
      setIRStateEditRawTimings("");
      setIRStateEditProtocol(PANASONIC_40);
      setIRStateEditCode("");
      setIRStateEditPreset("");
      return;
    }
    setIRStateEditName(state.name);
    setIRStateEditValues(stateValuesToForm(state.state));
    setIRStateEditFormat(state.format);
    setIRStateEditRawTimings(timingsToFormValue(state.timings_us));
    setIRStateEditProtocol(state.protocol ?? PANASONIC_40);
    setIRStateEditCode(state.code?.trim().toUpperCase() ?? "");
    setIRStateEditPreset(state.pico_command ?? "");
  };

  const syncIRCommandEditForm = (command?: IRCommand) => {
    if (!command) {
      setIRCommandEditName("");
      setIRCommandEditFormat("raw_samples");
      setIRCommandEditPreset("");
      setIRCommandEditRawTimings("");
      setIRCommandEditProtocol(PANASONIC_40);
      setIRCommandEditCode("");
      setIRCommandEditRepeat("");
      return;
    }

    setIRCommandEditName(command.name);
    setIRCommandEditFormat(command.format);
    setIRCommandEditPreset(command.pico_command ?? "");
    setIRCommandEditRawTimings(timingsToFormValue(command.timings_us));
    setIRCommandEditProtocol(command.protocol ?? PANASONIC_40);
    setIRCommandEditCode(command.code?.trim().toUpperCase() ?? "");
    setIRCommandEditRepeat(
      command.repeat === undefined ? "" : String(command.repeat)
    );
  };

  useEffect(() => {
    const command = irCommands.find((item) => item.id === selectedIRCommandId);
    if (!selectedRoomId || !selectedDeviceId || command?.format !== "code" || !command.protocol) {
      return;
    }
    let active = true;
    fetchIRExecutionOptions(selectedRoomId, selectedDeviceId, command.protocol)
      .then((options) => {
        if (!active) return;
        setIRCommandExecutionOptions(options);
        setIRCommandExecutionOptionsError("");
      })
      .catch((error) => {
        if (!active) return;
        setIRCommandExecutionOptions(null);
        setIRCommandExecutionOptionsError(error instanceof Error ? error.message : "execution optionsの取得に失敗しました");
      });
    return () => { active = false; };
  }, [irCommands, selectedDeviceId, selectedIRCommandId, selectedRoomId]);

  useEffect(() => {
    const state = irStates.find((item) => item.id === selectedIRStateId);
    if (!selectedRoomId || !selectedDeviceId || state?.format !== "code" || !state.protocol) {
      return;
    }
    let active = true;
    fetchIRExecutionOptions(selectedRoomId, selectedDeviceId, state.protocol)
      .then((options) => {
        if (!active) return;
        setIRStateExecutionOptions(options);
        setIRStateExecutionOptionsError("");
      })
      .catch((error) => {
        if (!active) return;
        setIRStateExecutionOptions(null);
        setIRStateExecutionOptionsError(error instanceof Error ? error.message : "execution optionsの取得に失敗しました");
      });
    return () => { active = false; };
  }, [irStates, selectedDeviceId, selectedIRStateId, selectedRoomId]);

  const loadIRConfig = async (roomId: string, device?: Device) => {
    const requestVersion = ++irConfigRequestVersionRef.current;
    setIRConfigError("");
    setIRConfigScope(null);

    if (!roomId || !device) {
      setIRConfigLoading(false);
      setIRConfigMessage("IR Adapter設定を表示するDeviceを選択してください");
      syncIRConfigForm();
      return;
    }

    if (device.primary_adapter !== "ir") {
      setIRConfigLoading(false);
      setIRConfigMessage("");
      syncIRConfigForm();
      return;
    }

    try {
      setIRConfigLoading(true);
      setIRConfigMessage("IR Adapter設定を取得中...");

      const config = await fetchIRConfig(roomId, device.id);
      if (requestVersion !== irConfigRequestVersionRef.current) {
        return;
      }
      syncIRConfigForm(config);
      setIRConfigScope({
        roomId,
        deviceId: device.id,
        remoteType: config.ir_remote_type,
      });
      setIRConfigMessage("IR Adapter設定を取得しました");
    } catch (error) {
      if (requestVersion !== irConfigRequestVersionRef.current) {
        return;
      }
      const message =
        error instanceof Error ? error.message : "IR Adapter設定の取得に失敗しました";

      if (message.includes("IR設定が見つかりません")) {
        syncIRConfigForm();
        setIRConfigScope({ roomId, deviceId: device.id, remoteType: "command" });
        setIRConfigMessage(
          "IR Adapter設定は未作成です。初期値を編集して保存できます。"
        );
        return;
      }

      setIRConfigError(message);
      setIRConfigMessage("");
    } finally {
      if (requestVersion === irConfigRequestVersionRef.current) {
        setIRConfigLoading(false);
      }
    }
  };

  const loadIRStateSchema = async (roomId: string, device?: Device) => {
    const requestVersion = ++irStateSchemaRequestVersionRef.current;
    setIRStateSchemaError("");
    setIRStateSchemaInvalidStateIDs([]);

    if (
      !roomId ||
      !device ||
      device.primary_adapter !== "ir" ||
      irConfigScope?.roomId !== roomId ||
      irConfigScope?.deviceId !== device.id ||
      irConfigScope?.remoteType !== "stateful"
    ) {
      setIRStateSchemaLoaded(false);
      setIRStateSchemaExists(false);
      setIRStateSchemaExistingStateIDs([]);
      syncIRStateSchemaForm();
      setIRStateSchemaMessage("");
      setIRStateSchemaLoading(false);
      return;
    }

    try {
      setIRStateSchemaLoading(true);
      setIRStateSchemaLoaded(false);
      setIRStateSchemaMessage("IR State Schemaを取得中...");

      const schema = await fetchIRStateSchema(roomId, device.id);
      if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
      setIRStateSchemaExists(true);
      setIRStateSchemaLoaded(true);
      setIRStateSchemaExistingStateIDs([]);
      syncIRStateSchemaForm(schema);
      setIRStateSchemaMessage(`IR State Schemaを${Object.keys(schema).length} field取得しました`);
    } catch (error) {
      if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
      if (
        error instanceof IRStateAPIError &&
        error.status === 404 &&
        error.payload.error === "ir state schema not found"
      ) {
        setIRStateSchemaExists(false);
        setIRStateSchemaLoaded(true);
        syncIRStateSchemaForm();
        setIRStateSchemaMessage("IR State Schemaは未作成です。既存Stateとの互換性を確認してください。");

        try {
          const states = await fetchIRStates(roomId, device.id);
          if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
          setIRStateSchemaExistingStateIDs(states.map((state) => state.id));
        } catch (stateError) {
          if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
          setIRStateSchemaExistingStateIDs([]);
          setIRStateSchemaError(
            stateError instanceof Error
              ? `${stateError.message}。Schema未作成でもState一覧の確認は後で再試行できます。`
              : "既存State一覧の取得に失敗しました。Schema未作成でも後で再試行できます。"
          );
        }
        return;
      }

      setIRStateSchemaExists(false);
      setIRStateSchemaLoaded(true);
      setIRStateSchemaExistingStateIDs([]);
      syncIRStateSchemaForm();
      setIRStateSchemaError(
        error instanceof Error
          ? error.message
          : "IR State Schemaの取得に失敗しました"
      );
      setIRStateSchemaMessage("");
    } finally {
      if (requestVersion === irStateSchemaRequestVersionRef.current) {
        setIRStateSchemaLoading(false);
      }
    }
  };

  const handleApplyIRStateSchemaTemplate = () => {
    const template = irStateSchemaTemplates.find(
      (candidate) => candidate.id === irStateSchemaTemplateId
    );
    if (!template) {
      window.alert("適用するState Schemaテンプレートを選択してください。");
      return;
    }

    setIRStateSchemaFields(template.fields.map((field) => ({ ...field })));
    setIRStateSchemaFieldErrors({});
    setIRStateSchemaInvalidStateIDs([]);
    setIRStateSchemaError("");
    setIRStateSchemaMessage(
      `${template.label}テンプレートをフォームへ適用しました。内容を確認して保存してください。`
    );
  };

  const handleAddIRStateSchemaField = () => {
    setIRStateSchemaFields((fields) => [
      ...fields,
      { key: "", type: "string", values: "", min: "", max: "" },
    ]);
    setIRStateSchemaFieldErrors({});
  };

  const handleUpdateIRStateSchemaField = (
    index: number,
    field: keyof IRStateSchemaFieldDraft,
    value: string
  ) => {
    const current = irStateSchemaFields[index];
    if (!current) return;
    let updated: IRStateSchemaFieldDraft;
    if (field === "type") {
      const { next, removed } = planFieldTypeChange(fieldFromDraft(current), value as IRStateSchema[string]["type"]);
      if (removed.length && !window.confirm(`型変更で制約を除去します: ${removed.join(", ")}。変更後: ${JSON.stringify(next)}。よろしいですか？`)) return;
      updated = { ...current, type: next.type, definition: JSON.stringify(next) };
    } else {
      updated = { ...current, [field]: value };
    }
    setIRStateSchemaFields((fields) => fields.map((entry, currentIndex) => currentIndex === index ? updated : entry));
    setIRStateSchemaFieldErrors((errors) => {
      const remaining = { ...errors };
      delete remaining[index];
      return remaining;
    });
  };

  const handleRemoveIRStateSchemaField = (index: number) => {
    setIRStateSchemaFields((fields) => fields.filter((_, currentIndex) => currentIndex !== index));
    setIRStateSchemaFieldErrors({});
  };

  const handleUpdateIRStateSchema = async (
    event: FormEvent<HTMLFormElement>
  ) => {
    event.preventDefault();

    if (!selectedRoomId || !selectedDevice) {
      setIRStateSchemaError("IR State Schemaを保存するDeviceを選択してください");
      setIRStateSchemaMessage("");
      return;
    }

    const { schema, errors: fieldErrors } = schemaFromDrafts(irStateSchemaFields);

    if (Object.keys(fieldErrors).length > 0) {
      setIRStateSchemaFieldErrors(fieldErrors);
      setIRStateSchemaError("");
      setIRStateSchemaMessage("入力内容を確認してください");
      window.alert(
        "IR State Schemaを保存できません。field名・型・enum values・min / maxを確認してください。詳細は各fieldの近くに表示されています。"
      );
      return;
    }

    if (Object.keys(schema).length === 0) {
      setIRStateSchemaError("fieldを1つ以上追加してください");
      setIRStateSchemaMessage("");
      window.alert("IR State Schemaを保存できません。fieldを1つ以上追加してください。");
      return;
    }

    const requestVersion = irStateSchemaRequestVersionRef.current;
    setIRStateSchemaLoading(true);
    let affectedStates: IRState[];
    try {
      affectedStates = await fetchIRStates(selectedRoomId, selectedDevice.id);
      if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
    } catch (error) {
      if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
      setIRStateSchemaLoading(false);
      setIRStateSchemaError(`保存前に既存Stateへの影響を確認できません: ${error instanceof Error ? error.message : "取得失敗"}`);
      return;
    }
    const changedFields = [...new Set([...Object.keys(irStateSchema ?? {}), ...Object.keys(schema)])].filter((id) => JSON.stringify(irStateSchema?.[id]) !== JSON.stringify(schema[id]));
    const impact = `変更field: ${changedFields.join(", ") || "なし"}。変更内容: ${changedFields.map((id) => `${id}: ${JSON.stringify(irStateSchema?.[id]) ?? "未設定"} → ${JSON.stringify(schema[id]) ?? "削除"}`).join(" / ")}。既存State: ${affectedStates.map((state) => `${state.id}（新Schema: ${stateSchemaIssues(schema, state.state).map((issue) => issue.message).join(", ") || "値は適合"}、${state.format === "raw_samples" ? state.count || state.captures?.length || state.timings_us?.length ? "学習済み" : "未学習" : "信号登録済み"}、値=${JSON.stringify(state.state)}）`).join(" / ") || "なし"}。表示情報の意味と実リモコンの対応は再確認してください。学習情報は自動変更しません。`;
    if (
      irStateSchemaExists &&
      !window.confirm(`既存のIR State Schemaを上書きします。${impact} 既存Stateとの整合性を確認してから保存されます。よろしいですか？`)
    ) {
      setIRStateSchemaLoading(false);
      setIRStateSchemaMessage("IR State Schemaの上書きをキャンセルしました");
      setIRStateSchemaError("");
      return;
    }
    if (
      !irStateSchemaExists &&
      affectedStates.length > 0 &&
      !window.confirm(`既存Stateがあるため、Schema保存時に整合性を確認します。${impact} 互換なSchemaであることを確認しましたか？`)
    ) {
      setIRStateSchemaLoading(false);
      setIRStateSchemaMessage("IR State Schemaの保存をキャンセルしました");
      setIRStateSchemaError("");
      return;
    }

    try {
      setIRStateSchemaLoading(true);
      setIRStateSchemaError("");
      setIRStateSchemaInvalidStateIDs([]);
      setIRStateSchemaMessage("IR State Schemaを保存中...");

      const savedSchema = await updateIRStateSchema(
        selectedRoomId,
        selectedDevice.id,
        schema
      );
      if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
      setIRStateSchemaExists(true);
      setIRStateSchemaExistingStateIDs([]);
      setIRStateSchemaFieldErrors({});
      syncIRStateSchemaForm(savedSchema);
      setIRStateSchemaMessage("IR State Schemaを保存しました");
    } catch (error) {
      if (requestVersion !== irStateSchemaRequestVersionRef.current) return;
      const invalidStateIDs =
        error instanceof IRStateAPIError
          ? error.payload.invalid_state_ids ?? []
          : [];
      if (error instanceof IRStateAPIError) {
        const reasons: Record<number, string> = {};
        for (const issue of error.payload.issues ?? []) {
          const index = irStateSchemaFields.findIndex((f) => f.key.trim() === issue.field_id);
          const reason = `${issue.state_id ? `State ${issue.state_id}: ` : ""}${issue.case_index !== undefined ? `case ${issue.case_index + 1}: ` : ""}${issue.code}: ${issue.message}`;
          if (index >= 0) reasons[index] = [reasons[index], reason].filter(Boolean).join(" / ");
        }
        setIRStateSchemaFieldErrors(reasons);
      }
      setIRStateSchemaInvalidStateIDs(invalidStateIDs);
      const message = (error instanceof Error ? error.message : "IR State Schemaの保存に失敗しました") +
        (error instanceof IRStateAPIError ? (error.payload.issues ?? []).map((issue) => ` / ${issue.field_id}${issue.case_index === undefined ? "" : ` case ${issue.case_index + 1}`}${issue.state_id ? ` State ${issue.state_id}` : ""}: ${issue.code} ${issue.message}`).join("") : "");
      setIRStateSchemaError(
        invalidStateIDs.length > 0
          ? `${message}。既存Stateと学習情報は保持されています。理由を確認し、旧Deviceを保全した別Deviceへの設定を検討してください。`
          : `${message}。入力内容と対象Deviceを確認して再試行してください。`
      );
      setIRStateSchemaMessage("");
      if (error instanceof IRStateAPIError && (error.status === 400 || error.status === 409)) {
        window.alert(
          invalidStateIDs.length > 0
            ? "IR State Schemaを保存できません。既存Stateと整合しません。対象Stateを確認してください。"
            : "IR State Schemaを保存できません。fieldの型・値と対象Stateの整合性を確認してください。"
        );
      }
    } finally {
      if (requestVersion === irStateSchemaRequestVersionRef.current) setIRStateSchemaLoading(false);
    }
  };

  const loadIRStates = async (
    roomId: string,
    device?: Device,
    preferredStateId = ""
  ) => {
    const requestVersion = ++irStateRequestVersionRef.current;
    ++irStateDetailRequestVersionRef.current;
    setIRStateExecutionMode("");
    if (
      !roomId ||
      !device ||
      device.primary_adapter !== "ir" ||
      irConfigScope?.roomId !== roomId ||
      irConfigScope?.deviceId !== device.id ||
      irConfigScope?.remoteType !== "stateful"
    ) {
      setIRStates([]);
      setIRStateListLoaded(false);
      setSelectedIRStateId("");
      irStateSelectionRef.current = { roomId: "", deviceId: "", stateId: "" };
      syncIRStateEditForm();
      setIRStateFieldErrors({});
      setIRStateMessage("");
      setIRStateError("");
      setIRStateLoading(false);
      return;
    }

    try {
      setIRStateLoading(true);
      setIRStateListLoaded(false);
      setIRStateError("");
      setIRStateMessage("IR State一覧を取得中...");
      const nextStates = await fetchIRStates(roomId, device.id);
      if (requestVersion !== irStateRequestVersionRef.current) return;

      setIRStates(nextStates);
      setIRStateListLoaded(true);
      if (nextStates.length === 0) {
        setSelectedIRStateId("");
        irStateSelectionRef.current = { roomId, deviceId: device.id, stateId: "" };
        syncIRStateEditForm();
        setIRStateMessage("");
        return;
      }

      const selectedStateStillExists = nextStates.some(
        (state) => state.id === selectedIRStateId
      );
      const preferredStateStillExists = nextStates.some(
        (state) => state.id === preferredStateId
      );
      const nextSelectedStateId = preferredStateStillExists
        ? preferredStateId
        : selectedStateStillExists
          ? selectedIRStateId
          : nextStates[0].id;
      const nextSelectedState = nextStates.find(
        (state) => state.id === nextSelectedStateId
      );
      setSelectedIRStateId(nextSelectedStateId);
      irStateSelectionRef.current = {
        roomId,
        deviceId: device.id,
        stateId: nextSelectedStateId,
      };
      syncIRStateEditForm(nextSelectedState);
      setIRStateMessage(`IR Stateを${nextStates.length}件取得しました`);
    } catch (error) {
      if (requestVersion !== irStateRequestVersionRef.current) return;
      setIRStates([]);
      setIRStateListLoaded(true);
      setSelectedIRStateId("");
      irStateSelectionRef.current = { roomId: "", deviceId: "", stateId: "" };
      syncIRStateEditForm();
      setIRStateError(
        error instanceof Error ? error.message : "IR State一覧の取得に失敗しました"
      );
      setIRStateMessage("");
    } finally {
      if (requestVersion === irStateRequestVersionRef.current) {
        setIRStateLoading(false);
      }
    }
  };

  const handleSelectIRState = async (state: IRState) => {
    if (!selectedRoomId || !selectedDevice) return;
    const roomId = selectedRoomId;
    const deviceId = selectedDevice.id;
    const detailRequestVersion = ++irStateDetailRequestVersionRef.current;
    irStateSelectionRef.current = { roomId, deviceId, stateId: state.id };
    setSelectedIRStateId(state.id);
    setIRStateExecutionMode("");
    syncIRStateEditForm(state);
    setIRStateFieldErrors({});
    setIRStateCaptureFieldErrors({});
    setIRStateSendRepeatError("");
    setIRStateError("");
    setIRStateMessage(`選択中IR State: ${state.name}`);

    try {
      const detail = await fetchIRState(roomId, deviceId, state.id);
      const selection = irStateSelectionRef.current;
      if (
        detailRequestVersion !== irStateDetailRequestVersionRef.current ||
        selection.roomId !== roomId ||
        selection.deviceId !== deviceId ||
        selection.stateId !== state.id
      ) {
        return;
      }
      setIRStates((states) =>
        states.map((current) => current.id === detail.id ? detail : current)
      );
      syncIRStateEditForm(detail);
    } catch (error) {
      const selection = irStateSelectionRef.current;
      if (
        detailRequestVersion !== irStateDetailRequestVersionRef.current ||
        selection.roomId !== roomId ||
        selection.deviceId !== deviceId ||
        selection.stateId !== state.id
      ) {
        return;
      }
      setIRStateError(
        error instanceof Error ? error.message : "IR State詳細の取得に失敗しました"
      );
    }
  };

  const parseIRStateValues = (
    values: Record<string, string>
  ): { state?: Record<string, unknown>; errors: Record<string, string>; discarded?: Record<string, string> } => {
    if (!irStateSchema) return { errors: { schema: "IR State Schemaを保存してください" } };
    const draft = parseStateDraft(irStateSchema, values);
    return { ...draft, state: Object.keys(draft.errors).length ? undefined : draft.state };
  };

  const stateErrorIssues = (error: unknown): string => {
    if (!(error instanceof IRStateAPIError) && !(error instanceof IRSendAPIError)) return "";
    const issues = error.payload.issues ?? [];
    setIRStateFieldErrors(Object.fromEntries(issues.map((issue) => [issue.field_id, `${issue.code}: ${issue.message}`])));
    return issues.map((issue) => ` / ${issue.field_id}${issue.case_index === undefined ? "" : ` case ${issue.case_index + 1}`}: ${issue.code} ${issue.message}`).join("");
  };

  const handleCreateIRState = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selectedRoomId || !selectedDevice || !irStateSchema) {
      setIRStateError("IR State Schemaを保存したstateful Deviceを選択してください");
      setIRStateMessage("");
      return;
    }
    const id = irStateCreateId.trim();
    const name = irStateCreateName.trim();
    const parsed = parseIRStateValues(irStateCreateValues);
    setIRStateFieldErrors(parsed.errors);
    if (!id || !name) {
      setIRStateError("IR State IDと表示名を入力してください");
      setIRStateMessage("");
      return;
    }
    if (!parsed.state) {
      setIRStateError("State入力fieldを確認してください");
      setIRStateMessage("");
      return;
    }
    const operation = operationFieldsForRequest(
      irStateCreateFormat,
      irStateCreateRawTimings,
      irStateCreateProtocol,
      irStateCreateCode,
      irStateCreatePreset
    );
    if (operation.error) {
      setIRStateError(operation.error);
      setIRStateMessage("");
      return;
    }

    if (parsed.discarded && Object.keys(parsed.discarded).length && !window.confirm(`適用外のdraft値を保存payloadから除外します: ${Object.entries(parsed.discarded).map(([key, value]) => `${key}=${JSON.stringify(value)}`).join(" / ")}。保存しますか？`)) {
      setIRStateMessage("IR State作成をキャンセルしました"); return;
    }
    const request: IRStateWriteRequest = {
      id,
      name,
      state: parsed.state,
      format: irStateCreateFormat,
      ...operation.fields,
    };
    try {
      setIRStateLoading(true);
      setIRStateError("");
      setIRStateMessage(`IR State作成中: ${name}`);
      const createdState = await createIRState(selectedRoomId, selectedDevice.id, request);
      setIRStateCreateId("");
      setIRStateCreateName("");
      setIRStateCreateValues({});
      setIRStateCreateFormat("raw_samples");
      setIRStateCreateRawTimings("");
      setIRStateCreateProtocol(PANASONIC_40);
      setIRStateCreateCode("");
      setIRStateCreatePreset("");
      setIRStateFieldErrors({});
      await loadIRStates(selectedRoomId, selectedDevice, createdState.id);
      setIRStateMessage(`IR Stateを作成しました: ${createdState.name}`);
    } catch (error) {
      setIRStateError((error instanceof Error ? error.message : "IR State作成に失敗しました") + stateErrorIssues(error));
      setIRStateMessage("");
    } finally {
      setIRStateLoading(false);
    }
  };

  const handleUpdateIRState = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const selectedState = irStates.find((state) => state.id === selectedIRStateId);
    if (!selectedRoomId || !selectedDevice || !selectedState) {
      setIRStateError("編集するIR Stateを選択してください");
      setIRStateMessage("");
      return;
    }
    const name = irStateEditName.trim();
    const protectedState = registeredState(selectedState);
    const parsed = protectedState ? { state: selectedState.state, errors: {}, discarded: {} } : parseIRStateValues(irStateEditValues);
    const repeatText = String(new FormData(event.currentTarget).get("repeat") ?? "");
    const repeat = repeatText.trim() === "" ? undefined : Number(repeatText);
    if (repeat !== undefined && (!Number.isInteger(repeat) || repeat < 0 || repeat > 5)) {
      setIRStateError("保存repeatは0から5の整数にしてください"); return;
    }
    setIRStateFieldErrors(parsed.errors);
    if (!name) {
      setIRStateError("IR Stateの表示名を入力してください");
      setIRStateMessage("");
      return;
    }
    if (!parsed.state) {
      setIRStateError("State入力fieldを確認してください");
      setIRStateMessage("");
      return;
    }
    if (isIRStateLearned(selectedState) && irStateEditFormat !== selectedState.format) {
      setIRStateError("学習済みraw_samples Stateはformatを変更できません");
      setIRStateMessage("");
      return;
    }

    const operation = operationFieldsForRequest(
      irStateEditFormat,
      irStateEditRawTimings,
      irStateEditProtocol,
      irStateEditCode,
      irStateEditPreset
    );
    if (operation.error) {
      setIRStateError(operation.error);
      setIRStateMessage("");
      return;
    }

    if (parsed.discarded && Object.keys(parsed.discarded).length && !window.confirm(`適用外のdraft値を保存payloadから除外します: ${Object.entries(parsed.discarded).map(([key, value]) => `${key}=${JSON.stringify(value)}`).join(" / ")}。保存しますか？`)) {
      setIRStateMessage("IR State更新をキャンセルしました"); return;
    }
    try {
      setIRStateLoading(true);
      setIRStateError("");
      setIRStateMessage(`IR State更新中: ${selectedState.name}`);
      const updatedState = await updateIRState(selectedRoomId, selectedDevice.id, selectedState.id, {
        name,
        ...(repeat !== undefined ? { repeat } : {}),
        ...(!protectedState ? { state: parsed.state, format: irStateEditFormat, ...operation.fields } : {}),
      });
      setIRStateFieldErrors({});
      await loadIRStates(selectedRoomId, selectedDevice, updatedState.id);
      setIRStateMessage(`IR Stateを更新しました: ${updatedState.name}`);
    } catch (error) {
      setIRStateError((error instanceof Error ? error.message : "IR State更新に失敗しました") + stateErrorIssues(error));
      setIRStateMessage("");
    } finally {
      setIRStateLoading(false);
    }
  };

  const handleDeleteIRState = async () => {
    const selectedState = irStates.find((state) => state.id === selectedIRStateId);
    if (!selectedRoomId || !selectedDevice || !selectedState) {
      setIRStateError("削除するIR Stateを選択してください");
      setIRStateMessage("");
      return;
    }
    if (!window.confirm(`IR State「${selectedState.name}」を削除します。よろしいですか？`)) {
      setIRStateMessage("IR State削除をキャンセルしました");
      setIRStateError("");
      return;
    }
    try {
      setIRStateLoading(true);
      setIRStateError("");
      setIRStateMessage(`IR State削除中: ${selectedState.name}`);
      const result = await deleteIRState(selectedRoomId, selectedDevice.id, selectedState.id);
      await loadIRStates(selectedRoomId, selectedDevice);
      setIRStateMessage(`IR StateをTrashへ移動しました: ${selectedState.name} (Trash ${result.trash_entry_id} / operation ${result.operation_id} / ${result.status})`);
    } catch (error) {
      setIRStateError(error instanceof Error ? error.message : "IR State削除に失敗しました");
      setIRStateMessage("");
    } finally {
      setIRStateLoading(false);
    }
  };

  const isIRStateLearned = (state?: IRState): boolean =>
    state?.format === "raw_samples" &&
    ((state.captures?.length ?? 0) > 0 ||
      (state.timings_us?.length ?? 0) > 0 ||
      (state.count ?? 0) > 0);

  const parseIRStateCaptureOptions = (): {
    samples?: number;
    maxAttempts?: number;
    error?: string;
    field?: "samples" | "maxAttempts";
  } => {
    const samples = Number(irStateCaptureSamples);
    const maxAttempts = Number(irStateCaptureMaxAttempts);
    if (!Number.isInteger(samples) || samples < 1 || samples > 7) {
      return { error: "samplesは1から7の整数で指定してください", field: "samples" };
    }
    if (
      !Number.isInteger(maxAttempts) ||
      maxAttempts < samples ||
      maxAttempts > 20
    ) {
      return {
        error: "max_attemptsはsamples以上20以下の整数で指定してください",
        field: "maxAttempts",
      };
    }
    return { samples, maxAttempts };
  };

  const parseIRStateSendRepeat = (): number | undefined | null => {
    if (irStateSendRepeat.trim() === "") return undefined;
    const repeat = Number(irStateSendRepeat);
    return Number.isInteger(repeat) && repeat >= 1 && repeat <= 5 ? repeat : null;
  };

  const getIRStateCaptureFailureGuidance = (message: string): string => {
    if (message.includes("有効なIR信号")) {
      return "既存の学習結果は維持されています。リモコンの向き・距離、samples、max_attemptsを確認して再試行してください。";
    }
    if (message.includes("代表となるIR信号")) {
      return "既存の学習結果は維持されています。複数回のcaptureで同じリモコン操作を行い、ノイズやReceiver配線を確認してください。";
    }
    if (message.includes("Schema")) {
      return "State Schemaと選択中Stateの整合性を確認してください。";
    }
    if (message.includes("Controller daemon")) {
      return "controller-daemonの起動状態、Pico serial接続、IR Receiver配線を手動確認してください。";
    }
    return "既存の学習結果は維持されています。対象State、IR Receiver配線、controller-daemonのログを手動確認してください。";
  };

  const getIRStateSendFailureGuidance = (message: string): string => {
    if (message.includes("まだ学習されていません")) {
      return "raw_samples Stateを学習してから送信を再試行してください。";
    }
    if (message.includes("repeat")) {
      return "repeatを空欄にするか、1から5の整数に修正してください。";
    }
    if (message.includes("Controller daemon")) {
      return "送信を繰り返さず、controller-daemonの起動状態、Pico serial接続、IR LED配線を手動確認してください。";
    }
    return "対象State、IR Adapter設定、controller-daemonとPicoの接続を手動確認してください。";
  };

  const getIRSendDeliveryGuidance = (error: IRSendAPIError): string => {
    const payload = error.payload;
    if (payload.delivery_state === "not_sent") {
      const alternative = payload.retryable_with.length > 0
        ? `別経路として${payload.retryable_with.join(", ")}を選択できます。`
        : "利用可能な別経路はありません。";
      return `送信は開始されていません。${alternative} 人が確認して経路を選び直してください。自動fallback・自動再送信はしません。`;
    }
    if (payload.delivery_state === "partially_sent") {
      return `${payload.requested_repeats}回中${payload.completed_repeats}回の成功を確認しました。家電の実状態を確認し、残りを自動再送信しないでください。`;
    }
    if (payload.delivery_state === "unknown") {
      return `一部または全部が送信済みの可能性があります。成功確認済みは${payload.completed_repeats}/${payload.requested_repeats}回です。再操作前に家電の実状態を確認してください。`;
    }
    return "送信結果と家電の実状態を確認してください。";
  };

  const handleCaptureIRState = async () => {
    const selectedState = irStates.find((state) => state.id === selectedIRStateId);
    if (!selectedRoomId || !selectedDevice || !selectedState) {
      setIRStateError("学習するIR Stateを選択してください");
      setIRStateMessage("");
      return;
    }
    const reason = stateOperationReason(irStateSchema ?? undefined, selectedState);
    if (reason) { setIRStateError(reason); setIRStateMessage(""); return; }
    if (selectedState.format !== "raw_samples") {
      setIRStateError("raw_samples形式のIR Stateだけを学習できます");
      setIRStateMessage("");
      return;
    }
    const options = parseIRStateCaptureOptions();
    if (options.error || options.samples === undefined || options.maxAttempts === undefined) {
      setIRStateCaptureFieldErrors(
        options.field ? { [options.field]: options.error ?? "入力を確認してください" } : {}
      );
      setIRStateError("");
      setIRStateMessage("capture入力内容を確認してください");
      return;
    }
    if (
      isIRStateLearned(selectedState) &&
      !window.confirm(
        `IR State「${selectedState.name}」を再学習します。成功時は既存の学習結果を置き換えます。よろしいですか？`
      )
    ) {
      setIRStateMessage("IR State再学習をキャンセルしました");
      setIRStateError("");
      return;
    }

    try {
      setIRStateLoading(true);
      setIRStateError("");
      setIRStateCaptureFieldErrors({});
      setIRStateMessage(
        `IR State学習中: ${selectedState.name} / リモコンを受信機へ向けて対象状態のボタンを押してください`
      );
      const response = await captureIRState(
        selectedRoomId,
        selectedDevice.id,
        selectedState.id,
        { samples: options.samples, max_attempts: options.maxAttempts }
      );
      await loadIRStates(selectedRoomId, selectedDevice, response.state.id);
      setIRStateMessage(
        `IR State学習を完了しました: ${response.state.name} / valid captures=${response.valid_captures}`
      );
    } catch (error) {
      const captureDetails =
        error instanceof IRStateAPIError && isIRStateCaptureErrorResponse(error.payload)
          ? ` valid captures=${error.payload.valid_captures}/${error.payload.required_samples}`
          : "";
      const message = error instanceof Error ? error.message : "IR State学習に失敗しました";
      setIRStateError(`${message}.${captureDetails} ${getIRStateCaptureFailureGuidance(message)}${stateErrorIssues(error)}`);
      setIRStateMessage("");
    } finally {
      setIRStateLoading(false);
    }
  };

  const handleSendIRState = async () => {
    const selectedState = irStates.find((state) => state.id === selectedIRStateId);
    if (!selectedRoomId || !selectedDevice || !selectedState) {
      setIRStateError("送信するIR Stateを選択してください");
      setIRStateMessage("");
      return;
    }
    const reason = stateOperationReason(irStateSchema ?? undefined, selectedState);
    if (reason) { setIRStateError(reason); setIRStateMessage(""); return; }
    if (selectedState.format === "raw_samples" && !isIRStateLearned(selectedState)) {
      setIRStateError("未学習のraw_samples Stateは送信できません。先に学習してください");
      setIRStateMessage("");
      return;
    }
    if ((selectedState.format === "raw_samples" || selectedState.format === "raw") && (selectedState.carrier_khz ?? Number(irConfigCarrierKHz)) !== 38) {
      setIRStateError("RAW送信の実効carrierは38kHzである必要があります");
      setIRStateMessage("");
      return;
    }
    const repeat = parseIRStateSendRepeat();
    if (repeat === null) {
      setIRStateSendRepeatError("repeatは1から5の範囲で指定してください");
      setIRStateError("");
      setIRStateMessage("送信入力内容を確認してください");
      return;
    }
    const executionMode = selectedState.format === "code" && irStateExecutionMode ? irStateExecutionMode : undefined;
    const stateExecutionOption = executionMode && irStateExecutionOptions?.protocol === selectedState.protocol
      ? irStateExecutionOptions?.execution_modes[executionMode]
      : undefined;
    if (selectedState.format === "code" && !stateExecutionOption?.available) {
      setIRStateError("利用可能なexecution modeを選択してください");
      setIRStateMessage("");
      return;
    }
    if (!window.confirm(`IR State「${selectedState.name}」を送信します。${stateSummary(irStateSchema ?? undefined, selectedState.state)}。execution mode: ${executionMode ?? "format既定経路"}。よろしいですか？`)) {
      setIRStateMessage("IR State送信をキャンセルしました");
      setIRStateError("");
      return;
    }

    try {
      setIRStateLoading(true);
      setIRStateError("");
      setIRStateSendRepeatError("");
      setIRStateMessage(`IR State送信中: ${selectedState.name}`);
      const response = await sendIRState(
        selectedRoomId,
        selectedDevice.id,
        selectedState.id,
        { ...(repeat === undefined ? {} : { repeat }), ...(executionMode ? { execution_mode: executionMode } : {}) }
      );
      setIRStateMessage(
        `IR State送信を完了しました: ${response.state_id} / mode=${response.execution_mode || "既定"} / delivery=${response.delivery_state} / repeat=${response.completed_repeats}/${response.requested_repeats}。実機の現在状態は取得しません。`
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : "IR State送信に失敗しました";
      const guidance = error instanceof IRSendAPIError ? getIRSendDeliveryGuidance(error) : getIRStateSendFailureGuidance(message);
      setIRStateError(`${message}。${guidance}${stateErrorIssues(error)}`);
      setIRStateMessage("");
    } finally {
      setIRStateLoading(false);
    }
  };

  const loadIRCommands = async (
    roomId: string,
    device?: Device,
    preferredCommandId = ""
  ) => {
    const requestVersion = ++irCommandRequestVersionRef.current;
    setIRCommandExecutionMode("");
    if (
      !roomId ||
      !device ||
      device.primary_adapter !== "ir" ||
      irConfigScope?.roomId !== roomId ||
      irConfigScope?.deviceId !== device.id ||
      irConfigScope?.remoteType !== "command"
    ) {
      setIRCommands([]);
      setIRCommandListLoaded(false);
      setSelectedIRCommandId("");
      syncIRCommandEditForm();
      setIRCommandMessage("");
      setIRCommandError("");
      setIRCommandLoading(false);
      return;
    }

    try {
      setIRCommandLoading(true);
      setIRCommandListLoaded(false);
      setIRCommandError("");
      setIRCommandMessage("IR Command一覧を取得中...");

      const nextCommands = await fetchIRCommands(roomId, device.id);
      if (requestVersion !== irCommandRequestVersionRef.current) return;
      setIRCommands(nextCommands);
      setIRCommandListLoaded(true);

      if (nextCommands.length === 0) {
        setSelectedIRCommandId("");
        syncIRCommandEditForm();
        setIRCommandMessage("");
        return;
      }

      const preferredCommandStillExists = nextCommands.some(
        (command) => command.id === preferredCommandId
      );
      const selectedCommandStillExists = nextCommands.some(
        (command) => command.id === selectedIRCommandId
      );
      const nextSelectedCommandId = preferredCommandStillExists
        ? preferredCommandId
        : selectedCommandStillExists
          ? selectedIRCommandId
          : nextCommands[0].id;

      setSelectedIRCommandId(nextSelectedCommandId);
      syncIRCommandEditForm(
        nextCommands.find((command) => command.id === nextSelectedCommandId)
      );
      setIRCommandMessage(`IR Commandを${nextCommands.length}件取得しました`);
    } catch (error) {
      if (requestVersion !== irCommandRequestVersionRef.current) return;
      setIRCommands([]);
      setIRCommandListLoaded(true);
      setSelectedIRCommandId("");
      syncIRCommandEditForm();
      setIRCommandError(
        error instanceof Error
          ? error.message
          : "IR Command一覧の取得に失敗しました"
      );
      setIRCommandMessage("");
    } finally {
      if (requestVersion === irCommandRequestVersionRef.current) {
        setIRCommandLoading(false);
      }
    }
  };

  const handleSelectIRCommand = (command: IRCommand) => {
    setSelectedIRCommandId(command.id);
    setIRCommandExecutionMode("");
    syncIRCommandEditForm(command);
    setIRCommandMessage(`選択中IR Command: ${command.name}`);
    setIRCommandError("");
  };

  const parseIRCommandRepeat = (value: string): number | undefined | null => {
    if (value.trim() === "") {
      return undefined;
    }

    const repeat = Number(value);
    if (!Number.isInteger(repeat) || repeat < 1 || repeat > 5) {
      return null;
    }

    return repeat;
  };

  const getIRCommandSendFailureGuidance = (message: string): string => {
    if (message.includes("まだ学習されていません")) {
      return "次の操作: raw_samples Commandを学習してから、送信を再試行してください。";
    }
    if (message.includes("repeat")) {
      return "次の操作: repeatを空欄にするか、1から5の整数に修正して再試行してください。";
    }
    if (message.includes("プリセット")) {
      return "次の操作: 許可済みのプリセットIR Commandを選択して保存し、再試行してください。";
    }
    if (message.includes("Controller daemon")) {
      return "手動確認: controller-daemonの起動状態、Pico serial接続、IR LED配線を確認してから再試行してください。";
    }
    if (message.includes("送信に対応していません")) {
      return "次の操作: raw_samplesまたはpreset_command形式のCommandを選択してください。";
    }

    return "次の操作: 設定と対象Deviceを確認して再試行してください。解消しない場合はbackendとcontroller-daemonのログを手動確認してください。";
  };

  const handleCreateIRCommand = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!selectedRoomId || !selectedDevice) {
      setIRCommandError("IR Commandを作成するDeviceを選択してください");
      setIRCommandMessage("");
      return;
    }

    const id = irCommandCreateId.trim();
    const name = irCommandCreateName.trim();
    const repeat = parseIRCommandRepeat(irCommandCreateRepeat);
    if (!id || !name) {
      setIRCommandError("IR Command IDと表示名を入力してください");
      setIRCommandMessage("");
      return;
    }
    if (repeat === null) {
      setIRCommandError("repeatは1から5の範囲で指定してください");
      setIRCommandMessage("");
      return;
    }
    const operation = operationFieldsForRequest(
      irCommandCreateFormat,
      irCommandCreateRawTimings,
      irCommandCreateProtocol,
      irCommandCreateCode,
      irCommandCreatePreset
    );
    if (operation.error) {
      setIRCommandError(operation.error);
      setIRCommandMessage("");
      return;
    }

    const command: IRCommandWriteRequest = {
      id,
      name,
      format: irCommandCreateFormat,
      ...operation.fields,
    };
    if (repeat !== undefined) {
      command.repeat = repeat;
    }

    try {
      setIRCommandLoading(true);
      setIRCommandError("");
      setIRCommandMessage(`IR Command作成中: ${name}`);

      const createdCommand = await createIRCommand(
        selectedRoomId,
        selectedDevice.id,
        command
      );

      setIRCommandCreateId("");
      setIRCommandCreateName("");
      setIRCommandCreateFormat("raw_samples");
      setIRCommandCreatePreset("");
      setIRCommandCreateRawTimings("");
      setIRCommandCreateProtocol(PANASONIC_40);
      setIRCommandCreateCode("");
      setIRCommandCreateRepeat("");
      await loadIRCommands(selectedRoomId, selectedDevice, createdCommand.id);
      setIRCommandMessage(`IR Commandを作成しました: ${createdCommand.name}`);
    } catch (error) {
      setIRCommandError(
        error instanceof Error ? error.message : "IR Command作成に失敗しました"
      );
      setIRCommandMessage("");
    } finally {
      setIRCommandLoading(false);
    }
  };

  const handleUpdateIRCommand = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const selectedCommand = irCommands.find(
      (command) => command.id === selectedIRCommandId
    );
    if (!selectedRoomId || !selectedDevice || !selectedCommand) {
      setIRCommandError("編集するIR Commandを選択してください");
      setIRCommandMessage("");
      return;
    }

    const name = irCommandEditName.trim();
    const repeat = parseIRCommandRepeat(irCommandEditRepeat);
    if (!name) {
      setIRCommandError("IR Commandの表示名を入力してください");
      setIRCommandMessage("");
      return;
    }
    if (repeat === null) {
      setIRCommandError("repeatは1から5の範囲で指定してください");
      setIRCommandMessage("");
      return;
    }
    const operation = operationFieldsForRequest(
      irCommandEditFormat,
      irCommandEditRawTimings,
      irCommandEditProtocol,
      irCommandEditCode,
      irCommandEditPreset
    );
    if (operation.error) {
      setIRCommandError(operation.error);
      setIRCommandMessage("");
      return;
    }

    const command: IRCommandWriteRequest = {
      name,
      format: irCommandEditFormat,
      ...operation.fields,
    };
    if (repeat !== undefined) {
      command.repeat = repeat;
    }

    try {
      setIRCommandLoading(true);
      setIRCommandError("");
      setIRCommandMessage(`IR Command更新中: ${selectedCommand.name}`);

      const updatedCommand = await updateIRCommand(
        selectedRoomId,
        selectedDevice.id,
        selectedCommand.id,
        command
      );

      await loadIRCommands(selectedRoomId, selectedDevice, updatedCommand.id);
      setIRCommandMessage(`IR Commandを更新しました: ${updatedCommand.name}`);
    } catch (error) {
      setIRCommandError(
        error instanceof Error ? error.message : "IR Command更新に失敗しました"
      );
      setIRCommandMessage("");
    } finally {
      setIRCommandLoading(false);
    }
  };

  const handleDeleteIRCommand = async () => {
    const selectedCommand = irCommands.find(
      (command) => command.id === selectedIRCommandId
    );
    if (!selectedRoomId || !selectedDevice || !selectedCommand) {
      setIRCommandError("削除するIR Commandを選択してください");
      setIRCommandMessage("");
      return;
    }

    if (!window.confirm(`IR Command「${selectedCommand.name}」を削除します。よろしいですか？`)) {
      setIRCommandMessage("IR Command削除をキャンセルしました");
      setIRCommandError("");
      return;
    }

    try {
      setIRCommandLoading(true);
      setIRCommandError("");
      setIRCommandMessage(`IR Command削除中: ${selectedCommand.name}`);

      const result = await deleteIRCommand(selectedRoomId, selectedDevice.id, selectedCommand.id);
      await loadIRCommands(selectedRoomId, selectedDevice);
      setIRCommandMessage(`IR CommandをTrashへ移動しました: ${selectedCommand.name} (Trash ${result.trash_entry_id} / operation ${result.operation_id} / ${result.status})`);
    } catch (error) {
      setIRCommandError(
        error instanceof Error ? error.message : "IR Command削除に失敗しました"
      );
      setIRCommandMessage("");
    } finally {
      setIRCommandLoading(false);
    }
  };

  const handleCaptureIRCommand = async () => {
    const selectedCommand = irCommands.find(
      (command) => command.id === selectedIRCommandId
    );
    if (!selectedRoomId || !selectedDevice || !selectedCommand) {
      setIRCommandError("学習するIR Commandを選択してください");
      setIRCommandMessage("");
      return;
    }
    if (selectedCommand.format !== "raw_samples") {
      setIRCommandError("raw_samples形式のIR Commandだけを学習できます");
      setIRCommandMessage("");
      return;
    }

    const samples = Number(irCommandCaptureSamples);
    const maxAttempts = Number(irCommandCaptureMaxAttempts);
    if (!Number.isInteger(samples) || samples < 1 || samples > 7) {
      setIRCommandError("samplesは1から7の範囲で指定してください");
      setIRCommandMessage("");
      return;
    }
    if (
      !Number.isInteger(maxAttempts) ||
      maxAttempts < samples ||
      maxAttempts > 20
    ) {
      setIRCommandError("max_attemptsはsamples以上20以下で指定してください");
      setIRCommandMessage("");
      return;
    }

    const isLearned =
      (selectedCommand.captures?.length ?? 0) > 0 ||
      (selectedCommand.timings_us?.length ?? 0) > 0 ||
      (selectedCommand.count ?? 0) > 0;
    if (
      isLearned &&
      !window.confirm(
        `IR Command「${selectedCommand.name}」を再学習します。現在の学習結果を更新します。よろしいですか？`
      )
    ) {
      setIRCommandMessage("IR Command再学習をキャンセルしました");
      setIRCommandError("");
      return;
    }

    try {
      setIRCommandLoading(true);
      setIRCommandError("");
      setIRCommandMessage(
        `学習待機中: リモコンのボタンを${samples}回押してください（最大${maxAttempts}回試行）`
      );

      const response = await captureIRCommand(
        selectedRoomId,
        selectedDevice.id,
        selectedCommand.id,
        { samples, max_attempts: maxAttempts }
      );

      await loadIRCommands(selectedRoomId, selectedDevice, response.command.id);
      const commandDetail = await fetchIRCommand(
        selectedRoomId,
        selectedDevice.id,
        response.command.id
      );
      setIRCommands((commands) =>
        commands.map((command) =>
          command.id === commandDetail.id ? commandDetail : command
        )
      );
      syncIRCommandEditForm(commandDetail);
      setIRCommandMessage(
        `学習完了: ${commandDetail.name} / valid captures=${response.valid_captures} / count=${commandDetail.count ?? 0}`
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "IR Command学習に失敗しました";
      setIRCommandError(`${message}。既存の学習結果は維持されています。`);
      setIRCommandMessage("");
    } finally {
      setIRCommandLoading(false);
    }
  };

  const handleSendIRCommand = async () => {
    const selectedCommand = irCommands.find(
      (command) => command.id === selectedIRCommandId
    );
    if (!selectedRoomId || !selectedDevice || !selectedCommand) {
      setIRCommandError("送信するIR Commandを選択してください");
      setIRCommandMessage("");
      return;
    }
    if (selectedCommand.format === "raw_samples" && !(
      (selectedCommand.captures?.length ?? 0) > 0 ||
      (selectedCommand.timings_us?.length ?? 0) > 0 ||
      (selectedCommand.count ?? 0) > 0
    )) {
      setIRCommandError("未学習のraw_samples Commandは送信できません");
      setIRCommandMessage("");
      return;
    }
    if ((selectedCommand.format === "raw_samples" || selectedCommand.format === "raw") && (selectedCommand.carrier_khz ?? Number(irConfigCarrierKHz)) !== 38) {
      setIRCommandError("RAW送信の実効carrierは38kHzである必要があります");
      setIRCommandMessage("");
      return;
    }

    const repeat = parseIRCommandRepeat(irCommandSendRepeat);
    if (repeat === null) {
      setIRCommandError("repeatは空欄または1から5の範囲で指定してください");
      setIRCommandMessage("");
      return;
    }

    const executionMode = selectedCommand.format === "code" && irCommandExecutionMode ? irCommandExecutionMode : undefined;
    const commandExecutionOption = executionMode && irCommandExecutionOptions?.protocol === selectedCommand.protocol
      ? irCommandExecutionOptions?.execution_modes[executionMode]
      : undefined;
    if (selectedCommand.format === "code" && !commandExecutionOption?.available) {
      setIRCommandError("利用可能なexecution modeを選択してください");
      setIRCommandMessage("");
      return;
    }
    const formatLabel = selectedCommand.format;
    const repeatLabel = repeat === undefined ? "標準値" : String(repeat);
    if (
      !window.confirm(
        `IR Command「${selectedCommand.name}」を送信します。形式: ${formatLabel}、execution mode: ${executionMode ?? "format既定経路"}、repeat: ${repeatLabel}。よろしいですか？`
      )
    ) {
      setIRCommandMessage("IR Command送信をキャンセルしました");
      setIRCommandError("");
      return;
    }

    try {
      setIRCommandLoading(true);
      setIRCommandError("");
      setIRCommandMessage(`送信中: ${selectedCommand.name}`);

      const request = { ...(repeat === undefined ? {} : { repeat }), ...(executionMode ? { execution_mode: executionMode } : {}) };
      const response = await sendIRCommand(
        selectedRoomId,
        selectedDevice.id,
        selectedCommand.id,
        request
      );

      setIRCommandMessage(
        `送信成功: ${selectedCommand.name} / format=${response.format} / mode=${response.execution_mode || "既定"} / delivery=${response.delivery_state} / repeat=${response.completed_repeats}/${response.requested_repeats}。家電の現在状態は取得していません。`
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "IR Command送信に失敗しました";
      const guidance = error instanceof IRSendAPIError ? getIRSendDeliveryGuidance(error) : getIRCommandSendFailureGuidance(message);
      setIRCommandError(`送信エラー: ${message}。${guidance}`);
      setIRCommandMessage("");
    } finally {
      setIRCommandLoading(false);
    }
  };

  const handleUpdateIRConfig = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!selectedRoomId || !selectedDevice) {
      setIRConfigError("IR Adapter設定を保存するDeviceを選択してください");
      setIRConfigMessage("");
      return;
    }

    const carrierKHz = Number(irConfigCarrierKHz);
    const defaultRepeat =
      irConfigDefaultRepeat.trim() === ""
        ? undefined
        : Number(irConfigDefaultRepeat);

    if (!Number.isFinite(carrierKHz) || carrierKHz <= 0) {
      setIRConfigError("carrier_khzは1以上の数値を入力してください");
      setIRConfigMessage("");
      return;
    }

    if (
      defaultRepeat !== undefined &&
      (!Number.isFinite(defaultRepeat) || defaultRepeat < 0)
    ) {
      setIRConfigError("default_repeatは0以上の数値を入力してください");
      setIRConfigMessage("");
      return;
    }

    const hasLoadedConfigForSelection =
      irConfigScope?.roomId === selectedRoomId &&
      irConfigScope?.deviceId === selectedDevice.id;
    if (
      hasLoadedConfigForSelection &&
      irConfigScope?.remoteType !== irConfigRemoteType &&
      !window.confirm(
        "IRリモコン種別を変更すると、既存のIR Command / IR Stateの利用方法に影響する可能性があります。変更を保存しますか？"
      )
    ) {
      setIRConfigMessage("IRリモコン種別の変更をキャンセルしました");
      setIRConfigError("");
      return;
    }

    const config: IRConfig = {
      ir_remote_type: irConfigRemoteType,
      carrier_khz: carrierKHz,
    };

    if (defaultRepeat !== undefined) {
      config.default_repeat = defaultRepeat;
    }

    try {
      setIRConfigLoading(true);
      setIRConfigError("");
      setIRConfigMessage("IR Adapter設定を保存中...");

      const savedConfig = await updateIRConfig(
        selectedRoomId,
        selectedDevice.id,
        config
      );

      syncIRConfigForm(savedConfig);
      setIRConfigScope({
        roomId: selectedRoomId,
        deviceId: selectedDevice.id,
        remoteType: savedConfig.ir_remote_type,
      });
      setIRConfigMessage("IR Adapter設定を保存しました");
    } catch (error) {
      setIRConfigError(
        error instanceof Error ? error.message : "IR Adapter設定の保存に失敗しました"
      );
      setIRConfigMessage("");
    } finally {
      setIRConfigLoading(false);
    }
  };

  const currentIRRemoteType =
    irConfigScope?.roomId === selectedRoomId &&
    irConfigScope?.deviceId === selectedDevice?.id
      ? irConfigScope.remoteType
      : "command";

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadIRConfig(selectedRoomId, selectedDevice);
    }, 0);

    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRoomId, selectedDeviceId, selectedDevice?.primary_adapter]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadIRCommands(selectedRoomId, selectedDevice);
    }, 0);

    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    selectedRoomId,
    selectedDeviceId,
    selectedDevice?.primary_adapter,
    irConfigScope?.roomId,
    irConfigScope?.deviceId,
    irConfigScope?.remoteType,
  ]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadIRStateSchema(selectedRoomId, selectedDevice);
    }, 0);

    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    selectedRoomId,
    selectedDeviceId,
    selectedDevice?.primary_adapter,
    irConfigScope?.roomId,
    irConfigScope?.deviceId,
    irConfigScope?.remoteType,
  ]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadIRStates(selectedRoomId, selectedDevice);
    }, 0);

    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    selectedRoomId,
    selectedDeviceId,
    selectedDevice?.primary_adapter,
    irConfigScope?.roomId,
    irConfigScope?.deviceId,
    irConfigScope?.remoteType,
  ]);

  const irAdapterConfigProps = {
    selectedRoom,
    selectedDevice,
    irConfigRemoteType,
    irConfigCarrierKHz,
    irConfigDefaultRepeat,
    irConfigMessage,
    irConfigError,
    irConfigLoading,
    configReady: Boolean(
      irConfigScope?.roomId === selectedRoomId &&
      irConfigScope?.deviceId === selectedDevice?.id
    ),
    onRefreshIRConfig: () => loadIRConfig(selectedRoomId, selectedDevice),
    onUpdateIRConfig: handleUpdateIRConfig,
    onIRConfigRemoteTypeChange: setIRConfigRemoteType,
    onIRConfigCarrierKHzChange: setIRConfigCarrierKHz,
    onIRConfigDefaultRepeatChange: setIRConfigDefaultRepeat,
  };

  const irStateSchemaProps = {
    selectedRoom,
    selectedDevice,
    irRemoteType: currentIRRemoteType,
    fields: irStateSchemaFields,
    fieldErrors: irStateSchemaFieldErrors,
    schemaExists: irStateSchemaExists,
    existingStateIDs: irStateSchemaExistingStateIDs,
    invalidStateIDs: irStateSchemaInvalidStateIDs,
    message: irStateSchemaMessage,
    error: irStateSchemaError,
    loading: irStateSchemaLoading,
    loaded: irStateSchemaLoaded,
    templates: irStateSchemaTemplates,
    selectedTemplateId: irStateSchemaTemplateId,
    onRefresh: () => loadIRStateSchema(selectedRoomId, selectedDevice),
    onSubmit: handleUpdateIRStateSchema,
    onTemplateChange: setIRStateSchemaTemplateId,
    onApplyTemplate: handleApplyIRStateSchemaTemplate,
    onAddField: handleAddIRStateSchemaField,
    onUpdateField: handleUpdateIRStateSchemaField,
    onRemoveField: handleRemoveIRStateSchemaField,
  };

  const irStateProps = {
    selectedRoom,
    selectedDevice,
    irRemoteType: currentIRRemoteType,
    schema: irStateSchema ?? undefined,
    states: irStates,
    selectedStateId: selectedIRStateId,
    message: irStateMessage,
    error: irStateError,
    loading: irStateLoading || !irStateListLoaded,
    listLoaded: irStateListLoaded,
    createId: irStateCreateId,
    createName: irStateCreateName,
    createValues: irStateCreateValues,
    createFormat: irStateCreateFormat,
    createRawTimings: irStateCreateRawTimings,
    createProtocol: irStateCreateProtocol,
    createCode: irStateCreateCode,
    createPreset: irStateCreatePreset,
    editName: irStateEditName,
    editValues: irStateEditValues,
    editFormat: irStateEditFormat,
    editRawTimings: irStateEditRawTimings,
    editProtocol: irStateEditProtocol,
    editCode: irStateEditCode,
    editPreset: irStateEditPreset,
    fieldErrors: irStateFieldErrors,
    captureSamples: irStateCaptureSamples,
    captureMaxAttempts: irStateCaptureMaxAttempts,
    sendRepeat: irStateSendRepeat,
    captureFieldErrors: irStateCaptureFieldErrors,
    sendRepeatError: irStateSendRepeatError,
    executionMode: irStateExecutionMode,
    executionOptions: irStateExecutionOptions,
    executionOptionsError: irStateExecutionOptionsError,
    carrierKHz: Number(irConfigCarrierKHz),
    onRefresh: () => loadIRStates(selectedRoomId, selectedDevice),
    onSelectState: handleSelectIRState,
    onCreate: handleCreateIRState,
    onUpdate: handleUpdateIRState,
    onDelete: handleDeleteIRState,
    onCapture: handleCaptureIRState,
    onSend: handleSendIRState,
    onCreateIdChange: setIRStateCreateId,
    onCreateNameChange: setIRStateCreateName,
    onCreateFormatChange: setIRStateCreateFormat,
    onCreateRawTimingsChange: setIRStateCreateRawTimings,
    onCreateProtocolChange: setIRStateCreateProtocol,
    onCreateCodeChange: setIRStateCreateCode,
    onCreatePresetChange: setIRStateCreatePreset,
    onCreateValueChange: (key: string, value: string | undefined) => {
        setIRStateCreateValues((values) => { const next = { ...values }; if (value === undefined) delete next[key]; else next[key] = value; return next; });
        setIRStateFieldErrors((errors) => {
          const next = { ...errors };
          delete next[key];
          return next;
        });
      },
    onEditNameChange: setIRStateEditName,
    onEditFormatChange: setIRStateEditFormat,
    onEditRawTimingsChange: setIRStateEditRawTimings,
    onEditProtocolChange: setIRStateEditProtocol,
    onEditCodeChange: setIRStateEditCode,
    onEditPresetChange: setIRStateEditPreset,
    onCaptureSamplesChange: (value: string) => {
        setIRStateCaptureSamples(value);
        setIRStateCaptureFieldErrors((errors) => {
          const next = { ...errors };
          delete next.samples;
          return next;
        });
      },
    onCaptureMaxAttemptsChange: (value: string) => {
        setIRStateCaptureMaxAttempts(value);
        setIRStateCaptureFieldErrors((errors) => {
          const next = { ...errors };
          delete next.maxAttempts;
          return next;
        });
      },
    onSendRepeatChange: (value: string) => {
        setIRStateSendRepeat(value);
        setIRStateSendRepeatError("");
      },
    onExecutionModeChange: setIRStateExecutionMode,
    onEditValueChange: (key: string, value: string | undefined) => {
        setIRStateEditValues((values) => { const next = { ...values }; if (value === undefined) delete next[key]; else next[key] = value; return next; });
        setIRStateFieldErrors((errors) => {
          const next = { ...errors };
          delete next[key];
          return next;
        });
      },
  };

  const irCommandProps = {
    selectedRoom,
    selectedDevice,
    irConfigReady: Boolean(
      irConfigScope?.roomId === selectedRoomId &&
      irConfigScope?.deviceId === selectedDevice?.id
    ),
    irConfigError,
    irRemoteType: currentIRRemoteType,
    irCommands,
    selectedIRCommandId,
    irCommandMessage,
    irCommandError,
    irCommandLoading: irCommandLoading || !irCommandListLoaded,
    irCommandListLoaded,
    irCommandCreateId,
    irCommandCreateName,
    irCommandCreateFormat,
    irCommandCreatePreset,
    irCommandCreateRawTimings,
    irCommandCreateProtocol,
    irCommandCreateCode,
    irCommandCreateRepeat,
    irCommandEditName,
    irCommandEditFormat,
    irCommandEditPreset,
    irCommandEditRawTimings,
    irCommandEditProtocol,
    irCommandEditCode,
    irCommandEditRepeat,
    irCommandCaptureSamples,
    irCommandCaptureMaxAttempts,
    irCommandSendRepeat,
    irCommandExecutionMode,
    irCommandExecutionOptions,
    irCommandExecutionOptionsError,
    carrierKHz: Number(irConfigCarrierKHz),
    onRefreshIRCommands: () => loadIRCommands(selectedRoomId, selectedDevice),
    onSelectIRCommand: handleSelectIRCommand,
    onCreateIRCommand: handleCreateIRCommand,
    onUpdateIRCommand: handleUpdateIRCommand,
    onDeleteIRCommand: handleDeleteIRCommand,
    onCaptureIRCommand: handleCaptureIRCommand,
    onSendIRCommand: handleSendIRCommand,
    onIRCommandCreateIdChange: setIRCommandCreateId,
    onIRCommandCreateNameChange: setIRCommandCreateName,
    onIRCommandCreateFormatChange: setIRCommandCreateFormat,
    onIRCommandCreatePresetChange: setIRCommandCreatePreset,
    onIRCommandCreateRawTimingsChange: setIRCommandCreateRawTimings,
    onIRCommandCreateProtocolChange: setIRCommandCreateProtocol,
    onIRCommandCreateCodeChange: setIRCommandCreateCode,
    onIRCommandCreateRepeatChange: setIRCommandCreateRepeat,
    onIRCommandEditNameChange: setIRCommandEditName,
    onIRCommandEditFormatChange: setIRCommandEditFormat,
    onIRCommandEditPresetChange: setIRCommandEditPreset,
    onIRCommandEditRawTimingsChange: setIRCommandEditRawTimings,
    onIRCommandEditProtocolChange: setIRCommandEditProtocol,
    onIRCommandEditCodeChange: setIRCommandEditCode,
    onIRCommandEditRepeatChange: setIRCommandEditRepeat,
    onIRCommandCaptureSamplesChange: setIRCommandCaptureSamples,
    onIRCommandCaptureMaxAttemptsChange: setIRCommandCaptureMaxAttempts,
    onIRCommandSendRepeatChange: setIRCommandSendRepeat,
    onIRCommandExecutionModeChange: setIRCommandExecutionMode,
  };

  return {
    irAdapterConfigProps,
    irStateSchemaProps,
    irStateProps,
    irCommandProps,
  };
}
