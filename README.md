# koya

カレントディレクトリをマウントした隔離コンテナを、コマンド一つで起動して中に入るためのツール。
用途別のイメージ（プロファイル）を同梱していて、用途ごとに隔離環境を分けられる。

```console
$ cd ~/src/myapp
$ koya run python             # python 用の隔離環境に入る（なければ作る）
(koya:python) /home/me/src/myapp $ exit
$ koya run node -- npm test   # node 用の環境でコマンドを1つ実行する
$ koya ls
PROFILE  STATUS   NAME
node     running  koya-node-myapp-3fa2b91c
python   running  koya-python-myapp-3fa2b91c
```

## インストール

podman か docker が必要です。

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
- 作ったファイルの所有者はホストのユーザーのままになります。
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

### 既定のプロファイル

`koya run` でプロファイルを省略すると、次の順に決めます。

1. カレントディレクトリから上にたどって見つかる `.koya` ファイル（中身はプロファイル名）
2. 環境変数 `KOYA_PROFILE`
3. `base`

## プロファイル

| 名前 | 内容 |
|---|---|
| `base` | Debian stable + git, curl, jq, sudo など |
| `python` | base + python3, pip, venv, uv |
| `node` | base + Node.js LTS, npm, corepack |
| `go` | base + Go 最新安定版 |
| `fedora` | Fedora 最新版 + git, curl, jq, sudo など（base とは独立。dnf が使える） |

### 自分でプロファイルを追加する

`~/.config/koya/profiles/<名前>/Dockerfile` を置くとプロファイルとして使えます。
同じディレクトリのファイルはビルドコンテキストに入ります。同梱と同じ名前にすると同梱を上書きします。

```dockerfile
# ~/.config/koya/profiles/db/Dockerfile
FROM localhost/koya/python
RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-client sqlite3 \
 && rm -rf /var/lib/apt/lists/*
```

`FROM localhost/koya/<名前>` と書くと、そのプロファイルを先にビルドしてから引き継ぎます。

## 環境変数

| 名前 | 意味 |
|---|---|
| `KOYA_RUNTIME` | 使うランタイム（`podman` / `docker` など）。既定は podman → docker の順に自動検出 |
| `KOYA_PROFILE` | 既定のプロファイル |

設計の詳細は [docs/design.md](docs/design.md) を参照。
