# koya claude: Claude Code（node プロファイル + @anthropic-ai/claude-code）
# 設定・認証・履歴（~/.claude, ~/.claude.json）を残すため、既定で永続ホームを使う。
# 端末が切れても Claude Code が動き続けるよう、既定でセッション（--session）を使う。
FROM localhost/koya/node

RUN npm install -g @anthropic-ai/claude-code \
 && npm cache clean --force \
 && dnf install -y --setopt=install_weak_deps=False ripgrep \
 && dnf clean all

# 更新はコンテナ内で自動に行わず、koya build claude でイメージを作り直して行う
# IS_SANDBOX: 隔離環境なので、root で動く場合（rootless の docker CLI 経由）でも
#             --dangerously-skip-permissions を使えるようにする
ENV DISABLE_AUTOUPDATER=1 \
    IS_SANDBOX=1

LABEL koya.home="true" \
      koya.session="true"
