package detect

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLegacy(t *testing.T) {
	rules, err := Load("testdata/permissions.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/legacy_permissions.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Permissions struct {
			Ask []string `json:"ask"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Permissions.Ask) != 90 {
		t.Fatalf("got %d legacy command prefixes", len(legacy.Permissions.Ask))
	}
	data, err = os.ReadFile("testdata/legacy_commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string][]string
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"bash", "zsh"} {
		for decision, commands := range cases {
			for _, command := range commands {
				t.Run(shell+"/"+command, func(t *testing.T) {
					matches, err := Match(command, shell, rules)
					if err != nil || (len(matches) > 0) != (decision == "ask") {
						t.Fatalf("matches=%v error=%v; expected %s", matches, err, decision)
					}
				})
			}
		}
		for _, text := range legacy.Permissions.Ask {
			prefix := strings.TrimSuffix(strings.TrimPrefix(text, "Bash("), ":*)")
			for _, command := range []string{prefix, prefix + " example"} {
				matches, err := Match(command, shell, rules)
				if err != nil || len(matches) == 0 {
					t.Errorf("%s %q: %v, %v", shell, command, matches, err)
				}
			}
			matches, err := Match(prefix+"-helper", shell, rules)
			if err != nil || len(matches) != 0 {
				t.Errorf("%s matched an unrelated command: %s-helper: %v %v", shell, prefix, matches, err)
			}
		}
	}
}

func TestShellExecutionContexts(t *testing.T) {
	rules, _ := Compile([][]string{{"git", "push"}})
	cases := []struct {
		command string
		ask     bool
	}{
		{`echo "$(git push origin main)"`, true},
		{"echo `git push`", true},
		{"cat <<EOF\n$(git push)\nEOF", true},
		{"cat <<'EOF'\n$(git push)\nEOF", false},
		{"cat <<\\EOF\n$(git push)\nEOF", false},
		{"cat <<-EOF\n\t$(git push)\n\tEOF", true},
		{"cat <<EOF\n\\$(git push)\nEOF", false},
		{"cat <<A <<'B'\n$(git push)\nA\n$(git push)\nB", true},
		{`value=$(git push)`, true},
		{`echo ${value:-$(git push)}`, true},
		{`cat <(git push)`, true},
		{`echo '$(git push)'`, false},
		{`echo git push`, false},
		{`"git push"`, false},
		{`git "push origin"`, false},
		{`git "pu"sh "$branch"`, true},
		{`git p\ush`, true},
		{`git $'pu\x73h'`, true},
		{`git "$operation"`, false},
		{`sh -c 'git push'`, true},
		{`bash --noprofile -l -c 'git push'`, true},
		{`zsh -o no_rcs -c 'git push'`, true},
		{`env -C '/tmp/a b' git push`, true},
		{`env FOO="$bar" git push`, true},
		{`env FOO="$(printf x)" git push`, true},
		{`noglob git push`, true},
		{`exec -a alternate git push`, true},
		{`if false; then git push; fi`, true},
		{`case x in x) git push;; esac`, true},
		{`f() { git push; }`, true},
		{`cd /foo/bar && git push`, true},
	}
	for _, shell := range []string{"bash", "zsh"} {
		for _, tc := range cases {
			t.Run(shell+"/"+tc.command, func(t *testing.T) {
				matches, err := Match(tc.command, shell, rules)
				if err != nil || (len(matches) > 0) != tc.ask {
					t.Fatalf("matches=%v err=%v; want ask=%t", matches, err, tc.ask)
				}
			})
		}
	}
}

func TestRuleSemantics(t *testing.T) {
	cases := []struct {
		pattern []string
		command string
		want    bool
	}{
		{[]string{"git", "commit|push"}, "git commit", true},
		{[]string{"git", "commit|push"}, "git push origin", true},
		{[]string{"git", "commit|push"}, "git commit-tree", false},
		{[]string{"git", "commit|push"}, "git prepush", false},
		{[]string{"git", "commit|push"}, "git push-helper", false},
		{[]string{"git", "push|push-helper"}, "git push-helper", true},
		{[]string{"git", `\Qpush`}, "git push", true},
		{[]string{"git", "commit|push"}, `git "commit --amend"`, false},
		{[]string{"git", "commit|push"}, "git\tpush", true},
		{[]string{"git", "commit|push"}, `git push "$remote"`, true},
		{[]string{"git", ".*"}, `git "$operation"`, false},
		{[]string{"git"}, `git "$operation"`, true},
		{[]string{"gh", "pr"}, `gh pr "$operation"`, true},
		{[]string{"git", "push", ".*"}, `git push "$remote"`, false},
		{[]string{"git", "push", "origin"}, "git -C /tmp push origin main", true},
		{[]string{"git", "push", "origin"}, "git push", false},
		{[]string{"git", "push", "origin"}, "git push upstream", false},
		{[]string{"gh", ".*", "create"}, "gh pr create", true},
		{[]string{"gh", ".*", "create"}, "gh issue create --title test", true},
		{[]string{"gh", ".*", "create"}, "gh pr view 1", false},
		{[]string{"git|gh", "status"}, "git status", true},
		{[]string{"git|gh", "status"}, "git-helper status", false},
		{[]string{"printf", "a b"}, `printf "a b"`, true},
		{[]string{"printf", "a b"}, `printf a b`, false},
		{[]string{"printf", "a", "b"}, `printf "a b"`, false},
		{[]string{"printf", ""}, `printf ''`, true},
		{[]string{"printf", ""}, `printf`, false},
		{[]string{"printf", "(?m)foo$"}, "printf 'foo\nbar'", false},
		{[]string{"printf", ".*"}, "printf 'foo\nbar'", false},
		{[]string{"printf", "(?s).*"}, "printf 'foo\nbar'", true},
	}
	for _, tc := range cases {
		rules, err := Compile([][]string{tc.pattern})
		if err != nil {
			t.Fatal(err)
		}
		matches, err := Match(tc.command, "bash", rules)
		if err != nil || (len(matches) > 0) != tc.want {
			t.Errorf("%v / %s: %v %v", tc.pattern, tc.command, matches, err)
		}
	}
}

func TestBraceExpansion(t *testing.T) {
	rules, err := Compile([][]string{{"git", "commit|push"}, {"gh", "pr", "create"}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		command string
		ask     bool
	}{
		{`git {push,-v} origin main`, true},
		{`git {commit,-a} -m x`, true},
		{`gh {pr,create} --fill`, true},
		{`git {-C,.} push`, true},
		{`{git,push} origin main`, true},
		{`git {pu{sh,ll},status}`, true},
		{`git {"push",status}`, true},
		{`git {push,"$remote"}`, true},
		{`git {"$operation",push}`, false},
		{`env {FOO="$value",git,push}`, true},
		{`git {'',push}`, false},
		{`git {status,push}`, false},
		{`gi{t,} push`, false},
		{`git "{push,-v}"`, false},
		{`git '{push,-v}'`, false},
		{`git \{push,-v\}`, false},
		{`git {push}`, false},
		{`git {push,-v`, false},
		{`echo {git,push}`, false},
		{`echo {status,$(git push)}`, true},
		{`bash -c 'git {push,-v}'`, true},
		{`zsh -c 'git {push,-v}'`, true},
		{`sh -c 'git {push,-v}'`, false},
	}
	for _, shell := range []string{"bash", "zsh"} {
		for _, tc := range cases {
			t.Run(shell+"/"+tc.command, func(t *testing.T) {
				matches, err := Match(tc.command, shell, rules)
				if err != nil || (len(matches) > 0) != tc.ask {
					t.Fatalf("matches=%v err=%v; want ask=%t", matches, err, tc.ask)
				}
			})
		}
	}
	for _, shell := range []string{"bash", "zsh", "sh"} {
		for _, tc := range []struct {
			pattern []string
			command string
			ask     bool
		}{
			{[]string{"git", "push"}, `git {,push}`, shell == "bash"},
			{[]string{"printf", "01", "02", "03"}, `printf {01..03}`, shell != "sh"},
			{[]string{"printf", "a1", "a2", "b1", "b2"}, `printf {a,b}{1,2}`, shell != "sh"},
			{[]string{"git", `\{push,-v\}`}, `git {push,-v}`, shell == "sh"},
		} {
			rules, err := Compile([][]string{tc.pattern})
			if err != nil {
				t.Fatal(err)
			}
			matches, err := Match(tc.command, shell, rules)
			if err != nil || (len(matches) > 0) != tc.ask {
				t.Errorf("%s / %s: matches=%v err=%v; want ask=%t", shell, tc.command, matches, err, tc.ask)
			}
		}
	}
}

func TestBraceExpansionLimit(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		if _, err := Match(`git {push,-v}{1..1000000}`, shell, nil); err == nil {
			t.Errorf("%s: expected an error for excessive brace expansion", shell)
		}
	}
}

func TestPatternsMatchOnceInConfigOrder(t *testing.T) {
	patterns := [][]string{{"git", "commit|push"}, {"gh", "pr", "create|merge"}}
	rules, err := Compile(patterns)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := Match("gh pr merge 1; git push; git commit -m message; git push", "zsh", rules)
	if err != nil || !reflect.DeepEqual(matches, patterns) {
		t.Fatalf("matches=%v err=%v", matches, err)
	}
}

func TestInvalidSyntaxAndConfig(t *testing.T) {
	for _, command := range []string{"echo 'unterminated", "cat <<EOF\ntext", "if true; then"} {
		if _, err := Match(command, "bash", nil); err == nil {
			t.Errorf("expected parse error for %q", command)
		}
	}
	if _, err := Match("git push", "fish", nil); err == nil {
		t.Fatal("accepted unknown shell")
	}
	for _, config := range []string{`{}`, `null`, `{"permissions":{"ask":["Bash(git push:*)"]}}`, `{"patterns":null}`, `{"patterns":"git"}`, `{"patterns":["git"]}`, `{"patterns":[[]]}`, `{"patterns":[null]}`, `{"patterns":[["git",null]]}`, `{"patterns":[["git",1]]}`, `{"patterns":[["git","["]]}`} {
		path := t.TempDir() + "/permissions.json"
		os.WriteFile(path, []byte(config), 0600)
		if _, err := Load(path); err == nil {
			t.Errorf("accepted %s", config)
		}
	}
	for _, pattern := range [][]string{nil, {}, {"git", "("}, {"git", "*"}, {"git", "push)|(?:commit"}} {
		if _, err := Compile([][]string{pattern}); err == nil {
			t.Errorf("accepted invalid pattern: %v", pattern)
		}
	}
	if _, err := Compile([][]string{{"git"}, {"gh", "["}}); err == nil || !strings.Contains(err.Error(), "pattern 2 word 2") {
		t.Errorf("invalid regex must identify its location: %v", err)
	}
	path := t.TempDir() + "/permissions.json"
	if err := os.WriteFile(path, []byte(`{"patterns":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	rules, err := Load(path)
	if err != nil || len(rules) != 0 {
		t.Errorf("empty patterns must be valid: %v %v", rules, err)
	}
	if _, err := Match("echo <(git push)", "sh", nil); err == nil {
		t.Fatal("accepted process substitution as POSIX")
	}
}
