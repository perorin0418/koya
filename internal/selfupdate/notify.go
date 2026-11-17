package selfupdate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// CheckInterval は、起動時に最新版を調べる間隔（1 日に数回）。
const CheckInterval = 6 * time.Hour

// checkTimeout は、起動時に最新版を調べるときの待ち時間の上限。起動を遅くしないよう短くする。
const checkTimeout = 3 * time.Second

// checkState は、前回の確認結果。キャッシュのファイルに JSON で書く。
type checkState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// StatePath は、確認結果を書いておくファイル。
// Linux: $XDG_CACHE_HOME/koya/update-check.json（既定 ~/.cache/koya/...）、Windows: %LOCALAPPDATA%\koya\...
func StatePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "koya", "update-check.json")
}

// Notice は、新しい版があればお知らせの文を返す（なければ空）。
// 前回の確認から CheckInterval 以上たっていれば GitHub に問い合わせ、結果を statePath に書く。
// 問い合わせに失敗しても確認した時刻は記録し、オフラインのときに毎回待たされないようにする。
func Notice(cur, statePath string) string {
	return notice(cur, statePath, time.Now(), func() (string, error) {
		r, err := latestWith(&http.Client{Timeout: checkTimeout})
		if err != nil {
			return "", err
		}
		return r.Tag, nil
	})
}

func notice(cur, statePath string, now time.Time, fetch func() (string, error)) string {
	if _, ok := parse(cur); !ok || statePath == "" {
		return "" // 開発版などは比べられない
	}
	var st checkState
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	// 時計が戻った場合（CheckedAt が未来）も調べ直す
	if age := now.Sub(st.CheckedAt); age >= CheckInterval || age < 0 {
		latest, err := fetch()
		st.CheckedAt = now
		if err == nil {
			st.Latest = latest
		}
		if data, err := json.Marshal(st); err == nil {
			if os.MkdirAll(filepath.Dir(statePath), 0o755) == nil {
				_ = os.WriteFile(statePath, data, 0o644)
			}
		}
	}
	if cmp, ok := Compare(cur, st.Latest); ok && cmp < 0 {
		return fmt.Sprintf("新しい版があります: %s → %s（koya self-update で更新できます）", cur, st.Latest)
	}
	return ""
}
