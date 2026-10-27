package filecontext

import (
	"flag"
	"fmt"
	"strconv"
)

type Flags struct {
	set             *flag.FlagSet
	current, legacy *bool
	lookup          func(string) (string, bool)
}

func RegisterFlags(set *flag.FlagSet, lookup func(string) (string, bool)) *Flags {
	return &Flags{set: set, lookup: lookup, current: set.Bool("enable-file-context", false, "enable Desktop Agent file context"), legacy: set.Bool("enable-chilling-file-context", false, "deprecated alias for --enable-file-context (uses built-in provider)")}
}
func (f *Flags) Enabled() (bool, error) {
	explicit := map[string]bool{}
	f.set.Visit(func(v *flag.Flag) { explicit[v.Name] = true })
	current, legacy := explicit["enable-file-context"], explicit["enable-chilling-file-context"]
	if current && legacy && *f.current != *f.legacy {
		return false, fmt.Errorf("conflicting file-context flags")
	}
	if current {
		return *f.current, nil
	}
	if legacy {
		return *f.legacy, nil
	}
	for _, name := range []string{"ARIEL_ENABLE_FILE_CONTEXT", "ARIEL_ENABLE_CHILLING_FILE_CONTEXT"} {
		if v, ok := f.lookup(name); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return false, fmt.Errorf("invalid boolean for %s", name)
			}
			return b, nil
		}
	}
	return false, nil
}
