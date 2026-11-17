# escape=`
# koya node（Windows コンテナ）: Node.js LTS、npm、corepack
FROM localhost/koya/base

RUN & C:\ProgramData\koya\fetch.ps1 `
      https://nodejs.org/dist/v26.13.1/node-v26.13.1-win-x64.zip `
      e2edda75bfb089654705cf65a13b58933bfd5f57148e8ebda916f58a2474f1f7 C:\node.zip; `
    tar.exe -xf C:\node.zip -C C:\; `
    Rename-Item C:\node-v26.13.1-win-x64 C:\nodejs; `
    Remove-Item C:\node.zip; `
    New-Item -ItemType Directory -Force C:\npm-global | Out-Null; `
    & C:\ProgramData\koya\add-path.ps1 C:\npm-global, C:\nodejs

# グローバルインストール先（Windows では prefix の直下にコマンドが置かれる）
ENV NPM_CONFIG_PREFIX=C:\npm-global
