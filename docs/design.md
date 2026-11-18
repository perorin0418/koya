# koya 設計

## 目的

カレントディレクトリをマウントした隔離コンテナを、コマンド一つで起動して中に入れるようにする。
用途別のイメージ（プロファイル）をツールに同梱し、用途ごとに隔離環境を分ける。

```console
$ cd ~/src/myapp
$ koya run python             # python 用の隔離環境に入る（なければ作る）
$ koya run node -- npm test   # node 用の環境でコマンドを1つ実行する
```

## 基本方針

| 項目 | 決めたこと | 理由 |
|---|---|---|
| 実装言語 | Go（標準ライブラリのみ） | 単一バイナリで配布できる。`go:embed` で Dockerfile を同梱できる |
| イメージの同梱方法 | Dockerfile をバイナリに埋め込み、初回利用時にローカルでビルドする | イメージ本体（数百MB）を配布物に含めずに済む。レジストリにも依存しない |
| コンテナランタイム | podman → docker の順に自動検出。`KOYA_RUNTIME` で指定も可 | どちらでも動かす。CLI の差分は小さい |
| コンテナの寿命 | 永続（作業ディレクトリ×プロファイルごとに1つ）。明示的に `koya rm` で消す | 入れたパッケージや履歴が次回も残る。毎回作り直すと遅い |
| 使い捨て実行 | `--rm` を付けると終了時に削除 | 一時的な検証用 |

## コンテナの単位と名前

コンテナは「作業ディレクトリの絶対パス × プロファイル」ごとに1つ。

```
koya-<プロファイル>-<ディレクトリ名>-<絶対パスの SHA-256 先頭8桁>
例: koya-python-myapp-3fa2b91c
```

- 同じディレクトリで同じプロファイルを再度呼ぶと、既存のコンテナに入る（停止中なら起動してから入る）。
- 同名ディレクトリが別の場所にあってもハッシュで区別する。
- コンテナにはラベルを付け、`koya ls` などはラベルで絞り込む。
  - `koya.managed=true`
  - `koya.profile=<名前>`
  - `koya.workdir=<絶対パス>`

## マウント

- カレントディレクトリを、コンテナ内の **同じ絶対パス** にマウントし、そこを作業ディレクトリにする。
  エラーメッセージやツールが出すパスがホストとコンテナで一致するため。
- マウントするのはカレントディレクトリだけ。ホームや SSH 鍵などは既定では渡さない（隔離が目的のため）。
- 追加のマウントは `-v` で指定できる（`docker run -v` と同じ書式）。
- SELinux の環境（Fedora など）では `:z` を付けてラベルを付け替える。

## ユーザーと権限

コンテナ内では **koya を実行したホストのユーザー**（同じユーザー名・ホームのパス）として動く。
マウントしたファイルの所有者もホストのユーザーのままになるようにする。

- イメージには一般ユーザーを作らない。コンテナを作った直後に、koya が root で `exec` して
  `/etc/passwd`・`/etc/group`・`/etc/shadow` にホストのユーザーを登録し、ホームディレクトリを作る。
  `/etc/sudoers.d` があればパスワードなし sudo も許可する（UID 0 で動く場合は不要）。
- 環境変数 `HOME`・`USER`・`LOGNAME` もホストのユーザーに合わせる。ホームのパスはホストと同じ（例: `/home/perorin`）。
- ランタイムごとの UID の対応:

| ランタイム | 起動の引数 | コンテナ内の UID |
|---|---|---|
| podman CLI（rootless） | `--userns=keep-id --user <UID>:<GID>` | ホストと同じ UID |
| docker CLI で rootless のランタイム（rootless docker、docker CLI からつないだ rootless podman） | `--user 0:0` | 0 |
| rootful docker / podman | `--user <UID>:<GID>` | ホストと同じ UID |

- docker CLI は `--userns=keep-id` を受け付けないため、rootless のランタイムを docker CLI から使う場合は
  UID 0 で動かす（rootless ではコンテナ内の root がホストのユーザーに対応するため、作ったファイルはホストのユーザーの所有になる）。
  このときは、ホストのユーザー名の UID 0 の行を `/etc/passwd` の root より前に入れ、`whoami` などでホストのユーザー名が出るようにする。

## 永続ホーム

設定で有効にしたときだけ、ホストのディレクトリをコンテナ内のホームにマウントする。
`~/.claude`・`~/.claude.json`・シェル履歴・`.gitconfig`・各種キャッシュなどが、コンテナを作り直しても残る。

- 有効にするかどうかは、次の順に決める。
  1. `koya run --home` / `--no-home`
  2. 設定ファイルの `home.enable`（プロファイル名の並び、または `all`）
  3. プロファイルの `LABEL koya.home="true"`（claude プロファイルはこれで既定で有効）
- ホスト上の置き場所は `<home.linux.dir>/<プロファイル名>/`（`home.scope = shared`、既定）。
  同じプロファイルなら全プロジェクトで同じホームを共有する（Claude Code へのログインが1回で済む）。
  `home.scope = project`（または `--home-scope project`）ならコンテナ名ごとに分ける。
- `home.linux.dir` の既定は `~/.local/share/koya/home`。ランタイムがリモート（ホストの Podman など）の場合は、
  ホストから同じパスで見える場所を指定する必要がある。
- ホームのマウントはコンテナ作成時にだけ効く。設定を変えたら `koya rm -f` で作り直す（違う場合は警告を出す）。
- イメージの `$HOME` の下はマウントで隠れるため、プロファイルの Dockerfile ではホームの下に何も入れない
  （同梱プロファイルは `/usr/local` や `/go` に入れている）。

### 設定ファイル

`~/.config/koya/config`（`$XDG_CONFIG_HOME/koya/config`）。外部ライブラリなしで読めるよう「キー = 値」の形にする。

```ini
home.linux.dir = /mnt/pool/koya-home   # 永続ホームの置き場所（Linux コンテナ用）
home.win.dir   = C:\koya\home-win      # 永続ホームの置き場所（Windows コンテナ用。既定は <home.linux.dir>-win）
home.enable = claude, python        # 既定で永続ホームを使うプロファイル（all で全部）
home.scope  = shared                # shared / project
```

- 置き場所のキーは Linux 用と Windows 用で対にする（`home.linux.dir` / `home.win.dir`）。
  旧名の `home.dir` は `home.linux.dir` と同じ意味で読む（既存の設定ファイルを壊さないため）。両方書いたらエラーにする。

## プロファイル（同梱イメージ）

最初に同梱するもの:

| 名前 | ベース | 内容 |
|---|---|---|
| `base` | fedora:latest | git, curl, ca-certificates, sudo, less, vim, jq, tmux など共通ツール |
| `python` | base | python3, pip, venv, uv, gcc（C 拡張のビルド用） |
| `node` | base | Node.js LTS, npm |
| `go` | base | Go 最新安定版, gcc（cgo 用） |
| `claude` | node | Claude Code。永続ホームが既定で有効。自動アップデートは切り、`koya build claude` で更新する |

- プロファイルは `profiles-linux/<名前>/Dockerfile` に置き、`go:embed` でバイナリに埋め込む。
- `base` 以外は `FROM localhost/koya/base` で base を引き継ぐ。依存するイメージは自動で先にビルドする。
- イメージ名は `localhost/koya/<名前>:latest`。`localhost/` を付けるのは、ローカルにないときに
  Docker Hub の同名イメージ（第三者のもの）を取ってこないようにするため。
- Dockerfile の内容（親プロファイルを含む）のハッシュをラベル `koya.hash` に入れ、
  変わったら次回起動時に自動で再ビルドする。既存のコンテナは古いイメージのままなので、作り直すよう警告を出す。

### ユーザー定義プロファイル

`~/.config/koya/profiles-linux/<名前>/Dockerfile` を置くと、プロファイルとして使える。
旧名の `~/.config/koya/profiles` に置いたものも読む（両方にあれば `profiles-linux` を優先）。
同名の同梱プロファイルがある場合はユーザー定義を優先する（同梱の上書き）。

## 隔離のオプション

| オプション | 効果 |
|---|---|
| `--net none` | ネットワークを切る（`--network none`）。信用しないコードを動かすとき用 |
| `--ro` | カレントディレクトリを読み取り専用でマウントする |
| `-e KEY=VAL` | 環境変数を渡す |
| `-p 8080:8080` | ポートを公開する |
| `-v src:dst` | 追加のマウント |
| `--home` / `--no-home` | 永続ホームを使う / 使わない |
| `--home-scope shared\|project` | 永続ホームの単位 |

これらはコンテナ作成時にだけ効く。既存のコンテナと違うオプションを指定した場合は、
警告を出して `koya rm` で作り直すよう促す（作成時の設定はラベル `koya.opts` に記録して比較する）。

## コマンド体系

```
koya run [プロファイル] [オプション] [-- コマンド...]   環境に入る / コマンドを実行する
koya attach [プロファイル] [セッション]  セッションにつなぎ直す
koya ls [-a]                  このディレクトリのコンテナとセッションの一覧（-a で全ディレクトリ）
koya stop [プロファイル]       停止する（省略時はこのディレクトリの全プロファイル）
koya rm [プロファイル] [-f]    削除する（省略時はこのディレクトリの全プロファイル）
koya profiles                 使えるプロファイルの一覧
koya build [プロファイル]      イメージをビルド/再ビルドする
koya self-update [--check]    koya 本体を最新版に更新する
koya version
koya help [コマンド]
```

- すべての操作はサブコマンドで指定する。引数なしの `koya` はヘルプを表示する。
- プロファイル名は必ずサブコマンドの後ろに来るので、プロファイル名とコマンド名がぶつかることはない。
- `koya run` でプロファイルを省略したときの既定は、カレントディレクトリから上にたどって見つかる `.koya` ファイル（中身はプロファイル名）、
  なければ環境変数 `KOYA_PROFILE`、なければ `base`。

## 起動の流れ

```
koya run python
  1. ランタイムを検出する（podman / docker）
  2. プロファイル python を解決する（ユーザー定義 → 同梱）
  3. イメージ localhost/koya/python があるか、koya.hash が一致するかを確認し、必要ならビルドする（依存の base も）
  4. コンテナ名を決める（koya-python-myapp-3fa2b91c）
  5. コンテナがなければ作成する: run -d --init --name ... -v $PWD:$PWD:z -w $PWD --entrypoint sleep <イメージ> infinity
     停止中なら start する
  6. root で exec して、ホストのユーザーをコンテナ内に登録する（作成したときだけ）
  7. exec -it -w $PWD <コンテナ> <コマンド or シェル> で中に入る
     （標準入力が端末なら対話シェル、パイプならスクリプトとして読む）

`--rm` のときも同じ手順でコンテナを作り、コマンドが終わったら削除する。
```

コンテナは `sleep infinity` で常駐させ、操作はすべて `exec` で行う。複数の端末から同じ環境に入れる。
（PID 1 にはランタイムの `--init` を使い、ゾンビプロセスを回収させる）

## セッション

端末が切れても、コンテナ内のコマンドが動き続け、あとでつなぎ直せるようにする。

- `koya run ... --session[=名前]` で、コマンドをコンテナ内の tmux のセッションの中で動かす。
  `tmux new-session -A -D -s <名前> -- <コマンド>` を使い、同名のセッションがあれば新しく起動せずにつなぐ
- `koya attach [プロファイル] [セッション]` で既存のセッションにつなぐ（`tmux attach-session -d`）
- つなぐときはほかのクライアントを切り離す（`-D` / `-d`）。docker / podman の `exec` は、手元の端末が切れても
  コンテナ内のプロセス（tmux のクライアント）が残るため、つなぎ直すたびに古いクライアントを片付ける
- `koya ls` の SESSIONS 列に、起動中のコンテナのセッションを出す（`*` はクライアントがつながっている）
- tmux の設定は base イメージの `/etc/koya/tmux.conf`。ステータス行なし（画面の行数を変えない）、マウスでスクロール、
  24bit カラー、OSC 52 のクリップボード。コマンドなしで始めたときは koya のプロンプト付きの bash
- 手元の端末の種類（`TERM`）の定義がコンテナにないと tmux が起動しないので、そのときは `xterm-256color` にする
- セッション名の既定はコマンド名（なければ `shell`）。tmux で使えない `.` と `:` は `-` に置き換える
- セッションには端末が必要（`ssh` なら `-t`）。`--rm` とは同時に使えない
- 使うかどうかは、`--session` / `--no-session`、プロファイルの `LABEL koya.session="true"`（claude プロファイルはこれで既定で有効）の順に決める。
  ラベルで有効になる場合、使えない状況（`--rm`・端末なし・`--win`）ではエラーにせず、セッションなしで動かす
- tmux を選んだ理由: つなぎ直したときに画面全体を描き直してくれる（dtach などは画面を復元しない）。
  画面を読んで状態を判定するツール（nagaya）とも組み合わせられる

## 本体の更新（self-update）

`koya self-update` で、GitHub Releases の最新版に入れ替える。

- `https://api.github.com/repos/perorin0418/koya/releases/latest` で最新版を調べ、今の OS・CPU 向けのアーカイブ
  （`koya_<版>_<OS>_<CPU>.tar.gz`、Windows は `.zip`）を取ってくる。`GITHUB_TOKEN` があれば API に付ける（回数制限の対策）
- `checksums.txt` の SHA-256 と照らし合わせてから展開する。合わなければ何もしない
- 実行ファイル（シンボリックリンクはたどる）と同じディレクトリに一時ファイルを書き、名前を変えて入れ替える。途中で失敗しても元のファイルは壊れない
- Windows は実行中のファイルを上書きできないので、元のファイルを `koya.exe.old` に退避してから置き換え、次の起動時に `.old` を消す
- 今の版が最新版と同じか新しければ何もしない。版が分からない（`dev` など）場合は `--force` を求める。`--check` は確認だけ
- 起動時のお知らせ: `self-update`・`version`・`help` 以外のコマンドの起動時に、前回の確認から 6 時間以上たっていれば
  最新版を調べ（待ち時間は 3 秒まで）、結果をキャッシュ（`<ユーザーのキャッシュ>/koya/update-check.json`）に記録する。
  新しい版があれば stderr に「新しい版があります: 今の版 → 最新版」と出す。失敗しても確認した時刻は記録し、オフラインで毎回待たされないようにする。
  stderr が端末でないとき、版が分からない（`dev`）とき、`KOYA_NO_UPDATE_CHECK` があるときは何もしない
- 名前を `upgrade` にしなかったのは、イメージやコンテナの更新と区別するため（`upgrade` は将来そちらに使える）

## Windows 対応

Docker Desktop / Podman Desktop（podman machine）を前提に、Windows でもネイティブに動かす。

| 項目 | Linux / macOS | Windows |
|---|---|---|
| コンテナへの入り方 | `syscall.Exec` で koya のプロセスを `docker exec` に置き換える | 置き換えができないので子プロセスとして実行し、その終了コードで終わる（Ctrl-C は子に任せる） |
| 作業ディレクトリのパス | ホストと同じパス | `C:\Users\me\src` → `/c/Users/me/src` |
| マウントのオプション | Linux は `:z`（SELinux のラベル付け替え） | 付けない（VM が仲介するため） |
| ユーザー | ホストの名前・UID・GID・ホームのパス | 名前は Windows のユーザー名を Linux で使える形にしたもの、UID/GID は 1000、ホームは `/home/<名前>` |
| 設定ディレクトリ | `~/.config/koya` | `%APPDATA%\koya` |
| 永続ホームの既定 | `~/.local/share/koya/home` | `%LOCALAPPDATA%\koya\home` |

- ネットワーク上のパス（UNC）とドライブの最上位は拒否する。
- Windows のファイルは Docker Desktop / podman machine 経由でマウントされ、コンテナ内からは書き込めるように見える
  （所有者の厳密な対応は VM 側に任せる）。

## Windows コンテナ（--win）

Windows 版の koya で `--win` を付けると、Linux コンテナの代わりに Windows コンテナで動かす。
目的は、Windows のドライブにあるファイルを VM 越し（9P など）でなくネイティブの速さで扱うこと。
Rancher Desktop は Windows コンテナに対応していないため、koya 専用のデーモンを別に動かす。

### デーモン

| 項目 | 決めたこと | 理由 |
|---|---|---|
| デーモン | Docker の静的バイナリ（`download.docker.com/win/static/stable/x86_64/docker-<版>.zip`）の `dockerd.exe` | Apache 2.0 で再配布・利用の制約がない。podman は Windows コンテナに対応していない |
| 版と照合 | 版と SHA-256 を koya のソースに固定する（`internal/winengine`） | 公式が `.sha256` を出していない版もあるため。版を上げるときは実物から計算し直す |
| 置き場所 | 実行ファイル: `%ProgramFiles%\koya\docker-<版>\`、データ: `%ProgramData%\koya\` | サービス（SYSTEM で動く）の実行ファイルを一般ユーザーが書き換えられない場所に置く |
| 起動方法 | Windows サービス `koya-docker`（`dockerd --register-service`） | dockerd は管理者権限が必要。毎回 UAC を出さずに済む |
| つなぎ方 | 名前付きパイプ `\\.\pipe\koya_docker`。`--group <ユーザー>` で koya を実行したユーザーに許可する | 既定の `docker_engine` は Rancher Desktop・Docker Desktop が使う。既定の ACL は管理者だけ |
| 設定 | `--config-file %ProgramData%\koya\daemon.json`（`{}`）、`--exec-root`・`--pidfile` も専用 | ほかの dockerd（`%ProgramData%\docker`）の設定や PID ファイルとぶつからないように |

- Runtime は `docker.exe -H npipe:////./pipe/koya_docker ...` で呼ぶ（`runtime.ForWindows`、`Prefix` に `-H` を入れる）。
- 管理者権限が要る処理（取得と展開・サービスの登録・起動・登録の解除）は、koya 自身を
  `Start-Process -Verb RunAs` で `koya win _install|_start|_uninstall --log <ファイル>` として実行する。
  昇格したプロセスの出力は別のウィンドウに出て消えるため、ログファイルに書かせて呼び出し元が表示する。
- 「コンテナー」機能の有無は、Host Compute Service（`vmcompute`）のサービスがあるかで判定する（管理者権限なしで分かる）。
  有効にするには管理者権限と再起動が要るため、koya は手順を表示するだけにする。
- サービスの状態は advapi32 の API（OpenService / QueryServiceStatus）で調べる（`sc.exe` の出力は言語で変わりうるため）。
- koya の Docker の版を上げたときは、新しい版を展開し、サービスを登録し直す。登録の解除は `sc.exe delete` で行う
  （古い `dockerd.exe` がなくても外せるように）。

### ベースイメージと分離

プロセス分離では、ホストとイメージのビルドが合っている必要がある。ホストのビルド（`RtlGetVersion`）から決める。

| ホストのビルド | タグ | 分離 |
|---|---|---|
| 26100 以降（Windows 11 24H2 / Server 2025） | `ltsc2025` | process |
| 20348〜26099（Server 2022 / Windows 11 21H2〜23H2） | `ltsc2022` | process |
| 17763（Server 2019 / Windows 10 1809） | `ltsc2019` | process |
| 17764〜20347（Windows 10 1903 以降） | `ltsc2019` | hyperv |

- `KOYA_WIN_BASE_TAG`・`KOYA_WIN_ISOLATION` で上書きできる。
- タグは `--build-arg BASE_TAG=<タグ>` で Dockerfile に渡し、ビルド引数もイメージのハッシュに含める（ホストを上げたら再ビルド）。
- `build` と `run` に `--isolation` を付ける（クライアント版 Windows の既定は hyperv のため）。

### Windows コンテナの永続ホーム

Windows コンテナでは、C: の中身のあるディレクトリにはマウントできない（Docker のドキュメント: マウント先は
「存在しないか空のディレクトリ、または C: 以外のドライブ」）。そのため `C:\Users\ContainerAdministrator` に重ねられない。

- ユーザーは ContainerAdministrator のまま、イメージにない `C:\Users\<ホストのユーザー名>` に永続ホームをマウントする
  （ホストの `DOMAIN\name` の name。イメージにある名前（Administrator・Public など）とぶつかるときは `koya-` を付ける）
- `USERPROFILE`・`HOME`・`APPDATA`・`LOCALAPPDATA` をそこに向ける（Node・Python・git などは環境変数でホームを決める）
- コンテナ作成時に ContainerAdministrator の `User Shell Folders`（AppData・Local AppData・Personal）も向ける
  （`SHGetKnownFolderPath` でたずねるツール向け）。`reg.exe` に引数を直接渡す（`cmd /c` だと引用符が崩れる）
- 残らないもの: API でホームそのもの（FOLDERID_Profile）をたずねるツールの書き込み、レジストリ（HKCU）の設定
- 置き場所は `home.win.dir`（既定 `<home.linux.dir>-win`、つまり `%LOCALAPPDATA%\koya\home-win`）。Linux 用と中身が違うため分ける。
  マウント元がないと Windows コンテナは起動しないため、`AppData\Roaming`・`AppData\Local`・`Documents` を先に作る
- 永続ホームを先にマウントし、その下の作業ディレクトリを重ねる（入れ子のマウントが動くことを確認済み）
- 入れ子のマウントでは、途中のディレクトリ（`C:\Users\<名前>\src` など）が作られず、lstat すると「存在しない」になる
  （Linux の Docker は作る）。Node の `fs.realpathSync` が失敗し、Claude Code が作業ディレクトリを開けない。
  そのため、作業ディレクトリが永続ホームの下にあるときは、永続ホームの中にマウント先のディレクトリを空で作っておく

ホストと同じ名前のユーザーをコンテナ内に作って動かす案（`net user /add` + `exec -u`）は、実機で試して採らなかった（2026-11-17）:
- パスワード付きのユーザーは `exec -u` でログオンできない（0x52e）。パスワードなしならログオンできる
- コンテナ内のユーザーはホストのユーザーとは別の SID になるため、本人と管理者だけが書き込める
  プロジェクト（`C:\Users\<名前>\...` の下）に書き込めない（`C:\tmp` のように Authenticated Users に権限があれば書ける）
- `C:\Users\<名前>` に永続ホームを先にマウントしておくと、Windows はそれを使わず `C:\Users\<名前>.<ホスト名>` にホームを作る

### Windows 用プロファイル

| 名前 | 中身 |
|---|---|
| base | Server Core（`BASE_TAG`）+ PowerShell 7.7 + Git for Windows 2.56（PortableGit、`C:\Git`） |
| node | base + Node.js 26 LTS（`C:\nodejs`）。グローバルの入れ先は `C:\npm-global` |
| python | base + Python 3.14（`C:\Python314`）+ uv。3.15 にしないのは wheel がそろっているため |
| dotnet | base + .NET SDK 10.0（`C:\dotnet`） |
| claude | node + Claude Code（npm）。bash は base の Git のものを使う（`CLAUDE_CODE_GIT_BASH_PATH`）。既定で永続ホームを使う |

- ツールは公式の配布物（zip / インストーラー）を直接取ってくる。パッケージマネージャー（Chocolatey・winget）は使わない。
  版とハッシュ（GitHub のリリースの digest、Node の SHASUMS256.txt、python.org の SHA-256、.NET の releases.json の SHA-512）を
  Dockerfile に固定し、base の `fetch.ps1` で照合する。
- PATH は `add-path.ps1` でマシンの環境変数（レジストリ）に書く。Windows コンテナでは `ENV PATH` が PATH を丸ごと置き換えるため。
- zip の展開は `Expand-Archive` ではなく `tar.exe` を使う（大きな zip で速い）。
- Git は PATH に `C:\Git\cmd` だけを足す（`usr\bin` を足すと `find` や `sort` が Windows のものと入れ替わる）。
  マウントしたディレクトリの所有者はホストのユーザーなので、`safe.directory=*` をシステムの設定に入れる。
- `.ps1` は BOM 付きの UTF-8 にする（Windows PowerShell 5.1 は BOM のないファイルをシステムの文字コードで読むため）。
- プロンプトに `(koya:<プロファイル>)` を出す（PowerShell の全ユーザー用プロファイル）。

### コンテナ

| 項目 | Linux コンテナ | Windows コンテナ |
|---|---|---|
| プロファイル | `profiles-linux/`・`%APPDATA%\koya\profiles-linux`（旧名 `profiles` も読む） | `profiles-win/`・`%APPDATA%\koya\profiles-win`（別系統。イメージ名は同じ `localhost/koya/<名前>` だがデーモンが別） |
| 常駐 | `--init` + `sleep infinity` | `powershell.exe` で `Start-Sleep` を繰り返す（`--init` はない） |
| マウント | `/c/...` に変換 | C: はそのまま、ほかのドライブは `C:\mnt\<ドライブ>\...`（コンテナには C: しかない） |
| ユーザー | ホストのユーザーを登録 | イメージの既定（ContainerAdministrator）。登録しない |
| シェル | bash | `pwsh.exe -NoLogo`。pwsh がないイメージでは `powershell.exe`（`cmd /c` で選ぶ。パイプなら `-NoProfile -Command -`） |
| セッション | 使える | 使えない（tmux がない） |
| 永続ホーム | `$HOME` にマウント | `C:\Users\<ホストのユーザー名>` にマウントし、環境変数などでホームを向ける（下記） |
| 既定のプロファイル | `.koya` → `KOYA_PROFILE` → base | `KOYA_WIN_PROFILE` → base |

- `ls`・`stop`・`rm`・`profiles`・`build` も `--win` で Windows コンテナ側を扱う。
- 対象は `--linux` / `--win` の対で選ぶ。何も付けなければ `--linux`（省略できる既定）。両方付けたらエラーにする。
- WSL の中の Linux 版 koya からは使えない（名前付きパイプとサービスの操作が Windows のものなため）。

### 動作確認と今後

確認済み（2026-11-17、Windows 11 ビルド 26200、Rancher Desktop を起動した状態）:
- 一般ユーザーから `koya run --win` で、UAC の確認 → 取得・照合・展開 → サービスの登録・起動 → base のビルド（`ltsc2025`、process）→ PowerShell に入るまで通る
- `--group <ドメイン>\<ユーザー>` で、一般ユーザーのまま koya 専用のパイプにつなげる。`--register-service` に渡した引数がサービスに記録される
- Rancher Desktop の dockerd と同時に動き、コンテナ内から名前解決と HTTPS の通信ができる（HNS の NAT ネットワークがぶつからない）
- D: のディレクトリが `C:\mnt\d\...` に、C: のディレクトリが同じパスにマウントされ、読み書きできる
- 同梱の5つ（base・node・python・dotnet・claude）のビルドが通り、python・node・dotnet・claude が動く
- claude で永続ホームを使い、コンテナを作り直してもログインが残る（`USERPROFILE`・`APPDATA` は永続ホーム、
  API でたずねたホームそのものは `C:\Users\ContainerAdministrator` のまま）

ファイルアクセスの速さ（2026-11-17。小さなファイル 5000 個の作成・更新日時の取得・読み込み・削除。単位は秒）:

| 置き場所 | 実行環境 | 作成 | 更新日時 | 読み込み | 削除 |
|---|---|---|---|---|---|
| D:（USB の SSD） | ホスト | 12.9 | 0.07 | 0.86 | 3.2 |
| | Windows コンテナ（--win） | 13.2 | 0.13 | 1.1 | 3.2 |
| | Linux コンテナ（Rancher Desktop） | 22.6 | 2.8 | 9.9 | 6.3 |
| C:（NVMe） | ホスト | 5.8 | 0.28 | 0.93 | 2.4 |
| | Windows コンテナ（--win） | 5.9 | 0.13 | 1.2 | 2.6 |
| | Linux コンテナ（Rancher Desktop） | 14.9 | 2.6 | 9.8 | 5.4 |

- Windows コンテナはホストとほぼ同じ速さ（読み込みで 1.3 倍程度）。Linux コンテナは 9P を経由するため、
  読み取り（更新日時・読み込み）が 10 倍以上遅く、ディスクを NVMe にしても変わらない。
- ホストでも作成が 1 ファイルあたり 1 ミリ秒以上かかるのは、Defender のリアルタイム保護の影響と見ている（未確認）。
  開発ドライブ（Dev Drive）に置くと改善する可能性がある。
- 計測は PowerShell（`[IO.File]` の API）と bash（`echo >`・`find`・`cat`・`rm`）で行ったため、処理の中身は完全には揃っていない。

今後:
- Visual Studio Build Tools（MSBuild・.NET Framework）のプロファイル。
- Hyper-V 分離への手動の切り替えオプション（`--isolation`）。

## リポジトリ構成

```
koya/
├── cmd/koya/main.go        エントリポイント、コマンドの振り分け
├── internal/runtime/       podman/docker の検出と呼び出し
├── internal/config/        設定ファイルの読み込み
├── internal/selfupdate/    本体の更新（self-update）
├── internal/winengine/     Windows コンテナ用デーモン（--win）の取得・サービス登録
├── internal/profile/       プロファイルの解決、埋め込み、ビルド
├── internal/container/     名前付け、作成、exec、ls/stop/rm
├── scripts/build-release.sh  リリース用バイナリの作成
├── profiles-linux/         同梱プロファイル（Linux コンテナ用。go:embed 対象）
│   ├── base/Dockerfile, bashrc
│   ├── python/Dockerfile
│   ├── node/Dockerfile
│   ├── go/Dockerfile
│   └── claude/Dockerfile
├── profiles-win/           同梱プロファイル（Windows コンテナ用。go:embed 対象）
│   ├── base/Dockerfile, fetch.ps1, add-path.ps1, profile.ps1
│   └── node/, python/, dotnet/, claude/
└── docs/design.md
```

インストールは `go install github.com/perorin0418/koya/cmd/koya@latest`、
または GitHub Releases に置くバイナリ。リリース用のバイナリは `scripts/build-release.sh <バージョン>` で作る
（Linux・macOS は tar.gz、Windows は zip）。

## 今回は対象外（あとで検討）

- GPU やデバイスの受け渡し
- devcontainer.json の読み込み
- リソース制限（`--memory`, `--cpus`）
