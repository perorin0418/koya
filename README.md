# koya

カレントディレクトリをマウントした隔離コンテナを、コマンド一つで起動して中に入るためのツール。
用途別のイメージ（プロファイル）を同梱していて、用途ごとに隔離環境を分けられる。

```console
$ cd ~/src/myapp
$ koya run python             # python 用の隔離環境に入る（なければ作る）
(koya:python) me ~/src/myapp $ exit
$ koya run node -- npm test   # node 用の環境でコマンドを1つ実行する
$ koya ls
PROFILE  STATUS   NAME
node     running  koya-node-myapp-3fa2b91c
python   running  koya-python-myapp-3fa2b91c
```

## インストール

podman か docker が必要です。Linux・macOS・Windows で動きます。
[Releases](https://github.com/perorin0418/koya/releases) からバイナリを取ってくるか、Go で入れます。

```bash
go install github.com/perorin0418/koya/cmd/koya@latest
```

### 更新

```bash
koya self-update           # 最新版に入れ替える
koya self-update --check   # 新しい版があるかだけ確かめる
```

GitHub Releases から今の OS・CPU 向けのファイルを取ってきて、`checksums.txt` で中身を確かめてから入れ替えます。
`/usr/local/bin` など書き込めない場所に置いている場合は `sudo koya self-update` を使ってください。
イメージ（Fedora のパッケージや Claude Code など）を新しくするには、別に `koya build` を使います。

koya は起動時に、6 時間に 1 回ほど新しい版が出ていないかを確かめ、出ていればお知らせします
（端末から使ったときだけ。結果はキャッシュに記録し、それ以外の起動では問い合わせません）。
止めるには環境変数 `KOYA_NO_UPDATE_CHECK=1` を設定してください。

## 使い方

```
koya run [プロファイル] [オプション] [-- コマンド...]   環境に入る / コマンドを実行する
koya attach [プロファイル] [セッション]   セッションにつなぎ直す
koya ls [-a]                   このディレクトリのコンテナとセッションの一覧（-a で全ディレクトリ）
koya stop [プロファイル]        停止する（省略時はこのディレクトリの全プロファイル）
koya rm [-f] [プロファイル]     削除する（省略時はこのディレクトリの全プロファイル）
koya profiles                  使えるプロファイルの一覧
koya build [プロファイル...]    イメージをビルドし直す（省略時は全プロファイル）
koya self-update [--check]     koya 本体を最新版に更新する
koya version
koya help [コマンド]
```

- コンテナは「ディレクトリ × プロファイル」ごとに1つ作られ、`koya rm` するまで残ります。
  2回目以降は同じコンテナに入るので、入れたパッケージなどもそのまま残ります。
- マウントするのはカレントディレクトリだけです（コンテナ内でも同じパス）。ホームや SSH 鍵は渡しません。
- コンテナ内では koya を実行したユーザー（同じユーザー名・ホームのパス）で動きます。作ったファイルの所有者もホストのユーザーのままです。sudo も使えます。
- イメージは初めて使うときに自動でビルドします。同梱の Dockerfile が更新されたら自動でビルドし直します。

### `koya run` のオプション

オプションはコンテナを作るときだけ効きます。変えたいときは `koya rm -f <プロファイル>` してから作り直します。

| オプション | 効果 |
|---|---|
| `--net none` | ネットワークを切る |
| `--ro` | カレントディレクトリを読み取り専用でマウントする |
| `-e KEY=VAL` | 環境変数（複数可） |
| `-p 8080:8080` | ポートを公開する（複数可） |
| `-v SRC:DST` | 追加でマウントする（複数可） |
| `--rm` | 使い捨て: 新しいコンテナで実行し、終了したら削除する |
| `--linux` / `--win` | Linux コンテナ（既定）/ Windows コンテナで動かす（下記） |
| `--session[=名前]` | セッションの中で動かす（下記）。これはコンテナ作成時に限らず毎回効く |
| `--home` / `--no-home` | 永続ホームを使う / 使わない（下記） |
| `--home-scope shared\|project` | 永続ホームをプロファイルごとに共有するか、プロジェクトごとに分けるか |

### 既定のプロファイル

`koya run` でプロファイルを省略すると、次の順に決めます。

1. カレントディレクトリから上にたどって見つかる `.koya` ファイル（中身はプロファイル名）
2. 環境変数 `KOYA_PROFILE`
3. `base`

## プロファイル

| 名前 | 内容 |
|---|---|
| `base` | Fedora 最新版 + git, curl, jq, sudo など（dnf が使える） |
| `python` | base + python3, pip, venv, uv, gcc |
| `node` | base + Node.js LTS, npm, corepack |
| `go` | base + Go 最新安定版, gcc |
| `claude` | node + Claude Code（永続ホームが既定で有効） |

## セッション

`--session` を付けると、コマンドをコンテナ内の tmux の中で動かします。
端末を閉じたり SSH が切れたりしても、コマンドは動き続けます。同じコマンドをもう一度実行するか、
`koya attach` でつなぎ直すと、同じ画面に戻ります。

```bash
koya run claude --session -- claude    # セッション「claude」で Claude Code を動かす
# …端末が切れる…
koya run claude --session -- claude    # 同じコマンドでつなぎ直す（新しく起動はしない）
koya attach claude                     # またはこれでつなぎ直す
koya ls                                # SESSIONS の列にセッションが出る（* はつながっている）
```

- セッション名の既定はコマンド名（`claude` など）、コマンドがなければ `shell` です。`--session=work` で名前を付けられます
- 同じセッションにつなぐと、ほかの端末からの接続は切り離されます（1つのセッションを見られるのは1か所だけ）
- tmux のステータス行は出さず、ホイールでスクロールできる設定にしています（文字の選択は Shift を押しながら）。
  tmux のキー操作（`Ctrl-b` に続けて押す）も使えます。`Ctrl-b d` で切り離せます
- セッションはコンテナの中にあるので、`koya stop` や `koya rm` をすると終わります
- SSH 越しに使うときは `ssh -t <ホスト> koya run claude --session -- claude` のように `-t` を付けてください
- 端末が切れた直後は、`koya ls` で `*`（つながっている）のまま見えることがあります。次につなぎ直したときに片付きます
- ユーザー定義プロファイルで使うには、イメージに `tmux` が必要です（`localhost/koya/base` を引き継げば入っています）

## 永続ホーム

有効にすると、ホストのディレクトリをコンテナ内のホームにマウントします。
`~/.claude`、`~/.claude.json`、シェル履歴、`.gitconfig`、各種キャッシュなどが、コンテナを作り直しても残ります。
ホストのホームとは別のディレクトリなので、隔離は保たれます。

```bash
koya run python --home     # その場で有効にする
koya run claude            # claude プロファイルは既定で有効
koya run claude --no-home  # 無効にする
```

置き場所は `<home.linux.dir>/<プロファイル名>/` で、同じプロファイルなら全プロジェクトで共有します
（Claude Code へのログインは1回で済みます）。設定は `~/.config/koya/config` に書きます。

```ini
home.linux.dir = /mnt/pool/koya-home   # 置き場所（既定: ~/.local/share/koya/home。旧名の home.dir も読む）
home.win.dir   = C:\koya\home-win      # --win の置き場所（既定: <home.linux.dir>-win）
home.enable = python                # 既定で有効にするプロファイル（all で全部）
home.scope  = shared                # shared: プロファイルごと / project: プロジェクトごと
```

ランタイムが別のマシンやホストで動いている場合（リモートの Podman など）は、`home.linux.dir` に
ランタイム側から同じパスで見えるディレクトリを指定してください。

### Claude Code

```bash
cd ~/src/myapp
koya run claude -- claude                                  # 初回はログインする
koya run claude -- claude --dangerously-skip-permissions   # 隔離環境なので確認なしで動かす
```

カレントディレクトリしか見えないので、ホストの他のファイルや SSH 鍵には触れません。
Claude Code の更新は `koya build claude` でイメージを作り直してから、`koya rm -f claude` で作り直します。

### 自分でプロファイルを追加する

`~/.config/koya/profiles-linux/<名前>/Dockerfile` を置くとプロファイルとして使えます（旧名の `profiles` に置いたものも読みます。両方にあれば `profiles-linux` が優先）。
同じディレクトリのファイルはビルドコンテキストに入ります。同梱と同じ名前にすると同梱を上書きします。

```dockerfile
# ~/.config/koya/profiles-linux/db/Dockerfile
FROM localhost/koya/python
RUN dnf install -y postgresql sqlite \
 && dnf clean all
```

`FROM localhost/koya/<名前>` と書くと、そのプロファイルを先にビルドしてから引き継ぎます。

- `$HOME` の下には何も入れないでください（ユーザーは koya が作成時に登録し、永続ホームでも隠れるため）。
- `LABEL koya.home="true"` を付けると、そのプロファイルでは永続ホームが既定で有効になります。

## Windows

Windows では、Docker Desktop か Podman Desktop（podman machine）で動きます。
PowerShell やコマンドプロンプトから、Linux と同じように使えます。

```powershell
cd C:\Users\me\src\myapp
koya run python
```

Linux・macOS との違い:

| 項目 | Windows での扱い |
|---|---|
| コンテナ内の作業ディレクトリ | `C:\Users\me\src\myapp` → `/c/Users/me/src/myapp` に変換してマウントする |
| コンテナ内のユーザー | Windows のユーザー名を Linux で使える形にしたもの（例: `taro.yamada`）、UID/GID は 1000、ホームは `/home/<名前>` |
| 設定ファイル | `%APPDATA%\koya\config` |
| ユーザー定義プロファイル | `%APPDATA%\koya\profiles-linux\<名前>\Dockerfile`（旧名の `profiles` も読む） |
| 永続ホームの既定の置き場所 | `%LOCALAPPDATA%\koya\home` |

- ネットワーク上のパス（`\\server\share`）やドライブの最上位（`C:\`）では実行できません。
- Git Bash（mintty）では端末として認識されず、対話シェルに入れないことがあります。
  その場合は Windows Terminal を使うか、`winpty koya run` で起動してください。
- WSL2 の中で Linux 版を使うこともできます。プロジェクトを WSL2 側（`/home/...`）に置くと、ファイルのアクセスが速くなります。

### Windows コンテナ（`--win`）

`run`・`ls`・`stop`・`rm`・`profiles`・`build` は、`--linux`（Linux コンテナ）か `--win`（Windows コンテナ）で対象を選びます。
どちらも付けなければ `--linux` です。

Windows 版の koya では、`--win` を付けると Linux コンテナの代わりに Windows コンテナ（プロセス分離）で動かせます。
VM を挟まないので、Windows のドライブにあるファイルをネイティブに近い速さで読み書きできます。
.NET Framework や MSBuild など、Windows のツールを隔離して使いたいときに向いています。

```powershell
cd D:\src\myapp
koya run --win                 # Windows の base 環境（PowerShell）に入る
koya ls --win
koya rm -f --win base
koya win status                # デーモンとホストの状態
```

- **koya 専用のデーモン**: Docker の公式の静的バイナリ（`dockerd.exe`、版は固定）を初めて使うときに自動で取ってきて、
  SHA-256 を照合してから `%ProgramFiles%\koya\` に置き、Windows サービス `koya-docker` として登録します。
  このとき管理者権限の確認（UAC）が出ます。Rancher Desktop・Docker Desktop とは別のパイプ（`\\.\pipe\koya_docker`）と
  データの置き場所（`%ProgramData%\koya\`）を使うので、ぶつかりません。
- **前提**: Windows の「コンテナー」機能（Pro / Enterprise / Education。Home は不可）。有効でなければ有効にする手順を表示します。
  有効にするには管理者権限と再起動が必要です。
- **プロファイル**: Windows 用の別系統です。同梱は次の5つです（ツールの版とハッシュは Dockerfile に固定）。

  | 名前 | 中身 |
  |---|---|
  | `base` | Server Core + PowerShell 7 + Git for Windows（`C:\Git`。bash も入る） |
  | `node` | base + Node.js LTS、npm、corepack |
  | `python` | base + Python 3.14、pip、venv、uv |
  | `dotnet` | base + .NET SDK 10（LTS） |
  | `claude` | node + Claude Code（既定で永続ホームを使う） |

  `%APPDATA%\koya\profiles-win\<名前>\Dockerfile` で追加できます。ベースイメージのタグは、
  ホストのビルドに合わせて koya が選び、ビルド引数 `BASE_TAG` で渡します。
- **マウント**: Windows コンテナには C: ドライブしかないため、C: のディレクトリはそのままのパス、
  ほかのドライブは `C:\mnt\<ドライブ>\...` にマウントします（例: `D:\src\myapp` → `C:\mnt\d\src\myapp`）。
- **永続ホーム（`--home`）**: `C:\Users\<ホストのユーザー名>` にマウントし、`USERPROFILE`・`HOME`・`APPDATA`・`LOCALAPPDATA` と
  ユーザーのフォルダの設定（AppData・ドキュメント）をそこに向けます。置き場所は Linux 用と分けて、設定の `home.win.dir`
  （既定は `<home.linux.dir>-win`、つまり `%LOCALAPPDATA%\koya\home-win`）。Windows の API でホームそのものをたずねるツールや、
  レジストリ（HKCU）に書かれる設定は残りません（Claude Code・git・npm・pip・uv は環境変数を見るので残ります）。
- **使えないもの**: `--session`・`koya attach`。ユーザーは `ContainerAdministrator` です。
- プロファイルを省略したときは `KOYA_WIN_PROFILE`、なければ `base` です（`.koya` は見ません）。
- 外すときは `koya win uninstall`（イメージとコンテナは `%ProgramData%\koya\docker` に残ります）。

詳しくは `koya help win` を参照。

## 環境変数

| 名前 | 意味 |
|---|---|
| `KOYA_RUNTIME` | 使うランタイム（`podman` / `docker` など）。既定は podman → docker の順に自動検出 |
| `KOYA_PROFILE` | 既定のプロファイル |
| `KOYA_WIN_PROFILE` | `--win` のときの既定のプロファイル |
| `KOYA_WIN_BASE_TAG` | `--win` のベースイメージ（servercore）のタグ。既定はホストのビルドから自動で選ぶ |
| `KOYA_WIN_ISOLATION` | `--win` の分離の方式（`process` / `hyperv`）。既定はホストのビルドから自動で選ぶ |

`koya help config` で設定ファイルの書き方を表示します。設計の詳細は [docs/design.md](docs/design.md) を参照。
