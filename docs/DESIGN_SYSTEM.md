# Design System

Home ControllerのUI・CSS・用語・操作の共通規約です。実用的で落ち着いた家電操作パネルとして、Room→Device→Command/Stateの対象と次の操作を明確にします。一般のAI作業規約は[AGENTS](../AGENTS.md)、開発確認は[DEVELOPMENT](DEVELOPMENT.md)を参照してください。

HTTP/送信結果は[API](API.md)、保存属性は[DATA_SCHEMA](DATA_SCHEMA.md)、条件判定・互換性は[STATEFUL_SCHEMA](STATEFUL_SCHEMA.md)が正本です。この文書で新しいAPI/保存仕様を定義しません。

## TokensとCSS

値は[variables.css](../frontend/src/styles/variables.css)の現行定義です。見た目の値を変更するときはsourceと本表を同時に見直します。定義を各componentへ複製せず、既存tokenとclassを優先します。

| Token | 値 | 用途 |
|---|---|---|
| --color-bg | #f0f0f0 | 全体背景 |
| --color-surface | #ffffff | card/panel/フォームの面 |
| --color-text | #222222 | 通常文字 |
| --color-text-muted | #666666 | 補足/metadata |
| --color-border | #bdbdbd | 枠と区切り |
| --color-primary | #3366cc | 主要操作/リンク/focus |
| --color-primary-hover | #2a52a3 | 主要操作hover |
| --color-danger | #b00020 | 危険操作/error |
| --spacing-xs | 2px | 小さな隙間 |
| --spacing-sm | 6px | 小要素間/button内 |
| --spacing-md | 12px | 標準間隔 |
| --spacing-lg | 18px | section間 |
| --spacing-xl | 24px | page block |
| --radius-sm | 4px | button/input |
| --radius-md | 6px | card/panel |
| --shadow-sm | none | 現行はshadowなし |
| --max-width | 960px | 主content幅 |
| --font-size-sm | 13px | 補足/metadata |
| --font-size-md | 14px | 標準本文/操作向けtoken |
| --font-size-lg | 22px | section見出し |
| --font-size-xl | 28px | page見出し |

[global.css](../frontend/src/styles/global.css)はborder-box、html 16px、body line-height 1.5、system font fallback、button/input等のfont継承を定義します。全本文が14pxで固定されるわけではありません。主要buttonは最小高さ44px、focus-visibleはprimaryのoutlineです。

CSS Modulesでcomponent責務を分け、同じ意味のclass/似たbutton style/独自色や余白を増やしません。cardはsurface・border・余白で境界を示し、現行はshadowを加えません。tokenや共通patternの追加は、既存要素で足りない理由・用途・影響を記録して設計判断を行います。意味のないanimation/装飾を追加しません。

## 画面と情報階層

| 画面 | 役割 |
|---|---|
| /、/rooms/:roomId | 日常操作。Room→Device→Command/Stateを選び専用buttonで送信 |
| /settings | Room/Device、IR config/Schema、Command/Stateの作成・編集・削除・学習 |
| /maintenance | Data Health、migration/cleanup、Backup、operation履歴 |
| /trash | Trashの一覧/詳細/復元 |
| /design-system | 現行の見た目の参照画面 |

routeは[App.tsx](../frontend/src/App.tsx)に対応します。旧/web-controllerと/raspy-timeはHomeへredirectします。Homeへ管理フォームや保守/Trashの取得を混ぜません。日常操作でAdapter内部情報を前面に出し過ぎず、設定ではIR方式と保存対象を明示します。将来Adapterを現在利用できる選択機能として描きません。

Header左はHomeへ戻る「Home Controller」の文字ブランドで、独立したロゴ/専用色/バッジを追加しません。HeaderのページnavはHome/Maintenance/Trash、Settingsへは各画面の設定導線を使います。右の時刻は初回取得・60秒ごとの再同期を行い、ブラウザ側で毎秒進めます。初回失敗は文字で示し、取得済み後の再同期失敗では既存時計を維持します。時刻だけの専用ページや見えるTimeラベルは置かず、アクセス可能な名前は残します。Footerは版とDesign Systemへの入口を表示します。

Roomは選択を明示しDevice数を示します。Deviceは名前・種別・利用可否をcardで示し、詳細設定と日常操作を分けます。通常件数（Room 2、選択Room内Device 2、操作6）では1280×800の初期画面に選択/操作を収めることを設計目標とします。全件数/環境の保証ではありません。mobileは縦スクロールを許し、320px幅/拡大時も横はみ出しや操作不能を避けます。

青文字のページ/画面内リンクは下線をhover時も維持します。Header/Footerの通常文字色リンクとブランドは下線なしです。並列ページリンクは折返し可能にし、既存spacing-md以上の間隔を使います。

## Button・Card・Form

主要操作には既存primaryButton、危険操作にはdangerButton、補助操作には既存button/statusButton等を使います。これらは現在のCSS classであり、汎用Button componentにprimary/secondary/danger/ghostというvariant APIが実装されているという意味ではありません。

対象名と動作を短く明示し、保存/送信/削除を混同しません。disabledの理由は文字で近くに示します。cardは見出し・情報・操作を分離し、button内へ別buttonを入れません。フォームにはlabelを付け、placeholderだけに頼らず、field近くへ入力エラーと次の操作を示します。保存buttonはform末尾を基本とします。

## 情報popover

[InfoPopover](../frontend/src/pages/web-controller/components/InfoPopover.tsx)を使い、補足情報を選択/送信から独立した「情報」buttonで開きます。非モーダルでhover/focusとtap/Enter/Spaceに対応し、Escapeで閉じてtriggerへfocusを戻します。aria-label/expanded/controlsとpanel見出しを保ち、画面端では配置/最大高を調整し内部スクロールを使います。

- Room情報は選択中Roomに対して見出し側に1個置く。Device欄でRoomと件数を重複表示しない。
- Device情報はcard見出し側、Command情報はcard選択buttonの兄弟要素として右上に置く。情報buttonで選択/送信しない。
- State情報は操作button上側右端。開いたpanelを周囲card/情報buttonより前へ表示する。
- 操作対象/操作名、送信不能理由、重大errorをpopoverだけへ隠さない。

mouse/touch/keyboard、focus復帰、狭幅/画面端、選択・送信の誤発火を確認します。詳細styleは[専用CSS](../frontend/src/pages/web-controller/components/InfoPopover.module.css)を使います。

## Schemaの用語と編集順

操作はリモコンの基本項目・型・候補・範囲から始め、必要な場合に任意入力/適用外/刻み/条件別制約を追加します。保存互換の内部区分を操作の前提にしません。raw JSONを直接編集するフォームに置換しません。

| 内部語 | 画面の説明 |
|---|---|
| field / field ID | 項目 / 保存する項目ID |
| selector | 条件に使う項目 |
| case | 条件別の設定 |
| presence | 入力の扱い（必須/任意/使わない） |
| min / max / step | 最小値 / 最大値 / 刻み |
| label / description / unit | 表示名 / 説明・リモコンのメモ / 単位 |

日本語の説明を添え、enum/number/boolean/stringを補足表示します。保存ID・API属性・enum候補そのものは翻訳して変えません。候補は番号・入力・右端削除を同じ1行に並べ、カンマ区切りにしません。空欄候補が空文字であることと、前後空白を保存することを案内します。

表示情報は任意の補助設定にまとめ、descriptionは複数行メモとしてState入力にも表示します。unitは表示用で値を変換しません。Schema全体メモは未実装で、fieldのdescriptionとは区別します。

条件入力ではANDの条件行とOR候補集合、boolean型と文字列、未指定と明示false/空文字を区別します。型変更/拡張切替で除去する制約、Schema変更による既存Stateへの影響を保存前に確認し、400/409のissuesをfield/case/State別に表示してdraftを保持します。templateは明示適用だけで、保存Deviceを自動変更しません。

State入力はSchemaの適用結果に応じて候補・範囲・刻み・単位とrequired/optional/absentを示します。適用外draftの除外field/valueを保存前に確認し、hidden値を送信しません。Home/送信確認には適用fieldのlabel/value/unit、情報にはfield IDと保存値を残します。意味を変える登録済みState編集は無効化し、新State作成を案内します。

## IR形式と送信確認

Command/Stateの形式入力は[IROperationFields](../frontend/src/pages/web-controller/components/IROperationFields.tsx)を共用します。format変更による旧属性の扱いを近くに案内します。raw_samples学習情報は通常フォームで編集せず、captureはraw_samplesだけに示します。rawはtimings/countを直接登録でき、件数・合計時間・単値上限の案内を維持します。presetはallowlist select、codeはprotocol/codeと実行方式候補を示します。

codeのbackend_raw/native_codeは利用可否/reasonを示し、人が選びます。未学習、Schema未取得/不適合、実効carrier非対応、経路未選択等の操作停止理由を送信前に表示します。送信は専用buttonと確認、再学習は学習済み対象の置換確認を行います。情報表示/選択/再読込だけで送信しません。

loading/error/empty/successを区別し、状態や危険性を色だけで伝えません。送信失敗はdelivery_stateと確認済みrepeat数、別経路候補、人の実機確認が必要なことを文章で示します。sentを家電の実状態表示に変換せず、自動再送/fallbackを加えません。

## Maintenanceと変更時の確認

Maintenance/Trashでは対象path/ID、dry-runの影響、backupの予定/作成済み状態、復元可否、operation/Trash識別子と結果を示します。候補表示やHTTP成功だけから適用/復旧完了を推測しません。最終物理削除のFrontend操作は設けません。手順・判断は[OPERATIONS](OPERATIONS.md)へ参照します。

UI変更時は既存token/componentの再利用、表記・対象・確認動作、loading/error/empty/success、keyboard/focus、320px/拡大/desktop配置を確認します。変更範囲に応じてFrontend test/lint/buildを行い、Browser/実機は必要な対象だけを運用者が確認します。token追加や操作体系の変更を、文書整理のための実装修正として行いません。将来案は[ROADMAP](../ROADMAP.md)で扱います。
