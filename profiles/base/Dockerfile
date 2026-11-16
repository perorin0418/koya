# koya base: Fedora 最新版 + 共通ツール
# コンテナ内のユーザーは、koya がコンテナ作成時に「koya を実行したホストのユーザー」として登録する。
# そのため、イメージには一般ユーザーを作らず、$HOME の下にも何も置かない（永続ホームで隠れるため）。
FROM registry.fedoraproject.org/fedora:latest

RUN dnf install -y --setopt=install_weak_deps=False \
      ca-certificates curl wget git openssh-clients sudo less vim-minimal jq \
      procps-ng unzip xz bash-completion make findutils which hostname \
      tmux ncurses \
 && dnf clean all

COPY bashrc /etc/koya/bashrc
COPY tmux.conf /etc/koya/tmux.conf

ENV LANG=C.UTF-8
CMD ["sleep", "infinity"]
