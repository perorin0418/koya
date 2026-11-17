# koya: URL からファイルを取ってきて、ハッシュを照合する（プロファイルのビルドで使う）。
# 使い方: & C:\ProgramData\koya\fetch.ps1 <URL> <SHA256 か SHA512（16進）> <保存先>
param(
    [Parameter(Mandatory)][string]$Url,
    [Parameter(Mandatory)][string]$Hash,
    [Parameter(Mandatory)][string]$OutFile
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Write-Host "koya: $Url"
Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
$algo = if ($Hash.Length -eq 128) { 'SHA512' } else { 'SHA256' }
$got = (Get-FileHash -Path $OutFile -Algorithm $algo).Hash
if ($got -ne $Hash.ToUpperInvariant()) {
    Remove-Item -Force $OutFile
    throw "checksum mismatch: $Url (expected $Hash, got $got)"
}
