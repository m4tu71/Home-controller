# Stateful Schema

Stateful型IR機器の条件判定・検証・互換性・登録済み信号の保護を定めます。保存先と属性・型は[DATA_SCHEMA](DATA_SCHEMA.md#state-schema)、HTTP route/status/応答は[API](API.md#state-schemaとstate)が正本です。

Schemaは機種名や特定field IDに依存しません。通常の設定温度と温度補正など、意味・単位・操作方法が違う値は別fieldで表します。同じ意味の値の範囲が条件で変わる場合は同じfieldのcaseを使えます。SchemaからIR波形を生成せず、Stateの意味と学習・登録した信号の対応を運用者が確認します。

## 条件の解決

1. 非空のSchema object、field ID/type、制約、selectorとcaseの重複を検証します。
2. Stateのselector値を検証し、各fieldの一致caseを決めます。どれも一致しなければbaseを使います。不正/未入力のselectorから適用条件を推測しません。
3. 一致caseではpresenceと型別制約を**一式置換**します。baseのmin/max/step/values/allow_emptyを部分継承しません。label/description/unitだけはcaseで省略するとbaseを継承します。
4. requiredはkey必須、optionalは省略可能で存在する値には検証を適用、absentはkeyの存在自体を拒否します。未知fieldも拒否します。
5. 値をenum/number/boolean/stringと解決済みの制約に照合します。省略はnull・0・false・空文字とは別です。nullはStateの型不適合であり、0/false/空文字は型と制約に合う場合だけ有効です。

すべてのfieldを省略可能なSchemaでは`state: {}`も有効です。`state: null`はobjectになりません。Schema自体は空objectにできません。

### selectorとcase

`when.all`内のselectorはAND、各`in`内の値はORです。selectorは**caseを持たない、無条件でrequiredのenumまたはboolean field**だけを参照できます。明示的な`cases: []`もcaseを持つ扱いでselectorにはできません。

自己参照・未知field/候補・循環を作る参照、同一caseでの同じselectorの重複、空のall/in、型違い・重複したin値を拒否します。booleanのfalseと文字列`"false"`は別です。

同じfieldの2 caseが同時成立し得るSchemaは拒否し、宣言順の優先順位を作りません。共通selectorの候補集合が交差し、片方だけのselectorにも有効値がある場合は重複です。異なるfieldのcaseが同時に成立することは正常です。baseへ到達しないほど条件を列挙すること自体は許可します。

### 型別制約の置換例

| base / 一致case | 結果 |
|---|---|
| enum base valuesがa、caseでvalues省略 | Schemaを拒否。baseの候補を継承しない |
| enum case valuesが空文字1件 | 空文字を有効候補として保持。空配列とは違う |
| number base min=0/max=10/step=2、case min=-2のみ | -2以上、上限・刻みなし |
| string base allow_empty=false、caseで省略 | caseは空文字を許可 |
| presence=absent | State keyは省略だけを許可し、Schemaに型別制約があれば拒否 |
| case labelを省略 / 空文字を指定 | 省略はbase継承、空文字は表示文言を消す。空labelはfield ID表示 |

拡張契約ではenumに非空の文字列候補、numberに有限min/maxと正のstep、stringにallow_emptyを使います。booleanに型別の値制約はありません。型に合わない制約は拒否します。候補は空文字を含め完全一致で保持し、trim/重複整理を行いません。拡張enumの重複候補は拒否します。

### 十進数の刻み

stepは有効なminを起点、minなしでは0を起点にします。最短の十進表現を有理数として扱い、binary floatの剰余やepsilonによる曖昧な許容を使いません。

| 制約 | 有効例 | 不適合例 |
|---|---|---|
| min=0.1、max=0.5、step=0.2 | 0.1 / 0.3 / 0.5 | 0.2 |
| minなし、step=0.1 | -0.2 / 0 / 0.3 | 0.35 |
| stepなし | min/max内の有限数値 | 範囲外/非有限値 |

min > max、step <= 0、有限の値を選べない数値制約はSchemaとして拒否します。numberに整数だけという暗黙条件はありません。

## 旧Schemaとの互換性

field単位で、`presence`・`step`・`allow_empty`・`cases`のいずれかを持つ場合に拡張契約を適用します。label/description/unitだけの追加は旧契約を変えません。表示文字列をHTMLとして解釈せず、型・適用状態・値制約を推測しません。動作属性のない旧fieldは全field必須として読み、起動時の一括変換をしません。

旧契約ではenumは非空values、numberはmin/maxをState検証に使い、string/booleanは無関係な旧values/min/maxを無視して保持します。ただしmin/maxが両方ある場合の大小検査は型にかかわらず残ります。旧契約の重複候補も自動整理しません。

| 旧fieldと変更 | 扱い |
|---|---|
| stringにmin=0、Stateは文字列 | 読取り・再保存可能。minを文字長制約にしない |
| numberにvalues=["unused"]、Stateは1 | 読取り・再保存可能。valuesをnumber制約にしない |
| stringにmin=2/max=1 | 旧契約でもSchema不正 |
| stringにmin=0、labelだけ追加 | 旧属性を保持し旧契約で扱う |
| 同じfieldへpresence=requiredを追加 | 拡張契約になり、型に無関係なminがあるため拒否 |
| 無関係な属性の除去を明示確認して拡張へ切替 | 新Schemaで既存Stateも検証し、適合する場合に保存 |

baseのpresence省略やallow_empty省略を保存時に自動補完しません。明示的なfalse・空表示文字列・空cases/values配列を省略と混同しません。

追加属性のnullを省略へ変換しません。Go decoderが認める属性名の大小文字違いでも、各生keyにnull拒否を適用します。拡張baseおよびcaseのvaluesはJSON文字列だけを許し、nullを空文字へ変換して受理しません。旧属性のdecode互換は維持します。未知属性を黙って落としてGET→PUTで失わせません。

## 保存と学習と送信時の保護

State POST/PUTはSchemaに照合し、Schema PUTは保存済みStateすべてとの整合性を確認します。衝突時はSchema/State/capturesを変更しません。capture/sendも保存済みStateを再検証してから機器を呼び、不適合なら開始しません。破損・読取り不能なStateを無視してSchemaを更新しません。

`raw_samples`のcaptures/timings_us/countはcapture APIの成功時だけ一括更新します。通常のPOST/PUTでは学習情報を直接変更できません。学習失敗時には以前の信号を保持します。HTTP切断や画面上のcancelだけから、Backendの処理中断や未保存を断定しません。

学習済みraw_samplesと登録済みraw/code/preset_commandでは、State値・format・carrier・登録信号の内容を変えるPUTを拒否します。名前・保存repeatなど信号の意味を変えない更新、同じ正規化済み内容のPUT、raw_samplesの再学習は可能です。内容を変えるには新Stateを作成します。未学習raw_samplesのformat変更は変更先の有効な信号属性がある場合に許可します。

### 既存Deviceから条件Schemaへ移る場合

旧Stateに仮の温度があり新Schemaでは適用外になる場合、新Schemaを先に保存すると衝突します。登録済みStateの値を先に変えて波形との対応を崩すこともできません。

1. 運用者が既存Schema/State/学習情報/configを読み、対象とBackup・復元方法を確認する。
2. 旧Deviceを保持したまま別IDのDeviceを作り、明示的にconfigと新Schemaを設定する。既存波形を意味の推測でコピーしない。
3. 新IDのStateを作り実リモコンで学習する。API結果と家電反応を分け、未学習Stateは日常操作へ切り替えない。
4. 全採用Stateと停止・サービス再起動後readbackを確認して使用先を切り替える。旧DeviceとBackupを保持し、削除は別判断とする。

失敗時は切替を止め、旧Schema/State/capturesを変更しません。切替後に旧Deviceへ戻す場合も、実リモコンとの対応が確認できたStateだけを利用します。新Deviceを自動削除しません。同一Device内の移行が必要なら、別の移行手順・隔離復元演習・rollback確認を用意してから実dataを扱います。

## Frontendと保守での共通扱い

Schema editorはbase/caseと表示情報を編集し、旧省略属性・無関係な旧属性を保持します。型変更や拡張切替で除去する属性を明示確認し、保存前に既存State ID/値/学習状態と新制約への適合を確認します。templateはdraftへの明示適用であり、既存Deviceを自動変更しません。

State formはselectorの解決に応じてpresence・候補・範囲・stepを表示します。適用外draftは画面内に保持できますが、除外するfield/valueを保存前に確認してpayloadから省きます。任意fieldの省略・空文字候補・未選択・0・falseを区別します。Schema未取得/不整合や保存State不整合では学習/送信を止め、未学習raw_samplesでは送信を止めます。日常表示と送信確認はlabel/value/unitを使い、field IDも確認可能にします。UI規約は[DESIGN_SYSTEM](DESIGN_SYSTEM.md)を参照してください。

Data HealthはSchema不正/取得不能・State不正/不適合を診断し、自動修正しません。Backup隔離復元は対応Schemaを含むvalid JSONに意味的readbackを行い、不整合や未知formatで検証済みへ昇格しません。旧Backup snapshot/inventory・Trash payloadを自動変換せず、registryだけを通常の状態遷移で更新します。

## 機種に依存しない例

以下は合成例です。実機設定へそのまま適用せず、機器が選べる値と信号の意味を確認してください。

```json
{
  "enabled": {"type": "boolean", "label": "動作"},
  "profile": {"type": "enum", "values": ["normal", "boost"]},
  "level": {
    "type": "number", "presence": "absent",
    "cases": [
      {"when": {"all": [{"field": "enabled", "in": [true]}, {"field": "profile", "in": ["normal"]}]}, "presence": "required", "min": 0, "max": 5, "step": 1},
      {"when": {"all": [{"field": "enabled", "in": [true]}, {"field": "profile", "in": ["boost"]}]}, "presence": "required", "min": 6, "max": 10, "step": 1}
    ]
  },
  "eco": {"type": "boolean", "presence": "optional"},
  "note": {"type": "string", "presence": "optional", "allow_empty": false}
}
```

| State値 | 結果 |
|---|---|
| `{"enabled":false,"profile":"normal"}` | 有効。level/eco/note省略 |
| `{"enabled":true,"profile":"normal","level":0,"eco":false}` | 有効。0/falseを保持 |
| `{"enabled":true,"profile":"boost","level":6}` | 有効 |
| `{"enabled":false,"profile":"normal","level":0}` | absent違反 |
| `{"enabled":true,"profile":"normal"}` | required違反 |
| `{"enabled":true,"profile":"boost","level":5}` | range違反 |
| `{"enabled":false,"profile":"normal","note":""}` | empty違反 |
| `{"enabled":"false","profile":"normal"}` | type違反 |

`{"note":{"type":"string","presence":"optional"}}`という別Schemaなら、`{}`と`{"note":""}`はともに有効で、別のState値として保持されます。

評価の実装は[Schema service](../backend/services/ir_state_schema.go)、省略/codecは[Schema model](../backend/models/ir_state_schema.go)、登録信号保護は[protection](../backend/services/ir_state_protection.go)、Backend/Frontendの共有確認例は[contract fixture](../backend/services/testdata/ir_state_schema_contract.json)を参照してください。
