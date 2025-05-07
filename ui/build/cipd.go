// Copyright 2025 Google Inc. All rights reserved.
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

package build

import (
	"bufio"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	cipdProxyPolicyPath = "build/soong/ui/build/cipd_proxy_policy.txtpb"
	cipdProxyUrlKey     = "CIPD_PROXY_URL"
)

type cipdProxy struct {
	cmd      *Cmd
	wg       sync.WaitGroup
	stopping atomic.Bool
}

func cipdPath(config Config) string {
	return filepath.Join("prebuilts/cipd", config.HostPrebuiltTag(), "cipd")
}

func shouldRunCIPDProxy(config Config) bool {
	cipdPath := cipdPath(config)
	_, err := os.Stat(cipdPath)
	return err == nil
}

func startCIPDProxyServer(ctx Context, config Config) *cipdProxy {
	ctx.Status.Status("Starting CIPD proxy server...")

	cmd := Command(ctx, config, "cipd", cipdPath(config), "proxy", "-proxy-policy", cipdProxyPolicyPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	cp := cipdProxy{cmd: cmd}
	cp.wg.Add(1)
	go func() {
		// Log any error output from cipd until it exits.
		defer cp.wg.Done()

		bufReader := bufio.NewReader(stderr)
		for {
			l, err := bufReader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					if !cp.stopping.Load() {
						err := cmd.Wait()
						ctx.Printf("cipd: unexpected EOF, process exited with %v", err)
					}
					break
				}
				ctx.Fatalf("cipd: %v %v", l, err)
			}
			ctx.Printf("cipd: %v", l)
		}
	}()

	bufReader := bufio.NewReader(stdout)
	for {
		l, err := bufReader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			ctx.Printf("cipd: unexpected EOF: %v\n", l)
			// The stderr goroutine will handle the EOF
			cp.wg.Wait()
		}

		if err != nil {
			log.Fatalf("Got %v reading from cipd process", err)
		}
		if strings.HasPrefix(l, cipdProxyUrlKey) {
			proxyUrl := strings.TrimSpace(l[len(cipdProxyUrlKey)+1:])
			config.environ.Set(cipdProxyUrlKey, proxyUrl)
			ctx.Println("Started CIPD proxy listening on", proxyUrl)
			break
		}
	}
	return &cp
}

func (c *cipdProxy) Stop() {
	c.stopping.Store(true)
	c.cmd.Process.Kill()
	c.wg.Wait()
}
