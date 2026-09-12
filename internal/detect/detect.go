package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

type Rule struct {
	Pattern []string
	words   []*regexp.Regexp
}

func Load(path string) ([]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var config struct {
		Patterns *[][]*string `json:"patterns"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	if config.Patterns == nil {
		return nil, fmt.Errorf("config requires a patterns array")
	}
	patterns := make([][]string, len(*config.Patterns))
	for i, pattern := range *config.Patterns {
		for j, word := range pattern {
			if word == nil {
				return nil, fmt.Errorf("pattern %d word %d must be a string", i+1, j+1)
			}
			patterns[i] = append(patterns[i], *word)
		}
	}
	return Compile(patterns)
}

func Compile(patterns [][]string) ([]Rule, error) {
	rules := make([]Rule, 0, len(patterns))
	for i, pattern := range patterns {
		if len(pattern) == 0 {
			return nil, fmt.Errorf("pattern %d must contain at least one word expression", i+1)
		}
		rule := Rule{Pattern: pattern}
		for j, expression := range pattern {
			re, err := regexp.Compile(expression)
			if err != nil {
				return nil, fmt.Errorf("pattern %d word %d: %w", i+1, j+1, err)
			}
			// An alternative must be able to consume the entire word.
			re.Longest()
			rule.words = append(rule.words, re)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func Variant(shell string) (syntax.LangVariant, error) {
	switch filepath.Base(shell) {
	case "bash":
		return syntax.LangBash, nil
	case "sh", "dash", "posix":
		return syntax.LangPOSIX, nil
	case "zsh":
		return syntax.LangZsh, nil
	default:
		return 0, fmt.Errorf("unsupported shell; select bash, sh, or zsh")
	}
}

func Match(command, shell string, rules []Rule) ([][]string, error) {
	lang, err := Variant(shell)
	if err != nil {
		return nil, err
	}
	var candidates [][]word
	if err := collect(command, lang, 0, &candidates); err != nil {
		return nil, err
	}
	matches := [][]string{}
	for _, rule := range rules {
		if slices.ContainsFunc(candidates, rule.matches) {
			matches = append(matches, rule.Pattern)
		}
	}
	return matches, nil
}

func (r Rule) matches(candidate []word) bool {
	if len(candidate) < len(r.words) {
		return false
	}
	for i, expression := range r.words {
		if !candidate[i].literal {
			return false
		}
		match := expression.FindStringIndex(candidate[i].text)
		if match == nil || match[0] != 0 || match[1] != len(candidate[i].text) {
			return false
		}
	}
	return true
}

type word struct {
	text    string
	literal bool
}

func literal(w *syntax.Word, lang syntax.LangVariant) ([]string, bool) {
	known := true
	quoted := false
	syntax.Walk(w, func(n syntax.Node) bool {
		switch v := n.(type) {
		case nil, *syntax.Word, *syntax.Lit:
		case *syntax.SglQuoted:
			quoted = true
		case *syntax.DblQuoted:
			quoted = true
			if v.Dollar {
				known = false
			}
		default:
			known = false
		}
		return known
	})
	if !known {
		return nil, false
	}
	// Fields also expands braces; only quote removal is needed at this stage.
	field := &syntax.Word{Parts: append([]syntax.WordPart(nil), w.Parts...)}
	for i, part := range field.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok || !strings.Contains(lit.Value, "{") {
			continue
		}
		var escaped strings.Builder
		for j := 0; j < len(lit.Value); j++ {
			c := lit.Value[j]
			if c == '{' {
				escaped.WriteByte('\\')
			}
			escaped.WriteByte(c)
			if c == '\\' && j+1 < len(lit.Value) {
				j++
				escaped.WriteByte(lit.Value[j])
			}
		}
		copy := *lit
		copy.Value = escaped.String()
		field.Parts[i] = &copy
	}
	values, err := expand.Fields(&expand.Config{}, field)
	if err != nil || len(values) > 1 {
		return nil, false
	}
	value := ""
	if len(values) == 1 {
		value = values[0]
	}
	if value == "" && !quoted && lang != syntax.LangZsh {
		return nil, true
	}
	return []string{value}, true
}

func collect(command string, lang syntax.LangVariant, depth int, out *[][]word) error {
	if depth > 32 {
		return fmt.Errorf("shell command nesting exceeds 32 levels")
	}
	f, err := syntax.NewParser(syntax.Variant(lang)).Parse(strings.NewReader(command), "")
	if err != nil {
		// Parser messages may contain command text, so report only its position.
		if e, ok := err.(syntax.ParseError); ok {
			return fmt.Errorf("cannot parse %s syntax at %s", lang, e.Pos)
		}
		return fmt.Errorf("unsupported or invalid %s syntax", lang)
	}
	var walkErr error
	syntax.Walk(f, func(n syntax.Node) bool {
		if walkErr != nil {
			return false
		}
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		words := make([]word, 0, len(call.Args))
		for _, arg := range call.Args {
			arg := *arg
			if lang != syntax.LangPOSIX {
				syntax.SplitBraces(&arg)
			}
			for expanded, err := range expand.BracesSeq(nil, &arg) {
				if err != nil {
					walkErr = err
					return false
				}
				values, known := literal(expanded, lang)
				if !known {
					value := "${dynamic}"
					if len(expanded.Parts) > 0 {
						if head, ok := expanded.Parts[0].(*syntax.Lit); ok && assignment.MatchString(head.Value) {
							value = strings.SplitN(head.Value, "=", 2)[0] + "=${dynamic}"
						}
					}
					values = []string{value}
				}
				for _, value := range values {
					words = append(words, word{value, known})
				}
			}
		}
		words = unwrap(words)
		if len(words) == 0 || !words[0].literal {
			return true
		}
		name := filepath.Base(words[0].text)
		if name == "sh" || name == "bash" || name == "zsh" || name == "dash" {
			if script, ok := shellScript(words[1:]); ok {
				inner, _ := Variant(name)
				walkErr = collect(script, inner, depth+1, out)
			}
		}
		words = normalize(words)
		if len(words) > 0 {
			*out = append(*out, words)
		}
		return true
	})
	return walkErr
}

func shellScript(words []word) (string, bool) {
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !w.literal || !strings.HasPrefix(w.text, "-") || w.text == "--" {
			break
		}
		if w.text == "-o" || w.text == "-O" || w.text == "--rcfile" || w.text == "--init-file" {
			i++
			continue
		}
		if !strings.HasPrefix(w.text, "--") && strings.Contains(w.text, "c") {
			if i+1 < len(words) && words[i+1].literal {
				return words[i+1].text, true
			}
			break
		}
	}
	return "", false
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*=`)

func unwrap(words []word) []word {
	for len(words) > 0 {
		if assignment.MatchString(words[0].text) {
			words = words[1:]
			continue
		}
		if !words[0].literal {
			return nil
		}
		name := filepath.Base(words[0].text)
		switch name {
		case "command", "builtin", "exec", "nohup", "noglob", "env":
			if name == "command" && len(words) > 1 && (words[1].text == "-v" || words[1].text == "-V") {
				return nil
			}
			words = words[1:]
			for len(words) > 0 && words[0].literal && strings.HasPrefix(words[0].text, "-") {
				option := words[0].text
				words = words[1:]
				if option == "--" {
					break
				}
				if (name == "env" && (option == "-u" || option == "--unset" || option == "-C" || option == "--chdir")) || (name == "exec" && option == "-a") {
					if len(words) == 0 {
						return nil
					}
					words = words[1:]
				}
			}
		default:
			return words
		}
	}
	return words
}

func normalize(words []word) []word {
	if len(words) == 0 || !words[0].literal {
		return nil
	}
	name := filepath.Base(words[0].text)
	count := 0
	switch name {
	case "git":
		count = 1
	case "gh":
		count = 2
	default:
		return words
	}
	result := []word{{name, true}}
	index := 1
	for index < len(words) && len(result) <= count {
		w := words[index]
		if !w.literal {
			break
		}
		if (name == "git" && (w.text == "-C" || w.text == "-c" || w.text == "--git-dir" || w.text == "--work-tree" || w.text == "--namespace" || w.text == "--config-env")) ||
			(name == "gh" && (w.text == "-R" || w.text == "--repo" || w.text == "--hostname")) {
			index += 2
		} else if strings.HasPrefix(w.text, "-") {
			index++
		} else {
			result = append(result, w)
			index++
		}
	}
	if index < len(words) {
		result = append(result, words[index:]...)
	}
	return result
}
