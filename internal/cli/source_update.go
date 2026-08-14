package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/i18n"
	"reasonix/internal/sourceupdate"

	"github.com/spf13/pflag"
)

const sourceUpdateTimeout = 15 * time.Second

var checkSourceUpdate = sourceupdate.Check

const sourceUpdateUsage = `用法：
  reasonix source-update --check [--root 路径] [--json]

只读检查本地源码工作树的 origin/main-v2 基线与官方 main-v2 是否有变化。
不会执行 git fetch、合并源码、下载二进制或替换任何 Reasonix 文件。
未提供 --root 时读取 REASONIX_SOURCE_ROOT。
`

func sourceUpdateCommand(args []string) int {
	fs := pflag.NewFlagSet("source-update", pflag.ContinueOnError)
	fs.SetInterspersed(true)
	var parseOutput bytes.Buffer
	fs.SetOutput(&parseOutput)
	fs.Usage = func() { _, _ = parseOutput.WriteString(sourceUpdateUsage) }
	check := fs.Bool("check", false, "执行只读源码更新检查")
	jsonOutput := fs.Bool("json", false, "输出机器可读 JSON")
	root := fs.String("root", "", "源码工作树路径；默认读取 REASONIX_SOURCE_ROOT")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			fmt.Fprint(os.Stdout, parseOutput.String())
			return 0
		}
		fmt.Fprint(os.Stderr, parseOutput.String())
		return 2
	}
	if !*check {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "source-update requires --check; this command is read-only")
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "source-update accepts no positional arguments")
		return 2
	}
	resolvedRoot := strings.TrimSpace(*root)
	if resolvedRoot == "" {
		resolvedRoot = strings.TrimSpace(os.Getenv("REASONIX_SOURCE_ROOT"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), sourceUpdateTimeout)
	defer cancel()
	result, err := checkSourceUpdate(ctx, resolvedRoot)
	if *jsonOutput {
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return 1
		}
		if err != nil {
			return 1
		}
		return 0
	}
	printSourceUpdateResult(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	return 0
}

func printSourceUpdateResult(result sourceupdate.Result) {
	fmt.Printf("源码更新状态：%s\n", result.Status)
	if result.SourceRoot != "" {
		fmt.Printf("源码目录：%s\n", result.SourceRoot)
	}
	if result.Branch != "" {
		fmt.Printf("当前分支：%s\n", result.Branch)
	}
	if result.LocalBase != "" {
		fmt.Printf("本地 main-v2 基线：%s\n", result.LocalBase)
	}
	if result.RemoteHead != "" {
		fmt.Printf("远程 main-v2：%s\n", result.RemoteHead)
	}
	if result.HasLocalPatches {
		fmt.Println("本地贡献提交：存在，未纳入远程更新判断")
	}
	if result.Message != "" {
		fmt.Printf("说明：%s\n", result.Message)
	}
}
