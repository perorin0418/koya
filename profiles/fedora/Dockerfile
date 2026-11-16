# koya fedora: Fedora 最新版 + 共通ツールと一般ユーザー dev（UID/GID 1000、パスワードなし sudo）
# base（Debian）とは独立したプロファイル。dnf でパッケージを入れられる。
FROM registry.fedoraproject.org/fedora:latest

RUN dnf install -y --setopt=install_weak_deps=False \
      ca-certificates curl wget git openssh-clients sudo less vim-minimal jq \
      procps-ng unzip xz bash-completion make findutils which hostname \
 && dnf clean all

RUN groupadd -g 1000 dev \
 && useradd -m -u 1000 -g 1000 -s /bin/bash dev \
 && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev \
 && chmod 0440 /etc/sudoers.d/dev \
 && chmod 0777 /home/dev

COPY bashrc /etc/koya/bashrc

ENV LANG=C.UTF-8
CMD ["sleep", "infinity"]
