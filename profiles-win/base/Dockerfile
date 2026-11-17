# escape=`
# koya base（Windows コンテナ）: Windows Server Core + Windows PowerShell
# BASE_TAG は koya がホストの OS のビルドに合わせて渡す（ltsc2022 / ltsc2025 など）。
# プロセス分離では、ホストとイメージのビルドが合っていないと起動できないため。
ARG BASE_TAG=ltsc2022
FROM mcr.microsoft.com/windows/servercore:${BASE_TAG}

SHELL ["powershell.exe", "-NoLogo", "-NoProfile", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]

ENV KOYA_WIN=1
CMD ["powershell.exe", "-NoLogo"]
