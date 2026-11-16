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

## 使い方

```
koya run [プロファイル] [オプション] [-- コマンド...]   環境に入る / コマンドを実行する
koya ls [-a]                   このディレクトリのコンテナ一覧（-a で全ディレクトリ）
koya stop [プロファイル]        停止する（省略時はこのディレクトリの全プロファイル）
koya rm [-f] [プロファイル]     削除する（省略時はこのディレクトリの全プロファイル）
koya profiles                  使えるプロファイルの一覧
koya build [プロファイル...]    イメージをビルドし直す（省略時は全プロファイル）
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

## 永続ホーム

有効にすると、ホストのディレクトリをコンテナ内のホームにマウントします。
`~/.claude`、`~/.claude.json`、シェル履歴、`.gitconfig`、各種キャッシュなどが、コンテナを作り直しても残ります。
ホストのホームとは別のディレクトリなので、隔離は保たれます。

```bash
koya run python --home     # その場で有効にする
koya run claude            # claude プロファイルは既定で有効
koya run claude --no-home  # 無効にする
```

置き場所は `<home.dir>/<プロファイル名>/` で、同じプロファイルなら全プロジェクトで共有します
（Claude Code へのログインは1回で済みます）。設定は `~/.config/koya/config` に書きます。

```ini
home.dir    = /mnt/pool/koya-home   # 置き場所（既定: ~/.local/share/koya/home）
home.enable = python                # 既定で有効にするプロファイル（all で全部）
home.scope  = shared                # shared: プロファイルごと / project: プロジェクトごと
```

ランタイムが別のマシンやホストで動いている場合（リモートの Podman など）は、`home.dir` に
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

`~/.config/koya/profiles/<名前>/Dockerfile` を置くとプロファイルとして使えます。
同じディレクトリのファイルはビルドコンテキストに入ります。同梱と同じ名前にすると同梱を上書きします。

```dockerfile
# ~/.config/koya/profiles/db/Dockerfile
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
| ユーザー定義プロファイル | `%APPDATA%\koya\profiles\<名前>\Dockerfile` |
| 永続ホームの既定の置き場所 | `%LOCALAPPDATA%\koya\home` |

- ネットワーク上のパス（`\\server\share`）やドライブの最上位（`C:\`）では実行できません。
- Git Bash（mintty）では端末として認識されず、対話シェルに入れないことがあります。
  その場合は Windows Terminal を使うか、`winpty koya run` で起動してください。
- WSL2 の中で Linux 版を使うこともできます。プロジェクトを WSL2 側（`/home/...`）に置くと、ファイルのアクセスが速くなります。

## 環境変数

| 名前 | 意味 |
|---|---|
| `KOYA_RUNTIME` | 使うランタイム（`podman` / `docker` など）。既定は podman → docker の順に自動検出 |
| `KOYA_PROFILE` | 既定のプロファイル |

`koya help config` で設定ファイルの書き方を表示します。設計の詳細は [docs/design.md](docs/design.md) を参照。
