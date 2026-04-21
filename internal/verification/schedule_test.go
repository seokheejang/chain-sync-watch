package verification_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestSchedule_Valid5Fields(t *testing.T) {
	s, err := verification.NewSchedule("0 */6 * * *", "UTC")
	require.NoError(t, err)
	require.Equal(t, "0 */6 * * *", s.CronExpr())
	require.Equal(t, "UTC", s.Timezone().String())
	require.False(t, s.IsZero())
}

func TestSchedule_Valid6Fields(t *testing.T) {
	s, err := verification.NewSchedule("0 0 */6 * * *", "Asia/Seoul")
	require.NoError(t, err)
	require.Equal(t, "Asia/Seoul", s.Timezone().String())
}

func TestSchedule_EmptyTimezoneDefaultsToUTC(t *testing.T) {
	s, err := verification.NewSchedule("* * * * *", "")
	require.NoError(t, err)
	require.Equal(t, "UTC", s.Timezone().String())
}

func TestSchedule_TrimsWhitespace(t *testing.T) {
	s, err := verification.NewSchedule("   * * * * *   ", "UTC")
	require.NoError(t, err)
	require.Equal(t, "* * * * *", s.CronExpr())
}

func TestSchedule_InvalidExpression(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"too few fields", "* * *"},
		{"too many fields", "* * * * * * *"},
		{"letters in field", "hello * * * *"},
		{"symbol in field", "@ * * * *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verification.NewSchedule(tc.expr, "UTC")
			require.Error(t, err)
		})
	}
}

func TestSchedule_InvalidTimezone(t *testing.T) {
	_, err := verification.NewSchedule("* * * * *", "Not/A/Zone")
	require.Error(t, err)
}

func TestSchedule_ZeroValue(t *testing.T) {
	var s verification.Schedule
	require.True(t, s.IsZero())
	require.Equal(t, "", s.CronExpr())
	require.Nil(t, s.Timezone())
}

func TestSchedule_AllowedCharacterMix(t *testing.T) {
	// Exercises the full cron character alphabet in one go.
	cases := []string{
		"*/5 * * * *",
		"0,15,30,45 * * * *",
		"0 0-6 * * *",
		"0 0 1 1-6 *",
		"0 0 ? * MON", // letters rejected by shape check
	}
	for i, expr := range cases {
		_, err := verification.NewSchedule(expr, "UTC")
		if i < 4 {
			require.NoErrorf(t, err, "expr=%q should be valid", expr)
		} else {
			require.Errorf(t, err, "expr=%q should be rejected (letters not supported)", expr)
		}
	}
}
