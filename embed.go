// Package koya は同梱プロファイル（Dockerfile 一式）を埋め込む。
package koya

import "embed"

// Profiles は profiles-linux/<名前>/（Linux コンテナ用）と profiles-win/<名前>/（Windows コンテナ用）以下のファイルを持つ。
//
//go:embed all:profiles-linux all:profiles-win
var Profiles embed.FS
