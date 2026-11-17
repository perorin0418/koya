# koya python: python3, pip, venv, uv（C 拡張のビルド用に gcc なども入れる）
FROM localhost/koya/base

RUN dnf install -y --setopt=install_weak_deps=False \
      python3 python3-pip python3-devel gcc gcc-c++ \
 && dnf clean all

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/
