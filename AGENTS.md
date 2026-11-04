# AI作業規約

この規約はリポジトリ全体に適用します。依頼の目的・範囲と既存差分を確認し、関連する正本と実装を読んでから小さく変更してください。ユーザーの明示指示を優先し、過去タスクの個別承認や実施結果を新しい操作の承認へ読み替えません。

## 変更範囲に応じた参照

全仕様の一括読取りを毎回必須にせず、対象に関係する文書・実装・testを選びます。

| 変更範囲 | 正本・入口 |
|---|---|
| 概要・構成・package責務 | [README](README.md)、[ARCHITECTURE](docs/ARCHITECTURE.md) |
| HTTP route/要求/応答/error | [API](docs/API.md)、[HTTP mux](backend/main.go)、[handlers](backend/handlers) |
| 保存先・JSON属性/型 | [DATA_SCHEMA](docs/DATA_SCHEMA.md)、[models](backend/models)、[repositories](backend/repositories) |
| 条件Schema・互換性・信号保護 | [STATEFUL_SCHEMA](docs/STATEFUL_SCHEMA.md)、[Schema service](backend/services/ir_state_schema.go) |
| IR操作・実機確認 | [IR_GUIDE](docs/IR_GUIDE.md)、[IR service](backend/services/ir_adapter_service.go)、[adapter](backend/adapters/ir) |
| UI・CSS・用語 | [DESIGN_SYSTEM](docs/DESIGN_SYSTEM.md)、[frontend/src](frontend/src) |
| 開発/test/build | [DEVELOPMENT](docs/DEVELOPMENT.md)、各module/package/lock/CMake |
| 設置/更新/復旧・通常保守 | [INSTALL](INSTALL.md)、[OPERATIONS](docs/OPERATIONS.md)、[TROUBLESHOOTING](docs/TROUBLESHOOTING.md) |
| 配布・公開 | [RELEASING](docs/RELEASING.md)、[Release notes](docs/releases/v1.0.0.md) |
| 将来案・変更要約・作業記録 | [ROADMAP](ROADMAP.md)、[CHANGELOG](CHANGELOG.md)、[記録の運用](docs/versions/README.md) |

文書と実装が衝突したら、現行契約をsourceで確かめます。文書整理の依頼を機能追加・実装の変更へ拡大しません。根拠が不明ならread-only調査を先に行い、解決しない前提・影響を明示します。

## 実装の不変条件

- Backendは単一Go binary。handlerはHTTP境界、serviceは検証/処理調整、repositoryは永続化、irはRAW/codeの処理、adapter/clientは機器通信を担当する。handlerへ直接file I/Oや機器命令構築を追加しない。
- Room→Device→Adapterを保ち、Remoteを最上位概念にしない。Device共通情報とAdapter固有保存を分ける。ECHONET Lite/Matter等の未実装案を既存機能として追加しない。
- 安定IDと表示名を分け、codeは文字列で保存する。JSONの属性名はGo field名でなくJSON tag、必須/省略・型はDTO/codec/serviceで確認する。
- raw_samplesの学習情報保護、登録済みStateの値/信号の対応、Schema更新の既存State照合を維持する。旧dataの属性を黙って除去・一括変換しない。
- codeの実行経路は明示選択し、現在のcapabilityを送信時にも検証する。delivery/repeat進捗を維持し、自動retry/fallbackを追加しない。送信API成功を家電の実状態として扱わない。
- atomic JSON、同filesystem Trash、保存plan/digest、write-ahead operation、rehearsal/apply backupの役割、中断時のread-only照合を維持する。通常DELETEを直接物理削除へ戻さない。最終物理削除をFrontend/HTTPへ追加しない。
- FrontendのAPI呼出しはapi層、操作状態はhook/componentへ置き、JSXへ通信処理を散在させない。既存React state・CSS Modules・token/componentを優先し、新依存・token・操作patternが必要なら理由と影響を示す。

詳細仕様をここへ全文複製せず、対応する正本を更新します。リファクタでは既存挙動を保ち、型・純粋処理・I/O・業務処理の責務を分け、機能変更と混ぜないでください。

## 実行境界

AI may runはread-only調査、sudo不要の有限ログ読取り、許可されたローカルbuild/test/lint、非破壊的なfile生成・編集です。開始位置はリポジトリroot等を示し、checkout先に依存しない相対pathを使います。OS上のconfig/設置先pathは正本に従う絶対pathを使います。

以下はManual onlyとして、必要な対象・理由・前提・確認/復旧方法を具体化して運用者へ提示します。

- sudo/systemctl、systemd unitの実配置/変更、Nginx deploy/reload、firewall/network/OS設定。
- Pico firmware書込み、mount先を含む実機の変更、実IR capture/send。
- 実data/Backup/Trashの作成・変更・移行・復元・最終削除、破壊的削除（既存build/distも含む）。
- 破壊的Git操作・履歴書換え。commit/tag/push・Release公開も明示依頼がなければ行わない。
- 依存導入/更新とinteractive serverはDEVELOPMENTの実行区分に従う。既存node_modulesやlockを無断で置換しない。

無印の実環境操作もManual onlyです。build成功は反映・flash・実機成功を意味せず、文書のcommand例は実行承認になりません。root/device/IDを推測して操作しないでください。

実data変更が必要なら、対象file/actionと影響、plan/backup/dry-run/rollback、停止条件を具体化し、運用者の確認前に適用しません。適用時は保存planと最新対象の照合が必要で、snapshotや旧版の結果を無条件流用しません。失敗後は現物・report・旧出力を保全し、再試行や旧側へのmergeを自動化しません。

## 調査・確認・報告

1. branch/statusと既存差分、対象の正本・実装・関連testを確認する。ユーザーの変更を上書きしない。
2. 依頼された範囲で、保持する契約と確認方法を決めて実施する。仕様の全文複製や過度な抽象化/大規模書換えを避ける。
3. 変更に適したtest/lint/build・文書検査を行い、失敗と対処・再確認を記録する。文書のみの変更はfile/anchor/参照/例/実装照合とdiff検査を中心にし、無関係なbuildや実機試験を追加しない。
4. JSON tagと応答shape、status/空値・省略、CLIの引数と必要tool版を実装で照合する。必要な検査をできなければ未検査と理由を残す。
5. 何を・なぜ変えたか、変更file、実行commandと結果、未実施のManual操作、残件を簡潔に報告する。ユーザー報告と直接照合を区別する。

候補commit・OS/arch・確認範囲を明記し、build/梱包/対象環境の実行/Browser/家電反応/公開判断を分けます。未検証事項を消さず、旧report/hashを新候補の結果へ書き換えません。作業記録のtemplateは[docs/versions/TEMPLATE](docs/versions/TEMPLATE.md)を使います。

## 文書と権利表記

仕様の正本を一つに保ち、要約・例は文書位置からの相対リンクで参照します。公開文書に個人home/drive・LAN IP・USB個体値・accountや、非公開資料への必須依存を混ぜません。OS標準配置・専用service account・loopback等の現行契約は維持します。

LICENSE、第三者notice本文と依存情報を文書削減のために変更しません。公開版/候補の記録、旧実施証拠、実dataは目的に沿って保全し、整理だけで削除・履歴操作・版番号/lock更新を行いません。
