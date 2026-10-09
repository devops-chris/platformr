package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// useColor reports whether output is a terminal that wants color: not when piped to
// a file, and not when NO_COLOR is set (https://no-color.org). FORCE_COLOR turns it on
// even when piped.
func useColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return isatty.IsTerminal(os.Stdout.Fd())
}

// lexerFor picks a syntax highlighter from the file name.
func lexerFor(path string) chroma.Lexer {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(base, ".tfvars"), strings.HasSuffix(base, ".nomad"):
		return lexers.Get("terraform")
	case base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".properties"):
		return lexers.Get("bash")
	}
	if l := lexers.Match(base); l != nil {
		return l
	}
	return nil
}

// highlight returns content with terminal colors for its file type, or unchanged when
// color is off or the type isn't recognized.
func highlight(path, content string) string {
	if !useColor() {
		return content
	}
	lexer := lexerFor(path)
	if lexer == nil {
		return content
	}
	style := styles.Get("github")
	if lipgloss.HasDarkBackground() {
		style = styles.Get("monokai")
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, content)
	if err != nil {
		return content
	}
	var b bytes.Buffer
	if err := formatters.Get("terminal256").Format(&b, style, it); err != nil {
		return content
	}
	return b.String()
}

// colorDiffLine colors one line of lineDiff output: red for removed, green for added.
func colorDiffLine(line string) string {
	if !useColor() {
		return line
	}
	switch {
	case strings.HasPrefix(line, "- "):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(line)
	case strings.HasPrefix(line, "+ "):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(line)
	}
	return line
}
