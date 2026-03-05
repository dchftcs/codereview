package highlight

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var (
	fmtr       chroma.Formatter
	style      *chroma.Style
	lexerCache sync.Map
)

// Init sets the Chroma style based on the theme. Call before using Line().
// Use "dark" for dark terminals (monokai), "light" for light terminals (github).
func Init(theme string) {
	fmtr = formatters.TTY256
	styleName := "monokai"
	if theme == "light" {
		styleName = "github"
	}
	style = styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
}

func init() {
	Init("dark")
}

// Line highlights a single line of code given the filename (for language detection).
func Line(filename, content string) string {
	lexer := getLexer(filename)
	if lexer == nil {
		return content
	}

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}

	var buf bytes.Buffer
	err = fmtr.Format(&buf, style, iterator)
	if err != nil {
		return content
	}

	// Remove any newlines that chroma may insert — we highlight single lines,
	// so newlines in the output would break layout.
	result := buf.String()
	result = strings.ReplaceAll(result, "\n", "")
	return result
}

func getLexer(filename string) chroma.Lexer {
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = filepath.Base(filename)
	}

	if cached, ok := lexerCache.Load(ext); ok {
		return cached.(chroma.Lexer)
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	lexerCache.Store(ext, lexer)
	return lexer
}
