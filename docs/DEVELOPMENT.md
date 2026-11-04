# Development Guide

一般のAI作業規約と実行境界は[AGENTS](../AGENTS.md)を参照してください。

開発環境とsourceからのbuild/test/lintを扱います。構成は[ARCHITECTURE](ARCHITECTURE.md)、HTTP/保存契約は[API](API.md)と[DATA_SCHEMA](DATA_SCHEMA.md)、UI規約は[DESIGN_SYSTEM](DESIGN_SYSTEM.md)、配布物の作成は[RELEASING](RELEASING.md)を参照してください。

## 開始位置と実行区分

以下の開発コマンドは**リポジトリrootを開始位置**とします。Goはconfig/backend/controller-daemonの3 moduleで、それぞれのdirectoryから実行します。rootにgo.workがあると仮定しません。生成先は既存の稼働物・固定証拠と分けた新規directoryを使います。

AI may run: read-only調査、ローカルbuild/test/lint、非破壊的なfile生成。Manual only: sudo/systemctl、Nginx反映、Pico flash、破壊的削除、実data/Backup/Trash/migration/IR操作、OS/network設定。無印の実環境操作もManual onlyです。依存の追加/更新は別途範囲を決め、文書整理ではpackage/lockを変更しません。

## 必要なtoolchain

| 対象 | 条件 |
|---|---|
| Go | 3 moduleのgo.mod宣言は1.22.2。宣言と依存を満たすtoolchain |
| Frontend | lockfileのengine条件を満たすNode/npm。現在のjsdomはNode ^22.22.2 / ^24.15.0 / >=26.0.0。例えば24系なら24.15.0以上を選び、他依存の条件もlockで確認 |
| Python | release toolsは3.9以上。実行版を記録 |
| Pico host test | CMake 3.13以上、native C++17 compiler。SDK/ボード接続不要 |
| Pico firmware | CMake、GNU Arm Embedded toolchain、初期化済みPico SDK/TinyUSB、SDKと対応するhost picotool |

現在の依存宣言は[frontend/package.json](../frontend/package.json)、[lock](../frontend/package-lock.json)、[backend/go.mod](../backend/go.mod)、[daemon/go.mod](../controller-daemon/go.mod)、[config/go.mod](../config/go.mod)が基準です。使用したtoolの版を検証記録へ残し、過去の成功環境を任意の最新版の保証にしません。

## Frontend

Manual only（依存treeを準備する場合。lockに従い、既存node_modulesが置換されることも確認）:

```bash
(cd frontend && npm ci)
```

npm ciはpackage/lockの不一致を拒否し、network/cacheとlifecycle scriptを使用します。依存versionの更新にはnpm install等の別作業が必要です。失敗をaudit fix --forceやlock書換えで回避しません。

開発用のESLint関連依存には、`minimatch@10`を`11.0.1`へ固定するnpm `overrides`を設定しています。
[GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249)への対処で、実行時依存は変更しません。
11系にはAPI変更があるため、依存元が対応する版へ更新できた際にこのoverrideの撤去を検討します。
変更時は監査に加え、lintの対象file/結果、Frontend testとbuildを確認してください。

AI may run（有限実行）:

```bash
(cd frontend && npm test)
(cd frontend && npm run lint)
(cd frontend && npm run build)
```

npm testはVitest/jsdom/React Testing Libraryで一度実行して終了します。buildはTypeScript検査とViteでfrontend/distを生成します。rootのVERSIONを検証して表示・JS/CSS asset名へ反映し、package/lockの版不一致は拒否します。buildだけではNginx配信物は更新されません。

Manual only / interactive（必要時に起動し終了する）:

```bash
(cd frontend && npm run test:watch)
(cd frontend && npm run dev)
(cd frontend && npm run preview)
```

development serverは利用者が接続先API/configを確認して使います。実稼働APIのdata/IRへ接続する画面操作を、自動テストと同一視しません。

## Goの確認とbuild

AI may run（各moduleのtest。実hostのserviceは起動しない）:

```bash
(cd config && go test ./...)
(cd backend && go test ./...)
(cd controller-daemon && go test ./...)
```

AI may run（新規一時directoryへbinary生成）:

```bash
build_output=$(mktemp -d /tmp/home-controller-dev.XXXXXX)
(cd backend && go build -o "$build_output/backend" .)
(cd controller-daemon && go build -o "$build_output/controller-daemon" .)
```

module間のreplaceは隣接するconfigを参照します。単独moduleだけを別directoryへcopyしてbuildしません。競合/並行処理を変更した場合は対象Go testのrace確認、必要なvet等を追加します。buildしたbinaryをserviceの実行pathへ自動copyしません。

Backend/daemonの実行には先頭の--configと正しい配置/権限が必要です。未指定で開発機の固定pathへfallbackする起動手順は使いません。隔離configと生成fixtureでの有限process検証は[RELEASING](RELEASING.md#配布候補の確認)を参照してください。

## Pico host test

AI may run（SDK/実機不要、rootから新規一時directoryへ生成）:

```bash
pico_host_build=$(mktemp -d /tmp/home-controller-pico-host.XXXXXX)
cmake -S pico-firmware/bedroom-usb/tests -B "$pico_host_build"
cmake --build "$pico_host_build" --parallel 4
(cd "$pico_host_build" && ctest --output-on-failure)
```

CTestはbuild directoryへ移動して実行するため、単独host testのCMake 3.13以上という条件を維持できます。配布build toolが要求する版は[RELEASING](RELEASING.md#実行条件と出力)を参照してください。

[parser test](../pico-firmware/bedroom-usb/tests/CMakeLists.txt)は命令parserのhost検査です。IR回路・USB・ボード上の動作成功を意味しません。

## Pico firmware build

SDKの準備/導入は運用者が確認します。次の絶対pathはSDK/picotoolの配置例placeholderで、実際の確認済みpathへ置換します。AI may runはbuildだけです。

```bash
export PICO_SDK_PATH=/absolute/path/to/pico-sdk
export PICOTOOL_DIR=/absolute/path/to/picotool/cmake-config-directory
pico_build=$(mktemp -d /tmp/home-controller-pico-build.XXXXXX)
cmake -S pico-firmware/bedroom-usb -B "$pico_build" \
  -DPICO_BOARD=pico2 -DCMAKE_BUILD_TYPE=Release \
  -Dpicotool_DIR="$PICOTOOL_DIR"
cmake --build "$pico_build" --parallel 4
```

生成物はそのdirectoryのbedroom_usb.uf2等です。Pico 2 Hはboard名pico2を使い、program metadataはroot VERSIONから設定します。SDKがpicotoolを取得する構成ではnetworkが必要になる場合があります。既存build directoryを削除せず、新規出力で再実行します。

flashはManual onlyです。運用者がBOOTSELの対象ボード/接続先・UF2/hash/metadataを確認し、対象に合う方法で書き込みます。固定の/dev/sdXを推測してmountしません。build成功をflash/実機成功として記録しません。

## 設定と配布文書の検査

AI may run（rootから新規出力へ。実service/host設定を変更しない検査）:

```bash
python3 tools/release/check_install.py --output .artifacts/install-check-new
```

.artifacts以下は**この実行で新たに生成する出力**で、旧証拠を入力として要求しません。出力名が使用済みなら別名にします。config test、隔離rootのsystemd verify、loopback一時portのNginx構文、7文書のpath/link存在とCLI表記を検査します。systemdは未実行のexecutable fixtureを使用し、service起動/本番port設置ではありません。

このtoolはanchor・公開対象集合・配布archive内のリンクを網羅しません。公開予定文書だけでfile/anchor/必須参照を別検査し、INSTALL/examplesはarchive構成だけでも確認します。sandboxのsocket等で検査できない場合は制約と未検査範囲を記録し、本番設定反映やsudoへ置き換えません。

## 変更時の確認と反映

変更範囲の仕様と実装を読み、対象test/lint/buildと文書リンク/差分を確認します。Frontend build、Go build、Pico buildの出力と、稼働版のasset/binary/firmwareは別に記録します。

配布候補はRELEASINGのclean commit/入力hash検査を通し、実hostへの反映は[INSTALL](../INSTALL.md)の保全・切替・復旧条件で運用者が行います。IR確認は[IR_GUIDE](IR_GUIDE.md)、運用再起動は[OPERATIONS](OPERATIONS.md)へ参照し、ここへ実host固有のcopy/削除手順を重複掲載しません。
