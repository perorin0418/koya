package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/perorin0418/koya/internal/container"
	"github.com/perorin0418/koya/internal/profile"
	"github.com/perorin0418/koya/internal/runtime"
	"github.com/perorin0418/koya/internal/winengine"
)

const containerUsage = `使い方: koya container <ls|stop|rm> [オプション] [プロファイル...]

  koya container ls [-a]          コンテナとセッションの一覧（koya ls と同じ）
  koya container stop [プロファイル...]  停止する（koya stop と同じ）
  koya container rm [-f] [プロファイル...]  削除する（koya rm と同じ）

どれも --linux（既定）か --win で対象を選びます。
`

const imageUsage = `使い方: koya image <ls|rm|prune> [--linux | --win] [オプション]

  koya image ls                       koya のイメージの一覧（--win ではベースイメージも含めて全部）
  koya image rm [-f] <プロファイル...>   プロファイルのイメージを消す（次に使うときにビルドし直す）
  koya image rm [-f] -a               koya のイメージを全部消す
  koya image prune                    ビルドし直して古くなったイメージを消す
                                      （--win では、今のホストで使わないベースイメージも消す）

  -f   そのイメージのコンテナがあれば、コンテナごと消す（全ディレクトリが対象）
`

// cmdContainer は koya container <ls|stop|rm> を、koya ls・stop・rm に振り分ける。
func cmdContainer(args []string) error {
	if len(args) == 0 {
		fmt.Print(containerUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "list":
		return cmdLs(rest)
	case "stop":
		return cmdStop(rest)
	case "rm":
		return cmdRm(rest)
	case "help", "-h", "--help":
		fmt.Print(containerUsage)
		return nil
	default:
		return fmt.Errorf("koya container %s というコマンドはありません\n\n%s", sub, containerUsage)
	}
}

// cmdImage は koya image <ls|rm|prune> を扱う。
func cmdImage(args []string) error {
	if len(args) == 0 {
		fmt.Print(imageUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("image " + sub)
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	force := fs.Bool("f", false, "")
	all := fs.Bool("a", false, "")
	pos, err := parseInterspersed(fs, rest)
	if err != nil {
		return flagErr(err, imageUsage)
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	switch sub {
	case "ls", "list":
		if len(pos) > 0 || *force || *all {
			return errors.New(imageUsage)
		}
		rt, err := detect(*win)
		if err != nil {
			return err
		}
		return imageLs(rt)
	case "rm":
		if *all == (len(pos) > 0) {
			return fmt.Errorf("消すプロファイルを指定するか、-a を付けてください\n\n%s", imageUsage)
		}
		rt, err := detect(*win)
		if err != nil {
			return err
		}
		return imageRm(rt, pos, *all, *force)
	case "prune":
		if len(pos) > 0 || *force || *all {
			return errors.New(imageUsage)
		}
		rt, err := detect(*win)
		if err != nil {
			return err
		}
		return imagePrune(rt)
	case "help", "-h", "--help":
		fmt.Print(imageUsage)
		return nil
	default:
		return fmt.Errorf("koya image %s というコマンドはありません\n\n%s", sub, imageUsage)
	}
}

// managedFilter は koya がビルドしたイメージだけに絞る条件。
var managedFilter = []string{"--filter", "label=" + container.LabelManaged + "=true"}

// imageLs は koya のイメージを一覧にする。
// Windows コンテナのデーモンは koya 専用なので、ベースイメージも含めて全部を出す（容量の確認のため）。
func imageLs(rt *runtime.Runtime) error {
	args := []string{"images", "--format", "{{.Repository}}:{{.Tag}}\t{{.ID}}\t{{.Size}}\t{{.CreatedSince}}"}
	if !rt.Windows {
		args = append(args, managedFilter...)
	}
	out, err := rt.Output(args...)
	if err != nil {
		return err
	}
	lines := nonEmptyLines(out)
	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "koya のイメージはありません")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IMAGE\tID\tSIZE\tCREATED")
	for _, l := range lines {
		fmt.Fprintln(tw, l)
	}
	return tw.Flush()
}

// imageRm はプロファイルのイメージを消す。-f なら、そのイメージのコンテナ（全ディレクトリ）も消す。
func imageRm(rt *runtime.Runtime, profs []string, all, force bool) error {
	if all {
		out, err := rt.Output(append([]string{"images", "--format", "{{.Repository}}"}, managedFilter...)...)
		if err != nil {
			return err
		}
		profs = profilesOfRepos(nonEmptyLines(out))
		if len(profs) == 0 {
			fmt.Fprintln(os.Stderr, "koya のイメージはありません")
			return nil
		}
	} else {
		for _, p := range profs {
			if !profile.ValidName(p) {
				return fmt.Errorf("プロファイル名 %q は使えません", p)
			}
			if err := rt.Quiet("image", "inspect", profile.ImageName(p)); err != nil {
				return fmt.Errorf("%s のイメージはありません", p)
			}
		}
	}

	// そのイメージを使うコンテナ。-f がなければ消さずに止める
	var ctrs []container.Info
	for _, p := range profs {
		infos, err := container.List(rt, "", p)
		if err != nil {
			return err
		}
		ctrs = append(ctrs, infos...)
	}
	if len(ctrs) > 0 && !force {
		var names []string
		for _, c := range ctrs {
			names = append(names, c.Profile+"（"+c.Workdir+"）")
		}
		return fmt.Errorf("このイメージのコンテナがあります: %s\nkoya rm で消すか、-f を付けてコンテナごと消してください", strings.Join(names, ", "))
	}
	for _, c := range ctrs {
		if err := rt.Quiet("rm", "-f", c.Name); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "コンテナを削除しました: %s (%s)\n", c.Profile, c.Workdir)
	}

	// 親のイメージ（FROM localhost/koya/...）は子が残っていると消せないことがあるので、
	// 消せたものがある限り、残りをやり直す。
	left := profs
	var errs []error
	for len(left) > 0 {
		var next []string
		errs = nil
		for _, p := range left {
			if err := rt.Quiet("rmi", profile.ImageName(p)); err != nil {
				next = append(next, p)
				errs = append(errs, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "イメージを削除しました: %s\n", p)
		}
		if len(next) == len(left) {
			break
		}
		left = next
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// imagePrune はビルドし直して名前のなくなった koya のイメージを消す。
// Windows コンテナでは、今のホストで使わないベースイメージ（mcr.microsoft.com/windows/...）も消す。
func imagePrune(rt *runtime.Runtime) error {
	args := []string{"image", "prune", "-f"}
	if !rt.Windows {
		// Linux のランタイムはほかの用途と共用なので、koya のものに限る
		args = append(args, managedFilter...)
	}
	if err := rt.Run(args...); err != nil {
		return err
	}
	if !rt.Windows {
		return nil
	}

	base, err := winengine.HostBase()
	if err != nil {
		return err
	}
	store := winStore(base)
	p, err := store.Get("base")
	if err != nil {
		return err
	}
	keep := fromImage(p.Files["Dockerfile"], store.BuildArgs)
	out, err := rt.Output("images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return err
	}
	for _, ref := range staleWinBases(nonEmptyLines(out), keep) {
		if err := rt.Quiet("rmi", ref); err != nil {
			// koya のイメージがまだ使っている（古いベースのままのプロファイルがある）
			fmt.Fprintf(os.Stderr, "残しました: %s（使っているイメージがあります。koya build --win でビルドし直すと消せます）\n", ref)
			continue
		}
		fmt.Fprintf(os.Stderr, "ベースイメージを削除しました: %s\n", ref)
	}
	return nil
}

// staleWinBases は、Windows のベースイメージ（mcr.microsoft.com/windows/...）のうち keep 以外を返す。
func staleWinBases(refs []string, keep string) []string {
	var out []string
	for _, r := range refs {
		if strings.HasPrefix(r, "mcr.microsoft.com/windows/") && !strings.HasSuffix(r, ":<none>") && r != keep {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// fromImage は Dockerfile の最初の FROM のイメージを、ビルド引数（${KEY} / $KEY）を埋めて返す。
func fromImage(dockerfile []byte, buildArgs []string) string {
	sc := bufio.NewScanner(strings.NewReader(string(dockerfile)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || !strings.EqualFold(f[0], "FROM") {
			continue
		}
		img := ""
		for _, x := range f[1:] {
			if !strings.HasPrefix(x, "--") {
				img = x
				break
			}
		}
		for _, a := range buildArgs {
			k, v, ok := strings.Cut(a, "=")
			if ok {
				img = strings.ReplaceAll(img, "${"+k+"}", v)
				img = strings.ReplaceAll(img, "$"+k, v)
			}
		}
		return img
	}
	return ""
}

// profilesOfRepos は localhost/koya/<名前> のリポジトリ名からプロファイル名を取り出す（名前のないものは除く）。
func profilesOfRepos(repos []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range repos {
		r = strings.TrimPrefix(r, "localhost/")
		name, ok := strings.CutPrefix(r, "koya/")
		if !ok || !profile.ValidName(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
