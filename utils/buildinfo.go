/*
Copyright 2022 The SODA Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package utils provide utilities like buildinfo
package utils

import (
	"encoding/json"
	"fmt"
	"runtime"

	log "github.com/sirupsen/logrus"
)

var (
	gitVersion string

	gitCommit string

	gitTreeState string

	buildDate string
)

// BuildInfo contains build information
type BuildInfo struct {
	GitVersion   string `json:"gitVersion"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	BuildDate    string `json:"buildDate"`
	GoVersion    string `json:"goVersion"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
}

// GetBuildInfo returns the overall code build info
func GetBuildInfo() BuildInfo {
	// These variables typically come from -ldflags settings
	return BuildInfo{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

func (info BuildInfo) Print() {
	printPretty, err := json.MarshalIndent(&info, "", "    ")
	if err != nil {
		log.Infof("%+v", info)
		return
	}
	log.Infof("%s", string(printPretty))
}
