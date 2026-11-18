# escape=`
# koya python（Windows コンテナ）: Python 3.14、pip、venv、uv
# 3.15 ではなく 3.14 にしているのは、Windows 向けのビルド済みパッケージ（wheel）がそろっているため。
FROM localhost/koya/base

RUN & C:\ProgramData\koya\fetch.ps1 `
      https://www.python.org/ftp/python/3.14.10/python-3.14.10-amd64.exe `
      92480239264744fe0386a1bd8275786fefa126e3129e56a68e22b52c73329965 C:\python.exe; `
    $p = Start-Process -FilePath C:\python.exe -ArgumentList '/quiet', 'InstallAllUsers=1', 'PrependPath=1', `
      'Include_test=0', 'Include_launcher=1', 'TargetDir=C:\Python314' -Wait -PassThru; `
    if ($p.ExitCode -ne 0) { throw ('python installer: exit ' + $p.ExitCode) }; `
    Remove-Item C:\python.exe

RUN & C:\ProgramData\koya\fetch.ps1 `
      https://github.com/astral-sh/uv/releases/download/0.13.8/uv-x86_64-pc-windows-msvc.zip `
      529e78a9cec6cd7e3e014f7ecd8bda0c113969d4ed0f2efeedb85c30943325b8 C:\uv.zip; `
    New-Item -ItemType Directory -Force C:\uv | Out-Null; `
    tar.exe -xf C:\uv.zip -C C:\uv; `
    if ($LASTEXITCODE -ne 0) { throw ('tar uv: exit ' + $LASTEXITCODE) }; `
    Remove-Item C:\uv.zip; `
    & C:\ProgramData\koya\add-path.ps1 C:\uv

ENV PYTHONUTF8=1
