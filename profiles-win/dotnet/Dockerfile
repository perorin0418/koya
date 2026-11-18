# escape=`
# koya dotnet（Windows コンテナ）: .NET SDK 10（LTS）
FROM localhost/koya/base

RUN & C:\ProgramData\koya\fetch.ps1 `
      https://builds.dotnet.microsoft.com/dotnet/Sdk/10.0.404/dotnet-sdk-10.0.404-win-x64.zip `
      330ddd2665b43cd2e428216a0d8ea1ac7bb19b5434fdc530e62010dc3b52823a620bf95489d628e1ed6f5122401793ee4887cca4a019122361a1231497f632ee `
      C:\dotnet.zip; `
    New-Item -ItemType Directory -Force C:\dotnet | Out-Null; `
    tar.exe -xf C:\dotnet.zip -C C:\dotnet; `
    if ($LASTEXITCODE -ne 0) { throw ('tar dotnet: exit ' + $LASTEXITCODE) }; `
    Remove-Item C:\dotnet.zip; `
    & C:\ProgramData\koya\add-path.ps1 C:\dotnet

ENV DOTNET_ROOT=C:\dotnet `
    DOTNET_NOLOGO=1
