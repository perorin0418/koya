// Package selfupdate は GitHub Releases から koya の最新版を取ってきて、実行ファイルを入れ替える。
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// Repo はリリースを取ってくる GitHub のリポジトリ。
const Repo = "perorin0418/koya"

var client = &http.Client{Timeout: 2 * time.Minute}

// Release は最新リリースの情報。
type Release struct {
	Tag    string
	Assets map[string]string // ファイル名 → ダウンロード URL
}

// Latest は最新リリースを調べる。
func Latest() (*Release, error) {
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	data, err := get("https://api.github.com/repos/" + Repo + "/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("最新リリースを調べられません: %w", err)
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("最新リリースの情報を読めません: %w", err)
	}
	r := &Release{Tag: body.TagName, Assets: map[string]string{}}
	for _, a := range body.Assets {
		r.Assets[a.Name] = a.URL
	}
	return r, nil
}

func get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "koya-self-update")
	if strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Accept", "application/vnd.github+json")
		// API の回数制限に引っかかったとき用（任意）
		if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// AssetName は、この OS・CPU 向けのアーカイブ名を返す。
func AssetName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("koya_%s_%s_%s%s", strings.TrimPrefix(tag, "v"), goos, goarch, ext)
}

// Compare は v1.2.3 形式のバージョンを比べる（a<b なら -1、a==b なら 0、a>b なら 1）。
// 読めない場合は ok=false。
func Compare(a, b string) (int, bool) {
	pa, ok1 := parse(a)
	pb, ok2 := parse(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1, true
		case pa[i] > pb[i]:
			return 1, true
		}
	}
	return 0, true
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	// v0.4.0-dev+abc のような後ろの部分は比較に使わない
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// checksumFor は checksums.txt（sha256sum の出力形式）から name のハッシュを探す。
func checksumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extract はアーカイブから koya の実行ファイルを取り出す。
func extract(archive []byte, name string) ([]byte, error) {
	bin := "koya"
	if strings.HasSuffix(name, ".zip") {
		bin = "koya.exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == bin && !f.FileInfo().IsDir() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s の中に %s がありません", name, bin)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == bin {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s の中に %s がありません", name, bin)
}

// Download は最新版の実行ファイルを取ってきて、チェックサムを確かめて返す。
func Download(r *Release) ([]byte, error) {
	name := AssetName(r.Tag, goruntime.GOOS, goruntime.GOARCH)
	url, ok := r.Assets[name]
	if !ok {
		return nil, fmt.Errorf("%s には %s/%s 向けのファイル（%s）がありません", r.Tag, goruntime.GOOS, goruntime.GOARCH, name)
	}
	sumsURL, ok := r.Assets["checksums.txt"]
	if !ok {
		return nil, fmt.Errorf("%s に checksums.txt がないため、中身を確かめられません", r.Tag)
	}
	sums, err := get(sumsURL)
	if err != nil {
		return nil, fmt.Errorf("checksums.txt を取ってこられません: %w", err)
	}
	want, ok := checksumFor(sums, name)
	if !ok {
		return nil, fmt.Errorf("checksums.txt に %s がありません", name)
	}
	archive, err := get(url)
	if err != nil {
		return nil, fmt.Errorf("%s を取ってこられません: %w", name, err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("%s のチェックサムが合いません（期待: %s、実際: %s）", name, want, got)
	}
	return extract(archive, name)
}

// Executable は入れ替える実行ファイルのパス（シンボリックリンクはたどる）。
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Replace は実行ファイル exe を bin の内容に入れ替える。
// 同じディレクトリに一時ファイルを書いてから名前を変えるので、途中で失敗しても元のファイルは壊れない。
// Windows では実行中のファイルを上書きできないので、元のファイルを .old に退避してから置き換える
// （.old は次に koya を起動したときに CleanupOld で消す）。
func Replace(exe string, bin []byte) error {
	info, err := os.Stat(exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".koya-update-*")
	if err != nil {
		return permissionHint(dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()|0o111); err != nil {
		return err
	}
	if goruntime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return permissionHint(dir, err)
		}
		if err := os.Rename(tmpName, exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
		return nil
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return permissionHint(dir, err)
	}
	return nil
}

func permissionHint(dir string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%s に書き込めません。管理者権限で実行するか（例: sudo koya self-update）、"+
			"書き込める場所に置いた koya で実行してください: %w", dir, err)
	}
	return err
}

// CleanupOld は Windows で前回の更新時に退避した .old を消す。
func CleanupOld() {
	if goruntime.GOOS != "windows" {
		return
	}
	if exe, err := Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}
