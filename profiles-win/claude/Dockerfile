# escape=`
# koya claude（Windows コンテナ）: Claude Code（node プロファイル + @anthropic-ai/claude-code）
# 設定・認証・履歴（~\.claude, ~\.claude.json）を残すため、既定で永続ホームを使う。
FROM localhost/koya/node

# postinstall（ネイティブのバイナリの準備）は、新しい npm では明示して許可しないと実行されない
RUN npm install -g --allow-scripts=@anthropic-ai/claude-code @anthropic-ai/claude-code; `
    if ($LASTEXITCODE -ne 0) { throw ('npm install: exit ' + $LASTEXITCODE) }; `
    npm cache clean --force

# 更新はコンテナ内で自動に行わず、koya build --win claude でイメージを作り直して行う
# IS_SANDBOX: 隔離環境なので、管理者（ContainerAdministrator）でも --dangerously-skip-permissions を使えるようにする
# CLAUDE_CODE_GIT_BASH_PATH: base の Git for Windows の bash を使う
ENV DISABLE_AUTOUPDATER=1 `
    IS_SANDBOX=1 `
    CLAUDE_CODE_GIT_BASH_PATH=C:\Git\bin\bash.exe

# koya.session: Windows コンテナではセッションを使えないため無視される（Linux 版とそろえて付けている）
LABEL koya.home="true" `
      koya.session="true"
