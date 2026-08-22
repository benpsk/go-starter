package console

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/benpsk/go-starter/internal/app"
)

// version is overridable at link time:
//
//	go build -ldflags "-X github.com/benpsk/go-starter/internal/console.version=1.2.3"
var version = ""

func helpCommand(r *Registry) Command {
	return Command{
		Name:     "help",
		Group:    GroupOther,
		Summary:  "Show the command list, or one command's flags",
		NeedsApp: false,
		Setup: func(fs *flag.FlagSet) RunFunc {
			return func(_ context.Context, _ *app.App, out io.Writer) error {
				if fs.NArg() == 0 {
					r.PrintHelp(out)
					return nil
				}
				return r.PrintCommandHelp(out, fs.Arg(0))
			}
		},
	}
}

func versionCommand() Command {
	return Command{
		Name:     "version",
		Group:    GroupOther,
		Summary:  "Show version and build information",
		NeedsApp: false,
		Setup: func(*flag.FlagSet) RunFunc {
			return func(_ context.Context, _ *app.App, out io.Writer) error {
				fmt.Fprintln(out, describeBuild())
				fmt.Fprintf(out, "go: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
				return nil
			}
		},
	}
}

// describeBuild reports the version, falling back to the VCS stamp the Go
// toolchain embeds, then to the module version, then to "dev".
func describeBuild() string {
	name := "Go Starter"
	if v := strings.TrimSpace(version); v != "" {
		return name + " " + v
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return name + " dev"
	}

	var revision, dirty string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if revision != "" {
		if len(revision) > 12 {
			revision = revision[:12]
		}
		return name + " " + revision + dirty
	}
	if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
		return name + " " + v
	}
	return name + " dev"
}
