export type DataHealthIssue = {
  id: string;
  severity: "warning" | "error";
  path: string;
  hash: string;
  message: string;
  has_destructive_actions: boolean;
};

export type DataHealthResponse = {
  ok: true;
  issues: DataHealthIssue[];
};

export type MaintenanceOperationSummary = {
  operation_id: string;
  plan_id?: string;
  type?: string;
  target?: string;
  status: string;
  started_at: string;
  completed_at: string | null;
  rehearsal_backup_id?: string;
  apply_backup_id?: string;
};

export type MaintenanceOperationAction = {
  action_id: string;
  action?: string;
  status: string;
  classification?: string;
  reason?: string;
  error?: string;
};

export type MaintenanceOperationDetail = MaintenanceOperationSummary & {
  previous_status?: string;
  actions: MaintenanceOperationAction[];
  error?: string;
  available_next_actions: string[];
};

export type MaintenanceOperationsResponse = {
  operations: MaintenanceOperationSummary[];
};

export async function fetchMaintenanceOperations(): Promise<MaintenanceOperationsResponse> {
  const response = await fetch("/api/controller/maintenance/operations", { cache: "no-store" });
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<MaintenanceOperationsResponse>;
}

export async function fetchMaintenanceOperationDetail(operationID: string): Promise<MaintenanceOperationDetail> {
  const response = await fetch(
    `/api/controller/maintenance/operations/${encodeURIComponent(operationID)}`,
    { cache: "no-store" },
  );
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<MaintenanceOperationDetail>;
}

export async function fetchDataHealth(): Promise<DataHealthResponse> {
  const response = await fetch("/api/controller/maintenance/data-health", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }
  return response.json() as Promise<DataHealthResponse>;
}

export type TrashEntrySummary = {
  entry_id: string;
  status: string;
  target_type: string;
  original_id: string;
  original_relative_path: string;
  deleted_at: string | null;
  eligible_after: string | null;
  last_checked_at?: string;
  final_deleted_at?: string;
  final_delete_files_deleted?: number;
  final_delete_files_total?: number;
  reason: string;
  operation_type: string;
  operation_id: string;
  restore_available: boolean;
};

export type TrashInventory = {
  kind: string;
  file_count: number;
  total_size: number;
  sha256: string;
};

export type TrashListResponse = {
  entries: TrashEntrySummary[];
  total: number;
  offset: number;
  limit: number;
};

export type TrashDetailResponse = TrashEntrySummary & { inventory: TrashInventory };

export type TrashRestorePreview = {
  entry_id: string;
  original_id: string;
  original_relative_path: string;
  expected_inventory_hash: string;
  path_available: boolean;
  id_available: boolean;
  restore_available: boolean;
  reason?: string;
};

export type TrashRestoreRequest = {
  expected_inventory_hash: string;
  confirm: true;
};

export type TrashRestoreResult = {
  entry_id: string;
  original_id: string;
  original_relative_path: string;
  operation_id: string;
};

export class TrashRestoreAPIError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "TrashRestoreAPIError";
    this.status = status;
  }
}

export type TrashListOptions = {
  type?: string;
  offset?: number;
  limit?: number;
};

export async function fetchTrashEntries(options: TrashListOptions = {}): Promise<TrashListResponse> {
  const query = new URLSearchParams();
  if (options.type) query.set("type", options.type);
  query.set("offset", String(options.offset ?? 0));
  query.set("limit", String(options.limit ?? 25));
  const response = await fetch(`/api/controller/maintenance/trash?${query.toString()}`, { cache: "no-store" });
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<TrashListResponse>;
}

export async function fetchTrashDetail(entryID: string): Promise<TrashDetailResponse> {
  const response = await fetch(`/api/controller/maintenance/trash/${encodeURIComponent(entryID)}`, { cache: "no-store" });
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<TrashDetailResponse>;
}

export async function previewTrashRestore(entryID: string): Promise<TrashRestorePreview> {
  const response = await fetch(
    `/api/controller/maintenance/trash/${encodeURIComponent(entryID)}/restore/dry-run`,
    { method: "POST", cache: "no-store" },
  );
  if (!response.ok) throw new TrashRestoreAPIError(await readAPIError(response), response.status);
  return response.json() as Promise<TrashRestorePreview>;
}

export async function restoreTrashEntry(entryID: string, request: TrashRestoreRequest): Promise<TrashRestoreResult> {
  const response = await fetch(
    `/api/controller/maintenance/trash/${encodeURIComponent(entryID)}/restore`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
      cache: "no-store",
    },
  );
  if (!response.ok) throw new TrashRestoreAPIError(await readAPIError(response), response.status);
  return response.json() as Promise<TrashRestoreResult>;
}

export type MigrationDryRunDecision = {
  room_id: string;
  device_id: string;
  target_id: string;
  state?: Record<string, unknown>;
  approve_raw_median?: boolean;
  pico_command?: string;
};

export type MigrationDryRunMapping = {
  action_id: string;
  source_path: string;
  target_kind: "command" | "state";
  room_id: string;
  device_id: string;
  target_id: string;
  decision: MigrationDryRunDecision;
  room?: Room;
  device?: Device;
  ir_config?: IRConfig;
  state_schema?: IRStateSchema;
};

export type MigrationDryRunPathSnapshot = {
  path: string;
  exists: boolean;
  kind?: string;
  size: number;
  sha256?: string;
};

export type MigrationDryRunPlanAction = {
  action_id: string;
  action: string;
  source: MigrationDryRunPathSnapshot;
  source_format: string;
  target_before: MigrationDryRunPathSnapshot;
  planned_writes: {
    path: string;
    kind: string;
    role: string;
    before: MigrationDryRunPathSnapshot;
    expected_after_state: string;
    content?: Record<string, unknown>;
  }[];
  target_format: string;
  transform: string;
  expected_after_state: string;
  decision?: MigrationDryRunDecision;
};

export type MigrationDryRunPlan = {
  version: number;
  plan_id: string;
  operation_type: string;
  payload_schema_id: string;
  payload: { actions: MigrationDryRunPlanAction[] };
  created_at: string;
  ready_to_apply: boolean;
  backup_requirement: string;
  actions: {
    action_id: string;
    action: string;
    selectable: boolean;
    destructive: boolean;
    backup_sources?: string[];
    backup_inventory_hash?: string;
    backup_file_count?: number;
    backup_total_size?: number;
  }[];
  plan_digest?: string;
};

export type MigrationDryRunResponse = {
  ok: true;
  plan: MigrationDryRunPlan;
};

export class MigrationDryRunAPIError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "MigrationDryRunAPIError";
    this.status = status;
  }
}

export async function createMigrationDryRun(
  mappings: MigrationDryRunMapping[],
): Promise<MigrationDryRunResponse> {
  const response = await fetch("/api/controller/maintenance/migration/dry-run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mappings }),
    cache: "no-store",
  });
  if (!response.ok) {
    throw new MigrationDryRunAPIError(
      await readAPIError(response),
      response.status,
    );
  }
  return response.json() as Promise<MigrationDryRunResponse>;
}

export type BackupCreateResponse = {
  backup_id: string;
  role: string;
  state: string;
  plan_id: string;
  action_ids: string[];
  file_count: number;
  total_size: number;
  inventory_hash: string;
};

export type BackupDetailResponse = BackupCreateResponse & {
  created_at: string;
  sources: string[];
  files: { path: string; size: number; sha256: string; json_status?: string }[];
};

export type BackupRestorePreview = {
  backup_id: string;
  role: string;
  state: string;
  target: string;
  required_bytes: number;
  available_bytes: number;
  source_inventory_hash: string;
  target_inventory_hash?: string;
  target_exists: boolean;
  can_rehearse: boolean;
  reason?: string;
};

export type BackupRestoreResult = {
  backup_id: string;
  role: string;
  previous_state: string;
  state: string;
  target: string;
  inventory_hash: string;
  file_count: number;
  total_size: number;
};

export async function createMigrationRehearsalBackup(
  plan_id: string,
  plan_digest: string,
  action_ids: string[],
): Promise<BackupCreateResponse> {
  const response = await fetch("/api/controller/maintenance/backups", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ plan_id, plan_digest, action_ids }),
    cache: "no-store",
  });
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<BackupCreateResponse>;
}

export async function fetchBackupDetail(backupID: string): Promise<BackupDetailResponse> {
  const response = await fetch(
    `/api/controller/maintenance/backups/${encodeURIComponent(backupID)}`,
    { cache: "no-store" },
  );
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<BackupDetailResponse>;
}

export async function previewBackupRehearsal(backupID: string): Promise<BackupRestorePreview> {
  const response = await fetch(
    `/api/controller/maintenance/backups/${encodeURIComponent(backupID)}/restore/dry-run`,
    { method: "POST", cache: "no-store" },
  );
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<BackupRestorePreview>;
}

export async function runBackupRehearsal(backupID: string): Promise<BackupRestoreResult> {
  const response = await fetch(
    `/api/controller/maintenance/backups/${encodeURIComponent(backupID)}/restore/rehearsal`,
    { method: "POST", cache: "no-store" },
  );
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<BackupRestoreResult>;
}

export type MigrationApplyRequest = {
  plan_id: string;
  plan_digest: string;
  action_ids: string[];
  rehearsal_backup_id: string;
  confirm: true;
};

export type MigrationApplyResult = {
  ok: true;
  operation_id: string;
  status: string;
  result?: string;
  rehearsal_backup_id: string;
  apply_backup_id: string;
  actions: { id: string; action: string; status: string; error?: string }[];
};

type MigrationApplyErrorResponse = {
  error?: string;
  operation_id?: string;
  status?: string;
  apply_backup_id?: string;
};

export class MigrationApplyAPIError extends Error {
  readonly status: number;
  readonly operationID?: string;
  readonly operationStatus?: string;
  readonly applyBackupID?: string;

  constructor(
    message: string,
    status: number,
    payload: MigrationApplyErrorResponse = {},
  ) {
    super(message);
    this.name = "MigrationApplyAPIError";
    this.status = status;
    this.operationID = payload.operation_id;
    this.operationStatus = payload.status;
    this.applyBackupID = payload.apply_backup_id;
  }
}

export async function applyMigration(
  request: MigrationApplyRequest,
): Promise<MigrationApplyResult> {
  const response = await fetch("/api/controller/maintenance/migration/apply", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
    cache: "no-store",
  });
  if (!response.ok) {
    const text = await response.text();
    let payload: MigrationApplyErrorResponse = {};
    try {
      const decoded: unknown = JSON.parse(text);
      if (decoded && typeof decoded === "object" && !Array.isArray(decoded)) {
        payload = decoded as MigrationApplyErrorResponse;
      }
    } catch {
      // Keep the raw server message when a proxy returns a non-JSON error.
    }
    const message = payload.error
      ? localizeAPIError(payload.error)
      : text || `API request failed with status ${response.status}`;
    throw new MigrationApplyAPIError(message, response.status, payload);
  }
  return response.json() as Promise<MigrationApplyResult>;
}

export type MigrationVerifyResult = {
  operation_id: string;
  plan_id: string;
  status: string;
  targets: {
    action_id: string;
    target_path: string;
    status: string;
    reason?: string;
    expected_sha256?: string;
    actual_sha256?: string;
  }[];
};

export async function verifyMigration(operationID: string): Promise<MigrationVerifyResult> {
  const response = await fetch("/api/controller/maintenance/migration/verify", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ operation_id: operationID }),
    cache: "no-store",
  });
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<MigrationVerifyResult>;
}

export type MigrationRollbackDependency = {
  path: string;
  kind: string;
  role: string;
  created_by_apply: boolean;
  shared_by_action_ids: string[];
};

export type MigrationRollbackTarget = {
  action_id: string;
  target_path: string;
  apply_sha256: string;
  current_exists: boolean;
  current_kind?: string;
  current_sha256?: string;
  eligible: boolean;
  reason: string;
  expected_after_state: string;
  dependencies: MigrationRollbackDependency[];
};

export type MigrationRollbackDryRunResult = {
  ok: true;
  plan: {
    version: number;
    plan_id: string;
    operation_type: string;
    payload_schema_id: string;
    payload: {
      migration_operation_id: string;
      migration_plan_id: string;
      migration_plan_digest: string;
      apply_backup_id: string;
      targets: MigrationRollbackTarget[];
    };
    created_at: string;
    ready_to_apply: boolean;
    backup_requirement: string;
    actions: {
      action_id: string;
      action: string;
      selectable: boolean;
      destructive: boolean;
      backup_sources?: string[];
      backup_inventory_hash?: string;
      backup_file_count?: number;
      backup_total_size?: number;
    }[];
    plan_digest?: string;
  };
  targets: MigrationRollbackTarget[];
};

export async function createMigrationRollbackDryRun(
  operationID: string,
): Promise<MigrationRollbackDryRunResult> {
  const response = await fetch("/api/controller/maintenance/migration/rollback/dry-run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ operation_id: operationID }),
    cache: "no-store",
  });
  if (!response.ok) throw new Error(await readAPIError(response));
  return response.json() as Promise<MigrationRollbackDryRunResult>;
}

export type MigrationRollbackApplyRequest = {
  plan_id: string;
  plan_digest: string;
  action_ids: string[];
  confirm: true;
};

export type MigrationRollbackApplyResult = {
  ok: true;
  operation_id: string;
  status: string;
  result?: string;
  apply_backup_id: string;
  actions: { action_id: string; status: string; trash_entry_id?: string }[];
};

type MigrationRollbackApplyErrorResponse = {
  error?: string;
  operation_id?: string;
  status?: string;
  apply_backup_id?: string;
};

export class MigrationRollbackApplyAPIError extends Error {
  readonly status: number;
  readonly operationID?: string;
  readonly operationStatus?: string;
  readonly applyBackupID?: string;

  constructor(message: string, status: number, payload: MigrationRollbackApplyErrorResponse = {}) {
    super(message);
    this.name = "MigrationRollbackApplyAPIError";
    this.status = status;
    this.operationID = payload.operation_id;
    this.operationStatus = payload.status;
    this.applyBackupID = payload.apply_backup_id;
  }
}

export async function applyMigrationRollback(
  request: MigrationRollbackApplyRequest,
): Promise<MigrationRollbackApplyResult> {
  const response = await fetch("/api/controller/maintenance/migration/rollback/apply", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
    cache: "no-store",
  });
  if (!response.ok) {
    const text = await response.text();
    let payload: MigrationRollbackApplyErrorResponse = {};
    try {
      const decoded: unknown = JSON.parse(text);
      if (decoded && typeof decoded === "object" && !Array.isArray(decoded)) {
        payload = decoded as MigrationRollbackApplyErrorResponse;
      }
    } catch {
      // Preserve the raw response text if a proxy returns a non-JSON error.
    }
    const message = payload.error
      ? localizeAPIError(payload.error)
      : text || `API request failed with status ${response.status}`;
    throw new MigrationRollbackApplyAPIError(message, response.status, payload);
  }
  return response.json() as Promise<MigrationRollbackApplyResult>;
}

export type CleanupIssueSelection = {
  issue_id: string;
  before_hash: string;
};

export type CleanupDryRunAction = {
  issue_id: string;
  action_id: string;
  action: "trash_file" | "trash_directory";
  destructive: boolean;
  affected_paths: string[];
  before_hash: string;
  expected_after_state: "moved_to_trash";
  backup_sources: string[];
  backup_inventory_hash: string;
  backup_file_count: number;
  backup_total_size: number;
};

export type CleanupDryRunResponse = {
  ok: true;
  plan_id: string;
  plan_digest: string;
  ready_to_apply: boolean;
  backup_requirement: "apply";
  backup_planned: boolean;
  actions: CleanupDryRunAction[];
};

export class CleanupDryRunAPIError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "CleanupDryRunAPIError";
    this.status = status;
  }
}

export async function createCleanupDryRun(
  issues: CleanupIssueSelection[],
): Promise<CleanupDryRunResponse> {
  const response = await fetch("/api/controller/maintenance/data-cleanup/dry-run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ issues }),
    cache: "no-store",
  });
  if (!response.ok) {
    throw new CleanupDryRunAPIError(await readAPIError(response), response.status);
  }
  return response.json() as Promise<CleanupDryRunResponse>;
}

export type CleanupApplyRequest = {
  plan_id: string;
  plan_digest: string;
  issues: CleanupIssueSelection[];
  action_ids: string[];
  confirm: true;
};

export type CleanupApplyResponse = {
  ok: true;
  operation_id: string;
  status: "verified";
  result: "verified";
  apply_backup_id: string;
  actions: { action_id: string; status: "verified"; trash_entry_id: string }[];
};

type CleanupApplyErrorResponse = {
  error?: string;
  operation_id?: string;
  status?: string;
  apply_backup_id?: string;
};

export class CleanupApplyAPIError extends Error {
  readonly status: number;
  readonly operationID?: string;
  readonly operationStatus?: string;
  readonly applyBackupID?: string;

  constructor(message: string, status: number, payload: CleanupApplyErrorResponse = {}) {
    super(message);
    this.name = "CleanupApplyAPIError";
    this.status = status;
    this.operationID = payload.operation_id;
    this.operationStatus = payload.status;
    this.applyBackupID = payload.apply_backup_id;
  }
}

export async function applyCleanup(request: CleanupApplyRequest): Promise<CleanupApplyResponse> {
  const response = await fetch("/api/controller/maintenance/data-cleanup/apply", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
    cache: "no-store",
  });
  if (!response.ok) {
    const text = await response.text();
    let payload: CleanupApplyErrorResponse = {};
    try {
      const decoded: unknown = JSON.parse(text);
      if (decoded && typeof decoded === "object" && !Array.isArray(decoded)) {
        payload = decoded as CleanupApplyErrorResponse;
      }
    } catch {
      // Preserve the raw response text if a proxy returns a non-JSON error.
    }
    const message = payload.error
      ? localizeAPIError(payload.error)
      : text || `API request failed with status ${response.status}`;
    throw new CleanupApplyAPIError(message, response.status, payload);
  }
  return response.json() as Promise<CleanupApplyResponse>;
}
export type Room = {
  id: string;
  name: string;
  sort_order?: number;
};

export type RoomDeleteResponse = {
  ok: boolean;
  room_id: string;
  trash_entry_id: string;
  operation_id: string;
  status: string;
  error?: string;
  detail?: string;
};

export type Device = {
  id: string;
  name: string;
  room_id: string;
  device_type: string;
  primary_adapter: string;
  capabilities?: string[];
};

export type DeviceDeleteResponse = {
  ok: boolean;
  room_id: string;
  device_id: string;
  trash_entry_id: string;
  operation_id: string;
  status: string;
  error?: string;
  detail?: string;
};

export type IRConfig = {
  ir_remote_type: "command" | "stateful";
  carrier_khz: number;
  default_repeat?: number;
};

export type IRCommandFormat =
  | "raw_samples"
  | "raw"
  | "code"
  | "preset_command";

export type IRExecutionMode = "backend_raw" | "native_code";
export type IRDeliveryState = "not_sent" | "partially_sent" | "unknown" | "sent";
export type IRVerificationStatus = "confirmed" | "unknown";

export type IRExecutionModeOption = {
  available: boolean;
  verification_status: IRVerificationStatus;
  reason: string;
};

export type IRExecutionOptionsResponse = {
  format: "code";
  protocol: string;
  execution_modes: {
    native_code: IRExecutionModeOption;
    backend_raw: IRExecutionModeOption;
  };
};

export type IRCapture = {
  attempt: number;
  ok: boolean;
  valid: boolean;
  count: number;
  timings_us?: number[];
  error?: string;
  captured_at: string;
};

export type IRCommand = {
  id: string;
  name: string;
  format: IRCommandFormat;
  carrier_khz?: number;
  count?: number;
  timings_us?: number[];
  captures?: IRCapture[];
  pico_command?: string;
  protocol?: string;
  code?: string;
  repeat?: number;
};

export type IRCommandWriteRequest = Partial<IRCommand>;

export type IRCommandCaptureRequest = {
  samples?: number;
  max_attempts?: number;
};

export type IRCommandCaptureResponse = {
  ok: boolean;
  command: IRCommand;
  required_samples: number;
  valid_captures: number;
};

export type IRCommandCaptureErrorResponse = {
  ok: false;
  error: "not enough valid captures";
  required_samples: number;
  valid_captures: number;
  count_distribution: Record<string, number>;
  captures: IRCapture[];
};

export type IRCommandSendRequest = {
  repeat?: number;
  execution_mode?: IRExecutionMode;
};

export type IRCommandSendResponse = {
  ok: boolean;
  room_id: string;
  device_id: string;
  command_id: string;
  format: IRCommandFormat;
  protocol: string;
  execution_mode: IRExecutionMode | "";
  delivery_state: IRDeliveryState;
  repeat: number;
  requested_repeats: number;
  completed_repeats: number;
  retryable_with: IRExecutionMode[];
};

export type IRCommandDeleteResponse = {
	ok: boolean;
	room_id: string;
	device_id: string;
	command_id: string;
	trash_entry_id: string;
	operation_id: string;
	status: string;
};

export type IRStateFieldType = "enum" | "number" | "boolean" | "string";

export type IRStatePresence = "required" | "optional" | "absent";
export type IRStateSelector = { field: string; in: (string | boolean)[] };
export type IRStateConstraints = {
  values?: string[];
  min?: number;
  max?: number;
  step?: number;
  allow_empty?: boolean;
  label?: string;
  description?: string;
  unit?: string;
};
export type IRStateFieldCase = IRStateConstraints & {
  when: { all: IRStateSelector[] };
  presence: IRStatePresence;
};
export type IRStateFieldSchema = IRStateConstraints & {
  type: IRStateFieldType;
  presence?: IRStatePresence;
  cases?: IRStateFieldCase[];
};
export type IRStateSchemaIssue = {
  field_id: string;
  code: string;
  message: string;
  case_index?: number;
  state_id?: string;
};

export type IRStateSchema = Record<string, IRStateFieldSchema>;

export type IRStateFormat = IRCommandFormat;

export type IRState = {
  id: string;
  name: string;
  state: Record<string, unknown>;
  format: IRStateFormat;
  carrier_khz?: number;
  count?: number;
  timings_us?: number[];
  captures?: IRCapture[];
  pico_command?: string;
  protocol?: string;
  code?: string;
  repeat?: number;
};

export type IRStateWriteRequest = Partial<IRState>;

export type IRStateCaptureRequest = {
  samples?: number;
  max_attempts?: number;
};

export type IRStateCaptureSuccessResponse = {
  ok: true;
  state: IRState;
  required_samples: number;
  valid_captures: number;
};

export type IRStateCaptureErrorResponse = {
  ok: false;
  error: "not enough valid captures";
  required_samples: number;
  valid_captures: number;
  count_distribution: Record<string, number>;
  captures: IRCapture[];
};

export type IRStateSendRequest = {
  repeat?: number;
  execution_mode?: IRExecutionMode;
};

export type IRStateSendResponse = {
  ok: true;
  room_id: string;
  device_id: string;
  state_id: string;
  format: IRStateFormat;
  protocol: string;
  execution_mode: IRExecutionMode | "";
  delivery_state: IRDeliveryState;
  repeat: number;
  requested_repeats: number;
  completed_repeats: number;
  retryable_with: IRExecutionMode[];
};

export type IRSendErrorResponse = {
  ok: false;
  error: string;
  detail: string;
  protocol: string;
  execution_mode: IRExecutionMode | "";
  delivery_state: IRDeliveryState;
  requested_repeats: number;
  completed_repeats: number;
  retryable_with: IRExecutionMode[];
  issues?: IRStateSchemaIssue[];
};

export type IRStateDeleteResponse = {
  ok: true;
  room_id: string;
  device_id: string;
  state_id: string;
  trash_entry_id: string;
  operation_id: string;
  status: string;
};

export type IRStateAPIErrorResponse = {
  ok: false;
  error: string;
  detail?: string;
  required_samples?: number;
  valid_captures?: number;
  count_distribution?: Record<string, number>;
  captures?: IRCapture[];
  invalid_state_ids?: string[];
  issues?: IRStateSchemaIssue[];
};

export function isIRStateCaptureErrorResponse(
  payload: IRStateAPIErrorResponse
): payload is IRStateCaptureErrorResponse {
  return (
    payload.error === "not enough valid captures" &&
    payload.required_samples !== undefined &&
    payload.valid_captures !== undefined &&
    payload.count_distribution !== undefined &&
    payload.captures !== undefined
  );
}

export class IRStateAPIError extends Error {
  readonly status: number;
  readonly payload: IRStateAPIErrorResponse;

  constructor(status: number, payload: IRStateAPIErrorResponse) {
    const localized = localizeAPIError(payload.error);
    super(payload.detail ? `${localized}: ${payload.detail}` : localized);
    this.name = "IRStateAPIError";
    this.status = status;
    this.payload = payload;
  }
}

export class IRSendAPIError extends Error {
  readonly status: number;
  readonly payload: IRSendErrorResponse;

  constructor(status: number, payload: IRSendErrorResponse) {
    const localized = localizeAPIError(payload.error);
    super(payload.detail ? `${localized}: ${payload.detail}` : localized);
    this.name = "IRSendAPIError";
    this.status = status;
    this.payload = payload;
  }
}

type APIErrorResponse = {
  ok?: boolean;
  error?: string;
  detail?: string;
};

function localizeAPIError(message: string): string {
  const messages: Record<string, string> = {
    "invalid room id": "Room IDが不正です",
    "room name is required": "Roomの表示名を入力してください",
    "room already exists": "同じRoom IDがすでに存在します",
    "room not found": "Roomが見つかりません",
    "room has devices": "RoomにDeviceが残っているため削除できません",
    "invalid device id": "Device IDが不正です",
    "device name is required": "Deviceの表示名を入力してください",
    "device already exists": "同じDevice IDがすでに存在します",
    "device not found": "Deviceが見つかりません",
    "invalid device type": "Device種別が不正です",
    "invalid primary adapter": "主操作方式が不正です",
    "room id mismatch": "Room IDが一致しません",
    "device id mismatch": "Device IDが一致しません",
    "ir config not found": "IR設定が見つかりません",
    "invalid ir remote type": "IRリモコン種別が不正です",
    "ir carrier_khz must be positive": "IR carrier_khzは1以上で入力してください",
    "ir default_repeat must not be negative": "IR default_repeatは0以上で入力してください",
    "device primary adapter is not ir": "Deviceの主操作方式がIRではありません",
    "failed to save ir config": "IR設定の保存に失敗しました",
    "failed to load ir config": "IR設定の取得に失敗しました",
    "ir remote type is not command": "IRリモコン種別がcommandではありません",
    "invalid ir command id": "IR Command IDが不正です",
    "ir command id mismatch": "IR Command IDが一致しません",
    "ir command name is required": "IR Commandの表示名を入力してください",
    "ir command already exists": "同じIR Command IDがすでに存在します",
    "ir command not found": "IR Commandが見つかりません",
    "invalid ir command format": "IR Command形式が不正です",
    "ir command learning data is read only": "IR Commandの学習データはcapture APIでのみ更新できます",
    "ir command format is not supported for capture": "このIR Command形式は学習に対応していません",
    "not enough valid captures": "有効なIR信号を必要数取得できませんでした",
    "ir command is not learned": "IR Commandはまだ学習されていません",
    "ir command format is not supported for send": "このIR Command形式は送信に対応していません",
    "invalid ir execution mode": "IR送信経路の指定が不正です",
    "ir execution mode is required": "IR送信経路を選択してください",
    "ir carrier is not supported for execution": "指定したcarrierではIR送信できません",
    "backend raw encoder is not available": "BackendのRAW変換を利用できません",
    "native code execution is not available": "Picoでのcode送信を利用できません",
    "ir code protocol is not supported": "指定したIR protocolには対応していません",
    "invalid ir code": "IR codeが不正です",
    "raw timing exceeds maximum": "RAW timingが上限を超えています",
    "raw duration exceeds maximum": "RAW信号の合計時間が上限を超えています",
    "too many raw timings": "RAW timingの件数が上限を超えています",
    "preset command is not allowed": "指定したプリセットIR Commandは許可されていません",
    "repeat must be between 1 and 5": "repeatは1から5の範囲で指定してください",
    "samples must be between 1 and 7": "samplesは1から7の範囲で指定してください",
    "max_attempts must be between samples and 20": "max_attemptsはsamples以上20以下で指定してください",
    "controller daemon error": "Controller daemonとの通信に失敗しました",
    "failed to load ir command": "IR Commandの取得に失敗しました",
    "failed to save ir command": "IR Commandの保存に失敗しました",
    "failed to delete ir command": "IR Commandの削除に失敗しました",
    "ir remote type is not stateful": "IRリモコン種別がstatefulではありません",
    "ir state schema not found": "IR State Schemaが見つかりません",
    "ir state schema conflicts with existing states": "既存のIR Stateが新しいSchemaと整合しません",
    "ir state does not match schema": "IR StateがSchemaに一致しません",
    "ir state not found": "IR Stateが見つかりません",
    "invalid ir state id": "IR State IDが不正です",
    "ir state id mismatch": "IR State IDが一致しません",
    "ir state name is required": "IR Stateの表示名を入力してください",
    "ir state already exists": "同じIR State IDがすでに存在します",
    "invalid ir state format": "IR State形式が不正です",
    "ir state learning data is read only": "IR Stateの学習データはcapture APIでのみ更新できます",
    "ir state format is not supported for capture": "このIR State形式は学習に対応していません",
    "ir state format is not supported for send": "このIR State形式は送信に対応していません",
    "ir state is not learned": "IR Stateはまだ学習されていません",
    "failed to infer representative signal": "代表となるIR信号を決定できませんでした",
    "invalid request body": "リクエスト内容が不正です",
    "failed to load ir state schema": "IR State Schemaの取得に失敗しました",
    "failed to save ir state schema": "IR State Schemaの保存に失敗しました",
    "failed to load ir state": "IR Stateの取得に失敗しました",
    "failed to save ir state": "IR Stateの保存に失敗しました",
    "failed to delete ir state": "IR Stateの削除に失敗しました",
  };

  return messages[message] ?? message;
}

async function readAPIError(response: Response): Promise<string> {
  const text = await response.text();

  if (!text) {
    return `API request failed with status ${response.status}`;
  }

  try {
    const data = JSON.parse(text) as APIErrorResponse;
    if (!data.error) {
      return text;
    }

    const message = localizeAPIError(data.error);
    return data.detail ? `${message}: ${data.detail}` : message;
  } catch {
    return text;
  }
}

async function readIRStateAPIError(response: Response): Promise<IRStateAPIError> {
  const text = await response.text();
  let payload: IRStateAPIErrorResponse;

  try {
    const data = JSON.parse(text) as Partial<IRStateAPIErrorResponse>;
    payload = {
      ok: false,
      error: data.error ?? `API request failed with status ${response.status}`,
      ...(data.detail ? { detail: data.detail } : {}),
      ...(data.required_samples !== undefined
        ? { required_samples: data.required_samples }
        : {}),
      ...(data.valid_captures !== undefined
        ? { valid_captures: data.valid_captures }
        : {}),
      ...(data.count_distribution
        ? { count_distribution: data.count_distribution }
        : {}),
      ...(data.captures ? { captures: data.captures } : {}),
      ...(data.issues ? { issues: data.issues } : {}),
      ...(data.invalid_state_ids
        ? { invalid_state_ids: data.invalid_state_ids }
        : {}),
    };
  } catch {
    payload = {
      ok: false,
      error: text || `API request failed with status ${response.status}`,
    };
  }

  return new IRStateAPIError(response.status, payload);
}

async function readIRSendAPIError(response: Response): Promise<IRSendAPIError> {
  const text = await response.text();
  let data: Partial<IRSendErrorResponse> = {};
  try {
    data = JSON.parse(text) as Partial<IRSendErrorResponse>;
  } catch {
    // The fallback below preserves the fixed client-side shape even if an
    // intermediary returns a non-JSON response.
  }
  return new IRSendAPIError(response.status, {
    ok: false,
    error:
      data.error ??
      (text || `API request failed with status ${response.status}`),
    detail: data.detail ?? "",
    protocol: data.protocol ?? "",
    execution_mode: data.execution_mode ?? "",
    delivery_state: data.delivery_state ?? "unknown",
    requested_repeats: data.requested_repeats ?? 0,
    completed_repeats: data.completed_repeats ?? 0,
    retryable_with: data.retryable_with ?? [],
    ...(data.issues ? { issues: data.issues } : {}),
  });
}

const roomsPath = "/api/controller/rooms";

function devicesPath(roomId: string): string {
  return `${roomsPath}/${encodeURIComponent(roomId)}/devices`;
}

function devicePath(roomId: string, deviceId: string): string {
  return `${devicesPath(roomId)}/${encodeURIComponent(deviceId)}`;
}

function irConfigPath(roomId: string, deviceId: string): string {
  return `${devicePath(roomId, deviceId)}/ir/config`;
}

function irCommandsPath(roomId: string, deviceId: string): string {
  return `${devicePath(roomId, deviceId)}/ir/commands`;
}

function irExecutionOptionsPath(roomId: string, deviceId: string): string {
  return `${devicePath(roomId, deviceId)}/ir/execution-options`;
}

function irCommandPath(
  roomId: string,
  deviceId: string,
  commandId: string
): string {
  return `${irCommandsPath(roomId, deviceId)}/${encodeURIComponent(commandId)}`;
}

function irStateSchemaPath(roomId: string, deviceId: string): string {
  return `${devicePath(roomId, deviceId)}/ir/state-schema`;
}

function irStatesPath(roomId: string, deviceId: string): string {
  return `${devicePath(roomId, deviceId)}/ir/states`;
}

function irStatePath(
  roomId: string,
  deviceId: string,
  stateId: string
): string {
  return `${irStatesPath(roomId, deviceId)}/${encodeURIComponent(stateId)}`;
}

export async function fetchRooms(): Promise<Room[]> {
  const response = await fetch(roomsPath);

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function createRoom(room: Room): Promise<Room> {
  const response = await fetch(roomsPath, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(room),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function updateRoom(roomId: string, room: Room): Promise<Room> {
  const response = await fetch(`${roomsPath}/${encodeURIComponent(roomId)}`, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(room),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function deleteRoom(roomId: string): Promise<RoomDeleteResponse> {
  const response = await fetch(`${roomsPath}/${encodeURIComponent(roomId)}`, {
    method: "DELETE",
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function fetchDevices(roomId: string): Promise<Device[]> {
  const response = await fetch(devicesPath(roomId));

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function createDevice(
  roomId: string,
  device: Device
): Promise<Device> {
  const response = await fetch(devicesPath(roomId), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(device),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function updateDevice(
  roomId: string,
  deviceId: string,
  device: Device
): Promise<Device> {
  const response = await fetch(devicePath(roomId, deviceId), {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(device),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function deleteDevice(
  roomId: string,
  deviceId: string
): Promise<DeviceDeleteResponse> {
  const response = await fetch(devicePath(roomId, deviceId), {
    method: "DELETE",
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function fetchIRConfig(
  roomId: string,
  deviceId: string
): Promise<IRConfig> {
  const response = await fetch(irConfigPath(roomId, deviceId));

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function updateIRConfig(
  roomId: string,
  deviceId: string,
  config: IRConfig
): Promise<IRConfig> {
  const response = await fetch(irConfigPath(roomId, deviceId), {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(config),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function fetchIRExecutionOptions(
  roomId: string,
  deviceId: string,
  protocol: string
): Promise<IRExecutionOptionsResponse> {
  const query = new URLSearchParams({ protocol });
  const response = await fetch(
    `${irExecutionOptionsPath(roomId, deviceId)}?${query.toString()}`
  );

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function fetchIRCommands(
  roomId: string,
  deviceId: string
): Promise<IRCommand[]> {
  const response = await fetch(irCommandsPath(roomId, deviceId));

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function fetchIRCommand(
  roomId: string,
  deviceId: string,
  commandId: string
): Promise<IRCommand> {
  const response = await fetch(irCommandPath(roomId, deviceId, commandId));

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function createIRCommand(
  roomId: string,
  deviceId: string,
  command: IRCommandWriteRequest
): Promise<IRCommand> {
  const response = await fetch(irCommandsPath(roomId, deviceId), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(command),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function updateIRCommand(
  roomId: string,
  deviceId: string,
  commandId: string,
  command: IRCommandWriteRequest
): Promise<IRCommand> {
  const response = await fetch(irCommandPath(roomId, deviceId, commandId), {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(command),
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function deleteIRCommand(
  roomId: string,
  deviceId: string,
  commandId: string
): Promise<IRCommandDeleteResponse> {
  const response = await fetch(irCommandPath(roomId, deviceId, commandId), {
    method: "DELETE",
  });

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function captureIRCommand(
  roomId: string,
  deviceId: string,
  commandId: string,
  request: IRCommandCaptureRequest = {}
): Promise<IRCommandCaptureResponse> {
  const response = await fetch(
    `${irCommandPath(roomId, deviceId, commandId)}/capture`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(request),
    }
  );

  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  return response.json();
}

export async function sendIRCommand(
  roomId: string,
  deviceId: string,
  commandId: string,
  request: IRCommandSendRequest = {}
): Promise<IRCommandSendResponse> {
  const response = await fetch(
    `${irCommandPath(roomId, deviceId, commandId)}/send`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(request),
    }
  );

  if (!response.ok) {
    throw await readIRSendAPIError(response);
  }

  return response.json();
}

export async function fetchIRStateSchema(
  roomId: string,
  deviceId: string
): Promise<IRStateSchema> {
  const response = await fetch(irStateSchemaPath(roomId, deviceId));

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function updateIRStateSchema(
  roomId: string,
  deviceId: string,
  schema: IRStateSchema
): Promise<IRStateSchema> {
  const response = await fetch(irStateSchemaPath(roomId, deviceId), {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(schema),
  });

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function fetchIRStates(
  roomId: string,
  deviceId: string
): Promise<IRState[]> {
  const response = await fetch(irStatesPath(roomId, deviceId));

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function fetchIRState(
  roomId: string,
  deviceId: string,
  stateId: string
): Promise<IRState> {
  const response = await fetch(irStatePath(roomId, deviceId, stateId));

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function createIRState(
  roomId: string,
  deviceId: string,
  state: IRStateWriteRequest
): Promise<IRState> {
  const response = await fetch(irStatesPath(roomId, deviceId), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(state),
  });

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function updateIRState(
  roomId: string,
  deviceId: string,
  stateId: string,
  state: IRStateWriteRequest
): Promise<IRState> {
  const response = await fetch(irStatePath(roomId, deviceId, stateId), {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(state),
  });

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function deleteIRState(
  roomId: string,
  deviceId: string,
  stateId: string
): Promise<IRStateDeleteResponse> {
  const response = await fetch(irStatePath(roomId, deviceId, stateId), {
    method: "DELETE",
  });

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function captureIRState(
  roomId: string,
  deviceId: string,
  stateId: string,
  request: IRStateCaptureRequest = {}
): Promise<IRStateCaptureSuccessResponse> {
  const response = await fetch(`${irStatePath(roomId, deviceId, stateId)}/capture`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw await readIRStateAPIError(response);
  }

  return response.json();
}

export async function sendIRState(
  roomId: string,
  deviceId: string,
  stateId: string,
  request: IRStateSendRequest = {}
): Promise<IRStateSendResponse> {
  const response = await fetch(`${irStatePath(roomId, deviceId, stateId)}/send`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw await readIRSendAPIError(response);
  }

  return response.json();
}
