// Package config は ~/.config/koya/config を読む。
//
// 形式は「キー = 値」の行。# 以降はコメント。
//
//	home.dir    = /mnt/pool/koya-home   # 永続ホームの置き場所
//	home.enable = claude, python        # 既定で永続ホームを使うプロファイル（all で全部）
//	home.scope  = shared                # shared（プロファイルごと）/ project（プロジェクトごと）
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// Config は koya の設定。
type Config struct {
	HomeDir    string   // 永続ホームの置き場所
	HomeEnable []string // 既定で永続ホームを使うプロファイル
	HomeScope  string   // shared / project
	Path       string   // 読み込んだファイル（なければ空）
}

// Dir は設定ディレクトリ。
// Linux / macOS: $XDG_CONFIG_HOME/koya（既定 ~/.config/koya）、Windows: %APPDATA%\koya
func Dir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" && goruntime.GOOS == "windows" {
		base, _ = os.UserConfigDir()
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "koya")
}

// DefaultHomeDir は永続ホームの既定の置き場所。
// Linux / macOS: $XDG_DATA_HOME/koya/home（既定 ~/.local/share/koya/home）、Windows: %LOCALAPPDATA%\koya\home
func DefaultHomeDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" && goruntime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "koya", "home")
}

// Load は設定ファイルを読む。ファイルがなければ既定値を返す。
func Load() (*Config, error) {
	c := &Config{HomeDir: DefaultHomeDir(), HomeScope: "shared"}
	path := filepath.Join(Dir(), "config")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c.Path = path
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: 「キー = 値」の形で書いてください", path, n)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "home.dir":
			c.HomeDir = expandHome(v)
		case "home.enable":
			c.HomeEnable = nil
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					c.HomeEnable = append(c.HomeEnable, p)
				}
			}
		case "home.scope":
			if v != "shared" && v != "project" {
				return nil, fmt.Errorf("%s:%d: home.scope は shared か project です", path, n)
			}
			c.HomeScope = v
		default:
			return nil, fmt.Errorf("%s:%d: 不明なキー %q", path, n, k)
		}
	}
	return c, sc.Err()
}

// HomeEnabled は、そのプロファイルで既定で永続ホームを使うかを返す。
func (c *Config) HomeEnabled(prof string) bool {
	for _, p := range c.HomeEnable {
		if p == prof || p == "all" {
			return true
		}
	}
	return false
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
