# koya base: 共通ツールと一般ユーザー dev（UID/GID 1000、パスワードなし sudo）
FROM docker.io/library/debian:stable-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl wget git openssh-client sudo less vim-tiny jq \
      procps unzip xz-utils bash-completion make \
 && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 1000 dev \
 && useradd -m -u 1000 -g 1000 -s /bin/bash dev \
 && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev \
 && chmod 0440 /etc/sudoers.d/dev \
 && chmod 0777 /home/dev

COPY bashrc /etc/koya/bashrc

ENV LANG=C.UTF-8
CMD ["sleep", "infinity"]
