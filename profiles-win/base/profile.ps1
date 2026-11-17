# koya: プロンプトにプロファイル名を出す（Windows PowerShell と PowerShell 7 の全ユーザー用プロファイル）
function prompt {
    $p = if ($env:KOYA_PROFILE) { $env:KOYA_PROFILE } else { 'win' }
    "(koya:$p) PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) "
}
