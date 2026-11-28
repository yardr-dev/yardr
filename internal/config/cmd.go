package config

import (
	"regexp"
	"strings"
)

// envNamePattern is what the shell takes for a variable's name, so a cmd's
// leading assignments (YARDR_PUSH=origin /path/to/merge-local) are told from
// the command they precede.
const envNamePattern = `[A-Za-z_][A-Za-z0-9_]*`

var cmdAssignment = regexp.MustCompile(`^` + envNamePattern + `=`)

// SplitCmd finds the command in a group's cmd: the words before it are
// variable assignments, which is how a script's options are set
// (YARDR_PUSH=origin /path/to/merge-local). It returns those words as
// written and the command's place, cmd[i:end], which is empty (i == end)
// for a cmd that holds assignments alone, or nothing.
func SplitCmd(cmd string) (assigns []string, i, end int) {
	for i < len(cmd) {
		for i < len(cmd) && (cmd[i] == ' ' || cmd[i] == '\t') {
			i++
		}
		end = WordEnd(cmd, i)
		if i == end {
			break
		}
		if !cmdAssignment.MatchString(cmd[i:end]) {
			return assigns, i, end
		}
		assigns = append(assigns, cmd[i:end])
		i = end
	}
	return assigns, i, i
}

// ScriptOf is the script a group's command runs: its first word that is no
// variable assignment, unquoted. alone is false for a command with
// assignments before the script or arguments after it. Only that word is
// looked at: what follows it may name any file of the machine.
func ScriptOf(cmd string) (script string, alone bool) {
	_, i, end := SplitCmd(cmd)
	if i == end {
		return "", false
	}
	return ShellUnquote(cmd[i:end]), strings.TrimSpace(cmd[:i]) == "" && strings.TrimSpace(cmd[end:]) == ""
}

// WordEnd is where the shell word starting at i ends: at the first blank
// outside quotes.
func WordEnd(s string, i int) int {
	var quote byte
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == 0 && (c == ' ' || c == '\t'):
			return i
		case c == '\\' && quote != '\'' && i+1 < len(s):
			i++
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case c == quote:
			quote = 0
		}
	}
	return i
}

// ShellUnquote is a shell word without its quotes.
func ShellUnquote(w string) string {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(w); i++ {
		c := w[i]
		switch {
		case c == '\\' && quote != '\'' && i+1 < len(w):
			i++
			out.WriteByte(w[i])
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case c == quote:
			quote = 0
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}
