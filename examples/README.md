# 配布用設定例

標準配置・新規設置・更新・復旧は配布物rootの[INSTALL.md](../INSTALL.md)が正本です。この案内とINSTALLはsource checkoutと配布archiveの両方で読めます。設定例をコピーした先のconfig.jsonを編集し、既存configやtemplateを本番の値で上書きしません。

## configのkey

[config.example.json](config.example.json)は構造の例です。pico_serial_pathのCHANGE_MEは実Picoを特定したby-idへ置換する必要があり、未置換では起動を拒否します。ほかのpathも実配置・権限と一致することを確認します。

| key | 標準値・意味 |
|---|---|
| http_listen | 127.0.0.1:3000。loopback待受。Nginx proxy_passと合わせる |
| data_root | /var/lib/web-controller/data |
| maintenance_root | /var/lib/web-controller/maintenance。plans/operationsも事前作成 |
| backup_root | /var/lib/web-controller/backups |
| trash_root | /var/lib/web-controller/trash。data_rootと同一filesystem |
| trash_retention_days | 30。正整数。既存entryの保持期限を自動短縮しない |
| software_root | /opt/web-controller/current。root管理のrelease symlink |
| web_root | /opt/web-controller/current/web。software_root配下かつNginx rootと一致 |
| socket_path | /run/web-controller/controller-daemon.sock |
| pico_serial_path | /dev/serial/by-id/CHANGE_ME。実機を確認したby-idへ置換 |

Backend/daemonとMaintenance CLIは先頭の`--config /etc/web-controller/config.json`で同じfileを読みます。configは単一のJSON object、最大64 KiBで、unknown key・余分なJSON・型不正を拒否します。http_listenは127.0.0.1または::1と有効なportだけを許し、localhost等の名前やLAN addressを待受値にしません。IPv6を使う場合はNginx側も対応させます。

全pathは正規化された絶対pathで、rootそのものやCHANGE_MEを含む未置換値を拒否します。data/maintenance/backup/trash/softwareは相互に重ねず、webはsoftware内、socket/serialはこれらの保護root外へ置きます。

## 所有者・権限・template

runtime/保守rootとその保護対象の祖先にsymlinkを置かず、service user所有0700にします。保守fileは0600。runtime JSONのmodeは各repositoryに従い、rootの0700で保護します。softwareのcurrentはrootが管理するrelease symlinkとして別扱いです。

configはcurrent userまたはroot所有の通常fileで0600/0640。標準配置はroot:web-controllerの0640とし、両serviceから読めるようにします。root同士の整合だけでなく、実在・アクセス・祖先/所有者/modeも設置時と各機能の利用時に検証します。

- [daemon unit](systemd/web-controller-daemon.service): User/Group=web-controller、SupplementaryGroups=dialout。RuntimeDirectoryがsocket親を同user/primary group所有0750で作り、daemonがsocket本体を0660にする。
- [Backend unit](systemd/web-controller-backend.service): 同user/groupとconfigを使用。daemonをWants/Afterで起動順に置くが、Requiresにはせず、Pico不通と通常data APIの利用可否を分ける。
- [Nginx設定](nginx/web-controller.conf): current/webを配信し、SPA fallbackとURIを保持する/api/ proxyを分離する。認証なしのままインターネットへ公開しない。

標準unitは旧WEB_CONTROLLER_*をUnsetEnvironmentで除きます。手動CLI等で併用する場合はconfigの対応値と完全一致が必要です。rootを変更する際は同filesystem、権限、ProtectHome、Nginx/socket等も照合します。既存unit/site/configは無条件で置換せず、INSTALLの保全・移行手順に従います。

設置先へのcopy、user/権限変更、systemd/Nginx反映、Pico設定・書込みは運用者が手動で行います。本案内は設定template自体を変更せず、個体固有のIP/USB値を配布しません。
