package filecontext

import (
	"flag"
	"testing"
)

func TestNeutralAndLegacyFlagsRespectExplicitDisableAndConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		want bool
		bad  bool
	}{
		{name: "default"},
		{name: "legacy", env: map[string]string{"ARIEL_ENABLE_CHILLING_FILE_CONTEXT": "true"}, want: true},
		{name: "neutral overrides legacy", env: map[string]string{"ARIEL_ENABLE_FILE_CONTEXT": "false", "ARIEL_ENABLE_CHILLING_FILE_CONTEXT": "true"}},
		{name: "explicit false", env: map[string]string{"ARIEL_ENABLE_FILE_CONTEXT": "true"}, args: []string{"--enable-chilling-file-context=false"}},
		{name: "neutral cli", args: []string{"--enable-file-context"}, want: true},
		{name: "conflict", args: []string{"--enable-file-context", "--enable-chilling-file-context=false"}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := flag.NewFlagSet("test", flag.ContinueOnError)
			flags := RegisterFlags(set, func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if err := set.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			got, err := flags.Enabled()
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
}
