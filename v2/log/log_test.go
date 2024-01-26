package log_test

import (
	"errors"
	"math"
	"testing"
	"unicode"

	"github.com/arobie1992/go-clarinet/v2/log"
)

func TestLevelAtLeast(t *testing.T) {
	type testCase struct {
		level    log.Level
		expected bool
	}
	tests := []struct {
		level log.Level
		cases []testCase
	}{
		{log.Trace(), []testCase{
			{log.Trace(), true},
			{log.Debug(), true},
			{log.Info(), true},
			{log.Warn(), true},
			{log.Error(), true},
		}},
		{log.Debug(), []testCase{
			{log.Trace(), false},
			{log.Debug(), true},
			{log.Info(), true},
			{log.Warn(), true},
			{log.Error(), true},
		}},
		{log.Info(), []testCase{
			{log.Trace(), false},
			{log.Debug(), false},
			{log.Info(), true},
			{log.Warn(), true},
			{log.Error(), true},
		}},
		{log.Warn(), []testCase{
			{log.Trace(), false},
			{log.Debug(), false},
			{log.Info(), false},
			{log.Warn(), true},
			{log.Error(), true},
		}},
		{log.Error(), []testCase{
			{log.Trace(), false},
			{log.Debug(), false},
			{log.Info(), false},
			{log.Warn(), false},
			{log.Error(), true},
		}},
	}
	for i, test := range tests {
		for j, c := range test.cases {
			actual := test.level.AtLeast(c.level)
			if actual != c.expected {
				t.Errorf(
					"Test %d:%d at least incorrect. Level: %s,\tCompared to: %s.\tExpected: %t,\tGot: %t",
					i,
					j,
					test.level,
					c.level,
					c.expected,
					actual,
				)
			}
		}
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    log.Level
		expected string
	}{
		{log.Trace(), "TRACE"},
		{log.Debug(), "DEBUG"},
		{log.Info(), "INFO"},
		{log.Warn(), "WARN"},
		{log.Error(), "ERROR"},
	}
	for i, test := range tests {
		if test.level.String() != test.expected {
			t.Errorf("Test %d wrong string output. Expected: %s, Got: %s", i, test.expected, test.level.String())
		}
	}
}

func TestParseLe(t *testing.T) {
	tests := []struct {
		in          string
		expectedOut log.Level
		expectedErr error
	}{
		{"trace", log.Trace(), nil},
		{"debug", log.Debug(), nil},
		{"info", log.Info(), nil},
		{"warn", log.Warn(), nil},
		{"error", log.Error(), nil},
		{"invalid", nil, errors.New("Unrecognized log level: invalid. Recognized levels are case insensitive [TRACE DEBUG INFO WARN ERROR]")},
	}
	for i, test := range tests {
		perms := []string{}
		if i > 4 {
			perms = append(perms, test.in)
		} else {
			bytes := []byte(test.in)
			numPerms := math.Pow(float64(2), float64(len(bytes)))
			for i := 0; i < int(numPerms); i += 1 {
				perm := []byte{}
				for pos := 0; pos < len(bytes); pos += 1 {
					b := bytes[len(bytes)-1-pos]
					mask := (i >> pos) & 1
					if mask == 0 {
						b = byte(unicode.ToUpper(rune(b)))
					}
					perm = append([]byte{b}, perm...)
				}
				perms = append(perms, string(perm))
			}
		}
		for j, p := range perms {
			level, err := log.ParseLevel(p)
			if level != test.expectedOut {
				t.Errorf("Test %d:%d incorrect output. Expected: %s, Got: %s", i, j, test.expectedOut, level)
			}
			if test.expectedErr == nil && err == nil {
				continue
			}
			if err == nil || test.expectedErr.Error() != err.Error() {
				t.Errorf("Test %d:%d incorrect error. Expected: '%s', Got: '%s'", i, j, test.expectedErr, err)
			}
		}
	}
}
