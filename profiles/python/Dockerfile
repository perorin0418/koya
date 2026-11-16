# koya python: python3, pip, venv, uv
FROM localhost/koya/base

RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-pip python3-venv python3-dev build-essential \
 && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/
