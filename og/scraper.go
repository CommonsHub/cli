package og

import (
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// FetchOGImage fetches the og:image from a URL
func FetchOGImage(pageURL string) string {
	resp, err := http.Get(pageURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	// Limit read to 1MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}

	return extractOGImage(string(body))
}

func extractOGImage(htmlContent string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.SelfClosingTagToken, html.StartTagToken:
			t := tokenizer.Token()
			if t.Data != "meta" {
				continue
			}
			var property, content string
			for _, a := range t.Attr {
				switch a.Key {
				case "property":
					property = a.Val
				case "content":
					content = a.Val
				}
			}
			if property == "og:image" && content != "" {
				return content
			}
		}
	}
}
