package moltbook

import (
	"fmt"
	"strconv"
	"strings"
)

// solveChallenge evaluates a simple two-operand arithmetic question of the form
// "A op B" (op ∈ + - * x) that moltbook may require before publishing a post,
// and returns the integer answer as a string. It deliberately handles only this
// narrow shape; anything else is an error (we never guess).
func solveChallenge(question string) (string, error) {
	q := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(question), "="))
	// Normalize the common multiplication glyphs to '*'.
	q = strings.NewReplacer("×", "*", "x", "*", "X", "*").Replace(q)

	for _, op := range []string{"+", "-", "*"} {
		// Find the operator with spaces around it first, then bare, to avoid
		// splitting a negative operand.
		idx := strings.Index(q, " "+op+" ")
		sep := " " + op + " "
		if idx < 0 {
			idx = strings.Index(q, op)
			sep = op
		}
		if idx <= 0 {
			continue
		}
		left := strings.TrimSpace(q[:idx])
		right := strings.TrimSpace(q[idx+len(sep):])
		a, err1 := strconv.Atoi(left)
		b, err2 := strconv.Atoi(right)
		if err1 != nil || err2 != nil {
			continue
		}
		switch op {
		case "+":
			return strconv.Itoa(a + b), nil
		case "-":
			return strconv.Itoa(a - b), nil
		case "*":
			return strconv.Itoa(a * b), nil
		}
	}
	return "", fmt.Errorf("unsupported challenge %q", question)
}
