// Package winengine は koya run --win で使う、koya 専用の Windows コンテナ用デーモン（dockerd.exe）を扱う。
//
// Docker の公式の静的バイナリ（docker-<版>.zip）を取ってきて SHA-256 を照合し、
// Program Files の下に置いて Windows サービスとして登録する。
// Rancher Desktop や Docker Desktop のデーモンとぶつからないよう、名前付きパイプ・データの置き場所・
// サービス名はすべて koya 専用のものを使う。
package winengine

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// 取ってくる Docker の静的バイナリ。版を上げるときは、ハッシュも実物から計算し直して書き換える。
const (
	DockerVersion = "29.9.3"
	DockerSHA256  = "7c4ff7bbb31579046cd34dd8b91f7cbd3ab3482a1a85330170be726ef792d751"
	downloadBase  = "https://download.docker.com/win/static/stable/x86_64/"
)

// koya 専用の名前。
const (
	ServiceName = "koya-docker"
	PipeName    = "koya_docker"
	Host        = "npipe:////./pipe/" + PipeName
)

// DownloadURL は取ってくる zip の URL。
func DownloadURL() string { return downloadBase + "docker-" + DockerVersion + ".zip" }

// Paths はデーモンが使うファイルの置き場所。
type Paths struct {
	BinDir   string // dockerd.exe / docker.exe（管理者だけが書き込める場所）
	DataRoot string // イメージやコンテナ
	ExecRoot string
	PidFile  string
	Config   string // daemon.json（ほかの dockerd の設定を読まないよう、専用のものを使う）
}

// PathsFor は Program Files と ProgramData の場所から Paths を作る。
func PathsFor(programFiles, programData string) Paths {
	data := filepath.Join(programData, "koya")
	return Paths{
		BinDir:   filepath.Join(programFiles, "koya", "docker-"+DockerVersion),
		DataRoot: filepath.Join(data, "docker"),
		ExecRoot: filepath.Join(data, "exec-root"),
		PidFile:  filepath.Join(data, "docker.pid"),
		Config:   filepath.Join(data, "daemon.json"),
	}
}

// DefaultPaths は環境変数から Paths を作る。
func DefaultPaths() Paths {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return PathsFor(pf, pd)
}

// Dockerd / DockerCLI は実行ファイルのパス。
func (p Paths) Dockerd() string   { return filepath.Join(p.BinDir, "dockerd.exe") }
func (p Paths) DockerCLI() string { return filepath.Join(p.BinDir, "docker.exe") }

// DaemonArgs はサービスとして登録するときの dockerd の引数。
// group はパイプにつなげるユーザー（またはグループ）の名前。既定では管理者しかつなげないため、
// koya を実行したユーザーを指定する。
func (p Paths) DaemonArgs(group string) []string {
	args := []string{
		"--register-service",
		"--service-name", ServiceName,
		"-H", Host,
		"--data-root", p.DataRoot,
		"--exec-root", p.ExecRoot,
		"--pidfile", p.PidFile,
		"--config-file", p.Config,
	}
	if group != "" {
		args = append(args, "--group", group)
	}
	return args
}

// Download は zip を取ってきて、SHA-256 を照合してから返す。
func Download() ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(DownloadURL())
	if err != nil {
		return nil, fmt.Errorf("%s を取ってこられません: %w", DownloadURL(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s を取ってこられません: %s", DownloadURL(), resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return b, Verify(b)
}

// Verify は zip の SHA-256 が固定の値と一致するかを確かめる。
func Verify(archive []byte) error {
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != DockerSHA256 {
		return fmt.Errorf("docker-%s.zip のチェックサムが合いません（期待: %s、実際: %s）", DockerVersion, DockerSHA256, got)
	}
	return nil
}

// Extract は zip から docker/ の下の .exe を dir に取り出す。
func Extract(archive []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	found := map[string]bool{}
	for _, f := range zr.File {
		base := path.Base(f.Name)
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(base), ".exe") {
			continue
		}
		if err := extractFile(f, filepath.Join(dir, base)); err != nil {
			return err
		}
		found[strings.ToLower(base)] = true
	}
	for _, need := range []string{"dockerd.exe", "docker.exe"} {
		if !found[need] {
			return fmt.Errorf("docker-%s.zip に %s がありません", DockerVersion, need)
		}
	}
	return nil
}

func extractFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// Base はホストの OS のビルド番号から決めた、ベースイメージのタグと分離の方式。
type Base struct {
	Tag       string // mcr.microsoft.com/windows/servercore のタグ（ltsc2022 など）
	Isolation string // process / hyperv
}

// BaseFor はホストのビルド番号に合うベースイメージを選ぶ。
// プロセス分離では、ホストとイメージのビルドが合っている必要がある
// （Windows 11 は Windows Server 2022 のイメージをプロセス分離で動かせる）。
// 環境変数 KOYA_WIN_BASE_TAG / KOYA_WIN_ISOLATION で上書きできる。
func BaseFor(build int) (Base, error) {
	var b Base
	switch {
	case build >= 26100: // Windows 11 24H2 以降 / Windows Server 2025
		b = Base{"ltsc2025", "process"}
	case build >= 20348: // Windows Server 2022 / Windows 11 21H2〜23H2
		b = Base{"ltsc2022", "process"}
	case build == 17763: // Windows Server 2019 / Windows 10 1809
		b = Base{"ltsc2019", "process"}
	case build > 17763: // Windows 10 1903 以降: 合うイメージがないので Hyper-V 分離で動かす
		b = Base{"ltsc2019", "hyperv"}
	}
	if v := os.Getenv("KOYA_WIN_BASE_TAG"); v != "" {
		b.Tag = v
	}
	if v := os.Getenv("KOYA_WIN_ISOLATION"); v != "" {
		if v != "process" && v != "hyperv" {
			return Base{}, fmt.Errorf("KOYA_WIN_ISOLATION は process か hyperv です（%q）", v)
		}
		b.Isolation = v
	}
	if b.Tag == "" || b.Isolation == "" {
		return Base{}, fmt.Errorf("この Windows（ビルド %d）に合うベースイメージが分かりません。KOYA_WIN_BASE_TAG と KOYA_WIN_ISOLATION で指定してください", build)
	}
	return b, nil
}

// QuoteArg は Windows のコマンドライン用に引数を1つ引用符で囲む（CommandLineToArgvW の規則）。
func QuoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\v\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteByte(c)
	}
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}

// JoinArgs は引数を Windows のコマンドラインの文字列にする。
func JoinArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = QuoteArg(a)
	}
	return strings.Join(q, " ")
}

// psQuote は PowerShell の単一引用符の文字列にする。
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ElevateScript は exe を管理者として実行し、終わるまで待ってその終了コードで終わる PowerShell のスクリプト。
func ElevateScript(exe string, args []string) string {
	return "$p = Start-Process -FilePath " + psQuote(exe) +
		" -ArgumentList " + psQuote(JoinArgs(args)) +
		" -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
}
