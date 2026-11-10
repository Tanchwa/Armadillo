/*
Copyright 2026.

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

// The gate is injected into pods as a native sidecar: an init container with
// restartPolicy Always. The kubelet does not start the pod's other containers
// until the gate's startup probe passes, and the probe passes once the loader
// on the node reports every profile the pod needs as loaded. That turns
// "profile not on the node yet" from a CreateContainerError with exponential
// backoff into a short wait.
//
//	gate          blocks until terminated; this is the container's command
//	gate check    exits 0 when every profile is loaded; this is the startup probe
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "check" {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
		<-stop
		return
	}

	fs := flag.NewFlagSet("check", flag.ExitOnError)
	host := fs.String("loader-host", os.Getenv("HOST_IP"), "The IP of the node, where the loader listens.")
	port := fs.String("loader-port", "9099", "The host port of the loader's gate endpoint.")
	profiles := fs.String("profiles", "", "Comma-separated names of the profiles the pod needs.")
	_ = fs.Parse(os.Args[2:])

	if err := check(*host, *port, strings.Split(*profiles, ",")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(host, port string, profiles []string) error {
	if host == "" {
		return fmt.Errorf("no loader host: set --loader-host or HOST_IP")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for _, p := range profiles {
		if p == "" {
			continue
		}
		u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/v1/loaded/" + p}
		resp, err := client.Get(u.String())
		if err != nil {
			return fmt.Errorf("asking the loader about %s: %w", p, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("profile %s is not loaded on this node yet (HTTP %d)", p, resp.StatusCode)
		}
	}
	return nil
}
