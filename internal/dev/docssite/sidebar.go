package docssite

import (
	"html"
	"html/template"
	"strings"
)

// renderSidebar returns the static HTML for the configured Sidebar.
// Returns an empty template.HTML when the sidebar has no entries so
// the page template can omit the <nav> wrapper.
func renderSidebar(s Sidebar, basePath string) template.HTML {
	if s.Intro == nil && len(s.Groups) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<ul class=\"sidebar-top\">\n")
	if s.Intro != nil {
		b.WriteString("  <li><a href=\"")
		b.WriteString(html.EscapeString(prefixLink(s.Intro.Link, basePath)))
		b.WriteString("\">")
		b.WriteString(html.EscapeString(s.Intro.Label))
		b.WriteString("</a></li>\n")
	}
	b.WriteString("</ul>\n")
	for _, g := range s.Groups {
		b.WriteString("<details class=\"sidebar-group\" open>\n")
		b.WriteString("  <summary>")
		b.WriteString(html.EscapeString(g.Label))
		b.WriteString("</summary>\n")
		b.WriteString("  <ul>\n")
		for _, item := range g.Items {
			b.WriteString("    <li><a href=\"")
			b.WriteString(html.EscapeString(prefixLink(item.Link, basePath)))
			b.WriteString("\">")
			b.WriteString(html.EscapeString(item.Label))
			b.WriteString("</a></li>\n")
		}
		b.WriteString("  </ul>\n")
		b.WriteString("</details>\n")
	}
	return template.HTML(b.String())
}

// prefixLink prepends BasePath to a sidebar Link unless the link is
// already absolute (starts with http(s):// or //). Empty BasePath is a
// no-op.
func prefixLink(link, basePath string) string {
	if basePath == "" {
		return link
	}
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "//") {
		return link
	}
	prefix := strings.TrimRight(basePath, "/")
	if !strings.HasPrefix(link, "/") {
		return prefix + "/" + link
	}
	if strings.HasPrefix(link, prefix+"/") || link == prefix {
		return link
	}
	return prefix + link
}
