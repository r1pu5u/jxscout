package htmlingestion

import (
	"net/url"
	"strings"

	"github.com/francisconeves97/jxscout/internal/core/errutil"

	"github.com/PuerkitoBio/goquery"
)

type extractedJS struct {
	baseURL       string
	scriptURLs    []string // script URLs
	inlineScripts []string // actual scripts that were inlined
}

func extractJS(htmlContent string, baseURLRaw string) (extractedJS, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return extractedJS{}, errutil.Wrap(err, "failed to read document")
	}

	baseURL, err := url.Parse(baseURLRaw)
	if err != nil {
		return extractedJS{}, errutil.Wrap(err, "failed to parse url")
	}

	inlineBase := baseURL
	if href, ok := doc.Find("base[href]").First().Attr("href"); ok {
		if ref, err := url.Parse(href); err == nil {
			inlineBase = baseURL.ResolveReference(ref)
		}
	}
	scriptURLs := extractJavascriptURLs(doc, baseURL)
	inlineScripts := extractInlineJavascript(doc)

	return extractedJS{
		baseURL:       inlineBase.String(),
		scriptURLs:    scriptURLs,
		inlineScripts: inlineScripts,
	}, nil
}

func extractJavascriptURLs(doc *goquery.Document, baseURL *url.URL) []string {
	if href, ok := doc.Find("base[href]").First().Attr("href"); ok {
		if ref, err := url.Parse(href); err == nil {
			baseURL = baseURL.ResolveReference(ref)
		}
	}
	urls := []string{}
	seen := map[string]bool{}
	doc.Find(`script[src],link[rel="modulepreload"],link[rel="preload"][as="script"]`).Each(func(_ int, selection *goquery.Selection) {
		src, ok := selection.Attr("src")
		if !ok {
			src, _ = selection.Attr("href")
		}
		ref, err := url.Parse(src)
		if err != nil {
			return
		}
		resolved := baseURL.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}
		resolved.Fragment = ""
		value := resolved.String()
		if !seen[value] {
			seen[value] = true
			urls = append(urls, value)
		}
	})
	return urls
}

func extractInlineJavascript(doc *goquery.Document) []string {
	inlineJS := []string{}

	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		_, exists := s.Attr("src")
		if exists {
			return
		}

		t, exists := s.Attr("type")
		if exists && (t == "application/ld+json" || t == "application/json") {
			return
		}

		content := s.Text()
		if content == "" {
			return
		}

		inlineJS = append(inlineJS, content)
	})

	return inlineJS
}
