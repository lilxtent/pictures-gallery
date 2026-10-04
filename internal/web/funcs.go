package web

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

var funcs = template.FuncMap{
	"paragraphs":  paragraphs,
	"lines":       lines,
	"details":     details,
	"imgURL":      images.URL,
	"srcset":      images.SrcSet,
	"paintingDir": images.PaintingDir,
}

var blankLine = regexp.MustCompile(`\n[ \t]*\n`)

// paragraphs splits plain text into paragraphs at blank lines.
func paragraphs(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, p := range blankLine.Split(s, -1) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lines splits a paragraph into its lines (rendered with <br> between them).
func lines(s string) []string { return strings.Split(s, "\n") }

// details is the one-line caption "Техника · Размер · Год".
func details(p store.Painting) string {
	var parts []string
	for _, s := range []string{p.Technique, p.Size} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if p.Year != 0 {
		parts = append(parts, strconv.Itoa(p.Year))
	}
	return strings.Join(parts, " · ")
}

// truncate collapses whitespace and shortens s to at most n runes, the last
// one being an ellipsis when s was cut.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:max(n-1, 0)])) + "…"
}

func imagesURL1200(p store.Painting) string {
	return images.URL(images.PaintingDir(p.ID), p.ImageVersion, 1200)
}
