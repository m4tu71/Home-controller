# 変更履歴

現在はv1.0.0の未公開候補です。以下の旧版は**公開前の開発沿革**であり、新しい公開リポジトリに過去Releaseが存在することを示しません。公開後の変更も本書へ要約し、候補固有の検証範囲や制約は各Release notesへ記録します。

## v1.0.0 candidate（未公開）

- 共通configと必須の先頭--config、loopback HTTP、標準systemd/Nginx配置を導入。
- Stateful Schemaへ条件別の必須/任意/適用外、候補・数値範囲/刻み、表示名/説明/単位を追加。通常温度と補正を分離し、省略と0/false/空文字を区別。
- FrontendでSchema条件編集、State入力と日常表示・送信確認を提供。既存Stateと衝突するSchema更新や登録済み信号の不整合を拒否。
- 4形式のIR/delivery、Maintenance/Backup/Trashの契約を維持し、両arch binary・共通web・Pico 2 H UF2、LICENSE/notice・設置例を含む配布候補を生成。
- 公開に向け文書の役割と正本を整理。新リポジトリ候補e7318deからのclean build/梱包・ローカル検証を完了。最終公開commit/tag・Release公開は未確定。
- 開発依存minimatchの修正版を限定overrideし、2026-11-04のnpm監査は全依存/実行時依存とも検出0件。Vite/Vitest設定の将来互換性警告を修正。

過去候補のamd64実行/本番確認と、更新候補arm64・heat/dry等の未検証範囲は[v1.0.0 Release notes](docs/releases/v1.0.0.md)を参照してください。旧候補の結果を新候補へ流用しません。

## 公開前の開発沿革

| 版・当時の記録日 | 主な導入・互換性変更 |
|---|---|
| v0.11.0 / 2026-09-28 | Room/Device/IR中心の日常UIとMaintenanceを分離。Data Health、planに基づく移行/cleanup/verify/rollback、Backup/隔離復元、Trash/復元、operation履歴とatomic JSONを統合。通常DELETEをTrash移動へ接続。旧IR Signal/Light/status/単独IR/Pico LEDのHTTP導線を廃止し、Device配下へ集約 |
| v0.10.0 / 2026-07-23 | Command/Stateのraw/code、Stateのpreset_commandの編集/送信。PANASONIC_40のbackend_rawとcapability確認済みnative_code、固定send error/delivery/repeat進捗、安全上限を追加。captureはraw_samples専用を維持 |
| v0.9.0 / 2026-07-22 | Stateful型のSchema/State管理とraw_samples学習/送信を追加。Schemaと既存Stateの整合、未学習409・capture不足422、学習情報保護を導入 |
| v0.8.0 / 2026-07-17 | Command型の管理/学習/再学習/送信、preset allowlist、repeat解決と学習情報保護を追加 |
| v0.7.0 / 2026-07-07 | Device配下IR Adapter、config/Command/Schema/Stateのmodel/repository/service/APIとconfig UIを導入。当時の送信準備入口から後続版で実行を拡張 |
| v0.6.0 / 2026-06-30 | Device管理UIとcomponent分割、選択保持、削除範囲の表示、danger色tokenを導入 |
| v0.5.1 / 2026-06-23 | root VERSIONからのFrontend版表示を追加 |
| v0.5.0 / 2026-06-23 | Room管理UI、tab選択/保持、入力/エラー案内を追加 |
| v0.4.0 / 2026-06-22 | Room/Device modelとCRUD、Device配下の保存構造、親子関係とRoom削除保護を導入 |
| v0.3.0 | Backendを単一Go binaryのままpackage責務へ分割し、既存挙動・保存互換を保つ構成へ整理 |
| v0.1.0 | React→Go→daemon→USB Serial→PicoのIR操作PoCとsystemd/Nginx構成を導入。旧照明statusは家電からの実状態取得ではなく送信履歴由来の推定だった |

旧sourceのTrash保管と物理最終削除は別の運用判断です。過去の版名/日付から、現在の環境や任意のsnapshotを旧版で復元可能とは推測しません。現行契約は[API](docs/API.md)・[DATA_SCHEMA](docs/DATA_SCHEMA.md)・[STATEFUL_SCHEMA](docs/STATEFUL_SCHEMA.md)を参照してください。
