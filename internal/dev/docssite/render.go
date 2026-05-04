package docssite

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// chromaDarkStyle / chromaLightStyle name the chroma themes the
// generated HTML uses for code highlighting -- the GitHub light/dark
// pair, picked to match the broader docs site theme.
const (
	chromaDarkStyle  = "github-dark"
	chromaLightStyle = "github"
)

// renderer holds the goldmark instance and the per-build options used
// to assemble each page.
type renderInfo struct {
	opts     BuildOptions
	gm       goldmark.Markdown
	sidebar  template.HTML
	basePath string
}

func newRenderer(opts BuildOptions) *renderInfo {
	style := buildChromaStyle()
	gm := goldmark.New(
		goldmark.WithExtensions(
			meta.Meta,
			extension.GFM,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithCustomStyle(style),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(false),
					chromahtml.TabWidth(4),
				),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			gmhtml.WithUnsafe(),
			renderer.WithNodeRenderers(),
		),
	)

	return &renderInfo{
		opts:     opts,
		gm:       gm,
		sidebar:  renderSidebar(opts.Sidebar, opts.BasePath),
		basePath: opts.BasePath,
	}
}

// buildChromaStyle picks a sensible chroma style. We use one of the
// bundled GitHub-flavoured themes; if the named style is unavailable
// chroma's fallback handles it.
func buildChromaStyle() *chroma.Style {
	if s := styles.Get(chromaDarkStyle); s != nil {
		return s
	}
	return styles.Fallback
}

// pageData is the input to templates/page.html.
type pageData struct {
	Title       string
	Description string
	BasePath    string
	Body        template.HTML
	Sidebar     template.HTML
	Widgets     template.HTML
	SiteTitle   string
	LiveReload  bool
}

// RenderPage parses the markdown source, expands shortcodes, and
// returns the fully rendered HTML page. dev controls whether the
// live-reload script tag is emitted; only Serve sets it.
func (r *renderInfo) RenderPage(relPath string, source []byte, dev bool) ([]byte, error) {
	processed, err := preprocessShortcodes(string(source))
	if err != nil {
		return nil, fmt.Errorf("preprocess shortcodes: %w", err)
	}
	processed = rewriteAbsoluteLinks(processed, r.basePath)

	ctx := parser.NewContext()
	var buf bytes.Buffer
	if err := r.gm.Convert([]byte(processed), &buf, parser.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("render markdown: %w", err)
	}
	body := buf.String()

	frontmatter := meta.Get(ctx)
	title := stringFromMeta(frontmatter, "title")
	description := stringFromMeta(frontmatter, "description")
	if title == "" {
		title = humanizeStem(relPath)
	}

	widgets := assembleWidgets(frontmatter)

	page := pageData{
		Title:       title,
		Description: description,
		BasePath:    r.basePath,
		Body:        template.HTML(body),
		Sidebar:     r.sidebar,
		Widgets:     widgets,
		SiteTitle:   r.opts.SiteTitle,
		LiveReload:  dev,
	}

	var out bytes.Buffer
	if err := pageTemplate.Execute(&out, page); err != nil {
		return nil, fmt.Errorf("execute page template: %w", err)
	}
	return out.Bytes(), nil
}

// stringFromMeta returns the YAML-frontmatter value at key as a
// string; empty if missing or wrong type.
func stringFromMeta(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// humanizeStem produces a fallback title from a source path. e.g.
// "guides/scaling.md" → "Scaling".
func humanizeStem(rel string) string {
	base := filepath.Base(rel)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "index" {
		return ""
	}
	parts := strings.Split(stem, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// assembleWidgets renders any widget partials the page opted into via
// frontmatter `widgets: [scaling-calculator, ...]`.
func assembleWidgets(frontmatter map[string]any) template.HTML {
	if frontmatter == nil {
		return ""
	}
	raw, ok := frontmatter["widgets"]
	if !ok {
		return ""
	}
	list, ok := raw.([]any)
	if !ok {
		return ""
	}
	var out strings.Builder
	for _, entry := range list {
		name, ok := entry.(string)
		if !ok {
			continue
		}
		switch name {
		case "scaling-calculator":
			out.WriteString(scalingCalculatorPartial)
		}
	}
	return template.HTML(out.String())
}

// linkRe matches a markdown link's URL inside the parentheses
// `](URL)`.
var linkRe = regexp.MustCompile(`\]\(([^)\s]+)(\s+"[^"]*")?\)`)

// rewriteAbsoluteLinks prefixes BasePath onto absolute internal
// markdown links. A link starting with "/" but not "//" is treated as
// internal; external links (`http://`, `https://`, `mailto:`, anchors
// starting with `#`) are untouched.
func rewriteAbsoluteLinks(src, basePath string) string {
	if basePath == "" {
		return src
	}
	prefix := strings.TrimRight(basePath, "/")
	return linkRe.ReplaceAllStringFunc(src, func(match string) string {
		groups := linkRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		url := groups[1]
		title := ""
		if len(groups) >= 3 {
			title = groups[2]
		}
		if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") {
			return match
		}
		if strings.HasPrefix(url, prefix+"/") || url == prefix {
			return match
		}
		return "](" + prefix + url + title + ")"
	})
}

// asideRe matches a paragraph-leading admonition block in the form:
//
//	:::tip Optional title
//	body content
//	:::
//
// The variant ("tip" / "caution" / "note" / unset for default) and the
// optional title sit on the opening fence; everything between the
// fences is admonition content.
var asideRe = regexp.MustCompile(`(?ms)^:::(tip|caution|note)?(?:[ \t]+([^\n]*))?\n(.*?)\n:::[ \t]*$`)

// widgetRe matches an inline widget marker on its own line:
//
//	{{< widget-name >}}
//
// Accepted widget names map to the same partial table as
// assembleWidgets's frontmatter path; an unrecognized name is left in
// the source untouched.
var widgetRe = regexp.MustCompile(`(?m)^\s*\{\{<\s*([a-z][a-z0-9-]*)\s*>\}\}\s*$`)

// flagTableRe matches the configuration-reference shortcode on its
// own line:
//
//	{{< flagtable section-name >}}
//
// The captured section name is dispatched to renderFlagTable, which
// emits the markdown table body.
var flagTableRe = regexp.MustCompile(`(?m)^\s*\{\{<\s*flagtable\s+([a-z][a-z0-9-]*)\s*>\}\}\s*$`)

// grpcRetryTableRe matches the gRPC retry-policy shortcode on its own
// line:
//
//	{{< grpcRetryTable >}}
//
// The shortcode takes no argument; renderGrpcRetryTable always emits
// the same table.
var grpcRetryTableRe = regexp.MustCompile(`(?m)^\s*\{\{<\s*grpcRetryTable\s*>\}\}\s*$`)

// scaleReasonTableRe matches the ScaleReason enum shortcode on its
// own line. Zero-argument; renderScaleReasonTable always emits the
// same table.
var scaleReasonTableRe = regexp.MustCompile(`(?m)^\s*\{\{<\s*scaleReasonTable\s*>\}\}\s*$`)

// transportTableRe matches the HTTP transport-settings shortcode on
// its own line. Zero-argument; renderTransportTable always emits
// the same table.
var transportTableRe = regexp.MustCompile(`(?m)^\s*\{\{<\s*transportTable\s*>\}\}\s*$`)

// messageTableRe matches the protobuf-message shortcode on its own
// line:
//
//	{{< messageTable Function >}}
//
// The captured argument is a proto message name -- uppercase-
// leading CamelCase. It intentionally does NOT reuse
// [flagTableRe]'s `[a-z][a-z0-9-]*` argument class.
var messageTableRe = regexp.MustCompile(`(?m)^\s*\{\{<\s*messageTable\s+([A-Z][A-Za-z0-9]*)\s*>\}\}\s*$`)

// metricsTableRe matches the Prometheus metrics-table shortcode on
// its own line:
//
//	{{< metricsTable controller >}}
//
// The captured argument is a component name -- lowercase-leading,
// matching [flagTableRe]'s argument class.
//
// Leading whitespace is restricted to `[ \t]*` (not `\s*`) so the
// regex does not consume the blank line preceding the shortcode --
// that blank line is what lets goldmark exit a `<div class=...>`
// HTML block and resume markdown parsing for the rendered table.
var metricsTableRe = regexp.MustCompile(`(?m)^[ \t]*\{\{<\s*metricsTable\s+([a-z][a-z0-9-]*)\s*>\}\}\s*$`)

// preprocessShortcodes expands custom shortcodes into raw HTML or
// markdown before goldmark sees the source: admonition fences
// (`:::variant`), the flag-table shortcode (`{{< flagtable name >}}`),
// and inline widget markers (`{{< widget-name >}}`). The frontmatter
// `widgets:` path remains the way to opt into a widget without
// editing the body. An unknown flagtable section returns a wrapped
// error so the build fails loudly; an unknown widget is left in the
// source untouched.
func preprocessShortcodes(src string) (string, error) {
	src = asideRe.ReplaceAllStringFunc(src, func(match string) string {
		groups := asideRe.FindStringSubmatch(match)
		variant := "note"
		if len(groups) >= 2 && groups[1] != "" {
			variant = groups[1]
		}
		title := ""
		if len(groups) >= 3 {
			title = strings.TrimSpace(groups[2])
		}
		body := ""
		if len(groups) >= 4 {
			body = groups[3]
		}
		return renderAside(variant, title, body)
	})
	var flagErr error
	src = flagTableRe.ReplaceAllStringFunc(src, func(match string) string {
		if flagErr != nil {
			return match
		}
		groups := flagTableRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		out, err := renderFlagTable(groups[1])
		if err != nil {
			flagErr = err
			return match
		}
		return out
	})
	if flagErr != nil {
		return "", flagErr
	}
	var retryErr error
	src = grpcRetryTableRe.ReplaceAllStringFunc(src, func(match string) string {
		if retryErr != nil {
			return match
		}
		out, err := renderGrpcRetryTable()
		if err != nil {
			retryErr = err
			return match
		}
		return out
	})
	if retryErr != nil {
		return "", retryErr
	}
	var scaleReasonErr error
	src = scaleReasonTableRe.ReplaceAllStringFunc(src, func(match string) string {
		if scaleReasonErr != nil {
			return match
		}
		out, err := renderScaleReasonTable()
		if err != nil {
			scaleReasonErr = err
			return match
		}
		return out
	})
	if scaleReasonErr != nil {
		return "", scaleReasonErr
	}
	var transportErr error
	src = transportTableRe.ReplaceAllStringFunc(src, func(match string) string {
		if transportErr != nil {
			return match
		}
		out, err := renderTransportTable()
		if err != nil {
			transportErr = err
			return match
		}
		return out
	})
	if transportErr != nil {
		return "", transportErr
	}
	var messageErr error
	src = messageTableRe.ReplaceAllStringFunc(src, func(match string) string {
		if messageErr != nil {
			return match
		}
		groups := messageTableRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		out, err := renderMessageTable(groups[1])
		if err != nil {
			messageErr = err
			return match
		}
		return out
	})
	if messageErr != nil {
		return "", messageErr
	}
	var metricsErr error
	src = metricsTableRe.ReplaceAllStringFunc(src, func(match string) string {
		if metricsErr != nil {
			return match
		}
		groups := metricsTableRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		out, err := renderMetricsTable(groups[1])
		if err != nil {
			metricsErr = err
			return match
		}
		return out
	})
	if metricsErr != nil {
		return "", metricsErr
	}
	src = widgetRe.ReplaceAllStringFunc(src, func(match string) string {
		groups := widgetRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		if html := widgetPartial(groups[1]); html != "" {
			return html
		}
		return match
	})
	return src, nil
}

// widgetPartial returns the HTML for a registered widget name, or the
// empty string for an unknown name. Keep this in sync with the
// frontmatter switch in assembleWidgets.
func widgetPartial(name string) string {
	switch name {
	case "scaling-calculator":
		return scalingCalculatorPartial
	}
	return ""
}

// renderAside returns the HTML for a single admonition. The output
// is a wrapping <aside> element keyed off the variant, a heading row
// with a default label, and a content section that goldmark renders
// nested HTML into using the line breaks of the admonition body. To
// keep the implementation shortcode-only (no recursive goldmark
// passes), the body is inserted as an HTML comment marker that
// goldmark passes through verbatim.
func renderAside(variant, title, body string) string {
	defaultTitle := map[string]string{
		"note":    "Note",
		"tip":     "Tip",
		"caution": "Caution",
	}[variant]
	if title == "" {
		title = defaultTitle
	}
	// Allow inline markdown in the admonition body by re-running the
	// caller's source through goldmark via a sentinel: emit a raw HTML
	// block whose inner content is the *already-trimmed* body wrapped
	// in <p>. Multi-paragraph bodies get split on blank lines.
	rendered := renderAsideBody(body)
	return fmt.Sprintf(
		"<aside class=\"aside aside-%s\" aria-label=%q>\n<p class=\"aside-title\">%s</p>\n<section class=\"aside-content\">\n%s\n</section>\n</aside>\n",
		html.EscapeString(variant),
		html.EscapeString(title),
		html.EscapeString(title),
		rendered,
	)
}

// renderAsideBody splits an admonition's plain-text body on blank
// lines and wraps each block in <p> tags, so a multi-paragraph
// admonition produces semantic paragraphs.
func renderAsideBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	paragraphs := regexp.MustCompile(`\n\s*\n`).Split(body, -1)
	out := strings.Builder{}
	for i, p := range paragraphs {
		if i > 0 {
			out.WriteByte('\n')
		}
		// Preserve simple inline markdown (`code`, *italic*, **bold**,
		// [link](url)) by passing the paragraph through a tiny inline
		// renderer.
		out.WriteString("<p>" + renderInline(strings.TrimSpace(p)) + "</p>")
	}
	return out.String()
}

// renderInline handles a small subset of inline markdown for use
// inside admonition bodies. Anything more complex should live in the
// markdown body proper, not in a shortcode argument.
var (
	inlineCodeRe = regexp.MustCompile("`([^`]+)`")
	inlineBoldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	inlineItalRe = regexp.MustCompile(`\*([^*]+)\*`)
	inlineLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

func renderInline(s string) string {
	s = html.EscapeString(s)
	s = inlineCodeRe.ReplaceAllString(s, "<code>$1</code>")
	s = inlineBoldRe.ReplaceAllString(s, "<strong>$1</strong>")
	s = inlineItalRe.ReplaceAllString(s, "<em>$1</em>")
	s = inlineLinkRe.ReplaceAllString(s, `<a href="$2">$1</a>`)
	return s
}
