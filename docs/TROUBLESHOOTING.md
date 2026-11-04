# Troubleshooting

一般のAI作業規約と実行境界は[AGENTS](../AGENTS.md)を参照してください。

症状・確認・対処をまとめます。標準配置は[INSTALL](../INSTALL.md)、通常保守と中断判断は[OPERATIONS](OPERATIONS.md)、IR手順は[IR_GUIDE](IR_GUIDE.md)、status/本文の正本は[API](API.md)です。

調査はread-onlyから始めます。sudo/systemctl、OS反映、Pico書込み、実data/Backup/Trash/IR操作はManual onlyで、無印の運用操作も運用者が対象を確認します。未知状態のroot/socket/dataを削除したり、chmod 666で回避したりしません。

## 起動・設定・配信

| 症状 | 確認 | 対処 |
|---|---|---|
| 起動直後に終了 | ExecStart先頭の--config、JSON unknown/trailing/サイズ、placeholder、絶対path、root非重複、config owner/mode、旧環境変数との不一致 | configのcopyで修正内容を確認。exampleで既存configを置換しない |
| socket directory拒否 | /run/web-controllerのdaemon UID/primary GID所有0750、socket0660、両unitのUser/Group、configのsocket_path一致 | 実効unit/RuntimeDirectoryを確認して運用者が修正。別socketへ勝手に接続しない |
| Backend healthyだがcontroller 503 | daemon service/socket、pico_serial_pathの実by-id、実ttyのowner/group/modeとdialout、USB接続 | 通常data APIと区別。Picoなしの検証は失敗診断として記録。IRを再送して調査しない |
| Nginx 502 | Backend active、直接loopback health、config http_listenとproxy_pass | BackendとNginxのどちらで止まるか切り分け、設定を保全して修正 |
| /settings等の直接URLが404 | Nginx rootとcurrent/web、try_filesのindex.html fallback | 配信元を確認。API 404をHTMLで隠す設定にしない |
| build後もUI/APIが古い | 実効ExecStart/WorkingDirectory/current、配信indexとasset名/hash、VERSION、browser cache | sourceのbuildと稼働版は別。INSTALLの更新手順で反映し、旧assetを無条件削除しない |
| Pico firmwareが古い | 書込み先の実ボード、選んだUF2/hash、program metadata | buildはflashではない。正しいボード/UF2を確認し、運用者が書込み後に確認 |
| Pico serial pathが変わる | /dev/serial/by-id/と実tty、共通config、USB権限 | ttyACM番号や別機器のIDを推測しない。権限/設定変更は運用者が行う |

AI may run（read-only、任意directoryから標準hostを調査）:

```bash
curl --fail --silent --show-error http://127.0.0.1:3000/api/health
curl --silent --show-error --write-out '\nHTTP %{http_code}\n' http://127.0.0.1/api/health/controller
journalctl --no-pager -u web-controller-backend.service -n 100
journalctl --no-pager -u web-controller-daemon.service -n 100
ls -l /dev/serial/by-id/
```

追加の権限が必要なログ確認はManual only。運用者が`sudo journalctl --no-pager -u web-controller-backend.service -n 100`等で有限範囲を採取します。共有前にhost情報やdata内容を確認します。

## HTTPエラーと削除

| 症状 | 確認・対処 |
|---|---|
| 404 / 405 | NetworkでURL/methodとAllowを確認。未登録pathとmethod違反は別。旧IR Signal/Light/status/単独IR/Pico LED routeは廃止済みで、Device配下APIへ移る。旧routeへ試しにPOSTしない |
| 413 / 415 / invalid request body | route別の本文上限、Content-Type、unknown field、空本文/余分なJSONを確認。本文なしPOSTは空白/{}も不可 |
| Room削除409 room has devices | Device metadataが残っている。運用者が対象Deviceと付随dataを確認してからDevice→Roomの順で判断する |
| Device削除後にadapter dataが残る | 通常DELETEはdevice.jsonだけをTrashへ移し再帰削除しない。通常APIは親不在subtreeを探索しない。Data Healthとcleanup dry-runで確認する |
| 親RoomがないDeviceはroom not found | 孤立dataを手編集で復活/削除せず、保全後にData Healthと対象選択を確認する |
| DELETEが500/応答不明 | operation ID/Trash ID/status、元pathとpayloadを確認。移動済みの可能性があるため自動再送しない |

## SchemaとState

400/409のissuesのfield_id/code/case_index/state_idを確認し、保存SchemaとStateを読み直します。case制約は一式置換で、適用外はnull/0ではなくkey省略です。0/false/空文字と未選択を混同しません。規則は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)を参照してください。

Schema PUTが既存Stateと衝突する場合はinvalid_state_idsを調べ、Schema/Stateを自動削除・修正しません。登録済みStateの意味や信号を変えるPUTはlearned_state_changeで拒否します。新Stateを作り確認するか、旧Deviceを保持して別Deviceで新Schemaを作る手順を選びます。学習dataは通常PUTで書き換えません。

## Captureが422・短い・遅延する

有効capture不足はrequired_samples/valid_captures/count_distribution/capturesの時刻・count・timings・errorを確認します。同じState全体を送っているか、受信方向・配線・入力変化を運用者が調べます。失敗時は保存済み学習情報を維持します。

過去の実機では、無操作でも短い波形を先に拾って早期422になる場合と、約5秒の無信号timeoutを観測しています。待機中の入力変化の原因は未特定で、4秒程度待った最初の信号を確実に受信できるとは未確認です。配線の目視や一度の成功だけで解決済みとしません。試行数増加/連続再学習を既定の対処にせず、必要なら専用の未学習テスト対象を用意します。

samplesは1〜7、max_attemptsはsamples〜20。samples=7で省略既定21は上限超過となるためmax_attemptsを明示します。0を省略値として送りません。

capture countが上限に張り付く場合は[Pico受信実装](../pico-firmware/bedroom-usb/ir_receiver.cpp)と[RAW上限](DATA_SCHEMA.md#format別の信号属性)、切詰め・ノイズ・機器の信号長を確認します。MAX_TIMINGS増加だけを対処にせず、Backend/daemon/Picoの共通上限と波形全体を確認し、実装修正が必要なら別作業にします。

## broken pipe・timeout

Backend socketは接続15秒、接続後の通信全体15秒。daemon serial readは通常2秒、IR_CAPTUREは7秒、Picoは信号開始待ち最大5秒と80ms無信号終端です。daemonの(0,nil) readはresponse read timeoutとして扱います。

同じ要求の開始/応答時刻、daemon/Pico errorと短いcaptureによる422を切り分けます。broken pipeだけで機器未送信を断定せず、timeout値を実機結果なしに延長しません。設定/接続/処理段階を確認し、sendならdelivery結果に従います。

## 送信失敗・家電が反応しない

| 結果 | 確認・対処 |
|---|---|
| 未学習409 | 対象raw_samplesの学習状況とSchemaを確認 |
| carrier/capabilityの409 | 実効carrier、codeの明示execution_modeと現在Picoの対応を確認。nativeのoptions取得済みでもsend時に再検証する |
| RAW/code/presetの400 | 保存count/配列長・安全上限・正規化code・allowlistをAPIと照合し、信号を推測修正しない |
| not_sent | 送信前拒否の理由とretryable_withを確認し、人が修正/経路選択を判断 |
| partially_sent / unknown | 成功repeat数と家電の実状態を確認してから再操作を判断。自動fallback/再送をしない |
| sentだが不一致 | API結果と実機反応は別。送信方向・回路・carrier、登録波形と実リモコン全状態の対応を確認 |

ACのON/OFFが逆に見える場合は、ボタン名だけでなくリモコン全体の表示・電源状態を確認します。State値から波形は生成されません。停止を含むStateごとに実信号を学習し、toggle信号を確定したON/OFFとして扱いません。信号の比較/再学習/試験送信はManual onlyです。

## Backupが利用できない

invalid backup boundaryはroot/祖先symlink・所有者・0700/0600・保護rootの重複を確認します。容量不足はsource合計と余裕、現在の空き容量を調べます。source変化・hash/count/size/byte不一致は停止し、draftやseal済みbundleを自動削除/修復しません。元JSONの破損/0 byte自体は保全可能でjson_statusに記録され、bundle metadataの破損とは別です。

restore dry-runのrole/state不適合はHTTP409。衝突/unsafe_target/容量不足等は200でもcan_rehearse=falseになり、CLIもpreviewのreasonを確認します。rehearsal実行で不可なら409、copy/readback/registry昇格失敗は503です。隔離targetやregistryが途中状態の可能性があるため、削除して再試行しません。

valid JSONの未知path/typed decode失敗、不正Schema、Stateに対応するSchema欠落、未知formatも昇格を止めます。失敗したreadbackを成功扱いにするためにpayloadやregistryを書き換えません。

## Trashの異常

| 症状 | 確認・対処 |
|---|---|
| 一覧/detail503 | metadata/保持期間/root/owner/mode/symlinkを確認。破損entryを飛ばさず止まる。payloadと履歴を保全 |
| different devices | data/Trashのfilesystemを確認。copy fallbackせず運用者が配置を見直す |
| entry exists / source or payload changed | ID再使用やdry-run以後のhash変化を確認。同じ操作を自動再実行しない |
| preparedのまま / SourceMoved=true | source不在・payload有無とinventory、TrashMoveErrorのStageを確認。rename後sync失敗は移動済みの場合がある |
| restore409 | 元path/ID衝突、hash変化、復元済み状態を確認。previewを再取得し、既存dataを上書きしない |
| restoring / restore503 | 元pathへ移動済みの可能性。両treeとoperationを照合し、復元要求を再送しない |
| final-delete拒否 | eligible_after/last_checked_at/現在時刻、TTY、元verified operationとbackup関連、残存payload/hashを照合。期限/metadataを手編集しない |
| final_deleting中断 | 元plan/operationと進捗を保全。同一planでの手動再実行条件は[OPERATIONS](OPERATIONS.md#最終物理削除)を確認 |

## Maintenance・保存の中断

invalid maintenance stateではroot/plans/operationsの存在・所有者0700・file0600と非重複/祖先symlinkを確認します。missingは対象ID、corruptはJSON/record、already existsはID再使用、revision conflictは古いrevisionを調べます。記録が読めない状態でapply/restoreを続けません。

recovery_requiredではprevious_statusとaction分類、before/intended/afterと現物を確認します。ambiguous/read失敗/invalid_snapshotは追加mutationを停止します。verifyや復元の候補表示だけで操作を承認しません。詳細は[OPERATIONSの中断判断](OPERATIONS.md#中断時の判断)。

atomic JSONのrename後parent sync失敗でも置換済みの可能性があります。dataが消えた/壊れた場合は全書込み元を止め、4 root・設定・logを先に保全し、該当operation/Trash/Backupと照合します。旧Gitのdataを無条件にrestoreしたり、live rootへarchiveを上書きしたりしません。
