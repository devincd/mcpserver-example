package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// The following fields are populated at build time using -ldflags -X.
// Note that DATE is omitted for reproducible builds
var (
	buildstamp       = "unknown"
	buildGitRevision = "unknown"
	buildTag         = "unknown"
	buildHub         = "unknown"
)

type BuildInfo struct {
	GitRevision   string `json:"build_git_revision"`
	GolangVersion string `json:"build_golang_version"`
	Timestamp     string `json:"build_timestamp"`
	OS            string `json:"build_os"`
	Arch          string `json:"build_arch"`
}

type DockerBuildInfo struct {
	Hub  string `json:"docker_build_hub"`
	Tag  string `json:"docker_build_tag"`
	OS   string `json:"docker_build_os"`
	Arch string `json:"docker_build_arch"`
}

var (
	// Info exports the build version information.
	Info       BuildInfo
	DockerInfo DockerBuildInfo
)

func (b BuildInfo) String() string {
	return fmt.Sprintf("%v-%v-%v-%v-%v",
		b.GitRevision,
		b.GolangVersion,
		b.Timestamp,
		b.OS,
		b.Arch)
}

func (b BuildInfo) JsonString() string {
	ret, _ := json.Marshal(b)
	return string(ret)
}

func (d DockerBuildInfo) string() string {
	return fmt.Sprintf("%v-%v-%v-%v",
		d.Hub,
		d.Tag,
		d.OS,
		d.Arch)
}

func (d DockerBuildInfo) JsonString() string {
	ret, _ := json.Marshal(d)
	return string(ret)
}

func init() {
	Info = BuildInfo{
		GitRevision:   buildGitRevision,
		GolangVersion: runtime.Version(),
		Timestamp:     buildstamp,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
	}

	DockerInfo = DockerBuildInfo{
		Hub:  buildHub,
		Tag:  buildTag,
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
}

func VersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Prints out build version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(os.Stdout, fmt.Sprintf("BuildInfo: %s\n", Info.JsonString()))
			fmt.Fprintf(os.Stdout, fmt.Sprintf("DockerBuildInfo: %s\n", DockerInfo.JsonString()))
			return nil
		},
	}
	return cmd
}
