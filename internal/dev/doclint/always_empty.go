package doclint

// alwaysEmptyRule is a no-op rule that registers itself at init time.
// Its purpose is to give the runner one registered rule to dispatch
// against so the registry path is exercised end-to-end before any real
// rule lands; it never produces findings.
type alwaysEmptyRule struct{}

func (alwaysEmptyRule) Name() string                      { return "always-empty" }
func (alwaysEmptyRule) Globs() []string                   { return []string{"**/*.md"} }
func (alwaysEmptyRule) Check(_ []File) ([]Finding, error) { return nil, nil }

func init() { register(alwaysEmptyRule{}) }
