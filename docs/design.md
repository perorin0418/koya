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
- ホスト上の置き場所は `<home.dir>/<プロファイル名>/`（`home.scope = shared`、既定）。
  同じプロファイルなら全プロジェクトで同じホームを共有する（Claude Code へのログインが1回で済む）。
  `home.scope = project`（または `--home-scope project`）ならコンテナ名ごとに分ける。
- `home.dir` の既定は `~/.local/share/koya/home`。ランタイムがリモート（ホストの Podman など）の場合は、
  ホストから同じパスで見える場所を指定する必要がある。
- ホームのマウントはコンテナ作成時にだけ効く。設定を変えたら `koya rm -f` で作り直す（違う場合は警告を出す）。
- イメージの `$HOME` の下はマウントで隠れるため、プロファイルの Dockerfile ではホームの下に何も入れない
  （同梱プロファイルは `/usr/local` や `/go` に入れている）。

### 設定ファイル

`~/.config/koya/config`（`$XDG_CONFIG_HOME/koya/config`）。外部ライブラリなしで読めるよう「キー = 値」の形にする。

```ini
home.dir    = /mnt/pool/koya-home   # 永続ホームの置き場所
home.enable = claude, python        # 既定で永続ホームを使うプロファイル（all で全部）
home.scope  = shared                # shared / project
```

## プロファイル（同梱イメージ）

最初に同梱するもの:

| 名前 | ベース | 内容 |
|---|---|---|
| `base` | fedora:latest | git, curl, ca-certificates, sudo, less, vim, jq など共通ツール |
| `python` | base | python3, pip, venv, uv, gcc（C 拡張のビルド用） |
| `node` | base | Node.js LTS, npm |
| `go` | base | Go 最新安定版, gcc（cgo 用） |
| `claude` | node | Claude Code。永続ホームが既定で有効。自動アップデートは切り、`koya build claude` で更新する |

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
| `--home` / `--no-home` | 永続ホームを使う / 使わない |
| `--home-scope shared\|project` | 永続ホームの単位 |

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
  6. root で exec して、ホストのユーザーをコンテナ内に登録する（作成したときだけ）
  7. exec -it -w $PWD <コンテナ> <コマンド or シェル> で中に入る
     （標準入力が端末なら対話シェル、パイプならスクリプトとして読む）

`--rm` のときも同じ手順でコンテナを作り、コマンドが終わったら削除する。
```

コンテナは `sleep infinity` で常駐させ、操作はすべて `exec` で行う。複数の端末から同じ環境に入れる。
（PID 1 にはランタイムの `--init` を使い、ゾンビプロセスを回収させる）

## リポジトリ構成

```
koya/
├── cmd/koya/main.go        エントリポイント、コマンドの振り分け
├── internal/runtime/       podman/docker の検出と呼び出し
├── internal/config/        設定ファイルの読み込み
├── internal/profile/       プロファイルの解決、埋め込み、ビルド
├── internal/container/     名前付け、作成、exec、ls/stop/rm
├── profiles/               同梱プロファイル（go:embed 対象）
│   ├── base/Dockerfile, bashrc
│   ├── python/Dockerfile
│   ├── node/Dockerfile
│   ├── go/Dockerfile
│   └── claude/Dockerfile
└── docs/design.md
```

インストールは `go install github.com/perorin0418/koya/cmd/koya@latest`、
または GitHub Releases に置くバイナリ（後で GoReleaser で用意する）。

## 今回は対象外（あとで検討）

- GPU やデバイスの受け渡し
- devcontainer.json の読み込み
- リソース制限（`--memory`, `--cpus`）
