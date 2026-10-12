//go:build cli

package main

import "errors"

// serve is the CLI-only build's answer to the server command: the build links
// no storage, so there is nothing to run.
func serve([]string) error {
	return errors.New("this tracelet is the CLI-only build; run the server from the Docker image ghcr.io/abogoyavlensky/tracelet or a tracelet-server release")
}
