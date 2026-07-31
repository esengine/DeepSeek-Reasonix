shfmt(1)

; To render and view: scdoc <shfmt.1.scd | man -l -

# NAME

shfmt - Format shell programs

# SYNOPSIS

*shfmt* [flags] [path...]

# DESCRIPTION

shfmt formats shell programs. If the only argument is a dash (*-*) or no
arguments are given, standard input will be used. If a given path is a
directory, all shell scripts found under that directory will be used.

If any EditorConfig files are found, they will be used to apply formatting
options. If any parser or printer flags are given to the tool, no EditorConfig
formatting options will be used. A default like *-i=0* can be used for this purpose.

shfmt's default shell formatting was chosen to be consistent, common, and
predictable. Some aspects of the format can be configured via printer flags.

# OPTIONS

## Generic flags

*--version*
	Show version and exit.

*-l[=0]*, *--list[=0]*
	Error with a list of files whose formatting differs from shfmt;
	paths are separated by a newline or a null character if -l=0

*-w*, *--write*
	Write result to file instead of stdout.

*-d*, *--diff*
	Error with a diff when the formatting differs.

	The diff uses color when the output is a terminal.
	To never use color, set a non-empty *NO_COLOR* or *TERM=dumb*.
	To always use color, set a non-empty *FORCE_COLOR*.

*--apply-ignore*
	Always apply EditorConfig ignore rules.

	When formatting files directly, ignore rules are skipped without this flag.
	Should be useful to any tools or editors which format stdin or a single file.
	When printing results to stdout, an ignored file results in no output at all.

*--filename* str
	Provide a name for the standard input file.

	Use of this flag is necessary for EditorConfig support to work with stdin,
	since EditorConfig files are found relative to the location of a script.

## Parser flags

*-ln*, *--language-dialect* <str>
	Language dialect (*bash*/*posix*/*mksh*/*bats*/*zsh*, default *auto*).

	When set to *auto*, the language is detected from the input filename,
	as long as it has a shell extension like *foo.mksh*. Otherwise, if the input
	begins with a shell shebang like *#!/bin/sh*, that's used instead.
	If neither come up with a result, *bash* is used as a fallback.

	The filename extension *.sh* is a special case: it implies *posix*,
	but may be overridden by a valid shell shebang.

*-p*, *--posix*
	Shorthand for *-ln=posix*.

*-s*, *--simplify*
	Simplify the code.

## Printer flags

*-i*, *--indent* <uint>
	Indent: *0* for tabs (default), *>0* for number of spaces.

*-bn*, *--binary-next-line*
	Binary ops like *&&* and *|* may start a line.

*-ci*, *--case-indent*
	Switch cases will be indented.

*-sr*, *--space-redirects*
	Redirect operators will be followed by a space.

*-kp*, *--keep-padding*
	Keep column alignment paddings.

	This flag is *DEPRECATED* and will be removed in the next major version.
	For more information, see: https://github.com/mvdan/sh/issues/658

*-fn*, *--func-next-line*
	Function opening braces are placed on a separate line.

*-mn*, *--minify*
	Minify the code to reduce its size (implies *-s*).

## Utility flags

*-f[=0]*, *--find[=0]*
	Recursively find all shell files and print the paths;
	paths are separated by a newline or a null character if -f=0.

*--to-json*
	Print syntax tree to stdout as a typed JSON.

*--from-json*
	Read syntax tree from stdin as a typed JSON.

# EXAMPLES

Format all the scripts under the current directory, printing which are modified:

	shfmt -l -w .

For CI, one can use a variant where formatting changes are just shown as diffs:

	shfmt -d .

The following formatting flags closely resemble Google's shell style defined in
<https://google.github.io/styleguide/shellguide.html>:

	shfmt -i 2 -ci -bn

Below is a sample EditorConfig file as defined by <https://editorconfig.org/>,
showing how to set supported options:

```
[*.sh]
# like -i=4
indent_style = space
indent_size = 4

# --language-dialect
shell_variant      = posix
simplify           = true
binary_next_line   = true
# --case-indent
switch_case_indent = true
space_redirects    = true
keep_padding       = true
# --func-next-line
function_next_line = true
minify             = true

# Ignore the entire "third_party" directory when calling shfmt on directories,
# such as "shfmt -l -w .". When formatting files directly,
# like "shfmt -w third_party/foo.sh" or "shfmt --filename=third_party/foo.sh",
# the ignore logic is applied only when the --apply-ignore flag is given.
[third_party/**]
ignore = true
```

EditorConfig sections may also use `[[shell]]`, `[[bash]]`, or `[[zsh]]` to match shell scripts,
which is particularly useful when scripts use a shebang but no extension.
Note that this feature is outside of the EditorConfig spec and may be changed in the future.

shfmt can also replace *bash -n* to check shell scripts for syntax errors. It is
more exhaustive, as it parses all syntax statically and requires valid UTF-8:

```
$ echo '${foo:1 2}' | bash -n
$ echo '${foo:1 2}' | shfmt >/dev/null
1:9: not a valid arithmetic operator: 2
$ echo 'foo=(1 2)' | bash --posix -n
$ echo 'foo=(1 2)' | shfmt -p >/dev/null
1:5: arrays are a bash feature
```

# AUTHORS

Maintained by Daniel Martí <mvdan@mvdan.cc>, who is assisted by other open
source contributors. For more information and development, see
<https://github.com/mvdan/sh>.
