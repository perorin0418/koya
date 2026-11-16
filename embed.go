// Package koya は同梱プロファイル（Dockerfile 一式）を埋め込む。
package koya

import "embed"

// Profiles は profiles/<名前>/ 以下のファイルを持つ。
//
//go:embed all:profiles
var Profiles embed.FS
