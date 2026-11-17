# koya node: Node.js LTS, npm, corepack
FROM docker.io/library/node:lts-slim AS node

FROM localhost/koya/base
RUN dnf install -y --setopt=install_weak_deps=False libatomic libstdc++ \
 && dnf clean all
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node /usr/local/include/node /usr/local/include/node
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && ln -s ../lib/node_modules/corepack/dist/corepack.js /usr/local/bin/corepack \
 && mkdir -p /usr/local/share/npm-global && chmod 0777 /usr/local/share/npm-global
# グローバルインストール先を一般ユーザーでも書き込める場所にする
ENV NPM_CONFIG_PREFIX=/usr/local/share/npm-global \
    PATH=/usr/local/share/npm-global/bin:$PATH
