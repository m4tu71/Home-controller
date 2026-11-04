# API

現行HTTP route・要求/応答・エラー・送信結果の正本です。保存属性は[DATA_SCHEMA](DATA_SCHEMA.md)、Stateful規則は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)、処理構成は[ARCHITECTURE](ARCHITECTURE.md)を参照してください。将来の共通Capability APIは[ROADMAP](../ROADMAP.md)で扱い、現行routeに含めません。

標準Backendは`127.0.0.1:3000`で待受し、Nginxは`/api/`のURIを保持してproxyします。APIに認証・HMAC署名plan・短命authorization token・apply-enable設定はありません。単一のローカル運用者向けで、認証なしでインターネットへ直接公開しないでください。

以下の例は契約説明用です。dataを書き換える要求、Backup/Trash/planの操作、IR capture/sendは運用者が対象を確認して実行します。文書の例示を実行許可として扱いません。例のID/hashは実環境の値に置き換えます。

## 共通のHTTP境界

この節のstatus・本文形式はBackendの応答契約です。Nginxを経由する場合はproxy側の本文上限（標準templateは`client_max_body_size 1m`）も適用されます。Nginxが生成する413や502等の応答は、BackendのJSONエラー形式を保証しません。

本文ありのPOST/PUTは`Content-Type: application/json`（charset等のparameter付き可）で単一JSON値を送ります。unknown field、余分なJSON値、malformed/空本文、型不正は400です。上限超過413をContent-Type/parseより先に判定し、非JSON Content-Typeは415です。1個のJSON値の後の空白は許可します。

| 対象 | 本文上限・要件 |
|---|---|
| Room/Device POST/PUT | 256 KiB |
| IR config/Schema PUT、Command/State POST/PUT/capture/send | 1 MiB |
| migration dry-run/apply、migration rollback dry-run/apply | 1 MiB |
| migration verify、cleanup、Backup作成、Trash restore、legacy-ir-cutover | 256 KiB |
| bedroom-preset dry-run、Backup restore dry-run/rehearsal、Trash restore dry-run | **0 byte**。空白や`{}`も400、256 KiB超過は413。Content-Type判定なし |

GET/DELETEへ本文0 byteの一律制約はありません。method違反はRoom/Device/IR/Maintenanceで405 JSONと`Allow`を返します。Healthのmethod違反はtext/plain 405、timeはmethod制限なしです。

通常エラーの基本形は`{"ok":false,"error":"message"}`。routeによりdetailや診断を付加します。成功を共通のok wrapperへ統一しません。未登録pathのmux 404と登録prefix内の404はbody形式が異なる場合があります。

| 状況 | Status・扱い |
|---|---|
| 不正ID/値/JSON/確認不足 | 400 |
| 不存在の対象/ID | 原則404。親不在や不正URLの扱いは各routeに従う |
| 作成ID重複、Schemaと既存State衝突、未学習送信、hash/digest/target変化、restore衝突 | 409 |
| 有効capture不足 | 422、学習詳細付き |
| Room/Device/IRの内部・controller失敗 | 500、sendでは下記の固定shape |
| Maintenance store/backup利用不可、破損record、readback失敗 | 原則503。作成済みoperationがあれば識別情報を付加 |

IR sendの入力/service失敗は[固定送信エラー](#送信結果と再操作)を使います。path/method dispatch段階の404/405は通常エラーであり、send専用shapeを仮定しません。

## Healthと時刻

| Method・path | 成功応答 | 失敗・例外 |
|---|---|---|
| GET /api/health | 200: ok/service/status | Backendの生存。全保守機能の利用可能性を保証しない |
| GET /api/health/controller | 200: ok/service/status/daemonOk/daemonRaw | daemonへLED_STATUSを要求。不通は503、ok=false/daemonOk=false/error |
| 任意method /api/time | 200: date/time/datetime | Backend hostのローカル日時。形式はYYYY-MM-DD、HH:mm:ss、YYYY-MM-DD HH:mm:ss |

controller healthは接続Picoとの応答確認であり、IRを送信しません。Pico未接続時の失敗とBackendの通常data APIの利用可能性を区別します。

## RoomとDevice

以下で`R=/api/controller/rooms`、`D=R/{roomId}/devices`とします。

| Method・path | 成功Status・body |
|---|---|
| GET R | 200、Room配列。sort_order昇順、同値はID順 |
| POST R | 201、作成したRoom |
| GET / PUT R/{roomId} | 200、Room |
| DELETE R/{roomId} | 200、削除結果とTrash/operation識別情報 |
| GET D | 200、Device配列 |
| POST D | 201、作成したDevice |
| GET / PUT D/{deviceId} | 200、Device |
| DELETE D/{deviceId} | 200、削除結果とTrash/operation識別情報 |

POST/PUTの属性と型は[Room](DATA_SCHEMA.md#room)と[Device](DATA_SCHEMA.md#device)の保存型に対応します。POSTはID必須、PUTの空IDはpathから補い、不一致は400です。Deviceのroom_idは空なら親pathから補い、不一致は400。名前はtrim後に必須です。未知device_type/primary_adapterは拒否します。Room/Device POSTの既存IDは409です。

作成例:

```json
{"id":"living","name":"リビング","sort_order":1}
```

```json
{"id":"remote_device","name":"IR機器","room_id":"living","device_type":"air_conditioner","primary_adapter":"ir","capabilities":["power","mode"]}
```

Deviceには親Roomのroom.jsonが必要で、親不在は`room not found`です。Roomにdevices/*/device.jsonが残るDELETEは409 `room has devices`。先に対象Deviceを確認します。

通常DELETEはRoomのroom.json、Deviceのdevice.jsonだけをTrashへ移します。配下directoryやadapter dataを再帰削除せず、通常APIは親metadata不在のdataを探索しません。Command/StateのDELETEも対象JSONをTrashへ移します。成功応答は`ok`、`room_id`、Deviceでは`device_id`、IR操作では`command_id`または`state_id`、`trash_entry_id`、`operation_id`、`status`を持ちます。保存Maintenance planは要求せず、単一対象のwrite-ahead delete operationを記録します。

対象競合/変化は409、Trash境界利用不可は503。operation作成後に結果が不確定なら500と生成済みoperation_id/trash_entry_id/statusを返し得ます。自動再送せず履歴と現物を確認してください。

## IR Adapter

`I=/api/controller/rooms/{roomId}/devices/{deviceId}/ir`とします。親Room/Deviceが存在し、Deviceのprimary_adapterがirである必要があります。

| Method・path | 成功Status・body |
|---|---|
| GET / PUT I/config | 200、IR config。PUTはupsertでも201にしない |
| GET I/execution-options?protocol=PANASONIC_40 | 200、codeの実行候補 |
| GET / PUT I/state-schema | 200、field IDをkeyとするSchema map |
| GET / POST I/commands | 200 Command配列 / 201 作成Command |
| GET / PUT / DELETE I/commands/{commandId} | 200 Command / Command / 削除結果 |
| POST I/commands/{commandId}/capture | 200、ok/command/required_samples/valid_captures |
| POST I/commands/{commandId}/send | 200、送信結果 |
| GET / POST I/states | 200 State配列 / 201 作成State |
| GET / PUT / DELETE I/states/{stateId} | 200 State / State / 削除結果 |
| POST I/states/{stateId}/capture | 200、ok/state/required_samples/valid_captures |
| POST I/states/{stateId}/send | 200、送信結果 |

configはir_remote_type、carrier_khz、default_repeatを扱います。属性/許容値は[IR config](DATA_SCHEMA.md#ir-config)を参照。config未作成は404 `ir config not found`。Command操作はcommand型、Schema/State操作はstateful型に限定し、種別不一致は400です。

### Commandの書込み

保存属性は[CommandとState](DATA_SCHEMA.md#commandとstate)を参照してください。POSTはid/name/format必須、PUTではpathのIDと保存値を基準に省略fieldを扱います。作成ID重複は409です。

raw_samplesのcaptures/timings_us/countは通常POST/PUTで学習情報を更新できません。作成時は空/0のみ許し、更新時は既存値を維持します。rawのtimings/count、codeのprotocol/code、preset_commandのpico_commandはformat別に検証します。

### State SchemaとState

statefulでないDeviceは400 `ir remote type is not stateful`。Schema/Stateのshapeは[State Schema](DATA_SCHEMA.md#state-schema)、評価規則は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)を参照してください。

Schema PUTは全既存Stateを照合します。不適合は409 `ir state schema conflicts with existing states`、`invalid_state_ids`と`issues`を返し、既存dataを変更しません。State読取り不能は500で更新を止めます。未知属性/null/不正JSON等のcodecエラーは400入力エラー、Schema validator不適合はissues付き400です。

State POST/PUTはSchema必須。未作成は404 `ir state schema not found`、State不適合は400 `ir state does not match schema`とissuesです。Schemaが許す`state: {}`は受理し、state:nullは400。Schemaがなくても既存Stateの一覧/詳細/削除は可能です。

登録済みStateの値・format・carrier・信号内容を変えるPUTは409 `registered ir state cannot be changed; create a new state`とcode `learned_state_change`で拒否します。name/repeatや同じ正規化済み内容の更新は可能です。raw_samplesの学習field直接更新は400 `ir state learning data is read only`。保存repeatは0または1〜5です。

capture/send前に保存Stateを再検証し、不適合なら409とissues、sendではnot_sent/completed_repeats=0で機器呼出し前に停止します。

issuesはfield_id/code/message、必要時case_index/state_idを持ちます。case_indexは0起点です。codeにはinvalid_schema、unknown_field、required、absent、type、enum、range、step、empty、learned_state_change等があります。

## IRの学習と送信

### Capture

Command/Stateのraw_samples専用です。他formatは400 `ir command format is not supported for capture`または`ir state format is not supported for capture`。

```json
{"samples":3,"max_attempts":9}
```

samples省略は3、範囲1〜7。max_attempts省略はsamples×3、範囲samples〜20です。従ってsamples=7ではmax_attemptsを20以下で明示しないと400になります。明示0を省略として扱いません。

成功200はok、commandまたはstateの全object、required_samples、valid_capturesを返し、学習情報を一括保存します。有効capture不足は422で次のfieldを返し、既存学習結果を保持します。

```json
{"ok":false,"error":"not enough valid captures","required_samples":3,"valid_captures":0,"count_distribution":{},"captures":[]}
```

capturesの保存型は[Capture](DATA_SCHEMA.md#capture)。配線/ノイズ等で短い波形が先に取得される場合と無入力timeoutを区別します。失敗後に既存学習済み信号を無条件で再学習しません。

### Sendの入力と実行方式

```json
{"repeat":1}
```

repeatは省略可、明示値1〜5。省略時は保存Command/State、IR configのdefault_repeat、1の順に解決します。保存0はfallback対象ですが、要求のrepeat:0は400です。

| format | 実行経路・条件 |
|---|---|
| raw_samples | 学習済み代表RAWをIR_SEND_RAWで送信。未学習は409 |
| raw | 保存RAWをIR_SEND_RAWで送信 |
| code + backend_raw | PANASONIC_40 encoderでRAWを生成しIR_SEND_RAWへ |
| code + native_code | 現在のPico capabilityでIR_SEND_CODE/対象protocolが確認できる場合のみ |
| preset_command | 保存allowlistのPico commandを送信 |

codeだけはexecution_mode必須です。省略は400 `ir execution mode is required`、他formatへの指定は400 `invalid ir execution mode`。

```json
{"repeat":1,"execution_mode":"backend_raw"}
```

RAW/raw_samples/backend_rawの実効carrierは38kHzのみ。非対応は409 `ir carrier is not supported for execution`、未学習は409 `ir command is not learned` / `ir state is not learned`、native不許可は409 `native code execution is not available`。いずれも送信前に停止します。

| 保存/送信validation | Status・固定error |
|---|---|
| 空/0/count不一致/整数範囲外のRAW | 400 invalid raw timings |
| RAW 1024件超 | 400 raw timings are too long |
| 単一timing 100000us超 | 400 raw timing exceeds maximum |
| RAW合計1000000us超 | 400 raw duration exceeds maximum |
| 未知protocol | 400 ir code protocol is not supported |
| codeの10桁hex/0x禁止等に違反 | 400 invalid ir code |
| allowlist外preset | 400 preset command is not allowed |
| repeat範囲外 | 400 repeat must be between 1 and 5 |
| controller通信失敗 | 500 controller daemon error |

属性・正規化・allowlistの定義は[DATA_SCHEMA](DATA_SCHEMA.md#format別の信号属性)。captureでは安全上限を超える候補をinvalidにし、必要数が足りなければ422にします。既存dataを自動変換しません。

### Execution options

protocolは必須でtrim後PANASONIC_40だけを受理します。失敗は400 `ir code protocol is not supported`。

```json
{
  "format":"code","protocol":"PANASONIC_40",
  "execution_modes":{
    "native_code":{"available":false,"verification_status":"unknown","reason":"PicoのPANASONIC_40対応を確認できません"},
    "backend_raw":{"available":true,"verification_status":"confirmed","reason":""}
  }
}
```

IR_CAPABILITIESの取得成功でcommandsにIR_SEND_CODE、code_protocolsに対象protocolがあればnativeはavailable=true/confirmed。応答成功で機能が欠ける場合はconfirmed/unavailable、取得失敗はunknown/unavailableです。raw_carrier_khzはnative許可条件に使いません。capabilityを永続化・再利用せず、optionsとsend双方が現在接続で再確認します。optionsは表示用snapshotで送信許可を予約しません。

## 送信結果と再操作

成功200の完全な例（Command。Stateはcommand_idの代わりにstate_id）:

```json
{
  "ok":true,"room_id":"living","device_id":"remote_device","command_id":"power",
  "format":"code","protocol":"PANASONIC_40","execution_mode":"backend_raw",
  "delivery_state":"sent","repeat":1,"requested_repeats":1,"completed_repeats":1,"retryable_with":[]
}
```

入力/service失敗は非該当値も含む固定shapeです。必要時issuesを追加します。retryable_withは空でも配列です。

```json
{
  "ok":false,"error":"invalid ir execution mode","detail":"","protocol":"",
  "execution_mode":"backend_raw","delivery_state":"not_sent",
  "requested_repeats":0,"completed_repeats":0,"retryable_with":[]
}
```

| delivery_state | 意味と判断 |
|---|---|
| not_sent | 送信前拒否と判定。retryable_withに候補があれば人が選ぶ |
| partially_sent | 先行repeat成功後、次を送信前拒否。進捗と家電の状態を確認する |
| unknown | 一部/全部が送信された可能性。write後timeout/read/parse失敗や未分類remote拒否等。再送前に家電を確認する |
| sent | 全要求repeatの送信処理が成功。家電の受信/動作/現在状態は別確認 |

先行repeat成功数はcompleted_repeatsで、unknown時も保持します。LIGHT_* presetのBackend 1 repeatはPicoへの1命令で、Pico内部は3 frameです。completed_repeatsは内部frame数ではありません。RAW/backend_rawは1回のexecutor送信で1 frame相当を扱います。

clientはconnect/write/read/response_parse/remote_rejectを区別しますが、これらの内部fieldをHTTP応答へ常に返す契約ではありません。機器からのok:falseを成功にせず、次の固定wire error_codeを持つremote_rejectだけを送信前拒否として分類します。

```text
unsupported_command
unsupported_protocol
invalid_code
invalid_raw_timing
invalid_raw_separator
empty_raw_timings
raw_timing_exceeds_maximum
raw_duration_exceeds_maximum
too_many_raw_timings
```

欠落・未知code・internal_error・write後の不確実な失敗を送信前拒否と推測しません。daemonはIR_SEND_CODEのprotocol/桁数/大文字hex/区切り数をserial write前に検証し、PicoのJSON応答は再生成せず中継します。serial timeout/read errorは明示的unsupportedとは別です。自動retry・別経路fallbackは行いません。

## Maintenance API一覧

`M=/api/controller/maintenance`。store利用不可時もroute自体は登録され、操作は503で停止します。

| Method・path | 成功Status・body |
|---|---|
| GET M/data-health | 200、ok/issues |
| POST M/data-cleanup/dry-run | 201、plan_id/plan_digest等とactionsをトップレベルで返す |
| POST M/data-cleanup/apply | 200、operationとaction結果 |
| POST M/migration/dry-run | 201、ok/plan（共通envelope） |
| POST M/bedroom-preset/dry-run | 201、ok/plan |
| POST M/migration/apply | 200、operation結果 |
| POST M/migration/verify | 200、statusとtarget検査結果。全適合とは限らない |
| POST M/migration/rollback/dry-run | 201、ok/plan（共通envelope）/targets |
| POST M/migration/rollback/apply | 200、operationとTrash結果 |
| POST M/legacy-ir-cutover/dry-run | 201、plan_id/plan_digest/source確認結果 |
| POST M/legacy-ir-cutover/apply | 200、operation/backup/Trash結果 |
| GET M/operations / M/operations/{operationId} | 200、operations配列のwrapper / 単一detail |
| POST M/backups | 201、backup_id/role/state/plan_id/action_ids/inventory概要 |
| GET M/backups/{backupId} | 200、backup詳細view |
| POST M/backups/{backupId}/restore/dry-run / restore/rehearsal | 200、隔離復元preview / 結果 |
| GET M/trash / M/trash/{entryId} | 200、pagination付き一覧 / 単一detail |
| POST M/trash/{entryId}/restore/dry-run / restore | 200、preview / 復元結果 |

`GET M/backups`の一覧は提供せず、405です。最終物理削除のHTTP routeは登録しません。

Data Health、cleanup、migration、bedroom-preset、legacy-ir-cutoverの専用routeはqueryを拒否します。その他は各handlerの契約に従い、一律拒否と解釈しません。

### Maintenance共通の適用境界

Backendは不変plan/digestを保存し、applyで同じplanとaction選択・confirmを要求します。保存source/path/type/hash/衝突/symlink/backup参照を再検証し、staleは原則409。plan作成は保守metadataを書きますがruntime dataを適用しません。

適用はoperation claim→別のpoint-in-time apply backup→対象再検証→write-ahead→mutation/readbackの順です。失敗後に同じplanを自動再送せず、生成済みoperationと現物を照合します。plan_id/action不正・確認不足は400、未知plan/actionは404、競合/変化は409、backup/store/破損record/readback等は503です。

apply失敗は基本JSONに加え、利用可能なoperation_id/status/apply_backup_idを返します。mutation前にoperationを捏造せず、内部absolute pathや生filesystem errorをMaintenance応答へ返しません。operation作成後の失敗は無変更を保証しません。

### Data HealthとCleanup

Data Health GETはread-onlyでCache-Control: no-store。空ならissues:[]。issueはid/severity/path/hash/message/has_destructive_actionsを持ち、pathはdata/表記の相対pathです。scan不能は503。

Schema不正/取得不能・State typed decode不正/Schema不適合はerror診断でhas_destructive_actions=false。unsupported_format診断も維持し、自動repairしません。

cleanup dry-run本文:

```json
{"issues":[{"issue_id":"empty_directory:rooms/living/devices/remote_device/adapters/ir","before_hash":"<Data Healthの64桁SHA-256>"}]}
```

issueを1件以上明示し、重複・未知issue（404）・stale hash（409）を拒否します。成功のトップレベルはok、plan_id、plan_digest、ready_to_apply、backup_requirement、backup_planned、actions。各actionはissue_id/action_id/action/destructive/affected_paths/before_hash/expected_after_state/backup_sources/backup_inventory_hash/backup_file_count/backup_total_sizeです。backup_plannedは作成済みではありません。

apply本文はplan_id、plan_digest、issues（同じissue_id/before_hash）、action_ids（同じ対象を一対一）、confirm:true。対象の修復や暗黙のaction追加は行わず、選択対象を自動backup後にTrashへ移します。成功はok、operation_id、status/result、apply_backup_id、actionsにaction_id/status/trash_entry_id。同じplan再送は409です。

### Migration

migration dry-runのmappingsは1〜100件です。要求例:

```json
{"mappings":[{"action_id":"migrate_power","source_path":"ir-signals/power.json","target_kind":"command","room_id":"living","device_id":"remote_device","target_id":"power","decision":{"room_id":"living","device_id":"remote_device","target_id":"power"}}]}
```

sourceは実在するir-signals/*.json通常file、target_kindはcommand/state。必要なRoom/Device/IR config/State Schemaはmappingのroom/device/ir_config/state_schemaに明示します。既存metadataの上書き・target衝突・unsafe pathは拒否。不足metadataはplanned_writesだけに置き、dry-runでは作成しません。decision.state、approve_raw_median、pico_command等の必要な人間判断がなければplanは保存できてもready_to_apply=falseです。保存変換は[DATA_SCHEMA](DATA_SCHEMA.md#plan-envelope)を参照。

bedroom-preset dry-runは完全な空本文を要求し、builtin_bedroom_presetのmigration.v2を保存します。固定Room bedroom/Bedroom、Device roomlight/Roomlight、command config（38kHz/repeat1）と6 Command（on_ch3/full_ch3/off_ch3/night_ch3/bright_up_ch3/bright_down_ch3）を対象とします。既存roomsがbackup対象で、rooms欠落や不完全/不一致scaffoldは拒否します。完全一致scaffold＋6 target欠落の場合はCommandだけを計画します。全6 actionの適用を必須とし、部分選択は拒否。過去claimは検証済みrollbackの条件を照合し、未rollbackの二重適用を拒否します。

migration apply本文:

```json
{"plan_id":"<plan ID>","plan_digest":"sha256:<digest>","action_ids":["migrate_power"],"rehearsal_backup_id":"<backup ID>","confirm":true}
```

同plan/actionのrehearsal_verified backupを要し、初回mutation前に別apply backupを記録します。create-onlyでtarget/scaffoldを作り、旧sourceは保持します。成功はok、operation_id/status/result、両backup ID、各action結果です。失敗時に部分作成targetを自動削除・再試行しません。

verify本文は`{"operation_id":"<migration operation ID>"}`。read-onlyでrepository/schema/format/count/timings/captures/State/metadata/source期待値とtarget hashを照合します。200でもstatusはverifiedまたはfailed。targetsはaction_id/target_path/status、失敗reason、期待/実測SHA-256を含み、欠落・破損・部分applyを修復しません。

rollback dry-runもoperation_idを受け、元operation/plan/apply backupの関連を確認してplanとtargetsを保存/返却します。targetsはapply時/current snapshot、eligible/理由、metadata/scaffold依存。応答はok/plan/targetsで、plan内にplan_id/plan_digest等を持ちます。新規作成とmutation完了の記録があり現在hashが同じtargetだけを候補にします。scaffoldは表示のみで削除候補ではありません。

rollback apply本文はplan_id、plan_digest、action_ids、confirm:true。対象のpoint-in-time backupを作り、未変更targetだけをTrashへ移します。旧source、元から存在したtarget、人が編集したtarget、scaffoldは変更せず、別planを含む同じtargetの二重claimを拒否します。

### 旧IR sourceの退避

legacy-ir-cutoverは廃止した旧IR CRUDとは別の現行Maintenance APIです。既存旧sourceの分類・検証済みmigrationの照合が必要で、退避済みsourceを再作成するAPIではありません。

- dry-run本文: migration_operation_id、sources配列（path/classification）。classificationはintentionally_skippedまたはinvalid。全fileを過不足なく分類し、JSON状態と照合する。
- 成功201: ok、plan_id/plan_digest、ready_to_apply、backup_requirement、action_id、source_path/source_file_count/source_total_size/source_inventory_hash、files/sources/migration_operation_id。
- apply本文: plan_id、plan_digest、rehearsal_backup_id、confirm_source_pathの厳密値`data/ir-signals`、confirm:true。action_idsは要求しない。
- apply成功200: ok、operation_id/status/result、両backup ID、action_id/trash_entry_id。別apply backupとwrite-aheadを保存してから旧subtreeを一度だけTrashへ移す。最終物理削除は行わない。

### Backup

POST M/backupsの本文はplan_id、plan_digest、action_ids。保存planとbackup source inventoryを再照合しrehearsal roleのbundleをsnapshot_verifiedで作成します。HTTPで任意source/pathやrole:applyを指定できません。作成応答の属性は一覧表のとおりで、ok wrapperはありません。

GET detailはbackup_id/role/state/plan_id/action_ids、created_at、sources、file_count/total_size/inventory_hash、filesを返します。内部absolute pathやsnapshot本体を公開しません。欠損/破損bundle等は503、不存在IDは404。

restore/dry-runはrehearsal/snapshot_verifiedだけを対象に、Backendが選ぶ隔離target、衝突、容量、inventory/byte/hashと復元可否を確認します。restore/rehearsalは同条件を再検証し隔離copyとreadback後にregistryをrehearsal_verifiedへ昇格します。本文は両方0 byteで、target/pathを要求から指定しません。dry-runのrole/state不適合は409ですが、衝突・unsafe target・容量不足のpreviewは200でcan_rehearse=falseとreasonを返します。rehearsal実行で復元不可なら409、root/bundle/readback失敗は503です。runtimeへの実復元APIではありません。

valid JSONはRoom/Device/IR repository、旧IRはMigrationSourceSignalへ型付きdecodeし、Schema/Stateの意味的整合も確認します。対応SchemaのないState snapshotや未知formatでは昇格しません。invalid/empty JSONは元byte保全を確認し、seal済みsnapshot/inventoryを変換しません。

rehearsal_backup_idは事前の隔離復元検証、apply_backup_idは初回mutation直前の自動snapshotです。apply bundleはpoint_in_time_verifiedで、rehearsal昇格対象ではありません。通常rollback/recoveryで古いrehearsalへ自動fallbackせず、巻戻る時点を人が判断します。

### Trash

GET一覧queryはtype（任意）、offset（0以上、既定0）、limit（1〜100、既定50）。未知・重複・不正queryは400です。応答はentries/total/offset/limit、空配列もentries:[]。totalはtype filter後、deleted_at降順/同時刻はentry ID順、未sealは末尾です。

一覧/detailはentry ID/status/種別、元識別子とdata root相対path、削除日時・保持期限・理由・operation関連・記録上の復元可否を返し、detailのみbefore inventoryを追加します。payload・内部absolute path・生metadataは返しません。不存在404、破損/root不備等503です。

restore/dry-runは空本文。previewはentry_id/original_id/original_relative_path/expected_inventory_hash/path_available/id_available/restore_available、不可時reasonです。衝突等でもpreview自体は200、restore_available=false。runtimeとTrashを変更しません。

restore本文:

```json
{"expected_inventory_hash":"sha256:<直前previewのhash>","confirm":true}
```

hashはentry/元識別子/path/削除metadata/file種別・size・内容を固定し、last_checked_at等を除きます。復元直前にmetadata/inventory/hash/元path/ID衝突/symlinkを再検証し、不一致は409、未知field/任意path/確認不足は400。不存在404、二重restoreは409です。

成功は復元識別子・相対path・operation_id。restoring→Linux amd64/arm64のrenameat2(RENAME_NOREPLACE)→readback→restoredの順で既存dataを上書きしません。未対応環境は停止し、rename後の失敗は自動再試行せずoperationと両treeを照合します。Room/Device metadataだけの削除では、自身の残った親directoryを衝突にしません。

保持期間はconfigの正整数trash_retention_daysから削除時にentry固有eligible_afterを固定します。後の短縮や時刻巻戻しで期限を回避せず、期限満了だけで自動削除しません。最終削除はTTY確認を伴うhost local CLIのみで、HTTP/Frontendから提供しません。手順は[OPERATIONS](OPERATIONS.md)。

### Operation履歴

GET listは`{"operations":[...]}`、detailは単一recordです。summaryはoperation_id/plan_id/type/target/status/started_at/completed_at、利用可能なbackup ID。detailはprevious_status、actions（action_id/action/status/classification/reason/error）、error、available_next_actionsを追加します。

safe tokenでないtarget/actionは省略し、path・before/intended/after snapshot・生のpath入りerrorを返しません。破損/scan失敗は503、不正ID400、不存在404。編集・削除APIはありません。

recovery_requiredのaction分類はunchanged/applied_unconfirmed/trashed_unconfirmed/ambiguous等です。候補verify/restore/new_dry_runは人の検討用で、mutation APIの存在や実行許可を意味しません。ambiguousではmanual_inspectionだけを示します。中断migrationは複数pathの完了を単一snapshotで証明できないため、ambiguous/reason=migration_multi_path_manual_reviewとなります。verifyは対象を照合するだけでruntimeの修復・operationの成功化を保証しません。

## 廃止APIの互換性

旧`/api/controller/rooms/bedroom/ir/signals...`、`light/...`、単独`ir/test`・`ir/capture`、`pico-led/...`は404です。通常学習・送信はDevice配下のCommand/Stateへ移行しています。旧statusの代替Commandや家電実状態表示は提供しません。controller-daemon/Picoの低層命令まで廃止した意味ではありません。

既存の旧JSONのdecode互換とMaintenance移行/退避routeは保持しますが、旧runtime sourceを自動復元しません。Trashの保持と最終物理削除は別の運用判断です。

実装の入口は[backend/main.go](../backend/main.go)、[handlers](../backend/handlers)、[services](../backend/services)、[models](../backend/models)、[Frontend client](../frontend/src/api/controller.ts)です。
