package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
)

var (
	Version = "dev"
	Commit  = ""
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Dirty     bool   `json:"dirty,omitempty"`
	Time      string `json:"time,omitempty"`
	GoVersion string `json:"go_version"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

var currentInfo = sync.OnceValue(loadInfo)

func Current() Info {
	return currentInfo()
}

func loadInfo() Info {
	i := Info{
		Version:   Version,
		Commit:    Commit,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		if i.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			i.Version = bi.Main.Version
		}

		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				i.Time = s.Value
			case "vcs.modified":
				i.Dirty = s.Value == "true"
			}
		}
	}

	if i.Commit == "" {
		i.Commit = "unknown"
	}

	return i
}

func (i Info) String() string {
	commit := i.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}

	dirty := ""
	if i.Dirty {
		dirty = "+dirty"
	}

	return fmt.Sprintf("%s (%s%s)", i.Version, commit, dirty)
}

func String() string {
	return Current().String()
}
