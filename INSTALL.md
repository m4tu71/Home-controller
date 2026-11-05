# Home Controller v1.0.1 設置・更新手順

## 対象・検証状態

本手順はv1.0.1候補の標準配置を扱います。正式archiveのbuild/梱包は未実施です。標準配置はLinux向けのsystemd/Nginx構成で、以下の表はv1.0.0以前の検証結果です。v1.0.1の設置済み・実機確認済みとして流用しません。

| 候補・確認日 | OS / architecture | 確認範囲 |
|---|---|---|
| 新リポジトリ候補e7318de / 2026-11-04 | Linux amd64・arm64 | clean build/梱包・ELF/UF2検査・checksum照合。amd64隔離smokeとoffline設置設定検査成功。実設置/Browser/家電は未確認、arm64実行再検証は対象外 |
| 更新候補020c35b / 2026-11-02 | Ubuntu 24.04.5 LTS / amd64 | WSL2の独立fixture・切替/復旧回帰、本番hostの新配置/health・サービス再起動後API/画面・採用実機 |
| 更新候補020c35b | Linux arm64 | build/梱包のみ。実行再検証は今回は対象外・未検証 |
| 旧候補cdd7ca2 / 2026-10-20 | Ubuntu 24.04.5 LTS / amd64・arm64、Ubuntu 26.04.1 LTS / amd64 | archiveからの新規設置・Picoなし起動・非送信CRUD/削除/保守読取り・サービス再起動後readback |

旧候補のarm64/Ubuntu 26.04の成功を更新候補の検証済み表示に流用しません。他distribution・未確認kernelの動作も保証しません。文書を更新して再梱包した場合、binary/web/UF2の一致と設定例の整合を再検査し、archiveのchecksumはその梱包に対応するSHA256SUMSを使用します。v1.0.0はbuild e7318de・再梱包65bff34をmain550adb5へ対応付けてRelease公開済みです。v1.0.1はGo更新を含む別候補として、clean commitからのbuild/梱包・checksum確認後に公開を判断します。

対象PicoはPico 2 H（build board名pico2）。IR用の送受信回路が必要。RAW送信は現行38kHzのみ。UF2の書込み・家電の動作確認はManual only。Picoなしでもdaemon/Backendと通常APIは起動できるが、controller healthの成功やIR操作は保証しない。

この手順はsource checkoutを使わない新規設置と、末尾の更新・復旧を扱う。旧配置からの移行は更新前に実効unit/config/socket/配信先と4 rootの対応を確定する。v0.11.0は過去snapshotに限った互換性記録であり、条件付きSchemaを含む現dataの一般的な復旧先として使わない。旧serviceと新unitを同じport/socketで同時起動しない。

**以下のsudo、systemctl、Nginx反映、USB操作、実data設定は運用者が手動実行する。** v1.0.1の配布予定はMIT LICENSE・第三者noticeを含む両arch archive、独立UF2、SHA256SUMSです。以下の手順は正式候補生成後に同じ版・同じReleaseの配布物を使って実行します。旧候補の確認範囲は上表を参照してください。

## 1. 必要なソフトと配置

Ubuntuにsystemd、Nginx、curl、tar、sha256sum、USB serialのdialout groupが必要。binary/webの実行にGo、Node、Pico SDK、source checkoutは不要。

```bash
sudo apt update
sudo apt install nginx curl ca-certificates
uname -m
```

`x86_64`にはlinux-amd64、`aarch64`にはlinux-arm64のarchiveを使う。それ以外のarchへ流用しない。

```text
/opt/web-controller/releases/v1.0.1/{bin,web,examples,INSTALL.md,LICENSE,THIRD_PARTY_NOTICES.md,VERSION}
/opt/web-controller/current -> /opt/web-controller/releases/v1.0.1
/etc/web-controller/config.json
/etc/systemd/system/web-controller-{backend,daemon}.service
/etc/nginx/sites-available/web-controller.conf
/var/lib/web-controller/{data,maintenance,backups,trash}
/var/lib/web-controller/maintenance/{plans,operations}
/run/web-controller/controller-daemon.sock
```

## 2. archive確認・展開（新規設置）

対応archのarchiveと同じReleaseのSHA256SUMSをdownloadする。次のpathは実際のdownload先へ置き換える。別archや違う版のassetを混ぜない。次のblockは対象archiveのchecksumを検査し、一覧表示後に確認入力を待つ。最上位が`v1.0.1/`で想定fileのみを含み、絶対pathや`..`がないことを確認してから、端末で`expand`と入力する。それ以外の入力・入力失敗、checksum行の欠落・重複・不一致では停止し、展開しない。

```bash
(
  set -e
  cd /absolute/path/to/downloads
  release_name=web-controller-v1.0.1-linux-amd64.tar.gz
  # arm64環境では上のfilenameをlinux-arm64へ変更
  release_archive="$PWD/$release_name"
  release_dir=/opt/web-controller/releases/v1.0.1
  test -f "$release_archive"
  archive_checksum=$(awk -v name="$release_name" \
    '$2 == name { print; found++ } END { if (found != 1) exit 1 }' SHA256SUMS)
  sha256sum --check --strict <<< "$archive_checksum"
  tar -tzf "$release_archive"
  read -r -p '一覧を確認しましたか？ 展開する場合だけ expand と入力: ' archive_confirmation </dev/tty
  test "$archive_confirmation" = expand
  sudo test ! -e "$release_dir"
  sudo test ! -e /opt/web-controller/current
  sudo test ! -L /opt/web-controller/current
  sudo install -d -o root -g root -m 0755 /opt/web-controller/releases
  sudo tar -xzf "$release_archive" -C /opt/web-controller/releases --no-same-owner
  sudo chown -R root:root "$release_dir"
  sudo find "$release_dir" -type d -exec chmod 0755 {} +
  sudo find "$release_dir" -type f -exec chmod 0644 {} +
  sudo chmod 0755 "$release_dir/bin/backend" "$release_dir/bin/controller-daemon"
  sudo ln -s "$release_dir" /opt/web-controller/current
)
```

展開後はVERSIONが1.0.1で、bin/webが二重directoryの内側にないことを確認する。失敗時は途中の展開を勝手に消さず、既存配置・所有者・archive内容を確認する。

## 3. service user・runtime/保守root

専用の同一user/primary groupを両processで共有する。新規user作成前に既存accountを確認する。

```bash
getent passwd web-controller
getent group web-controller
# accountが存在しない新規設置でのみ実行
sudo useradd --system --user-group --home-dir /var/lib/web-controller --no-create-home --shell /usr/sbin/nologin web-controller
sudo install -d -o root -g root -m 0755 /var/lib/web-controller
sudo install -d -o web-controller -g web-controller -m 0700 \
  /var/lib/web-controller/data \
  /var/lib/web-controller/maintenance \
  /var/lib/web-controller/maintenance/plans \
  /var/lib/web-controller/maintenance/operations \
  /var/lib/web-controller/backups \
  /var/lib/web-controller/trash
stat -c '%d %U:%G %a %n' /var/lib/web-controller/data /var/lib/web-controller/trash
```

data/trashのdevice番号は一致させる（atomic rename条件）。これらのrootと祖先にsymlinkを使わず、protected root同士を重ねない。softwareのcurrent symlinkはrootだけが管理し、実data/保守rootと分離する。既存rootがある環境で上記所有者/mode変更を無条件適用しない。

## 4. 共通config・Pico by-id

```bash
sudo install -d -o root -g web-controller -m 0750 /etc/web-controller
if sudo test -e /etc/web-controller/config.json; then
  echo '既存configを保持します。新規exampleで上書きしないでください。'
else
  sudo install -o root -g web-controller -m 0640 \
    /opt/web-controller/current/examples/config.example.json \
    /etc/web-controller/config.json
fi
ls -l /dev/serial/by-id/
sudoedit /etc/web-controller/config.json
```

対象Picoを特定し、pico_serial_pathのCHANGE_MEを実際のby-idへ置換する。ttyACM0等の列挙番号や別機器のIDを推測しない。configの全keyは[examples/README.md](examples/README.md)参照。JSONのunknown key/trailing value、相対path、placeholder、root重複、不正mode/所有者を起動時に拒否する。configはroot:web-controller 0640を維持する。

Picoなしの使い捨て設置検証に限り、`/dev/serial/by-id/validation-not-connected`を未接続検証値として使える（実機の識別子ではない）。通常API起動を確認し、実機設置前に実by-idへ変更する。Pico未接続でもconfigには有効な形式のby-id pathが必要で、CHANGE_MEのままでは起動しない。

```bash
sudo -u web-controller test -r /etc/web-controller/config.json
# 実機接続後: readlink結果とowner/group/modeを確認
readlink -f /dev/serial/by-id/ACTUAL_DEVICE_ID
ls -l /dev/serial/by-id/ACTUAL_DEVICE_ID
```

daemon unitのSupplementaryGroups=dialoutでserialアクセスを与える。権限不足時は実ttyのgroupとudev設定を確認し、chmod 666で回避しない。

## 5. systemd（daemon → Backend）

```bash
sudo install -m 0644 /opt/web-controller/current/examples/systemd/web-controller-daemon.service /etc/systemd/system/web-controller-daemon.service
sudo install -m 0644 /opt/web-controller/current/examples/systemd/web-controller-backend.service /etc/systemd/system/web-controller-backend.service
sudo systemd-analyze verify /etc/systemd/system/web-controller-daemon.service /etc/systemd/system/web-controller-backend.service
sudo systemctl daemon-reload
sudo systemctl enable --now web-controller-daemon.service
sudo systemctl enable --now web-controller-backend.service
sudo systemctl --no-pager --full status web-controller-daemon.service web-controller-backend.service
```

daemonのRuntimeDirectory=web-controllerにより/run/web-controllerをuser/primary group=web-controller、0750で作成する。socket本体はdaemonが0660へ設定。BackendはdaemonをWants/AfterとするがRequiresにしないので、daemon不在でも通常APIを提供する。Type=simple/AfterはPicoの応答準備完了まで保証しない。起動直後のhealthは短時間待って確認する。

```bash
curl --fail --silent --show-error http://127.0.0.1:3000/api/health
curl --silent --show-error --write-out '\nHTTP %{http_code}\n' http://127.0.0.1:3000/api/health/controller
sudo ls -ld /run/web-controller
sudo ls -l /run/web-controller/controller-daemon.sock
```

通常healthは200/healthyが必要。Pico/daemon未接続時のcontroller healthは503があり得るので別に記録し、通常API不良と混同しない。IR capture/sendは未接続なら使えない。

## 6. Nginx・SPA fallback

先に既存Nginx設定をBackupする。以下はUbuntu標準default siteのsymlinkがある新規専用host用で、他siteや既存の本サービス設定を変更しない。検査失敗時はblock全体を停止し、Backup先/設定/既存linkを確認する。

```bash
(
  set -e
  sudo test ! -e /etc/nginx.before-web-controller
  sudo test ! -L /etc/nginx.before-web-controller
  sudo test ! -e /etc/nginx/sites-available/web-controller.conf
  sudo test ! -L /etc/nginx/sites-available/web-controller.conf
  sudo test ! -e /etc/nginx/sites-enabled/web-controller.conf
  sudo test ! -L /etc/nginx/sites-enabled/web-controller.conf
  # 変更前に競合と新規専用hostのdefault symlinkを確認
  sudo test -L /etc/nginx/sites-enabled/default
  ls -l /etc/nginx/sites-enabled/default
  sudo cp -a /etc/nginx /etc/nginx.before-web-controller
  sudo install -m 0644 /opt/web-controller/current/examples/nginx/web-controller.conf /etc/nginx/sites-available/web-controller.conf
  sudo unlink /etc/nginx/sites-enabled/default
  sudo ln -s /etc/nginx/sites-available/web-controller.conf /etc/nginx/sites-enabled/web-controller.conf
  sudo nginx -t
  # -t成功後にだけ実行
  sudo systemctl reload nginx
  curl --fail --silent --show-error http://127.0.0.1/api/health
  curl --fail --silent --show-error http://127.0.0.1/
  curl --fail --silent --show-error http://127.0.0.1/settings
  curl --fail --silent --show-error http://127.0.0.1/rooms/installation-check
)
```

SPAの直接URLはindex.htmlを返すが、存在しないRoomの作成を意味しない。/api/はURIを保持してloopback:3000へ転送し、APIの404をSPAに置き換えない。server_nameは環境固有IPなしの例で、名前ベースで複数siteを使うhostでは実hostnameに設定する。LAN内の単一運用者向けで、認証なしのままインターネットへ公開しない。

## 7. 保存・再起動後の確認

ブラウザでHomeとSettingsを開き、同じVERSIONの表示・asset名、Room/Device/IR config/Schema/Command/Stateの保存・再読込みを確認する。Picoなしではcapture/sendをせず、未学習Stateのまま保持する。State選択・一覧更新だけで送信しないことも確認。

```bash
sudo systemctl restart web-controller-daemon.service
sudo systemctl restart web-controller-backend.service
curl --fail --silent --show-error http://127.0.0.1/api/health
```

保存値・学習情報とMaintenanceのread-only一覧を再読込みし、再起動前と比較する。Picoを使う環境ではcontroller health、学習・明示送信と実家電の動作を別に確認する。候補ごと・実行したarchitectureごとにOS/kernel/arch・配置・unit状態・HTTP・Browser結果を記録する。build成功や他候補の結果だけで未実行archを合格にしない。サービス再起動とOS再起動も分けて記録する。

## 8. Maintenance CLI・ログ・停止

すべてconfig引数を先頭に置き、runtime所有者と同じuserで実行する。実backup/Trashの操作は対象と保全方法を確認した運用者のみが行う。

```bash
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance restore-dry-run BACKUP_ID
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance restore-rehearsal BACKUP_ID
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance trash-final-delete-dry-run --entry-id ENTRY_ID
sudo -u web-controller /opt/web-controller/current/bin/backend --config /etc/web-controller/config.json maintenance trash-final-delete --plan-id PLAN_ID
```

rehearsalは隔離先へ復元しruntimeへ上書きしない。最終削除はTTYでentry IDの再入力と保持期限/plan照合が必要。HTTP/Frontendに最終削除APIはない。失敗時に同じplanやBackup IDを無条件再試行しない。

```bash
sudo journalctl --no-pager -u web-controller-backend.service -n 100
sudo journalctl --no-pager -u web-controller-daemon.service -n 100
sudo systemctl stop web-controller-backend.service
sudo systemctl stop web-controller-daemon.service
```

daemonだけ停止しても通常APIは提供されるがcontroller health/IR操作は使えない。両方止める時はBackendを先に止める。通常のmaintenance実行userに古いWEB_CONTROLLER_*があるとconfig不一致を拒否するため、旧設定を整理してから実行する。

## 9. 更新前の保全と互換性確認

以下は今後の更新で満たす条件で、上表の過去候補の完了記録とは別です。このINSTALLと同梱examplesだけで確認できるよう記載しています。

1. 対象host/arch、旧/新VERSION・asset/checksum、currentの実体、全書込み元と停止対象を特定する。実効ExecStart/WorkingDirectory/User/Group、unit/drop-in、自動起動設定、Nginx設定と配信先、port/socket、Pico by-idを記録する。新configは旧configのcopyから照合し、exampleで上書きしない。
2. Backendを先に、daemonを後に止め、CLI/別process/利用者等の全書込み元を停止する。旧software・config・unit/drop-in・Nginx・data/maintenance/backups/trashの**同時点の組**を、これらのrootの外の新しい保存先へ保全する。以前のsnapshotは更新直前の保全を代替しない。
3. 相対path・file種類・size・SHA-256・数値UID/GID・mode・symlink先をinventoryに残す。ACL/拡張属性を使用しているか調べ、必要なら保全と復元に含める。softwareのcurrent等の正当なlinkはリンクと実体を対応付ける。socket本体はcopyせずdaemonに作成させる。
4. 保全物を別hostにも保持し、転送前後checksumを照合する。さらに空の隔離先へ復元し、内容/hash・構造・数値所有者とaccountの対応・mode・必要なACL/拡張属性・plan/operation/Backup/Trashの参照を確認する。archiveのhash一致だけで復元可能とは判定しない。
5. 同じsnapshotの独立copyと旧/新software/configの組で、起動・health・保存値/Schema/学習情報・履歴のreadback、画面/直接URL、サービス再起動後保持、切替→旧版復旧→再切替を確認する。Picoなしの確認ではcontroller health失敗を記録し、IR実機成功と合算しない。対象を変えた候補には別の検証結果を残す。
6. 新版に書込みが生じた後の差分保全、復旧時点と失われる利用可能性、担当者・停止条件を決める。旧版が新しいSchema/履歴属性を読めるか、編集時に失わないかを確認する。読取り成功だけで旧版PUTの安全性を仮定しない。

旧版と同等に読めないSchema、壊れた参照、未解決の権限/容量/衝突/差分があれば切替を止める。旧固定pathや旧unitを使う環境は、実source/destination・設定対応と隔離演習を個別に確定し、標準配置への単純なcurrent切替で済ませない。

## 10. 新releaseへの切替

対応archの新archiveと同じ候補のSHA256SUMSを照合し、未使用のrelease directoryへ展開する。既存release・4 root・保守履歴を上書き/初期化せず、file/mode/ownerとconfig・unit・web/socketの対応を確認する。新版の設定keyが必要なら旧configのcopyで検査する。

全書込み元停止と上記保全の確認後、運用者が実際の新release名へ置換して実行する。開始位置は任意で、以下は設置先の絶対path。

```bash
# Manual only: NEW_VERSIONを検査済みの新release名へ置換
(
  set -e
  sudo test -d /opt/web-controller/releases/NEW_VERSION
  sudo test -L /opt/web-controller/current
  sudo test ! -e /opt/web-controller/current.next
  sudo test ! -L /opt/web-controller/current.next
  sudo ln -s /opt/web-controller/releases/NEW_VERSION /opt/web-controller/current.next
  sudo mv -Tf /opt/web-controller/current.next /opt/web-controller/current
  sudo systemctl start web-controller-daemon.service
  sudo systemctl start web-controller-backend.service
  sudo nginx -t
)
```

同filesystem内でsymlinkをrenameする。既存currentが通常directory/fileなら止めて配置を確認する。config/unit/Nginxも変える更新では、確認したcopyと差分に基づき別々に反映する。unit変更時はdaemon-reload、Nginx設定変更時は構文検査成功後reloadが必要。変更がなければ不要。

起動後に通常health、controller health、VERSION/配信asset/hash、保存JSONと学習情報、保守一覧/関連参照、画面/直接URLを基準と比較する。サービス再起動後も照合する。運用者が選定したIRのAPI結果と家電反応は別記録とし、確認完了前に全利用者の書込みを再開しない。

## 11. 失敗時と旧版への復旧

1. 利用者と全書込み元を止め、Backend→daemonの順に停止する。新版の4 root、software/config/unit/Nginx、logを現状のまま別保存し、inventory/hash/metadataを確認する。新旧の原本を上書きしない。
2. 更新直前の基準と新版停止後の4 rootを比較し、追加/更新/削除、Schema/State、未完了operation、plan/Backup/Trashの参照差分を記録する。比較不能・書込みの有無不明は差分ありとして扱う。
3. 差分がなくても、旧software/config/4 rootが検証した同じ組であり、権限・参照・pathが正しいことを確認する。差分がある場合は保全先・復旧時点・旧版で利用できない情報を運用者が確認するまで復旧しない。新版rootを旧rootへ自動copy/merge/逆変換しない。
4. 検証済みの旧release・config・unit/drop-in・Nginx・4 rootを組として復旧する。softwareだけ戻してdata互換が成立したとは扱わず、live rootへBackupを無条件展開しない。復旧に必要なrootの復元は旧/new原本と分けた検証済みcopyを使う。
5. currentを旧releaseへ戻す場合も、上の未使用current.next→renameの手順を使い、daemon→Backendで起動する。旧設定のNginxを構文検査し、必要時のみreloadする。health・画面・保存値・保守履歴と必要な実機を比較し、サービス再起動後も確認する。

保全物の欠損/hash不一致、衝突、owner/mode/ACL不一致、未承認差分、port/socket競合、Schema/履歴の不整合、health/画面/必要な実機確認失敗があれば、追加書込み・自動復旧を止めて現状とlogを保全する。退避した新版差分や旧root/旧softwareは自動削除しない。

この手順は更新の判断条件であり、すべての旧版/機種/OSで復旧検証済みという意味ではない。
