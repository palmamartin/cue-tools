// Copyright 2021 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/cue-sh/tools/internal/codereviewcfg"
	"github.com/go-git/go-git/v5"
	"github.com/google/go-github/v31/github"
	"golang.org/x/build/gerrit"
)

// eventType values define an enumeration of the various
// GitHub repository dispatch workflows that can be triggered
// by cueckoo
type eventType string

const (
	eventTypeRuntrybot eventType = "runtrybot"
	eventTypeMirror    eventType = "mirror"
	eventTypeImportPR  eventType = "importpr"
)

type repositoryDispatchPayload struct {
	Type    eventType   `json:"type"`
	Payload interface{} `json:"payload"`
}

// config holds the configuration that is loaded from the codereview config
// found within the root of the git directory that contains the working
// directory. Put another way, cueckoo needs to be run from within the main
// cue repo.
type config struct {
	// repo is the local cue repository
	repo *git.Repository

	// gerritURL is the URL of the Gerrit instance
	gerritURL string

	// githubOwner is the organisation/user to which the GitHub repo belongs
	githubOwner string

	// githubRepo is the name of the GitHub repo
	githubRepo string

	// githubClient is the client for using the GitHub API
	githubClient *github.Client

	// gerritClient is the client for using the Gerrit API
	gerritClient *gerrit.Client
}

func loadConfig() *config {
	var res config
	rep, err := git.PlainOpenWithOptions(".", &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	check(err, "failed to find git repository: %v", err)
	res.repo = rep

	wt, err := rep.Worktree()
	check(err, "failed to get worktree: %v", err)

	cfg, err := codereviewcfg.Config(wt.Filesystem.Root())
	check(err, "failed to load codereview config: %v", err)

	gerritURL := cfg["gerrit"]
	if gerritURL == "" {
		raise("missing Gerrit server in codereview config")
	}
	githubURL := cfg["github"]
	if githubURL == "" {
		raise("missing GitHub server in codereview config")
	}
	res.gerritURL, err = codereviewcfg.GerritURLToServer(gerritURL)
	check(err, "failed to derived Gerrit server from %v: %v", gerritURL, err)

	res.githubOwner, res.githubRepo, err = codereviewcfg.GithubURLToParts(githubURL)
	check(err, "failed to derive GitHub owner and repo from %v: %v", githubURL, err)

	auth := github.BasicAuthTransport{
		Username: os.Getenv("GITHUB_USER"),
		Password: os.Getenv("GITHUB_PAT"),
	}
	res.githubClient = github.NewClient(auth.Client())
	res.gerritClient = gerrit.NewClient(res.gerritURL, gerrit.NoAuth)

	return &res
}

func (c *config) triggerRepositoryDispatch(payload github.DispatchRequestOptions) error {
	_, resp, err := c.githubClient.Repositories.Dispatch(context.Background(), c.githubOwner, c.githubRepo, payload)
	if err != nil {
		return fmt.Errorf("failed to send dispatch event: %v", err)
	}
	if resp.StatusCode/100 != 2 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			body = []byte("(failed to read body)")
		}
		return fmt.Errorf("dispatch call did not succeed; status code %v\n%s", resp.StatusCode, body)
	}
	return nil
}

func buildDispatchPayload(msg string, et eventType, payload interface{}) (ro github.DispatchRequestOptions, err error) {
	rp := repositoryDispatchPayload{
		Type:    et,
		Payload: payload,
	}
	byts, err := json.Marshal(rp)
	if err != nil {
		return ro, fmt.Errorf("failed to marshal payload: %v", err)
	}
	rm := json.RawMessage(byts)

	ro.EventType = msg
	ro.ClientPayload = &rm

	return
}