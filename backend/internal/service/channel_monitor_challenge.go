package service

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
)

// monitorChallengePromptTemplate 1:1
const monitorChallengePromptTemplate = `Calculate and respond with ONLY the number, nothing else.

Q: 3 + 5 = ?
A: 8

Q: 12 - 7 = ?
A: 5

Q: %d %s %d = ?
A:`

// monitorChallengeNumberRegex
var monitorChallengeNumberRegex = regexp.MustCompile(`-?\d+`)

// monitorChallenge +
type monitorChallenge struct {
	Prompt   string
	Expected string
}

// generateChallenge
//   - [monitorChallengeMin, monitorChallengeMax]
//   - 50% %
//   -
//
//
func generateChallenge() monitorChallenge {
	a := randIntInRange(monitorChallengeMin, monitorChallengeMax)
	b := randIntInRange(monitorChallengeMin, monitorChallengeMax)

	if rand.IntN(2) == 0 { //nolint:gosec // only used for generating test questions, no security impact
		return monitorChallenge{
			Prompt:   fmt.Sprintf(monitorChallengePromptTemplate, a, "+", b),
			Expected: strconv.Itoa(a + b),
		}
	}

	hi, lo := a, b
	if lo > hi {
		hi, lo = lo, hi
	}
	return monitorChallenge{
		Prompt:   fmt.Sprintf(monitorChallengePromptTemplate, hi, "-", lo),
		Expected: strconv.Itoa(hi - lo),
	}
}

// randIntInRange [min, max]
func randIntInRange(minVal, maxVal int) int {
	if maxVal <= minVal {
		return minVal
	}
	return minVal + rand.IntN(maxVal-minVal+1) //nolint:gosec
}

// validateChallenge
func validateChallenge(responseText, expected string) bool {
	if responseText == "" || expected == "" {
		return false
	}
	matches := monitorChallengeNumberRegex.FindAllString(responseText, -1)
	for _, m := range matches {
		if m == expected {
			return true
		}
	}
	return false
}
