//go:build tools
// +build tools

/*
Copyright 2019 The Kubernetes Authors.

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

// main is the main package for the release notes generator.
package main

import (
	"flag"
	"fmt"
	"log"
	"os/exec"

	"github.com/blang/semver/v4"
	"github.com/pkg/errors"
)

/*
This tool prints all the titles of all PRs in between to references.

Use these as the base of your release notes.
*/

func main() {
	cmd := newNotesCmd()
	if err := cmd.run(); err != nil {
		log.Fatal(err)
	}
}

type notesCmdConfig struct {
	repo                        string
	fromRef                     string
	toRef                       string
	newTag                      string
	branch                      string
	prefixAreaLabel             bool
	preReleaseVersion           bool
	deprecation                 bool
	addKubernetesVersionSupport bool
}

func readCmdConfig() *notesCmdConfig {
	config := &notesCmdConfig{}

	flag.StringVar(&config.repo, "repository", "kubernetes-sigs/cluster-api", "The repo to run the tool from.")
	flag.StringVar(&config.fromRef, "from", "", "The tag or commit to start from. It must be formatted as heads/<branch name> for branches and tags/<tag name> for tags. If not set, it will be calculated from release.")
	flag.StringVar(&config.toRef, "to", "", "The ref (tag, branch or commit to stop at. It must be formatted as heads/<branch name> for branches and tags/<tag name> for tags. If not set, it will default to branch.")
	flag.StringVar(&config.branch, "branch", "", "The branch to generate the notes from. If not set, it will be calculated from release.")
	flag.StringVar(&config.newTag, "release", "", "The tag for the new release.")

	flag.BoolVar(&config.prefixAreaLabel, "prefix-area-label", true, "If enabled, will prefix the area label.")
	flag.BoolVar(&config.preReleaseVersion, "pre-release-version", false, "If enabled, will add a pre-release warning header. (default false)")
	flag.BoolVar(&config.deprecation, "deprecation", true, "If enabled, will add a templated deprecation warning header.")
	flag.BoolVar(&config.addKubernetesVersionSupport, "add-kubernetes-version-support", true, "If enabled, will add the Kubernetes version support header.")

	flag.Parse()

	return config
}

type notesCmd struct {
	config *notesCmdConfig
}

func newNotesCmd() *notesCmd {
	config := readCmdConfig()
	return &notesCmd{
		config: config,
	}
}

func (cmd *notesCmd) run() error {
	if err := validateConfig(cmd.config); err != nil {
		return err
	}

	if err := computeConfigDefaults(cmd.config); err != nil {
		return err
	}

	if err := ensureInstalledDependencies(); err != nil {
		return err
	}

	from, to := parseRef(cmd.config.fromRef), parseRef(cmd.config.toRef)

	printer := newReleaseNotesPrinter(cmd.config.repo, from.value)
	printer.isPreRelease = cmd.config.preReleaseVersion
	printer.printDeprecation = cmd.config.deprecation
	printer.printKubernetesSupport = cmd.config.addKubernetesVersionSupport

	generator := newNotesGenerator(
		newGithubFromToPRLister(cmd.config.repo, from, to, cmd.config.branch),
		newPREntryProcessor(cmd.config.prefixAreaLabel),
		printer,
	)

	return generator.run()
}

func ensureInstalledDependencies() error {
	if !commandExists("gh") {
		return errors.New("gh GitHub CLI not available. GitHub CLI is required to be present in the PATH. Refer to https://cli.github.com/ for installation")
	}

	return nil
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func validateConfig(config *notesCmdConfig) error {
	if config.fromRef == "" && config.newTag == "" {
		return errors.New("at least one of --from or --release need to be set")
	}

	if config.branch == "" && config.newTag == "" {
		return errors.New("at least one of --branch or --release need to be set")
	}

	if config.fromRef != "" {
		if err := validateRef(config.fromRef); err != nil {
			return err
		}
	}

	if config.toRef != "" {
		if err := validateRef(config.toRef); err != nil {
			return err
		}
	}

	return nil
}

// computeConfigDefaults calculates the value the non specified configuration fields
// based on the set fields.
func computeConfigDefaults(config *notesCmdConfig) error {
	if config.fromRef != "" && config.branch != "" && config.toRef != "" {
		return nil
	}

	newTag, err := semver.ParseTolerant(config.newTag)
	if err != nil {
		return errors.Wrap(err, "invalid --release, is not a semver")
	}

	if config.fromRef == "" {
		if newTag.Patch == 0 {
			// If patch = 0, this a new minor release
			// Hence we want to read commits from
			config.fromRef = "tags/" + fmt.Sprintf("v%d.%d.0", newTag.Major, newTag.Minor-1)
		} else {
			// if not new minor release, this is a new patch, just decrease the patch
			config.fromRef = "tags/" + fmt.Sprintf("v%d.%d.%d", newTag.Major, newTag.Minor, newTag.Patch-1)
		}
	}

	if config.branch == "" {
		config.branch = defaultBranchForNewTag(newTag)
	}

	if config.toRef == "" {
		config.toRef = "heads/" + config.branch
	}

	return nil
}

// defaultBranchForNewTag calculates the branch to cut a release
// based on the new release tag.
func defaultBranchForNewTag(newTag semver.Version) string {
	if newTag.Patch == 0 {
		if len(newTag.Pre) == 0 {
			// for new minor releases, use the release branch
			return releaseBranchForVersion(newTag)
		} else if len(newTag.Pre) == 2 && newTag.Pre[0].VersionStr == "rc" && newTag.Pre[1].VersionNum >= 1 {
			// for the second or later RCs, we use the release branch since we cut this branch with the first RC
			return releaseBranchForVersion(newTag)
		}

		// for any other pre release, we always cut from main
		// this includes all beta releases and the first RC
		return "main"
	}

	// If it's a patch, we use the release branch
	return releaseBranchForVersion(newTag)
}

func releaseBranchForVersion(version semver.Version) string {
	return fmt.Sprintf("release-%d.%d", version.Major, version.Minor)
}
