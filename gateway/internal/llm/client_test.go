package llm

import (
	"strings"
	"testing"
)

func TestBuildPromptWithCategories(t *testing.T) {
	p := buildPrompt([]string{"Grocery", "Shopping", "Utilities", "Travel", "Household"})
	for _, want := range []string{
		"Grocery, Shopping, Utilities, Travel, Household",
		"exactly one of these allowed values",
		"price", "place", "date", "Return ONLY a valid JSON object",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "groceries") {
		t.Errorf("prompt should not contain free-form examples when categories are given")
	}
}

func TestBuildPromptWithoutCategories(t *testing.T) {
	p := buildPrompt(nil)
	if !strings.Contains(p, "groceries") {
		t.Errorf("generic prompt should keep free-form examples, got:\n%s", p)
	}
	if strings.Contains(p, "allowed values") {
		t.Errorf("generic prompt should not constrain categories")
	}
}
