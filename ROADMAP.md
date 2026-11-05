# Roadmap

この文書は将来の目標と対象外を扱います。現行構成は[ARCHITECTURE](docs/ARCHITECTURE.md)、HTTP契約は[API](docs/API.md)、保存形式は[DATA_SCHEMA](docs/DATA_SCHEMA.md)、公開前の開発沿革は[CHANGELOG](CHANGELOG.md)を参照してください。

## 現在地と直近の範囲

v1.0.0にはRoom / Device / IR Adapter、4形式のIR操作、条件付きStateful Schema、Maintenance、標準配置と配布物作成の基盤があり、Release公開済みです。現在はGo更新を含む[v1.0.1候補](docs/releases/v1.0.1.md)を準備しています。公開時は移管後のcommit・asset・checksum・版・noticeと確認範囲を対応付け、公開判断を別に行います。過去候補の実績や未検証範囲は[READMEの確認範囲](README.md#制約と確認範囲)を参照してください。

直近は公開文書の正本と参照関係を整え、その後にtoolsの整理・移管、新リポジトリのcommitからのbuild・梱包・検証を行います。現在未検証のarm64再実行やheat/dry運転を、文書整理の完了条件に追加しません。旧Trashの物理最終削除は保持期限と別確認を要する独立した運用判断です。

## 拡張の優先順

以下は未実装の計画です。版名は計画上の目安であり、提供時期・HTTP route・保存JSONを確約しません。

| 順序・目安 | 目標 | 設計時に決める範囲 |
|---|---|---|
| 1 / v1.1 | ECHONET Lite Adapter基盤 | 機器探索PoC、EOJ/機器識別、状態取得、Deviceとの関連付け |
| 2 / v1.2 | ECHONET Liteエアコン操作 | 電源・mode・温度・風量の取得/設定とエラー処理 |
| 3 / v1.3 | 共通Capability API | IR / ECHONET Lite / Matterの操作モデルと状態の意味、Adapter間の差異 |
| 4 / v1.4 | Matter Adapter PoC | Device Typeと内部Capabilityの対応、DeviceService経由の操作 |
| 5 / v1.5 | Matter Controller連携 | 照明・エアコン・スイッチ相当Commandを対象候補にした相互運用 |

共通CapabilityではDevice単位の`GET/POST .../state`等が候補ですが、現行routeではありません。IRは登録済みCommand/Stateを選んで送信し、ECHONET Liteはプロパティを取得/設定するという性質の違いがあります。送信履歴や推定状態を、家電から取得した実状態として表示しない設計が必要です。MatterはIR/ECHONET Liteを直接呼ばず、共通のDeviceServiceを経由する構想です。

## 未確定の保存・表示案

- ECHONET LiteのEOJ・instance・探索結果、Matterのendpoint・Device Type・bridge設定をDeviceのAdapter別領域へ持つ構想があります。保存属性・ファイル名・互換性は未確定です。Deviceの`primary_adapter`に識別文字列を保存できても、実行Adapterが存在する意味にはなりません。
- System Settings、既定Room、複数controller targetの管理は未実装です。現行の共通configと混同しません。
- Schema全体の機種・リモコン・説明書・未確認事項のメモは未実装です。現行のfield別`description`とは別に保存場所・HTTP互換性・Backup/readback・表示先を設計します。Schema rootへ予約名のfieldを勝手に追加しません。

これらの案は、実装時にAPI・DATA_SCHEMA・STATEFUL_SCHEMA等へ現行契約として反映します。未実装のJSONを現行設定例として配布しません。

## v1.0.0の対象外

ECHONET Lite、Matter、高度な自動化、スケジュール実行、ユーザー認証、複数Picoの完全管理、スマホUIの完全最適化は対象外です。追加protocol encoder、非38kHz RAW実行も現行の提供範囲に含みません。新機能は文書整理から切り離し、調査・互換性・確認範囲を定めて扱います。
