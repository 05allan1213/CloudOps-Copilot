package runbook

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Chunk struct {
	ID       string
	DocFile  string
	DocTitle string
	Heading  string
	Text     string
}

func ChunkDocument(doc Document) []Chunk {
	if len(doc.Sections) == 0 {
		if strings.TrimSpace(doc.Body) == "" {
			return nil
		}
		return []Chunk{{
			ID:       doc.File + ":body",
			DocFile:  doc.File,
			DocTitle: doc.Title,
			Heading:  "",
			Text:     doc.Body,
		}}
	}

	var chunks []Chunk
	for _, sec := range doc.Sections {
		if sec.Heading == "" && sec.Text == "" {
			continue
		}
		if utf8.RuneCountInString(sec.Text) <= 500 {
			chunks = append(chunks, Chunk{
				ID:       doc.File + ":" + sec.Heading,
				DocFile:  doc.File,
				DocTitle: doc.Title,
				Heading:  sec.Heading,
				Text:     sec.Text,
			})
			continue
		}
		parts := ChunkText(sec.Text, 500)
		for i, part := range parts {
			chunks = append(chunks, Chunk{
				ID:       fmt.Sprintf("%s:%s:%d", doc.File, sec.Heading, i+1),
				DocFile:  doc.File,
				DocTitle: doc.Title,
				Heading:  sec.Heading,
				Text:     part,
			})
		}
	}
	return chunks
}

func ChunkDocuments(docs []Document) []Chunk {
	var chunks []Chunk
	for _, doc := range docs {
		chunks = append(chunks, ChunkDocument(doc)...)
	}
	return chunks
}

func ChunkText(text string, maxChars int) []string {
	if utf8.RuneCountInString(text) <= maxChars {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var buf strings.Builder

	for _, para := range paragraphs {
		runeCount := utf8.RuneCountInString(para)

		if runeCount > maxChars {
			if buf.Len() > 0 {
				chunks = append(chunks, buf.String())
				buf.Reset()
			}
			lines := strings.Split(para, "\n")
			for _, line := range lines {
				lineRunes := utf8.RuneCountInString(line)
				if lineRunes > maxChars {
					if buf.Len() > 0 {
						chunks = append(chunks, buf.String())
						buf.Reset()
					}
					remaining := line
					for utf8.RuneCountInString(remaining) > maxChars {
						cut := cutByRunes(remaining, maxChars)
						chunks = append(chunks, cut)
						remaining = remaining[len(cut):]
					}
					if remaining != "" {
						buf.WriteString(remaining)
					}
				} else if buf.Len() == 0 {
					buf.WriteString(line)
				} else if utf8.RuneCountInString(buf.String())+1+lineRunes <= maxChars {
					buf.WriteByte('\n')
					buf.WriteString(line)
				} else {
					chunks = append(chunks, buf.String())
					buf.Reset()
					buf.WriteString(line)
				}
			}
			continue
		}

		if buf.Len() == 0 {
			buf.WriteString(para)
		} else if utf8.RuneCountInString(buf.String())+2+runeCount <= maxChars {
			buf.WriteString("\n\n")
			buf.WriteString(para)
		} else {
			chunks = append(chunks, buf.String())
			buf.Reset()
			buf.WriteString(para)
		}
	}

	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}

	return chunks
}

func cutByRunes(s string, maxChars int) string {
	runeCount := 0
	for i := range s {
		if runeCount == maxChars {
			return s[:i]
		}
		runeCount++
	}
	return s
}
