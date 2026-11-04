# Data Schema

現行の保存先・JSON属性・型の正本です。HTTP要求/応答は[API](API.md)、条件判定・互換性・編集保護は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)、配置と権限は[INSTALL](../INSTALL.md)と[設定例](../examples/README.md)を参照してください。未実装の保存案は[ROADMAP](../ROADMAP.md)で扱います。

## 保存rootと構成

`data/`はconfigの`data_root`を表す論理表記です。標準値は`/var/lib/web-controller/data`。保守の`maintenance_root`・`backup_root`・`trash_root`はdata/software/webと分離します。旧環境変数を設定する場合は共通configの対応値と完全一致が必要で、別配置を選ぶ手段にはなりません。

```text
{data_root}/rooms/{roomId}/
├── room.json
└── devices/{deviceId}/
    ├── device.json
    └── adapters/ir/
        ├── ir_config.json
        ├── commands/{commandId}.json
        ├── state_schema.json
        └── states/{stateId}.json
```

IRのcommand型はcommands、stateful型はSchema/statesを操作します。Deviceの`primary_adapter == "ir"`では必要に応じて空の`adapters/ir`を作りますが、それだけでconfigや信号が登録されるわけではありません。

通常DELETEはmetadata JSONをTrashへ移し、配下directoryを再帰削除しません。Command/Stateの学習情報も同一JSONに含むため一緒に保管されます。RoomはDevice metadataが残る場合に削除を拒否します。親metadataがない配下dataやTrashを通常APIが自動探索・復元することはありません。

## IDと共通型

Room/Device/Command/StateおよびSchema field IDは`^[a-z0-9]+(?:_[a-z0-9]+)*$`に従う安定したASCII文字列です。表示名は日本語を使えます。表示名変更をIDの変更として扱いません。内部のplan/operation/backup/Trash IDは各storeが発行・検証する別の識別子です。

以下でintegerはGoのint/uintに対応する整数、numberは有限のJSON数値です。`omitempty`の属性は保存型のゼロ値で省略される場合があります。HTTP書込みDTOは省略を区別するため保存型と異なります。JSONの一括書換え・未知属性の推測による補完は行いません。

## Room

保存先: `data/rooms/{roomId}/room.json`。型は[models.Room](../backend/models/room.go)。

| 属性 | 型 | 内容 |
|---|---|---|
| id | string | Room ID |
| name | string | 必須の表示名 |
| sort_order | integer、省略可 | 表示優先度。0は保存時省略可 |

```json
{"id":"living","name":"リビング","sort_order":1}
```

## Device

保存先: `data/rooms/{roomId}/devices/{deviceId}/device.json`。型は[models.Device](../backend/models/device.go)。

| 属性 | 型 | 内容 |
|---|---|---|
| id / name / room_id | string | Device ID・表示名・親Room ID |
| device_type | string | 機器種別（light、air_conditioner、tv、fan、switch、pc、unknown） |
| primary_adapter | string | 主操作方式（ir、echonet_lite、matter） |
| capabilities | string[]、省略可 | 共通機能の識別文字列 |

`primary_adapter`が保存属性名です。`adapter_type`は説明上の概念名で、Device属性には使いません。ECHONET Lite/Matterの値を保存できても対応Adapterの実行は未実装です。

```json
{"id":"remote_device","name":"IR機器","room_id":"living","device_type":"air_conditioner","primary_adapter":"ir","capabilities":["power","mode"]}
```

## IR config

保存先: `data/rooms/{roomId}/devices/{deviceId}/adapters/ir/ir_config.json`。

| 属性 | 型 | 内容 |
|---|---|---|
| ir_remote_type | string | command / stateful |
| carrier_khz | integer | 正のcarrier周波数。保存可能値と実行可能値は別 |
| default_repeat | integer、省略可 | 0または1〜5。0/省略は送信時fallback対象 |

```json
{"ir_remote_type":"stateful","carrier_khz":38,"default_repeat":1}
```

## CommandとState

Command保存先は`adapters/ir/commands/{commandId}.json`、Stateは`adapters/ir/states/{stateId}.json`です。いずれも`device.json`があるDevice directoryを基準とします。型は[IR model](../backend/models/ir_adapter.go)。

| 属性 | 型 | 内容 |
|---|---|---|
| id / name | string | 対象ID・必須の表示名 |
| state | object、Stateのみ | Schemaに従うfield ID→値のmap。省略とnull/0/false/空文字を区別 |
| format | string | raw_samples / raw / code / preset_command |
| carrier_khz | integer、省略可 | 正のcarrier値。0/省略はconfig参照 |
| count | integer、省略可 | 代表timings数。学習前は0/省略 |
| timings_us | uint[]、省略可 | 代表RAW timings（microseconds） |
| captures | Capture[]、省略可 | 学習試行の記録 |
| pico_command | string、省略可 | preset_commandのallowlist値 |
| protocol / code | string、省略可 | code形式のprotocolと正規化code |
| repeat | integer、省略可 | 0または1〜5。0/省略はconfig参照 |

未学習のState例（Schemaがこのstateを許すことが前提）:

```json
{"id":"standby","name":"待機","state":{"enabled":false},"format":"raw_samples"}
```

### format別の信号属性

| format | 保存内容と制約 |
|---|---|
| raw_samples | captures/count/timings_usをcapture成功時だけ更新。通常POST/PUTで学習データを直接変更しない。未学習は作成できる |
| raw | 直接登録するcount/timings_us。countは配列長と一致 |
| code | protocolはPANASONIC_40。codeはtrim後大文字の10桁hex文字列、0x prefixなし |
| preset_command | pico_commandは下記allowlistのみ。LIGHT_STATUS_CH3等の問い合わせは信号ではない |

preset allowlist:

```text
LIGHT_ON_CH3
LIGHT_FULL_CH3
LIGHT_OFF_CH3
LIGHT_NIGHT_CH3
LIGHT_BRIGHT_UP_CH3
LIGHT_BRIGHT_DOWN_CH3
```

RAWの新規保存・capture候補・send直前の代表timingsは1〜1024件、各値1〜100000us、合計1000000us以下、Picoのunsigned intへ変換可能であることを検証します。既存の上限超過dataを自動変換せず、送信時に拒否します。正の非38kHz値の保存と、現行RAW経路での実行可否を区別します。

code例:

```json
{"id":"power","name":"電源","format":"code","protocol":"PANASONIC_40","code":"344A90BC2C","repeat":3}
```

`execution_mode`とexecution-options/deliveryはHTTP上の一時的な実行選択・結果であり、Command/State JSONへ保存しません。送信方式・repeat解決・エラーは[API](API.md#irの学習と送信)を参照してください。登録済みStateの値と信号の保護は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md#保存と学習と送信時の保護)が正本です。

## State Schema

保存先: `data/rooms/{roomId}/devices/{deviceId}/adapters/ir/state_schema.json`。
rootはfield IDをkeyとする**非空object**です。versionやnotesのwrapperはありません。属性の型は[IRStateFieldSchema](../backend/models/ir_adapter.go)と[case/selector model](../backend/models/ir_state_schema.go)に対応します。

| base field属性 | 型 | 省略時・用途 |
|---|---|---|
| type | string、必須 | enum / number / boolean / string |
| values | string[]、省略可 | enumの候補。適用されるenumでは非空配列が必要 |
| min / max | number、各省略可 | numberの下限/上限。旧契約の他型属性は互換規則に従う |
| presence | string、省略可 | required / optional / absent。省略はrequired |
| step | number、省略可 | 正の刻み。省略は刻み制限なし |
| allow_empty | boolean、省略可 | stringの空文字許可。省略はtrue |
| label / description / unit | string、各省略可 | 表示名/説明/単位。空文字も保持し、型や制約の推定に使わない |
| cases | case[]、省略可 | 条件別定義。明示した空配列を省略へ変換しない |

caseは`when`と`presence`が必須です。型はbaseから固定で使い、case内にtypeは置きません。`values/min/max/step/allow_empty/label/description/unit`はbaseと同じ型で指定できます。

| 条件属性 | 型 | 形状 |
|---|---|---|
| when | object | allを持つ |
| when.all | selector[] | 非空配列、AND条件 |
| selector.field | string | 参照するfield ID |
| selector.in | array | 非空。参照enumの文字列、またはboolean。OR条件 |

追加属性のnull、未知属性を拒否します。拡張baseと全caseのvalues要素はJSON文字列だけを許します。旧属性の読取り互換、拡張判定、case制約置換、selectorの制限、十進step、省略属性の保持は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)を参照してください。

```json
{
  "enabled": {"type":"boolean","label":"動作"},
  "level": {
    "type":"number","presence":"absent","label":"強さ",
    "cases":[{"when":{"all":[{"field":"enabled","in":[true]}]},"presence":"required","min":0,"max":5,"step":1}]
  }
}
```

これは合成例です。機種固有のmodeや温度を固定仕様にしません。Schema全体メモは未実装です。

## Capture

Command/Stateのcaptures内に保存します。

| 属性 | 型 | 内容 |
|---|---|---|
| attempt | integer | 試行番号 |
| ok / valid | boolean | 機器応答の成功 / Backendの検証結果 |
| count | integer | timings数 |
| timings_us | uint[]、省略可 | 受信timings |
| error | string、省略可 | 失敗説明 |
| captured_at | string | time.TimeのJSON日時 |

```json
{"attempt":1,"ok":true,"valid":true,"count":3,"timings_us":[340,500,370],"captured_at":"2026-01-01T00:00:00Z"}
```

短い配列は構造説明用であり、実機で学習に合格した波形を意味しません。学習失敗時は保存済みcaptures/代表timings/countを変更しません。

## Maintenance state

配置はconfigの`maintenance_root`。root/plans/operationsはservice user所有0700、通常file0600とし、他の保護rootとの重複・symlink等を拒否します。

```text
{maintenance_root}/
├── plans/{plan_id}.json
└── operations/{operation_id}.json
```

### Plan envelope

[MaintenancePlanEnvelope](../backend/models/maintenance_plan.go)が保存型の正本です。作成後同一IDで上書きしません。

| 属性 | 型 | 内容 |
|---|---|---|
| version | integer | 共通envelopeの版（現在1） |
| plan_id / operation_type / payload_schema_id | string | 識別子・操作種別・payloadの版 |
| payload | object | 操作別payload |
| created_at | string | 作成日時 |
| ready_to_apply | boolean | 未決定事項と計画検証の結果。適用承認を表さない |
| backup_requirement | string | none / apply / rehearsal_and_apply |
| actions | action[] | 選択・保全境界 |
| plan_digest | string | digest fieldを除いたcanonicalized envelopeのSHA-256 |

共通actionは`action_id/action`（string）、`selectable/destructive`（boolean）、省略可能な`backup_sources/backup_directories`（string[]）、`backup_files`（BackupFile[]）、`backup_inventory_hash`（string）、`backup_file_count/backup_total_size`（integer）を持ちます。保存inventoryと実sourceの一致を再検証します。

| payload_schema_id | 保存する内容と境界 |
|---|---|
| cleanup.v1 | actionsにissue_id/action_id、trash_fileまたはtrash_directory、destructive、affected_paths、before_hash、expected_after_state。重複・親子path・symlink・root外・既知配置外を拒否 |
| migration.v1 | actionsにsource相対path/size/SHA-256、source_format、未存在target、parent事前snapshot、planned_writes、target_format/transform、decision。RAW本体は計画へ複製せずsourceから再読込 |
| migration.v2 | builtin_bedroom_presetの固定6 action。source snapshotは空、Room/Device/configとCommand対応を固定。rooms subtreeをbackup対象とし6 targetをclaim |
| migration_rollback.v1 | 元operation/plan/digest/apply backup ID、targetのapply時/current snapshot、eligible/理由、metadata/scaffold依存と共有action。新規作成・未変更・完了証跡のあるtargetだけが選択可能 |
| legacy_ir_cutover.v1 | migration_operation_id、固定source_pathのir-signals、旧fileごとのpath/classification、単一archive actionとsubtree全inventory。旧IR退避のMaintenance APIで使用。廃止済みの旧IR CRUDとは別 |
| trash_final_delete.v1 | entry metadata snapshot、current_time、file相対path/size/SHA-256、directory、元operation/backup関連。local CLIで同一plan/operationの進捗を照合 |

migration.v1の変換はraw_samples維持、raw維持、明示承認したraw_median→raw、Commandのpreset_command維持です。不足metadata/Schemaは型付きscaffoldとして明示された内容だけをplanned_writesへ置き、既存metadataを上書きしません。State値・raw_median承認・presetの選択等が未決定ならactionを選択不可、ready_to_apply=falseにします。

migration.v2はroomsが存在し、対象scaffoldが未作成、または固定内容に完全一致するscaffoldと欠落した6 Commandがある場合に作成できます。不完全・不一致のscaffoldは拒否し、再作成時も既存scaffoldを上書きしません。過去のtarget claimは検証済みrollbackによる解放条件を照合します。HTTPの入力は[APIのMigration](API.md#migration)を参照してください。

### Operation record

保存型は[MaintenanceOperation](../backend/models/maintenance_operation.go)。HTTPのhistory viewとは別です。

| 属性群 | 型・内容 |
|---|---|
| version / id / revision | integer / string / unsigned integer。revisionは更新時CASで1増加 |
| started_at / completed_at | 日時 / 任意の完了日時 |
| target / type / status / result / error | string。type/result/errorは省略可 |
| plan_id / plan_digest / claimed_paths | 任意のstring/string/string[]。作成後変更不可の関連・claim |
| rehearsal_backup_id / apply_backup_id | 任意のstring。役割・保全時点を区別 |
| actions | id/action/status、任意のbefore_state/intended_state/after_state snapshotとerror |
| events | at/action_id/kindと任意snapshot/error。pre_mutation/mutation_returned/post_readback/action_error |
| recovery | previous_status、checked_at、各action_id/classification/reason |

snapshotはruntimeと任意trashに、root相対path・exists・存在時kind/sha256を持ちます。before/intendedは同じpathを対象とします。形式不足・root不備・symlink・不一致は推測で修復しません。

共通coordinatorはprepared→applying→applied→verifying→verified、失敗時partial_failure/verification_failedを記録します。起動時照合でrecovery_requiredと元status・action分類を保存します。これは署名付き監査chainではなく、復旧判断用checkpointです。同じplanやclaim対象の二重適用を排他制御し、plan/operationを自動削除しません。

## Backup bundle

配置はconfigの`backup_root`。root/bundle/data directoryはservice user所有0700、snapshot file/metadataは0600です。bundle IDを再利用しません。

```text
{backup_root}/
├── {backup_id}/
│   ├── data/                    # data root相対pathを保持したsnapshot
│   ├── inventory.json           # seal後不変
│   └── registry.json            # role/stateの別管理
└── .restore-rehearsal/{backup_id}/
```

保存型は[BackupBundle/BackupRegistry](../backend/models/backup.go)。

| inventory.json属性 | 型・内容 |
|---|---|
| version / id / role | integer/string/string。roleはrehearsalまたはapply |
| plan_id / action_ids | 任意string / string[] |
| created_at / sources / directories | 日時 / source相対path配列 / directory相対path配列 |
| files | path(string)、size(integer)、sha256(string)、任意json_status(valid/invalid/empty)の配列 |
| file_count / total_size / inventory_hash | integer/integer/string |

registryはversion/id/role/state/updated_atを持ちます。stateはdraft、snapshot_verified、point_in_time_verified、rehearsal_verified。破損・0 byte JSONも元byte列を保存し、sealのためにparse成功を要求しません。metadata自体は正常に読める必要があります。

rehearsal roleのsnapshot_verified bundleを隔離targetへcopyし、byte/hash/count/sizeとvalid JSONのrepository/Schema/State readbackに成功した場合だけregistryをrehearsal_verifiedにします。旧IR JSONは[MigrationSourceSignal](../backend/models/migration_source_signal.go)へ型付きdecodeします。invalid/emptyはbyte保全を確認し、対応不明のvalid JSON pathや不正なSchema/Stateでは昇格しません。Stateだけで対応Schemaを含まないsnapshotも不適合です。

apply roleはmutation直前に自動作成してpoint_in_time_verifiedでsealし、事前の隔離rehearsalは要求しません。seal済み本体/inventoryと隔離targetを自動上書きせず、registryだけを通常遷移で更新します。両backup IDの利用判断は[OPERATIONS](OPERATIONS.md)を参照してください。

## Trash entry

配置はconfigの`trash_root`。dataと同一filesystem、root/entry directoryはservice user所有0700、metadataは0600。payloadは元modeのままprivate entry内に隔離します。

```text
{trash_root}/{entry_id}/
├── metadata.json
└── payload                     # 元file/directoryをrename
```

保存型は[TrashEntry](../backend/models/trash_entry.go)。

| metadata属性群 | 型・内容 |
|---|---|
| version / id / status | integer/string/string。prepared/sealed/restoring/restored/final_deleting/final_deleted |
| original_relative_path / original_id / target_type | string。元のdata root相対path・識別子・対象種別 |
| reason / operation_type / operation_id | string。理由と元operation |
| before_inventory | kind、file_count、total_size、sha256 |
| deleted_at / eligible_after / last_checked_at | 任意の日時。entry固有の保持期限を固定 |
| restore_available | boolean。記録上の復元可否、実行時は再検証 |
| restore_operation_id / restored_at | 任意string/日時 |
| final_delete_plan_id / final_delete_operation_id | 任意string |
| final_delete_files_deleted / final_delete_files_total / final_deleted_at | 省略可の進捗数・完了日時 |

prepared保存後にrenameし、source不在・payload inventory/hashを読戻してsealedとします。保持期間はconfigの正整数trash_retention_daysからrename成功時のUTC deleted_atを基準に固定し、後の設定短縮で既存eligible_afterを変えません。時計後退は停止条件です。期限満了だけで削除しません。

restoreは衝突・変化を再検証し、restoring→上書き禁止rename→readback→restoredの順です。最終削除もplan/元operation/backup/保持期限/残存fileを照合し、metadata tombstoneを残します。payloadや固定証拠・期限を手編集して復旧扱いにしません。
