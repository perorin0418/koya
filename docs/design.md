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

マウントしたファイルの所有者がホストのユーザーのままになるようにする。

- 各イメージには UID/GID 1000 の一般ユーザー `dev` を作り、パスワードなし sudo を許可する。
- podman CLI（rootless）: `--userns=keep-id:uid=1000,gid=1000` でホストのユーザーをコンテナ内の `dev` に対応づける。
- docker CLI で rootless のランタイム（rootless docker、または docker CLI からつないだ rootless podman）: `--user 0:0` で起動する。
  rootless ではコンテナ内の root がホストのユーザーに対応するため、作ったファイルはホストのユーザーの所有になる。
  （docker CLI は `--userns=keep-id` を受け付けない）
- docker（rootful）: `--user <ホストのUID>:<GID>` で起動する。ホストの UID が 1000 でない場合に備え、
  `HOME=/home/dev` を書き込み可能にしておく。

## プロファイル（同梱イメージ）

最初に同梱するもの:

| 名前 | ベース | 内容 |
|---|---|---|
| `base` | debian:stable-slim | git, curl, ca-certificates, sudo, less, vim, jq など共通ツール |
| `python` | base | python3, pip, venv, uv |
| `node` | base | Node.js LTS, npm |
| `go` | base | Go 最新安定版 |
| `fedora` | fedora:latest | base と同じ共通ツールと `dev` ユーザー。base とは独立 |

- プロファイルは `profiles/<名前>/Dockerfile` に置き、`go:embed` でバイナリに埋め込む。
- `base` 以外は `FROM localhost/koya/base` で base を引き継ぐ。依存するイメージは自動で先にビルドする。
- イメージ名は `localhost/koya/<名前>:latest`。`localhost/` を付けるのは、ローカルにないときに
  Docker Hub の同名イメージ（第三者のもの）を取ってこないようにするため。
- Dockerfile の内容（親プロファイルを含む）のハッシュをラベル `koya.hash` に入れ、
  変わったら次回起動時に自動で再ビルドする。既存のコンテナは古いイメージのままなので、作り直すよう警告を出す。

### ユーザー定義プロファイル

`~/.config/koya/profiles/<名前>/Dockerfile` を置くと、プロファイルとして使える。
同名の同梱プロファイルがある場合はユーザー定義を優先する（同梱の上書き）。

## 隔離のオプション

| オプション | 効果 |
|---|---|
| `--net none` | ネットワークを切る（`--network none`）。信用しないコードを動かすとき用 |
| `--ro` | カレントディレクトリを読み取り専用でマウントする |
| `-e KEY=VAL` | 環境変数を渡す |
| `-p 8080:8080` | ポートを公開する |
| `-v src:dst` | 追加のマウント |

これらはコンテナ作成時にだけ効く。既存のコンテナと違うオプションを指定した場合は、
警告を出して `koya rm` で作り直すよう促す（作成時の設定はラベル `koya.opts` に記録して比較する）。

## コマンド体系

```
koya run [プロファイル] [オプション] [-- コマンド...]   環境に入る / コマンドを実行する
koya ls [-a]                  このディレクトリのコンテナ一覧（-a で全ディレクトリ）
koya stop [プロファイル]       停止する（省略時はこのディレクトリの全プロファイル）
koya rm [プロファイル] [-f]    削除する（省略時はこのディレクトリの全プロファイル）
koya profiles                 使えるプロファイルの一覧
koya build [プロファイル]      イメージをビルド/再ビルドする
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
  6. exec -it -w $PWD <コンテナ> <コマンド or シェル> で中に入る
     （標準入力が端末なら対話シェル、パイプならスクリプトとして読む）
```

コンテナは `sleep infinity` で常駐させ、操作はすべて `exec` で行う。複数の端末から同じ環境に入れる。
（PID 1 にはランタイムの `--init` を使い、ゾンビプロセスを回収させる）

## リポジトリ構成

```
koya/
├── cmd/koya/main.go        エントリポイント、コマンドの振り分け
├── internal/runtime/       podman/docker の検出と呼び出し
├── internal/profile/       プロファイルの解決、埋め込み、ビルド
├── internal/container/     名前付け、作成、exec、ls/stop/rm
├── profiles/               同梱プロファイル（go:embed 対象）
│   ├── base/Dockerfile
│   ├── python/Dockerfile
│   ├── node/Dockerfile
│   ├── go/Dockerfile
│   └── fedora/Dockerfile, bashrc
└── docs/design.md
```

インストールは `go install github.com/perorin0418/koya/cmd/koya@latest`、
または GitHub Releases に置くバイナリ（後で GoReleaser で用意する）。

## 今回は対象外（あとで検討）

- GPU やデバイスの受け渡し
- devcontainer.json の読み込み
- コンテナ内での Claude Code など AI エージェント用プロファイル（候補として有力）
- リソース制限（`--memory`, `--cpus`）
