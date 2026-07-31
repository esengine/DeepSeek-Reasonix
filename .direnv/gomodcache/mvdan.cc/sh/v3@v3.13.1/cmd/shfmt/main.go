// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// shfmt formats shell programs.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	maybeio "github.com/google/renameio/v2/maybe"
	diffpkg "github.com/rogpeppe/go-internal/diff"
	"golang.org/x/term"
	"mvdan.cc/editorconfig"

	"mvdan.cc/sh/v3/fileutil"
	"mvdan.cc/sh/v3/syntax"
	"mvdan.cc/sh/v3/syntax/typedjson"
)

type boolStringValue string

func (b *boolStringValue) Set(val string) error {
	*b = boolStringValue(val)
	return nil
}
func (b *boolStringValue) String() string {
	return string(*b)
}
func (*boolStringValue) IsBoolFlag() bool { return true }

func boolStringVar(p *string, name string, value string, usage string) {
	*p = value
	flag.Var((*boolStringValue)(p), name, usage)
}

func langVariantVar(p *syntax.LangVariant, name string, value syntax.LangVariant, usage string) {
	*p = value
	flag.Var(p, name, usage)
}

type multiFlag[T any] struct {
	short, long string
	val         T
}

func flagVal[T any](short, long string, val T, register func(*T, string, T, string)) *multiFlag[T] {
	f := &multiFlag[T]{short, long, val}
	if short != "" {
		register(&f.val, short, val, "")
	}
	if long != "" {
		register(&f.val, long, val, "")
	}
	return f
}

var (
	// Generic flags.
	versionFlag = flagVal("", "version", false, flag.BoolVar)
	list        = flagVal("l", "list", "false", boolStringVar)
	write       = flagVal("w", "write", false, flag.BoolVar)
	diff        = flagVal("d", "diff", false, flag.BoolVar)
	applyIgnore = flagVal("", "apply-ignore", false, flag.BoolVar)
	filename    = flagVal("", "filename", "", flag.StringVar)

	// Parser flags.
	lang     = flagVal("ln", "language-dialect", syntax.LangAuto, langVariantVar)
	posix    = flagVal("p", "posix", false, flag.BoolVar)
	simplify = flagVal("s", "simplify", false, flag.BoolVar)
	// TODO: when promoting exp.recover to a stable flag, add it as an EditorConfig knob too, and perhaps rename to recover-errors
	expRecover = flagVal("", "exp.recover", 0, flag.IntVar)

	// Printer flags.
	indent      = flagVal("i", "indent", 0, flag.UintVar)
	binNext     = flagVal("bn", "binary-next-line", false, flag.BoolVar)
	caseIndent  = flagVal("ci", "case-indent", false, flag.BoolVar)
	spaceRedirs = flagVal("sr", "space-redirects", false, flag.BoolVar)
	keepPadding = flagVal("kp", "keep-padding", false, flag.BoolVar)
	funcNext    = flagVal("fn", "func-next-line", false, flag.BoolVar)
	minify      = flagVal("mn", "minify", false, flag.BoolVar)

	// Utility flags.
	find     = flagVal("f", "find", "false", boolStringVar)
	toJSON   = flagVal("tojson", "to-json", false, flag.BoolVar) // TODO(v4): remove "tojson" for consistency
	fromJSON = flagVal("", "from-json", false, flag.BoolVar)

	// useEditorConfig will be false if any parser or printer flags were used.
	useEditorConfig = true

	parser            *syntax.Parser
	printer           *syntax.Printer
	readBuf, writeBuf bytes.Buffer
	color             bool

	copyBuf = make([]byte, 32*1024)
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: shfmt [flags] [path ...]

shfmt formats shell programs. If the only argument is a dash ('-') or no
arguments are given, standard input will be used. If a given path is a
directory, all shell scripts found under that directory will be used.

  --version  show version and exit

  -l[=0], --list[=0]  error with a list of files whose formatting differs from shfmt;
                      paths are separated by a newline or a null character if -l=0
  -w,     --write     write result to file instead of stdout
  -d,     --diff      error with a diff when the formatting differs
  --apply-ignore      always apply EditorConfig ignore rules
  --filename str      provide a name for the standard input file

Parser options:

  -ln, --language-dialect str  bash/posix/mksh/bats/zsh, default "auto"
  -p,  --posix                 shorthand for -ln=posix
  -s,  --simplify              simplify the code

Printer options:

  -i,  --indent uint       0 for tabs (default), >0 for number of spaces
  -bn, --binary-next-line  binary ops like && and | may start a line
  -ci, --case-indent       switch cases will be indented
  -sr, --space-redirects   redirect operators will be followed by a space
  -kp, --keep-padding      keep column alignment paddings
  -fn, --func-next-line    function opening braces are placed on a separate line
  -mn, --minify             minify the code to reduce its size (implies -s)

Utilities:

  -f[=0], --find[=0]  recursively find all shell files and print the paths;
                      paths are separated by a newline or a null character if -f=0
  --to-json           print syntax tree to stdout as a typed JSON
  --from-json         read syntax tree from stdin as a typed JSON

Formatting options can also be read from EditorConfig files; see 'man shfmt'
for a detailed description of the tool's behavior.
For more information and to report bugs, see https://github.com/mvdan/sh.
`)
	}
	flag.Parse()

	if versionFlag.val {
		version := "(unknown)"
		if info, ok := debug.ReadBuildInfo(); ok {
			mod := &info.Main
			if mod.Replace != nil {
				mod = mod.Replace
			}
			version = mod.Version
		}
		fmt.Println(version)
		return
	}
	if posix.val && lang.val != syntax.LangAuto {
		fmt.Fprintf(os.Stderr, "-p and -ln=lang cannot coexist\n")
		os.Exit(1)
	}
	if list.val != "true" && list.val != "false" && list.val != "0" {
		fmt.Fprintf(os.Stderr, "only -l and -l=0 allowed\n")
		os.Exit(1)
	}
	if find.val != "true" && find.val != "false" && find.val != "0" {
		fmt.Fprintf(os.Stderr, "only -f and -f=0 allowed\n")
		os.Exit(1)
	}
	simplify.val = simplify.val || minify.val
	flag.Visit(func(f *flag.Flag) {
		// This list should be in sync with the grouping of parser and printer options
		// as shown by ./shfmt.1.scd.
		switch f.Name {
		case lang.short, lang.long,
			posix.short, posix.long,
			simplify.short, simplify.long,
			indent.short, indent.long,
			binNext.short, binNext.long,
			caseIndent.short, caseIndent.long,
			spaceRedirs.short, spaceRedirs.long,
			keepPadding.short, keepPadding.long,
			funcNext.short, funcNext.long,
			minify.short, minify.long:
			useEditorConfig = false
		}
	})
	parser = syntax.NewParser(syntax.KeepComments(true))
	printer = syntax.NewPrinter(syntax.Minify(minify.val))

	syntax.RecoverErrors(expRecover.val)(parser)

	if !useEditorConfig {
		if posix.val {
			// -p equals -ln=posix
			lang.val = syntax.LangPOSIX
		}

		syntax.Indent(indent.val)(printer)
		syntax.BinaryNextLine(binNext.val)(printer)
		syntax.SwitchCaseIndent(caseIndent.val)(printer)
		syntax.SpaceRedirects(spaceRedirs.val)(printer)
		syntax.KeepPadding(keepPadding.val)(printer)
		syntax.FunctionNextLine(funcNext.val)(printer)
	}

	// Decide whether or not to use color for the diff output,
	// as described in shfmt.1.scd.
	if os.Getenv("FORCE_COLOR") != "" {
		color = true
	} else if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
	} else if term.IsTerminal(int(os.Stdout.Fd())) {
		color = true
	}
	// TODO(v4): show the help text on zero arguments,
	// having the user run `shfmt -` if they want to format stdin.
	// Using a dash is more explicit, and new users can easily be
	// confused by `shfmt` seemingly hanging forever.
	if flag.NArg() == 0 || (flag.NArg() == 1 && flag.Arg(0) == "-") {
		name := "<standard input>"
		if toJSON.val {
			name = "" // the default is not useful there
		}
		if filename.val != "" {
			name = filename.val
		}
		if err := formatStdin(name); err != nil {
			if err != errFormattingDiffers {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		return
	}
	if filename.val != "" {
		fmt.Fprintln(os.Stderr, "-filename can only be used with stdin")
		os.Exit(1)
	}
	if toJSON.val {
		fmt.Fprintln(os.Stderr, "--to-json can only be used with stdin")
		os.Exit(1)
	}
	status := 0
	for _, path := range flag.Args() {
		explicit := true
		if err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			defer func() { explicit = false }()
			if err != nil {
				return err
			}
			if entry.IsDir() && vcsDir.MatchString(entry.Name()) {
				return filepath.SkipDir
			}
			// If the path is not an explicit arg, or if --apply-ignore is set,
			// we find and apply EditorConfig ignore rules.
			if !explicit || applyIgnore.val {
				// We don't know the language variant at this point yet, as we are walking directories
				// and we first want to tell if we should skip a path entirely.
				//
				// TODO: Should the call to Find with the language name check "ignore" too, then?
				// Otherwise, a [[bash]] section with ignore=true is effectively never used.
				//
				// TODO: Should there be a way to explicitly turn off ignore rules when walking?
				// Perhaps swapping the default to --apply-ignore=auto and allowing --apply-ignore=false?
				// I don't imagine it's a particularly useful scenario for now.
				props, err := ecQuery.Find(path, []string{"shell"})
				if err != nil {
					return err
				}
				if props.Get("ignore") == "true" {
					if entry.IsDir() {
						return filepath.SkipDir
					} else {
						return nil
					}
				}
			}
			conf := fileutil.ConfIsScript
			// If the path is an explicit arg and it points to a symlink,
			// resolve that symlink so that we don't try to format non-regular files.
			if explicit && entry.Type()&fs.ModeSymlink != 0 {
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				entry = fs.FileInfoToDirEntry(info)
			}
			// If the path is not an explicit arg, or it's not regular, or --find is set,
			// then we check for extensions and shebangs.
			if !explicit || !entry.Type().IsRegular() || find.val == "true" {
				conf = fileutil.CouldBeScript2(entry)
				if conf == fileutil.ConfNotScript {
					return nil
				}
			}
			err = formatPath(path, conf == fileutil.ConfIfShebang)
			if err == errFormattingDiffers {
				status = 1
				err = nil
			} else if err != nil {
				fmt.Fprintln(os.Stderr, err)
				status = 1
			}
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	os.Exit(status)
}

var vcsDir = regexp.MustCompile(`^\.(git|svn|hg)$`)

var errFormattingDiffers = fmt.Errorf("")

func formatStdin(name string) error {
	if write.val {
		return fmt.Errorf("-w cannot be used on standard input")
	}
	if applyIgnore.val {
		// Mimic the logic from walkPath to apply the ignore rules.
		props, err := ecQuery.Find(name, []string{"shell"})
		if err != nil {
			return err
		}
		if props.Get("ignore") == "true" {
			return nil
		}
	}
	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	l := lang.val
	if l == syntax.LangAuto {
		if l = langFromFilename(name); l == syntax.LangAuto {
			shebangLang := fileutil.Shebang(src)
			if err := l.Set(shebangLang); err != nil {
				// Fall back to bash.
				l = syntax.LangBash
			}
		}
	}
	return formatBytes(src, name, l)
}

func langFromFilename(name string) syntax.LangVariant {
	// Detect shell config files, which typically have no extension nor shebang.
	// Note that these are not matched by [fileutil.CouldBeScript2],
	// because these files are typically not part of source code projects,
	// so finding these files when formatting entire directories is unnecessary.
	switch strings.TrimPrefix(filepath.Base(name), ".") {
	case "bash_profile", "bashrc", "bash_logout":
		// Same as the usual fallback, but we can avoid trying to read a shebang.
		return syntax.LangBash
	case "zshenv", "zprofile", "zshrc", "zlogin", "zlogout":
		return syntax.LangZsh
	}

	lang := syntax.LangAuto // fallback when none is found
	if ext := strings.TrimPrefix(filepath.Ext(name), "."); ext != "sh" {
		// Note that ".sh" doesn't mean it's POSIX Shell for sure.
		// A shebang in the file contents could say Bash or some other POSIX-like shell.
		lang.Set(ext)
	}
	return lang
}

var ecQuery = editorconfig.Query{
	FileCache:   make(map[string]*editorconfig.File),
	RegexpCache: make(map[string]*regexp.Regexp),
}

func propsOptions(lang syntax.LangVariant, props editorconfig.Section) (_ syntax.LangVariant, validLang bool) {
	// if shell_variant is set to a valid string, it will take precedence
	langErr := lang.Set(props.Get("shell_variant"))
	syntax.Variant(lang)(parser)

	size := uint(0)
	if props.Get("indent_style") == "space" {
		size = 8
		if n := props.IndentSize(); n > 0 {
			size = uint(n)
		}
	}
	syntax.Indent(size)(printer)

	syntax.BinaryNextLine(props.Get("binary_next_line") == "true")(printer)
	// TODO(v4): rename to case_indent for consistency with flags
	syntax.SwitchCaseIndent(props.Get("switch_case_indent") == "true")(printer)
	syntax.SpaceRedirects(props.Get("space_redirects") == "true")(printer)
	syntax.KeepPadding(props.Get("keep_padding") == "true")(printer)
	// TODO(v4): rename to func_next_line for consistency with flags
	syntax.FunctionNextLine(props.Get("function_next_line") == "true")(printer)

	minify := props.Get("minify") == "true"
	syntax.Minify(minify)(printer)
	// Note that --simplify is not actually a parser option, so we use a global var.
	// Just like the CLI flags, minify=true implies simplify=true.
	simplify.val = minify || props.Get("simplify") == "true"

	return lang, langErr == nil
}

func formatPath(path string, checkShebang bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	l := lang.val
	shebangForAuto := false
	if l == syntax.LangAuto {
		if l = langFromFilename(path); l == syntax.LangAuto {
			shebangForAuto = true
		}
	}
	readBuf.Reset()
	if checkShebang || shebangForAuto {
		n, err := io.ReadAtLeast(f, copyBuf[:32], len("#!/bin/sh\n"))
		switch {
		case !checkShebang:
			// only wanted the shebang for LangAuto
		case err == io.EOF, errors.Is(err, io.ErrUnexpectedEOF):
			return nil // too short to have a shebang
		case err != nil:
			return err // some other read error
		}
		shebangLang := fileutil.Shebang(copyBuf[:n])
		if checkShebang && shebangLang == "" {
			return nil // not a shell script
		}
		if shebangForAuto {
			if err := l.Set(shebangLang); err != nil {
				// Fall back to bash.
				l = syntax.LangBash
			}
		}
		readBuf.Write(copyBuf[:n])
	}
	switch find.val {
	case "true":
		fmt.Println(path)
		return nil
	case "0":
		fmt.Print(path)
		fmt.Print("\000")
		return nil
	}
	if _, err := io.CopyBuffer(&readBuf, f, copyBuf); err != nil {
		return err
	}
	f.Close()
	return formatBytes(readBuf.Bytes(), path, l)
}

func editorConfigLangs(l syntax.LangVariant) []string {
	// All known shells match [[shell]].
	// As a special case, bash and the bash-like bats also match [[bash]],
	// and zsh also matches [[zsh]].
	// We can later consider others like [[mksh]] or [[posix-shell]],
	// just consider what list of languages the EditorConfig spec might eventually use.
	switch l {
	case syntax.LangBash, syntax.LangBats:
		return []string{"shell", "bash"}
	case syntax.LangZsh:
		return []string{"shell", "zsh"}
	case syntax.LangPOSIX, syntax.LangMirBSDKorn, syntax.LangAuto:
		return []string{"shell"}
	}
	return nil
}

func formatBytes(src []byte, path string, fileLang syntax.LangVariant) error {
	fileLangFromEditorConfig := false
	if useEditorConfig {
		props, err := ecQuery.Find(path, editorConfigLangs(fileLang))
		if err != nil {
			return err
		}
		fileLang, fileLangFromEditorConfig = propsOptions(fileLang, props)
	} else {
		syntax.Variant(fileLang)(parser)
	}
	var node syntax.Node
	var err error
	if fromJSON.val {
		node, err = typedjson.Decode(bytes.NewReader(src))
		if err != nil {
			return err
		}
	} else {
		node, err = parser.Parse(bytes.NewReader(src), path)
		if err != nil {
			if s, ok := err.(syntax.LangError); ok && lang.val == syntax.LangAuto {
				if fileLangFromEditorConfig {
					return fmt.Errorf("%w (parsed as %s via EditorConfig)", s, fileLang)
				}
				return fmt.Errorf("%w (parsed as %s via -%s=%s)", s, fileLang, lang.short, lang.val)
			}
			return err
		}
	}
	// Note that --simplify is treated as a parser option as it happens
	// immediately after parsing, even if it's not a [syntax.ParserOption] today.
	if simplify.val {
		syntax.Simplify(node)
	}
	if toJSON.val {
		// must be standard input; fine to return
		// TODO: change the default behavior to be compact,
		// and allow using --to-json=pretty or --to-json=indent.
		return typedjson.EncodeOptions{Indent: "\t"}.Encode(os.Stdout, node)
	}
	writeBuf.Reset()
	printer.Print(&writeBuf, node)
	res := writeBuf.Bytes()
	if !bytes.Equal(src, res) {
		switch list.val {
		case "true":
			fmt.Println(path)
		case "0":
			fmt.Print(path)
			fmt.Print("\000")
		}
		if write.val {
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("refusing to atomically replace %q with a regular file as it is not one already", path)
			}
			perm := info.Mode().Perm()
			// TODO: support atomic writes on Windows?
			if err := maybeio.WriteFile(path, res, perm); err != nil {
				return err
			}
		}
		if diff.val {
			diffBytes := diffpkg.Diff(path+".orig", src, path, res)
			if !color {
				os.Stdout.Write(diffBytes)
				return errFormattingDiffers
			}
			// The first three lines are the header with the filenames, including --- and +++,
			// and are marked in bold.
			current := terminalBold
			os.Stdout.WriteString(current)
			for i, line := range bytes.SplitAfter(diffBytes, []byte("\n")) {
				last := current
				switch {
				case i < 3: // the first three lines are bold
				case bytes.HasPrefix(line, []byte("@@")):
					current = terminalCyan
				case bytes.HasPrefix(line, []byte("-")):
					current = terminalRed
				case bytes.HasPrefix(line, []byte("+")):
					current = terminalGreen
				default:
					current = terminalReset
				}
				if current != last {
					os.Stdout.WriteString(current)
				}
				os.Stdout.Write(line)
			}
			return errFormattingDiffers
		}
		if list.val != "false" && !write.val {
			return errFormattingDiffers
		}
	}
	if list.val == "false" && !write.val && !diff.val {
		os.Stdout.Write(res)
	}
	return nil
}

const (
	terminalGreen = "\u001b[32m"
	terminalRed   = "\u001b[31m"
	terminalCyan  = "\u001b[36m"
	terminalReset = "\u001b[0m"
	terminalBold  = "\u001b[1m"
)
