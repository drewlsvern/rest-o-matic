package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Problem severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Problem is one thing wrong with a config file, located in the file where
// that can be worked out. Line and Column are 1-based, and 0 when unknown.
type Problem struct {
	Severity   string
	Message    string
	Job        string
	Repository string
	Path       string
	Line       int
	Column     int
}

// String is the problem as one line of text: its line, what it concerns,
// and the message.
func (p Problem) String() string {
	var b strings.Builder
	if p.Line > 0 {
		fmt.Fprintf(&b, "line %d: ", p.Line)
	}
	switch {
	case p.Job != "":
		fmt.Fprintf(&b, "job %q: ", p.Job)
	case p.Repository != "":
		fmt.Fprintf(&b, "repository %q: ", p.Repository)
	}
	b.WriteString(p.Message)
	return b.String()
}

// Check reads, parses and validates the config file at path, and returns
// every problem found, sorted by where it is in the file, problems with no
// location first. The config is nil when the file couldn't be read or
// parsed; it is valid when no problem is an error.
func Check(path string) (*Config, []Problem) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []Problem{{Severity: SeverityError, Message: fmt.Sprintf("reading config %s: %v", path, err)}}
	}
	cfg, root, err := parse(data)
	if err != nil {
		return nil, parseProblems(err)
	}

	var problems []Problem
	res := Validate(cfg)
	for _, err := range res.Errors {
		p := Problem{Severity: SeverityError, Message: err.Error()}
		var ve ValidationError
		if errors.As(err, &ve) {
			p.Message, p.Job, p.Repository, p.Path = ve.Message, ve.Job, ve.Repository, ve.Path
		}
		problems = append(problems, p)
	}
	for _, w := range res.Warnings {
		problems = append(problems, Problem{Severity: SeverityWarning, Message: w.Message, Job: w.Job, Repository: w.Repository, Path: w.Path})
	}
	for i := range problems {
		problems[i].Line, problems[i].Column = locate(root, problems[i].Path)
	}
	problems = append(problems, unknownKeys(root)...)
	sortProblems(problems)
	return cfg, problems
}

// HasErrors reports whether any of problems is an error.
func HasErrors(problems []Problem) bool {
	for _, p := range problems {
		if p.Severity == SeverityError {
			return true
		}
	}
	return false
}

func sortProblems(problems []Problem) {
	sort.SliceStable(problems, func(i, j int) bool {
		a, b := problems[i], problems[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
}

// lineRe finds the line number in yaml.v3's messages ("yaml: line 3: ...",
// "line 3: cannot unmarshal ...") and in this package's own.
var lineRe = regexp.MustCompile(`^(?:yaml: )?line (\d+): `)

// yamlParserErrors are the syntax errors that come from yaml.v3's parser
// rather than its scanner. For these it reports the 0-based line of the
// construct being parsed (an unclosed `{`, say), and leaves the line out
// when that is 0; for scanner errors the line is 1-based.
var yamlParserErrors = map[string]bool{
	"did not find expected <stream-start>":   true,
	"did not find expected <document start>": true,
	"did not find expected node content":     true,
	"did not find expected key":              true,
	"did not find expected '-' indicator":    true,
	"did not find expected ',' or '}'":       true,
	"did not find expected ',' or ']'":       true,
	"found undefined tag handle":             true,
	"found incompatible YAML document":       true,
	"found duplicate %YAML directive":        true,
	"found duplicate %TAG directive":         true,
}

// parseProblems turns an error from parsing into problems. A type error
// from decoding lists one message per bad value; each becomes a problem.
func parseProblems(err error) []Problem {
	messages := []string{err.Error()}
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		messages = typeErr.Errors
	}
	var problems []Problem
	for _, msg := range messages {
		p := Problem{Severity: SeverityError, Message: strings.TrimPrefix(msg, "yaml: ")}
		if m := lineRe.FindStringSubmatch(msg); m != nil {
			p.Line, _ = strconv.Atoi(m[1])
			p.Message = msg[len(m[0]):]
		}
		if strings.HasPrefix(msg, "yaml: ") && yamlParserErrors[p.Message] {
			p.Line++
		}
		problems = append(problems, p)
	}
	sortProblems(problems)
	return problems
}

// locate finds the field at a dotted path in the node tree, as the decoder
// would read it: through aliases and merge keys. It returns the position
// of the field's value, where a value reached through an alias is placed
// at the alias, and one merged in with `<<` at that merge. When the field
// is missing, it returns the position of the
// deepest key on the path that is present; when none is, 0, 0. yaml.v3
// counts lines and columns from 1, and columns in characters.
func locate(root *yaml.Node, path string) (line, column int) {
	if root == nil || path == "" {
		return 0, 0
	}
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	var key *yaml.Node // the deepest key on the path found so far
	// useSite is the first alias the path went through: where, in the
	// part of the file being read, the value comes from somewhere else.
	var useSite *yaml.Node
	for _, seg := range strings.Split(path, ".") {
		if n.Kind == yaml.AliasNode && useSite == nil {
			useSite = n
		}
		var next, nextKey *yaml.Node
		switch r := resolve(n); r.Kind {
		case yaml.MappingNode:
			for _, e := range mappingEntries(r, 0) {
				if e.key == seg {
					next, nextKey = e.value, e.keyNode
					if e.via != nil && useSite == nil {
						useSite = e.via
					}
					break
				}
			}
		case yaml.SequenceNode:
			if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(r.Content) {
				next, nextKey = r.Content[i], r.Content[i]
			}
		}
		if next == nil {
			switch {
			case useSite != nil:
				return useSite.Line, useSite.Column
			case key != nil:
				return key.Line, key.Column
			}
			return 0, 0
		}
		n, key = next, nextKey
	}
	if useSite != nil {
		return useSite.Line, useSite.Column
	}
	return n.Line, n.Column
}
