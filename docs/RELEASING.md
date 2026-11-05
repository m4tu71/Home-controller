# Releasing

clean commitからのbuild・検査・梱包・checksum・候補確認・公開判断を扱います。v1.0.0はRelease公開済みです。現在準備中の[v1.0.1 Release notes](releases/v1.0.1.md)に変更点・検証範囲と残件を記載し、正式候補のcommit/tagと公開判断は別途確定します。v1.0.0の結果は[旧Release notes](releases/v1.0.0.md)を参照してください。過去候補の成功report・checksumを新候補の結果に読み替えません。

開発環境は[DEVELOPMENT](DEVELOPMENT.md)、配布物からの設置・更新・復旧は[INSTALL](../INSTALL.md)が正本です。各architectureのbuild成功、OS上の実行、Browser、Pico書込み、家電反応、公開承認を別々に記録します。

## 実行条件と出力

以下は**リポジトリrootから実行**します。例の.artifacts配下は今回新たに生成するbuild/report/検査用出力で、旧非公開の証拠を入力として要求しません。使用済みの出力は上書き/削除せず別名を使います。これらの生成物はsource入力や公開文書の必須参照にはしません。

AI may runは許可されたローカルbuild/test/検査・非破壊的生成のみです。依存導入、実環境反映、Pico書込み、実data/IR、commit/tag/push・Release公開は運用者が対象と段階を確認して行います。build toolはOS package/SDKを自動導入せず、serviceを反映したり公開したりしません。

Go/Node/npm/Python/CMake/Arm compiler/Pico SDK/picotoolを準備し、SDKの初期化・必要submoduleと対応を確認します。配布build toolは`ctest --test-dir`を使うため、CMake/CTest 3.20以上が必要です。Pico SDK側がより新しい版を要求する場合はその条件も満たします。単独host testのCMake 3.13以上という条件とは区別します。実toolchainの版とSDK commitを記録します。SDK/picotoolの以下の絶対pathはplaceholderで、実配置へ置換します。

## Clean build

review済み変更を運用者がcommitしたclean treeから最終候補を作ります。VERSION、Frontend package/lock/root packageの版が一致する必要があります。ローカルdirty snapshotによる検証は正式候補と区別します。

```bash
# rootから。最終候補は運用者のreview/commit後、依存導入も確認して実行
export PICO_SDK_PATH=/absolute/path/to/pico-sdk
export PICOTOOL_DIR=/absolute/path/to/picotool/cmake-config-directory
python3 tools/release/build.py --require-clean --install-deps --output .artifacts/release-build-new
```

[build.py](../tools/release/build.py)はtracked/untrackedの非ignored変更を--require-cleanで拒否します。既知のsource領域と設定例から隔離sourceを作り、runtime data・node_modules・既存dist/build/監査出力を入力としてcopyしません。

--install-depsは隔離sourceでnpm ciし、cache/network/lifecycle scriptを使います。省略時は既存frontend/node_modulesをsymlinkしてnpm lsで検査するため、依存tree再利用とnpm-ciをreportで区別します。最終候補の例はnpm-ci方式です。依存導入失敗を自動upgradeで回避しません。

| 段階 | 検査内容 |
|---|---|
| 版 | VERSION/package/lock一致、Footerの版とJS/CSS asset名・参照 |
| 自動確認 | config/Backend/daemonのGo test、Frontend test/lint/build |
| Linux binary | CGO_ENABLED=0、GOOS=linux、GOARCH=amd64/arm64、trimpath/buildvcs=false。両binaryのELF64/LSB、machine 62/183、Go metadata |
| Pico host | 別build directoryのnative parser test/ctest |
| Firmware | PICO_BOARD=pico2 / RP2350 ARM Secure、program版、UF2 block/family（secure payload 0xe48bff59、許可する補助block 0xe48bff57） |
| 入力/成果物 | source-manifestの各入力hashをbuild前後で照合、出力artifact hashを記録 |

生成物はVERSION、linux-amd64/binとlinux-arm64/binのbackend/controller-daemon、共通web、firmware/web-controller-pico2h-vVERSION.uf2です。build-report.json、source-manifest.json、source.patch、source/logs/pico-host/pico-build等は監査用で、丸ごと配布しません。

build-reportのbuild_passed、dirty、git_head、dependency_mode、各command/exit code、toolchain、成果物hashを確認します。失敗時は出力とlogsを保持し、原因修正後に新出力で再実行します。build成功で実機/設置/公開の合否を推定しません。

配布候補は[DEVELOPMENTのGo検査](DEVELOPMENT.md#goの確認とbuild)に従い、配布するOS/archのsourceと生成binaryをgovulncheckで確認します。使用Go版・scanner版・検査日と結果を候補に対応付けて保存し、npm監査0件をGoの安全性へ読み替えません。公開済みのtagや添付物を更新する場合は別の版・候補として扱い、同じ名前のassetを黙って差し替えません。

## 梱包と第三者notice

成功したclean buildと現在の梱包入力を照合してから実行します。AI may run（ローカル検査・生成のみ）:

```bash
python3 tools/release/npm_notices.py \
  --frontend .artifacts/release-build-new/source/frontend \
  --notices THIRD_PARTY_NOTICES.md --check
python3 tools/release/package.py --require-clean \
  --build .artifacts/release-build-new --output .artifacts/release-package-new
```

[package.py](../tools/release/package.py)はbuild成功/版/clean、成果物hash、現在のproduction入力hash・新規sourceの有無を検査します。testとexamplesに所定の例外があり、examplesは現在の梱包入力を別検査します。buildと梱包commitが異なる場合は両方を記録します。梱包文書だけの変更でbinaryを再利用する場合も、入力hashが一致することが前提で、production入力が変われば再buildします。

npm runtime noticeはbuild時lockと導入済みLICENSE/NOTICEに一致する必要があります。依存変更で更新が必要なら別の変更範囲でreviewし、npm_notices.pyの--checkなしは本文を書き換える操作であると区別します。今回の文書整理では第三者notice本文・LICENSE・依存情報を変更しません。

LICENSEがない場合は通常梱包を拒否します。--allow-missing-license / --allow-dirty-buildはローカル検証専用であり、例外候補を公開しません。権利者判断・review/commit後に正式候補をclean build/例外なし梱包で作ります。file存在の検査はライセンス条件の判断を代替しません。

| 出力 | 用途 |
|---|---|
| web-controller-vVERSION-linux-amd64.tar.gz | amd64 binaryと共通web/設置文書/設定例 |
| web-controller-vVERSION-linux-arm64.tar.gz | arm64 binaryと同上。実行検証とは別 |
| web-controller-pico2h-vVERSION.uf2 | 独立したPico 2 H firmware |
| SHA256SUMS | 上記3 assetのchecksum |
| package-report.json、inspection/ | 内容・展開照合の監査用。配布assetに含めない |

archive最上位はvVERSION/。同梱するのはbin/backend、bin/controller-daemon、web、VERSION、INSTALL.md、LICENSE、THIRD_PARTY_NOTICES.md、examplesのREADME/config/systemd 2 unit/Nginx templateだけです。READMEやdocs等のsource文書、実data、編集済みconfig、依存treeを丸ごと入れません。

directory/binは0755、その他fileは0644、ownerはroot:root、tar/gzip時刻は0で作成します。全memberのallowlist・重複・type/mode/byte列とpathを検査し、link/特殊file/絶対path/traversalを拒否した後、inspection/{arch}/opt/web-controller/releases/vVERSIONへ展開してhash/modeを読戻します。ELF/UF2とchecksumも再検査します。

既知のhome/LAN IP/USB個体値patternをsource/成果物/同梱文書に検査しますが、未知の秘密情報を網羅した保証ではありません。梱包allowlistと公開本文レビューを併用します。

## 配布候補の確認

AI may run（checksum検査。rootから）:

```bash
(cd .artifacts/release-package-new && sha256sum -c SHA256SUMS)
```

候補のbyte列とhashを固定し、再梱包したらその候補固有のSHA256SUMS/reportを使います。旧assetを上書きして結果を引き継ぎません。package_passed=trueでもrelease_readyは常にfalseで、梱包toolは配備/実行/実機の進行や公開承認を判定しません。

設定templateのoffline検査は[DEVELOPMENT](DEVELOPMENT.md#設定と配布文書の検査)に従います。check_install.pyは7文書のfile存在等を検査しますが、全公開文書のanchor/選定集合やarchive単体成立を網羅しません。公開対象だけを選び、全local linkのfile/anchorと旧文書名・証拠への必須依存を別検査します。INSTALL/examplesはarchive内にあるfileだけで参照を辿れることを確認します。

native amd64 hostでは、[smoke_package.py](../tools/release/smoke_package.py)で生成fixtureだけの有限process検査を行えます。AI may runはこの隔離検査だけで、実service/実data/Pico/IRは使いません。rootから、検査済みの**今回の**展開先を指定します（VERSIONは実際の版に置換）。

```bash
python3 tools/release/smoke_package.py \
  --release-root .artifacts/release-package-new/inspection/amd64/opt/web-controller/releases/vVERSION \
  --output .artifacts/release-smoke-new
```

release-rootとoutputは現行toolが要求するprojectの.artifacts内、native hostはx86_64です。toolは一時root/config・loopback一時port・未接続serial値を生成し、候補processとfixtureのCRUDを確認して終了します。サービス管理/Nginx/Browser/実機成功やarm64実行の代替ではありません。

その後、運用者が選定した候補/OS/archでINSTALLに従い、新規設置・非送信CRUD/削除・保存/サービス再起動・画面/直接URLを確認します。更新候補では同じsnapshotの切替/復旧回帰、実効権限・参照・差分保全も確認します。IR/firmwareの実機は別の明示範囲で[IR_GUIDE](IR_GUIDE.md)に従い、API結果と家電反応を分けます。

記録には候補commit/build/梱包commit、全asset/checksum、OS/kernel/arch、toolchain、実行範囲、報告者と直接照合の区別、未検証事項・error/残件を残します。現候補arm64の実行再検証なし、heat/dry等の範囲は[README](../README.md#制約と確認範囲)を維持し、旧候補の結果で埋めません。文書整理のために追加実機試験を要求しません。

候補の機能・検証範囲・既知制約は[v1.0.1 Release notes](releases/v1.0.1.md)へ集約します。

## 公開前の判断と公開後の照合

1. 移管する文書/sourceの一覧、LICENSE/notice、版、公開対象外の旧記録の除外を確認する。新リポジトリへ移管した場合、その確定commitから候補を作る。旧Git履歴のcommit・reportを新commitの証拠として使わない。
2. tag対象commit、build/梱包commit・入力hash、VERSION、asset名/metadata、SHA256SUMSと検証範囲を対応付ける。Release notesには実施/未実施を明記し、公開前を公開済みにしない。
3. 運用者が例外候補でないこと、候補の制約/残件、ライセンスと公開内容を確認して公開を判断する。AIへの文書整理依頼やpackage_passedはtag/push/Release作成の許可ではない。
4. 公開を承認された対象リポジトリで、運用者が確認済みcommitにtagを付け、対応する3 assetとSHA256SUMSをReleaseへ登録し、Release notesと対応付ける。新しいbyte列を同じ候補のassetとして黙って差し替えない。
5. 公開後にdownloadしたassetのhashを同じReleaseのSHA256SUMSと照合し、tag/commit・版・asset一覧・notesを再確認する。公開先と確認日を別記録に残す。

公開判断や配備後の確認はtoolの外で管理します。失敗した候補のreport/assetを保全し、成功reportやchecksumを手で書き換えません。
