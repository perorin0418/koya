# escape=`
# koya base（Windows コンテナ）: Windows Server Core + PowerShell 7 + Git for Windows（bash 付き）
# BASE_TAG は koya がホストの OS のビルドに合わせて渡す（ltsc2022 / ltsc2025 など）。
# プロセス分離では、ホストとイメージのビルドが合っていないと起動できないため。
# ツールの版とハッシュは固定する。版を上げるときは、公式の配布元のハッシュに書き換える。
ARG BASE_TAG=ltsc2022
FROM mcr.microsoft.com/windows/servercore:${BASE_TAG}

SHELL ["powershell.exe", "-NoLogo", "-NoProfile", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]

# ビルド用の補助スクリプト（子のプロファイルでも使う）とプロンプト
COPY fetch.ps1 add-path.ps1 profile.ps1 C:/ProgramData/koya/

# PowerShell 7
RUN & C:\ProgramData\koya\fetch.ps1 `
      https://github.com/PowerShell/PowerShell/releases/download/v7.7.0/PowerShell-7.7.0-win-x64.zip `
      7c74e03b20a75b59715847439d046a6214b1863ee79b187872980f139bb4a05f C:\pwsh.zip; `
    New-Item -ItemType Directory -Force 'C:\Program Files\PowerShell\7' | Out-Null; `
    tar.exe -xf C:\pwsh.zip -C 'C:\Program Files\PowerShell\7'; `
    Remove-Item C:\pwsh.zip; `
    & C:\ProgramData\koya\add-path.ps1 'C:\Program Files\PowerShell\7'

# Git for Windows（PortableGit。bash.exe も入る。PATH には cmd だけを足し、Windows のコマンドを隠さない）
# 置き場所は空白を含まない C:\Git にする（RUN の中で二重引用符を使うと、引数の受け渡しで崩れやすいため）
RUN & C:\ProgramData\koya\fetch.ps1 `
      https://github.com/git-for-windows/git/releases/download/v2.56.1.windows.1/PortableGit-2.56.1-64-bit.7z.exe `
      9dd04a608ceb5f6ab32a73709e0ecb159d211da71b697c45ff81f6dd0c6b49c7 C:\git.7z.exe; `
    $p = Start-Process -FilePath C:\git.7z.exe -ArgumentList '-oC:\Git', '-y' -Wait -PassThru; `
    if ($p.ExitCode -ne 0) { throw ('PortableGit: exit ' + $p.ExitCode) }; `
    Remove-Item C:\git.7z.exe; `
    & C:\ProgramData\koya\add-path.ps1 C:\Git\cmd; `
    # マウントしたディレクトリの所有者はホストのユーザーなので、所有者の確認（dubious ownership）を外す
    & C:\Git\cmd\git.exe config --system --add safe.directory '*'

# プロンプト（Windows PowerShell と PowerShell 7 の全ユーザー用プロファイル）
RUN Copy-Item C:\ProgramData\koya\profile.ps1 C:\Windows\System32\WindowsPowerShell\v1.0\profile.ps1; `
    Copy-Item C:\ProgramData\koya\profile.ps1 'C:\Program Files\PowerShell\7\profile.ps1'

ENV KOYA_WIN=1
CMD ["pwsh.exe", "-NoLogo"]
