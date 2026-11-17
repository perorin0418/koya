# koya: マシンの PATH の先頭にディレクトリを足す（プロファイルのビルドで使う）。
# Windows コンテナでは Dockerfile の ENV PATH が PATH を丸ごと置き換えてしまうため、レジストリのほうに書く。
param([Parameter(Mandatory)][string[]]$Dirs)
$ErrorActionPreference = 'Stop'
$cur = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$new = (@($Dirs) + ($cur -split ';' | Where-Object { $_ -and ($Dirs -notcontains $_) })) -join ';'
[Environment]::SetEnvironmentVariable('Path', $new, 'Machine')
