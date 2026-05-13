package runbook

import (
	"strings"
	"unicode"
)

var stopWords = map[string]struct{}{
	"的": {}, "了": {}, "是": {}, "在": {}, "和": {}, "有": {}, "与": {}, "及": {},
	"等": {}, "或": {}, "为": {}, "中": {}, "怎": {}, "么": {},
	"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "in": {}, "on": {}, "at": {}, "to": {},
	"for": {}, "of": {}, "with": {}, "and": {}, "or": {}, "not": {}, "but": {},
	"this": {}, "that": {}, "it": {},
}

func Tokenize(text string) []string {
	runes := []rune(text)
	var tokens []string
	i := 0
	for i < len(runes) {
		r := runes[i]
		if isIdentRune(r) {
			j := i
			for j < len(runes) && isIdentRune(runes[j]) {
				j++
			}
			token := strings.ToLower(string(runes[i:j]))
			if _, stop := stopWords[token]; !stop && token != "" {
				tokens = append(tokens, token)
			}
			i = j
		} else if unicode.Is(unicode.Han, r) {
			ch := string(r)
			if _, stop := stopWords[ch]; !stop {
				tokens = append(tokens, ch)
			}
			i++
		} else {
			i++
		}
	}
	return tokens
}

func TokenizeBigram(tokens []string) []string {
	result := make([]string, 0, len(tokens)+len(tokens)-1)
	result = append(result, tokens...)
	for i := 0; i < len(tokens)-1; i++ {
		result = append(result, tokens[i]+tokens[i+1])
	}
	return result
}

func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) && !unicode.Is(unicode.Han, r) || unicode.IsDigit(r) || r == '_'
}
