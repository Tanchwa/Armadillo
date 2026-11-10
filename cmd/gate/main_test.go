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

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// loadedProfile is the one profile the fake loader reports as loaded.
const loadedProfile = "armadillo.shop.myapp"

func TestCheck(t *testing.T) {
	// Stands in for the loader: only loadedProfile is loaded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/loaded/"+loadedProfile {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	if err := check(host, port, []string{loadedProfile}); err != nil {
		t.Errorf("loaded profile: %v", err)
	}
	if err := check(host, port, []string{loadedProfile, "armadillo.shop.other"}); err == nil {
		t.Error("passed although one profile is not loaded")
	}
	if err := check("", port, []string{loadedProfile}); err == nil {
		t.Error("passed without a loader host")
	}
	srv.Close()
	if err := check(host, port, []string{loadedProfile}); err == nil {
		t.Error("passed although the loader is unreachable")
	}
}
