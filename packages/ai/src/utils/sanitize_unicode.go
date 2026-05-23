package utils

func SanitizeSurrogates(text string) string {
	out := make([]rune, 0, len(text))
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		if char >= 0xD800 && char <= 0xDBFF {
			if i+1 < len(runes) && runes[i+1] >= 0xDC00 && runes[i+1] <= 0xDFFF {
				out = append(out, char, runes[i+1])
				i++
			}
			continue
		}
		if char >= 0xDC00 && char <= 0xDFFF {
			continue
		}
		if char == '\uFFFD' {
			continue
		}
		out = append(out, char)
	}
	return string(out)
}
