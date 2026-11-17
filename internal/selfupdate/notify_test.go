package selfupdate

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update-check.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	calls := 0
	fetch := func(tag string, err error) func() (string, error) {
		return func() (string, error) { calls++; return tag, err }
	}

	// 初回は問い合わせる
	if got := notice("v0.7.0", path, now, fetch("v0.8.0", nil)); !strings.Contains(got, "v0.7.0 → v0.8.0") {
		t.Errorf("初回: %q", got)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}

	// 間隔内は問い合わせず、記録した結果でお知らせする
	if got := notice("v0.7.0", path, now.Add(time.Hour), fetch("v0.9.0", nil)); !strings.Contains(got, "v0.8.0") {
		t.Errorf("間隔内: %q", got)
	}
	if calls != 1 {
		t.Fatalf("間隔内に問い合わせた: calls = %d", calls)
	}

	// 更新済みならお知らせしない
	if got := notice("v0.8.0", path, now.Add(time.Hour), fetch("", nil)); got != "" {
		t.Errorf("更新済み: %q", got)
	}

	// 間隔を過ぎたら問い合わせ直す。失敗したら前回の結果を使い、時刻は記録する
	later := now.Add(CheckInterval)
	if got := notice("v0.7.0", path, later, fetch("", errors.New("offline"))); !strings.Contains(got, "v0.8.0") {
		t.Errorf("失敗時: %q", got)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	notice("v0.7.0", path, later.Add(time.Minute), fetch("", errors.New("offline")))
	if calls != 2 {
		t.Fatalf("失敗後すぐに問い合わせ直した: calls = %d", calls)
	}

	// 開発版は比べられないので何もしない
	if got := notice("dev", path, later.Add(CheckInterval), fetch("v0.9.0", nil)); got != "" || calls != 2 {
		t.Errorf("開発版: %q calls=%d", got, calls)
	}
}
