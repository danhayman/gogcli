package cmd

import (
	"fmt"
	"sort"
	"strings"
)

type selectorMatch struct {
	ID   string
	Name string
}

func findByIDOrCaseFoldName(input, kind string, options []selectorMatch) (*selectorMatch, bool, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return nil, false, nil
	}

	for _, option := range options {
		if strings.TrimSpace(option.ID) == in {
			match := option
			return &match, true, nil
		}
	}

	var matches []selectorMatch
	for _, option := range options {
		name := strings.TrimSpace(option.Name)
		if name == "" || !strings.EqualFold(name, in) {
			continue
		}
		matches = append(matches, selectorMatch{
			ID:   strings.TrimSpace(option.ID),
			Name: name,
		})
	}

	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		match := matches[0]
		return &match, true, nil
	default:
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Name == matches[j].Name {
				return matches[i].ID < matches[j].ID
			}
			return matches[i].Name < matches[j].Name
		})
		parts := make([]string, 0, len(matches))
		for _, match := range matches {
			label := match.Name
			if label == "" {
				label = "(unnamed)"
			}
			parts = append(parts, fmt.Sprintf("%s (%s)", label, match.ID))
		}
		return nil, false, usagef("ambiguous %s %q; matches: %s", kind, in, strings.Join(parts, ", "))
	}
}
