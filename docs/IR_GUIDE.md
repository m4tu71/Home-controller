# IR設定と操作

Room/Deviceの設定、Command/Stateの登録、学習・明示送信、確認方法をまとめます。実data作成・更新・削除と実機の学習/送信は**Manual only**です。対象と信号の意味を確認した運用者が実施し、この文書の例を実行許可として扱いません。

HTTPと送信結果は[API](API.md)、保存形式は[DATA_SCHEMA](DATA_SCHEMA.md)、条件規則・互換性は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)、障害時は[TROUBLESHOOTING](TROUBLESHOOTING.md)が正本です。配置・Pico接続は[INSTALL](../INSTALL.md)を参照してください。

## 開始前の確認

| 対象 | 記録・確認すること |
|---|---|
| 本体とリモコン | メーカー/型番、ボタン型か状態全体を送る型か、採用する操作と停止の意味 |
| 実行環境 | Frontend/Backend/daemon/firmwareの版、配布候補/hash、実効unit/config/data root、Pico by-id、送受信回路 |
| 信号の方式 | carrier/protocolと対応の根拠。RAW実行は現行38kHz、codeはPANASONIC_40 |
| 保存対象 | 衝突しない安定IDと表示名、必要なfield/許容値、既存Schema/State/学習情報とBackup・復元方法 |
| 確認範囲 | 実機で試すCommand/State/停止、repeat、未検証mode・温度・風量等 |

controller healthを確認し、対象の機器と保存dataを特定します。ファイル名や旧Trashの信号から機種・温度・modeを推測しません。リモコン操作自体でも家電が動くため、学習中の実機の変化も運用者が把握します。

## Room・Device・IR config

1. Settings（/settings）で対象Roomを選ぶ。存在しない設置環境だけ新Roomを作る。
2. 対象Deviceを作成/選択し、機器種別とprimary_adapter=irを設定する。既存機器に新しいSchemaを導入する場合は学習情報を保全し、衝突があれば別IDの新Deviceを使う。
3. IR Adapterのir_remote_typeを、ボタンごとのcommandまたは状態全体のstatefulに設定する。確認済みのcarrier、初期default_repeatを保存する。38kHz/repeat1は現行RAW用の例で、未知機器の対応を証明する値ではない。
4. 一覧更新・再読込で同じRoom/Device/configを確認する。Settingsは`/settings?room={roomId}&device={deviceId}`、日常画面は`/rooms/{roomId}?device={deviceId}`で対象を選べる。

設定だけで送信しません。Device削除はmetadataをTrashへ移す操作で、subtree全体の初期化ではありません。既存dataを作り直すための削除を先に行わないでください。

## ボタン型Command

command型Deviceでは、安定ID、表示名、formatとその信号属性を登録します。学習する場合はraw_samplesを選び、未学習Commandとして保存します。`raw_samples`の学習情報である`captures`・`timings_us`・`count`は、通常フォーム/APIから直接登録・更新せず、学習成功時に保存します。`raw`形式の`timings_us`・`count`は下表のとおり直接登録する信号属性です。

| format | 登録と使用の方針 |
|---|---|
| raw_samples | 実リモコンで同じ操作を複数学習し、代表波形を保存して再送 |
| raw | 内容を確認したtimings/countを直接登録。安全上限を満たす必要がある |
| code | 確認したPANASONIC_40 codeを文字列で登録。送信時に実行経路を明示選択 |
| preset_command | 対応する固定Pico命令を選ぶ。一般の全機器向け信号ではない |

許可属性・正規化・preset allowlistは[DATA_SCHEMA](DATA_SCHEMA.md#format別の信号属性)を参照してください。LIGHT_STATUS_CH3は送信Commandにしません。照明CHや特定コードの確認結果を他機種の仕様に一般化しません。

## Stateful SchemaとState

実リモコンで選べる項目・値・mode別の有無を確認してSchemaを作ります。temperature/mode等の特定field名は必須ではありません。通常温度と補正のように意味や単位が違う値は別fieldにし、機種に存在しない温度や架空のoff modeを必須fieldの穴埋めに使いません。

1. Settingsで型・候補・範囲を設定し、必要なfieldだけpresence/cases/step/表示情報を追加する。templateも内容確認後にdraftへ明示適用する。
2. 保存前に既存Stateと学習済み信号への影響を確認する。Schema更新409や登録済みStateの変更拒否を回避するために、保存fileを直接書き換えない。
3. 各採用Stateを安定ID・表示名・実リモコンの全状態値・formatで保存する。requiredを満たし、optionalは入力/省略を選ぶ。absentになったdraftの除外を確認する。0/false/空文字と省略を区別する。
4. 停止も、確認したリモコン全状態とSchemaに沿う独立Stateとして登録する。停止時に保持されるmode/温度の意味を決め、未確認値を推測しない。

raw_samplesはState値から波形を生成する方式ではありません。Stateごとに、その意味に合うリモコン信号を学習します。登録済みStateの意味/信号を変える場合は新Stateを作り、旧Device/Stateを確認前に削除しません。別Device移行の条件は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md#保存と学習と送信時の保護)を参照してください。

## 学習・再学習

1. 対象Command/Stateと実リモコンを同じ操作/全状態へ合わせる。StateではSchemaと保存値の整合も確認する。
2. samples/max_attemptsを[API](API.md#capture)の範囲で指定する。例は3/9。samples=7ではmax_attemptsを20以下に明示する。
3. 学習を明示開始し、学習済み対象の再学習では確認ダイアログを承諾する。受信機へ同じ信号を送る。途中でmode/温度等を変えて別状態のsampleを混ぜない。
4. HTTP結果、required/valid数、count分布・各captureの時刻/波形/error、保存後の代表timingsとcountを確認する。成功時だけ学習情報が一括更新される。
5. 失敗後は再読込して以前の学習情報を確認する。422・短いcapture・無入力timeoutを区別して原因を調べ、連続再試行や試行数増加を自動で行わない。

学習はraw_samples専用です。再学習の確認ダイアログで取り消した場合は要求を送らないことと、既存情報が変わらないことを確認します。一度送ったHTTP要求は、画面を閉じる/通信を切るだけでBackendも中断したと断定しません。結果不明なら読み直します。

## 日常画面から送信

1. Homeまたは部屋URLでRoom/Deviceと対象Command/Stateを選ぶ。選択・情報表示・一覧更新だけでは送信されないことを確認する。
2. 信号名/State値、実効repeat、codeならexecution_modeを確認する。初回の実機確認は小さいrepeat（例1）から運用者が判断する。保存repeat/default_repeatのfallbackと明示repeatは[API](API.md#sendの入力と実行方式)に従う。
3. 専用送信buttonと確認ダイアログで明示送信する。未学習、Schema不適合、carrier不一致、native capability未確認等の利用不可理由を解消せず連打しない。
4. delivery_state・requested_repeats/completed_repeats、HTTP errorと家電の実反応を別に記録する。sentでも温度到達・内部mode・実状態を断定しない。

not_sentは送信前拒否の理由を確認します。partially_sent/unknownは一部/全部が送信済みの可能性を踏まえ、家電を確認してから再操作を判断します。codeのoptionsは現在の表示用snapshotで、send時にもcapabilityを確認します。nativeからbackend_raw等への自動fallbackや自動再送はしません。

## 保存・再起動後の確認

再起動前にDevice/config/Schema/Command/Stateの全値・format・count・学習情報をGET/画面で記録します。稼働unitとconfig/data rootを確認し、運用者が[OPERATIONS](OPERATIONS.md#日常の確認とservice操作)の順でサービスを再起動します。

再読込後に同じ対象と保存値、未学習/学習済み表示、情報・選択だけでは送信されないことを照合します。その後、運用者が採用対象と停止を明示送信して家電反応を再確認します。サービス再起動・OS再起動・実機試験を分け、未試験のmode/全組合せまで成功に広げません。

## 記録する項目

候補/commit・asset/hash、OS/arch、各component/firmwareの版、service/config/root、機種/リモコン、Room/Device/操作ID、保存Schema/State、学習の要求と結果、repeat/経路/delivery、実機反応、再起動前後の差分、未検証範囲・失敗を記録します。既存reportを新候補の結果へ書き換えず、新しい結果を別に残します。

過去の照明/AC確認は[READMEの確認範囲](../README.md#制約と確認範囲)に限定して扱います。この汎用手順の作成で機種の追加検証を完了したことにはなりません。
