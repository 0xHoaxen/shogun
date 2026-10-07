package fetch

import (
	"bufio"
	"bytes"
	"strings"
)

// rule is one Allow or Disallow line of a robots.txt group.
type rule struct {
	allow bool
	path  string
}

// robots is the rules that apply to one user agent.
type robots struct {
	rules []rule
}

// parseRobots reads the group of robots.txt that names agent, or the "*" group
// when none does. Matching follows RFC 9309: the longest matching path wins and
// Allow beats Disallow on a tie.
func parseRobots(body []byte, agent string) robots {
	agent = strings.ToLower(agent)
	var named, wildcard []rule
	var inNamed, inWildcard, sawRule bool
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if sawRule {
				inNamed, inWildcard, sawRule = false, false, false
			}
			v := strings.ToLower(value)
			if v == "*" {
				inWildcard = true
			} else if v != "" && strings.Contains(agent, v) {
				inNamed = true
			}
		case "allow", "disallow":
			sawRule = true
			r := rule{allow: key == "allow", path: value}
			if inNamed {
				named = append(named, r)
			}
			if inWildcard {
				wildcard = append(wildcard, r)
			}
		}
	}
	if len(named) > 0 {
		return robots{rules: named}
	}
	return robots{rules: wildcard}
}

// allowed reports whether the path may be fetched. An empty Disallow allows
// everything, and with no matching rule the path is allowed.
func (r robots) allowed(path string) bool {
	if path == "" {
		path = "/"
	}
	bestLen, allow := -1, true
	for _, rl := range r.rules {
		if rl.path == "" || !strings.HasPrefix(path, rl.path) {
			continue
		}
		if n := len(rl.path); n > bestLen || (n == bestLen && rl.allow) {
			bestLen, allow = n, rl.allow
		}
	}
	return allow
}
