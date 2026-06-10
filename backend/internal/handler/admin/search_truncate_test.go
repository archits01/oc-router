//go:build unit

package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// truncateSearchByRune
func truncateSearchByRune(search string, maxRunes int) string {
	if runes := []rune(search); len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return search
}

func TestTruncateSearchByRune(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		wantLen  int // 期望的 rune length
	}{
		{
			name:     "纯中文超长",
			input:    string(make([]rune, 150)),
			maxRunes: 100,
			wantLen:  100,
		},
		{
			name:     "纯 ASCII 超长",
			input:    string(make([]byte, 150)),
			maxRunes: 100,
			wantLen:  100,
		},
		{
			name:     "empty string",
			input:    "",
			maxRunes: 100,
			wantLen:  0,
		},
		{
			name:     "恰好 100 个字符",
			input:    string(make([]rune, 100)),
			maxRunes: 100,
			wantLen:  100,
		},
		{
			name:     "不足 100 字符不截断",
			input:    "hello世界",
			maxRunes: 100,
			wantLen:  7,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateSearchByRune(tc.input, tc.maxRunes)
			require.Equal(t, tc.wantLen, len([]rune(result)))
		})
	}
}

func TestTruncateSearchByRune_PreservesMultibyte(t *testing.T) {
	// 101
	input := ""
	for i := 0; i < 101; i++ {
		input += "中"
	}
	result := truncateSearchByRune(input, 100)

	require.Equal(t, 100, len([]rune(result)))
	//
	require.Equal(t, 300, len(result))
}

func TestTruncateSearchByRune_MixedASCIIAndMultibyte(t *testing.T) {
	// 50 + 51 = 101
	input := ""
	for i := 0; i < 50; i++ {
		input += "a"
	}
	for i := 0; i < 51; i++ {
		input += "中"
	}
	result := truncateSearchByRune(input, 100)

	runes := []rune(result)
	require.Equal(t, 100, len(runes))
	// 'a'，''
	require.Equal(t, 'a', runes[0])
	require.Equal(t, 'a', runes[49])
	require.Equal(t, '中', runes[50])
	require.Equal(t, '中', runes[99])
}
