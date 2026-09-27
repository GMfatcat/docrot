package markdown

import "testing"

// TestIgnoreDirectiveCoversAFence: a lone <!-- docrot:ignore --> whose next
// line opens a fenced block ignores the whole block, not just the fence.
func TestIgnoreDirectiveCoversAFence(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		ignored    []int
		notIgnored []int
		rule       string // when set, check RuleIgnored for this rule instead of Ignored
	}{
		{
			name:       "backtick fence",
			lines:      []string{"a", "<!-- docrot:ignore -->", "```bash", "go build ./cmd/<x>", "```", "b"},
			ignored:    []int{3, 4, 5},
			notIgnored: []int{1, 2, 6},
		},
		{
			name:       "tilde fence with a longer closer",
			lines:      []string{"<!-- docrot:ignore -->", "~~~", "x", "~~~~", "after"},
			ignored:    []int{2, 3, 4},
			notIgnored: []int{5},
		},
		{
			name:       "shorter run does not close",
			lines:      []string{"<!-- docrot:ignore -->", "````", "```", "still inside", "````", "after"},
			ignored:    []int{2, 3, 4, 5},
			notIgnored: []int{6},
		},
		{
			name:       "unclosed fence reaches the end",
			lines:      []string{"<!-- docrot:ignore -->", "```", "x", "y"},
			ignored:    []int{2, 3, 4},
			notIgnored: []int{1},
		},
		{
			name:       "rule-scoped directive covers the fence for that rule",
			lines:      []string{"<!-- docrot:ignore missing-command -->", "```sh", "./cmd/service", "```", "b"},
			ignored:    []int{2, 3, 4},
			notIgnored: []int{5},
			rule:       "missing-command",
		},
		{
			name:       "a plain next line is still just one line",
			lines:      []string{"<!-- docrot:ignore -->", "x", "```", "y", "```"},
			ignored:    []int{2},
			notIgnored: []int{3, 4, 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doc(tt.lines...)
			is := func(l int) bool {
				if tt.rule != "" {
					return d.RuleIgnored(l, tt.rule)
				}
				return d.Ignored(l)
			}
			for _, l := range tt.ignored {
				if !is(l) {
					t.Errorf("line %d should be ignored", l)
				}
			}
			for _, l := range tt.notIgnored {
				if is(l) {
					t.Errorf("line %d should not be ignored", l)
				}
			}
		})
	}
}
