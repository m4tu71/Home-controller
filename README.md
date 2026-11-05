# Home Controller

ブラウザから部屋・機器を選び、赤外線リモコンの信号を登録・学習・送信するセルフホスト型Home Controllerです。React製の画面、Go製のBackendとcontroller-daemon、Raspberry Pi Pico 2 Hのfirmwareで構成します。

```text
Browser → Nginx → Backend → Unix Domain Socket → controller-daemon → USB Serial → Pico → IR
```

v1.0.0はRelease公開済みです。現在のsourceは、Go標準ライブラリの既知脆弱性に対処する**v1.0.1の未公開候補**です。[v1.0.1 Release notes](docs/releases/v1.0.1.md)に変更点と確認範囲を記載しています。

## 使える機能

- Room / Device / IR Adapterを設定し、ボタン型のCommandと状態型のStateを管理する。
- `raw_samples`を学習し、`raw`・`code`・`preset_command`を含む4形式で明示送信する。
- Stateful Schemaで、条件別の必須・任意・適用外項目、候補・数値範囲・刻み・表示情報を設定する。
- Data Health、保存planに基づくMaintenance、Backupと隔離復元演習、Trashと復元、operation履歴を利用する。

IRの送受信にはPicoと送受信回路が必要です。送信APIの成功は家電の実状態を保証しません。送信失敗時は結果を確認して人が再操作を判断し、自動再送・自動fallbackは行いません。

## 利用を始める

配布物からの設置・更新・復旧は[INSTALL.md](INSTALL.md)、設定例は[examples/README.md](examples/README.md)を参照してください。標準構成はLinuxのsystemd/Nginx、専用service user、loopback待受のBackendです。Go/Node/Pico SDKは配布済みbinaryとwebの実行には不要です。

日常保守は[運用手順](docs/OPERATIONS.md)、症状別の確認は[Troubleshooting](docs/TROUBLESHOOTING.md)を参照してください。OSへの反映・Pico書込み・実dataやIRの操作は、対象と手順を確認した運用者が行います。

## 制約と確認範囲

単一のローカル運用者を想定し、ユーザー認証は実装していません。認証なしAPIをインターネットへ直接公開しないでください。ECHONET Lite、Matter、スケジュール実行、高度な自動化、複数Picoの完全管理は未対応です。

Linux amd64/arm64向けbuild・梱包に対応します。v1.0.1の正式候補build/梱包は未実施で、Go更新のローカル検証とは区別します。v1.0.0候補`e7318de`では両archの生成・検査とamd64の隔離smokeを確認しました。同候補の実設置・Browser・家電動作は未確認で、arm64実行再検証は今回対象外です。過去の更新候補020c35bはUbuntu 24.04 amd64で確認し、同候補arm64の実行再検証は未実施です。旧候補の結果を流用しません。採用実機のheat/dry運転、OS再起動・本番plan正常apply等の対象外と、auto内部mode/実測温度の未確認事項を含む詳細は[v1.0.0 Release notes](docs/releases/v1.0.0.md)を参照してください。過去候補の実績と新候補の確認範囲を区別します。

RAW実行は現在38kHz、codeはPANASONIC_40に対応します。captureは`raw_samples`専用です。詳細は[HTTP API](docs/API.md)を参照してください。

## 開発・仕様を読む

| 文書 | 役割 |
|---|---|
| [DEVELOPMENT](docs/DEVELOPMENT.md) | 開発環境、build/test/lint |
| [RELEASING](docs/RELEASING.md) | 配布物のbuild・梱包・検査と公開判断 |
| [IR_GUIDE](docs/IR_GUIDE.md) | IR設定・学習・明示送信と実機確認 |
| [ARCHITECTURE](docs/ARCHITECTURE.md) | 構成、package責務、要求処理・永続化・保守の流れ |
| [API](docs/API.md) | 現行HTTP route、入出力・エラー・送信結果 |
| [DATA_SCHEMA](docs/DATA_SCHEMA.md) | 保存先、JSON属性・型 |
| [STATEFUL_SCHEMA](docs/STATEFUL_SCHEMA.md) | 条件判定・互換性・学習済み信号の保護 |
| [DESIGN_SYSTEM](docs/DESIGN_SYSTEM.md) | UI・CSS・用語・操作規約 |
| [AGENTS](AGENTS.md) | AI作業の参照案内・実行境界・確認/報告規約 |
| [版記録の運用](docs/versions/README.md) | 今後の調査・Step計画・実施記録 |
| [ROADMAP](ROADMAP.md) | 将来計画と対象外 |

版の変更要約は[CHANGELOG](CHANGELOG.md)、ライセンスは[LICENSE](LICENSE)、第三者noticeは[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES.md)を参照してください。
