# Operations Guide

一般のAI作業規約と実行境界は[AGENTS](../AGENTS.md)を参照してください。

通常運用、Backup/Trash/Maintenance、中断時の判断を扱います。初回設置・版更新・復旧の正本は[INSTALL](../INSTALL.md)、症状別対処は[TROUBLESHOOTING](TROUBLESHOOTING.md)、学習・送信は[IR_GUIDE](IR_GUIDE.md)、HTTPは[API](API.md)、保存形式は[DATA_SCHEMA](DATA_SCHEMA.md)です。

## 実行区分と標準構成

**Manual only**: sudo/systemctl、Nginx反映、Pico書込み、実dataの作成/更新/移行/削除、実Backup/Trash操作、IR capture/send。無印の運用コマンドも運用者が対象を確認して実行します。実行例の記載は承認ではありません。

AI may runは、許可された範囲のread-only調査や有限出力のログ確認、開発用build/test/文書生成です。権限不足をsudoやmode緩和で回避しません。開発コマンドは[DEVELOPMENT](DEVELOPMENT.md)へ集約します。

両serviceは同じ`--config /etc/web-controller/config.json`を読みます。標準unitはweb-controller-backend.service / web-controller-daemon.service、専用user/groupはweb-controller。socketは`/run/web-controller/controller-daemon.sock`、data/maintenance/backups/trashは`/var/lib/web-controller/`配下です。設定key・root非重複/権限/同filesystem条件は[examples/README](../examples/README.md)を参照してください。

旧WEB_CONTROLLER_*を併用する場合はconfigの対応値と完全一致が必要です。標準unitはこれらをUnsetEnvironmentで除きます。手動CLIに古い環境が残る場合も同じ条件で拒否されます。configを直すために別rootへ無断で切り替えません。

## 日常の確認とservice操作

Homeで対象Room/Deviceを選び、信号名・State値・repeatを確認してから明示送信します。設定・一覧更新・State選択だけでは送信しません。API送信成功と家電反応を別に確認します。旧Light/status/単独IR/Pico LED HTTP routeは廃止済みで、Device配下を使います。

AI may run（read-only、標準hostでの例。任意directoryから実行可能）:

```bash
curl --fail --silent --show-error http://127.0.0.1/api/health
curl --silent --show-error --write-out '\nHTTP %{http_code}\n' http://127.0.0.1/api/health/controller
journalctl --no-pager -u web-controller-backend.service -n 100
journalctl --no-pager -u web-controller-daemon.service -n 100
```

通常healthはBackendの生存、controller healthはdaemon/Pico疎通を示します。Pico不通でも通常data APIは利用できます。controller healthの属性はdaemonOk/daemonRawです。ログを読めない場合は運用者へ引き継ぎ、継続出力の`-f`を自動調査の既定にしません。

Manual only（設置先のunitを確認して実行）:

```bash
sudo systemctl --no-pager --full status web-controller-backend.service web-controller-daemon.service
sudo systemctl stop web-controller-backend.service
sudo systemctl stop web-controller-daemon.service
sudo systemctl start web-controller-daemon.service
sudo systemctl start web-controller-backend.service
```

両processを止める順はBackend→daemon、起動はdaemon→Backendです。Type=simple/AfterはPicoの準備完了を保証しません。daemon単独停止時はIR/controller healthが使えません。起動後のhealth・保存値・画面を確認してから操作を再開します。

Nginx設定変更時だけ、運用者が保全済み設定との差分を確認し、`sudo nginx -t`成功後に`sudo systemctl reload nginx`を行います。source buildだけでは稼働binary/web/firmwareは更新されません。配布物更新はINSTALLの保全・切替手順を使います。

## Data HealthとCleanup

1. MaintenanceでData Healthを取得し、issue ID・相対path・severity・hash・破壊的actionの有無を確認する。診断だけではruntimeを修復しない。Schema/State不整合などは自動削除候補にしない。
2. 整理するissueだけを選びdry-runする。plan ID/digest、各actionの対象/hash、backup source/count/size/hashを確認する。dry-runは保守rootへplanを保存するがruntimeを変更しない。
3. 復元可能性と対象を運用者が確認し、同じissues/hash/action ID、confirm:trueを一度だけ指定する。Backendは排他claimと対象照合後、初回mutation前にapply backupを作り、選択対象だけをTrashへ移す。
4. 成功後のoperation_id/apply_backup_id/trash_entry_id、runtime不在とTrash/Backup詳細を照合する。破損/0 byte JSONも元byte列を保持する。backup成功はJSON修復ではない。

409/503、partial_failure、verification_failed、通信切断、中断では同じplanを無条件再applyしません。現物と履歴を保全し、下記の中断判断へ進みます。空directoryや孤立dataを手作業の再帰削除で解消しません。

## Migrationと事前復元演習

旧sourceを移す場合は実sourceと対象ID/State値/形式変換の人間判断を明示し、保存planとsource/target/scaffoldの照合を行います。ファイル名から家電やStateの意味を推測しません。builtin照明planは固定6 action全件を対象とする別方式です。要求の詳細は[APIのMigration](API.md#migration)を参照してください。

事前のrehearsal backupを作り隔離復元に合格した後、applyで同じplan/actionとrehearsal_backup_idを指定します。Backendは別のapply_backup_idを初回mutation前に作り、既存targetを上書きせず旧sourceを保持します。verifyはread-onlyで、HTTP 200でも各targetがverifiedかfailedかを確認します。

rollback dry-runは新規作成・適用完了の証跡があり、現在hashが適用時と同じtargetだけを候補にします。scaffold、元からあったdata、人が編集したtarget、旧sourceを巻き込まないことを確認します。rollback applyも対象をbackup後Trashへ移し、失敗時に自動再試行しません。

旧IR退避用legacy-ir-cutoverは旧subtreeの全file分類、migration/backup参照と厳密なsource確認を要します。廃止済み旧CRUDの代替ではなく、退避済みsourceの復活や最終物理削除も行いません。

## Backupの役割と照合

| role / state | 用途・判断 |
|---|---|
| rehearsal / snapshot_verified | 事前snapshotのbyte/hash検証済み。隔離復元演習の候補 |
| rehearsal / rehearsal_verified | byteとvalid JSONのrepository/Schema/State readbackを確認済み |
| apply / point_in_time_verified | 初回mutation直前の別snapshot。隔離演習を適用前条件にしない |
| draft | 失敗/作成途中。検証済みとして利用せず原物を保全 |

POST backupsは保存plan/digest/actionの保全対象からrehearsal bundleを作ります。任意pathやHTTPのrole:apply指定はできません。ID・role/state・plan/action・count/size/inventory/file hashを詳細で照合します。seal後のsnapshot/inventoryは不変、role/stateは別registryです。

隔離復元演習はBackend管理のbackup root内へcopyし、runtimeへ上書きしません。valid JSONのtyped readback、条件Schema/State/formatも検証します。対応Schemaを含まないState snapshotは合格しません。旧IRはmigration source型で読み、破損/空JSONは修復せずbyte一致を確認します。

Manual only。以下のBACKUP_ID等は実際の確認済みIDへ置換し、runtimeと同じservice userで実行します。開始位置は任意。

```bash
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance restore-dry-run BACKUP_ID
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance restore-rehearsal BACKUP_ID
```

CLI/HTTPともpreviewのcan_rehearse/reason・容量・衝突・source inventoryを確認してから演習します。CLIのdry-runはrole/state不適合でもpreviewを出せるため、終了codeだけで可否を判断しません。HTTP dry-runはrole/state不適合409、衝突/容量不足等は200 previewでcan_rehearse=falseです。

隔離targetは照合用に保持します。同じIDへ上書きせず、途中失敗時はregistry/targetを残して原因を調べます。通常rollback/recoveryは適用直前のapply backupを優先し、古いrehearsal backupへ自動fallbackしません。古い時点へ戻す必要があれば差分と時点を運用者が確認します。これらのCLIはlive root復元機能ではありません。

## Trashの確認と復元

Trash一覧/detailでentry ID、元ID/path、状態、削除日時、保持期限、理由、operation IDと記録上の復元可否を確認します。通常APIはTrashを一覧/取得/送信対象にしません。壊れたentryを黙って省略する一覧ではなく、不整合時は停止します。

復元は次の順に運用者が行います。

1. restore/dry-runを空本文で呼び、元path/IDの空き、restore_available/reasonとexpected_inventory_hashを確認する。
2. 同じentryへexpected_inventory_hashとconfirm:trueを送る。任意の復元先path/IDを指定しない。
3. 成功後のoperation、元metadataと付随data、Trash状態を確認する。Room/Device metadataだけの復元では、残存subtreeとの対応も確認する。

衝突/hash変化は409で止まり、上書きしません。restoringやrename後失敗では、元pathに戻っている可能性があるため再送しません。両treeとoperationを照合して判断します。

## 最終物理削除

最終削除はFrontend/HTTPにありません。保持期限経過後に対象を確認した運用者がBackend hostのlocal CLIで行います。設定のtrash_retention_days短縮は既存entryのeligible_afterを変えず、時計後退も拒否します。期限満了で自動削除しません。

Manual only。ENTRY_ID/PLAN_IDは確認済みの実IDに置換します。

```bash
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance trash-final-delete-dry-run --entry-id ENTRY_ID
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance trash-final-delete --plan-id PLAN_ID
```

previewのdeleted_at/eligible_after/current_time、inventory、元operation/backup関連を照合します。eligible=falseではplanを作れません。実行にはcontrolling TTYでentry IDの再入力が必要です。元operationがverifiedであること、関連backupのrole/state/source等を再検証し、計画されたfileだけを削除します。通常CRUD deleteのbackupなし関連は専用条件で判定します。

final_deletingで中断した場合だけは、元plan/operation、残存file/hash、削除進捗を人が照合したうえで**同じplan/operationの明示再実行**が可能です。一般applyの無条件再試行とは区別します。追加/変更fileやsymlinkを推測で削除せず、新plan/別entryへ切り替えて回避しません。完了後もmetadata tombstoneとoperationを保持します。

## 中断時の判断

起動時reconcilerはruntime/Trashを読み、operation metadataへrecovery_requiredとprevious_status/診断を記録します。runtimeの修復・自動resume/rollbackはしません。operation detailの分類を確認します。

| 分類・状況 | 判断 |
|---|---|
| unchanged | before snapshotと一致。未実施/安全な再適用の証明ではない |
| applied_unconfirmed / trashed_unconfirmed | operationのintended snapshotとの一致。完了の保証ではない |
| moved_unconfirmed / restored_unconfirmed | Trash repositoryの移動/復元後照合。operation viewの分類とは区別 |
| ambiguous、runtime_read_failed、trash_read_failed、invalid_snapshot | 記録/現物を保全し、追加mutationを止めてhost上で調査 |
| metadata読取り不能、rename後sync失敗 | file置換/移動済みの可能性。エラーだけから未変更と判断しない |

migrationの中断では複数pathの作成状態を単一snapshotから証明できないため、`ambiguous`・`migration_multi_path_manual_review`へ分類します。targetだけでなくscaffoldも含めて人が確認します。

人がverify、検証済みBackup/Trashからの復元、新dry-runを選びます。available_next_actionsは候補であってAPIの存在や操作許可を保証しません。operation/plan/digest/期限を手編集して成功化せず、履歴も自動削除しません。

ソフトウェア更新後の復旧は、INSTALLに従って新版の4 root・設定・logを先に別保存し、差分と復旧時点を確認します。softwareだけの差戻しやlive rootへの無条件展開、新版dataの旧側への自動mergeは行いません。
