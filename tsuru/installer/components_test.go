// Copyright 2016 tsuru-client authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package installer

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/fsouza/go-dockerclient"
	"github.com/fsouza/go-dockerclient/testing"
	"github.com/tsuru/config"
	"gopkg.in/check.v1"
)

func (s *S) TestInstallComponentsDefaultConfig(c *check.C) {
	tests := []struct {
		component     TsuruComponent
		containerName string
		image         string
		cmd           []string
		env           []string
	}{
		{&MongoDB{}, "mongo", "mongo:latest", []string(nil), []string(nil)},
		{&Redis{}, "redis", "redis:latest", []string(nil), []string(nil)},
		{&PlanB{}, "planb", "tsuru/planb:latest",
			[]string{"--listen", ":80",
				"--read-redis-host", "127.0.0.1",
				"--write-redis-host", "127.0.0.1",
			}, []string(nil)},
		{&Registry{}, "registry", "registry:2", []string(nil),
			[]string{"REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY=/var/lib/registry",
				"REGISTRY_HTTP_TLS_KEY=/certs/127.0.0.1:5000/registry-key.pem",
				"REGISTRY_HTTP_TLS_CERTIFICATE=/certs/127.0.0.1:5000/registry-cert.pem"}},
		{&TsuruAPI{}, "tsuru", "tsuru/api:latest", []string(nil),
			[]string{"MONGODB_ADDR=127.0.0.1",
				"MONGODB_PORT=27017",
				"REDIS_ADDR=127.0.0.1",
				"REDIS_PORT=6379",
				"HIPACHE_DOMAIN=127.0.0.1.nip.io",
				"REGISTRY_ADDR=127.0.0.1",
				"REGISTRY_PORT=5000",
				"TSURU_ADDR=http://127.0.0.1",
				"TSURU_PORT=8080",
			}},
	}
	c.Assert(len(tests), check.Equals, len(TsuruComponents))
	containerChan := make(chan *docker.Container)
	tlsConfig := testing.TLSConfig{
		CertPath:    s.TLSCertsPath.ServerCert,
		CertKeyPath: s.TLSCertsPath.ServerKey,
		RootCAPath:  s.TLSCertsPath.RootCert,
	}
	server, err := testing.NewTLSServer("127.0.0.1:0", containerChan, nil, tlsConfig)
	c.Assert(err, check.IsNil)
	defer server.Stop()
	mockMachine := &Machine{Address: server.URL(), IP: "127.0.0.1", CAPath: s.TLSCertsPath.RootDir}
	installConfig := NewInstallConfig("test")
	for _, tt := range tests {
		go tt.component.Install(mockMachine, installConfig)

		cont := <-containerChan
		c.Assert(cont.Name, check.Equals, tt.containerName)
		c.Assert(cont.State.Running, check.Equals, false)
		c.Assert(cont.Image, check.Equals, tt.image)
		c.Assert(cont.Config.Cmd, check.DeepEquals, tt.cmd)
		sort.Strings(cont.Config.Env)
		sort.Strings(tt.env)
		c.Assert(cont.Config.Env, check.DeepEquals, tt.env)

		cont = <-containerChan
		c.Assert(cont.State.Running, check.Equals, true)

	}
}

func (s *S) TestInstallComponentsCustomRegistry(c *check.C) {
	config.Set("docker-hub-mirror", "myregistry.com")
	defer config.Unset("docker-hub-mirror")
	tests := []struct {
		component TsuruComponent
		image     string
	}{
		{&MongoDB{}, "myregistry.com/mongo:latest"},
		{&Redis{}, "myregistry.com/redis:latest"},
		{&PlanB{}, "myregistry.com/tsuru/planb:latest"},
		{&Registry{}, "myregistry.com/registry:2"},
		{&TsuruAPI{}, "myregistry.com/tsuru/api:latest"},
	}
	c.Assert(len(tests), check.Equals, len(TsuruComponents))
	containerChan := make(chan *docker.Container)
	tlsConfig := testing.TLSConfig{
		CertPath:    s.TLSCertsPath.ServerCert,
		CertKeyPath: s.TLSCertsPath.ServerKey,
		RootCAPath:  s.TLSCertsPath.RootCert,
	}
	server, err := testing.NewTLSServer("127.0.0.1:0", containerChan, nil, tlsConfig)
	c.Assert(err, check.IsNil)
	defer server.Stop()
	mockMachine := &Machine{Address: server.URL(), CAPath: s.TLSCertsPath.RootDir}
	for _, tt := range tests {
		config := NewInstallConfig("test")
		go tt.component.Install(mockMachine, config)

		cont := <-containerChan
		c.Assert(cont.State.Running, check.Equals, false)
		c.Assert(cont.Image, check.Equals, tt.image)

		cont = <-containerChan
		c.Assert(cont.State.Running, check.Equals, true)
		c.Assert(cont.Image, check.Equals, tt.image)
	}
}

func (s *S) TestInstallPlanbHostPortBindings(c *check.C) {
	containerChan := make(chan *docker.Container)
	tlsConfig := testing.TLSConfig{
		CertPath:    s.TLSCertsPath.ServerCert,
		CertKeyPath: s.TLSCertsPath.ServerKey,
		RootCAPath:  s.TLSCertsPath.RootCert,
	}
	server, err := testing.NewTLSServer("127.0.0.1:0", containerChan, nil, tlsConfig)
	c.Assert(err, check.IsNil)
	defer server.Stop()
	mockMachine := &Machine{Address: server.URL(), CAPath: s.TLSCertsPath.RootDir}
	planb := &PlanB{}
	expectedExposed := map[docker.Port]struct{}{
		docker.Port("80/tcp"): {},
	}
	expectedBinds := map[docker.Port][]docker.PortBinding{
		"80/tcp": {{HostIP: "0.0.0.0", HostPort: "80"}},
	}
	config.Unset("docker-hub-mirror")
	installConfig := NewInstallConfig("test")
	go planb.Install(mockMachine, installConfig)
	cont := <-containerChan
	c.Assert(cont.HostConfig.PortBindings, check.DeepEquals, expectedBinds)
	c.Assert(cont.Config.ExposedPorts, check.DeepEquals, expectedExposed)
}

func (s *S) TestComponentStatusReport(c *check.C) {
	tlsConfig := testing.TLSConfig{
		CertPath:    s.TLSCertsPath.ServerCert,
		CertKeyPath: s.TLSCertsPath.ServerKey,
		RootCAPath:  s.TLSCertsPath.RootCert,
	}
	containerHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		cont := docker.Container{HostConfig: &docker.HostConfig{
			PortBindings: map[docker.Port][]docker.PortBinding{},
		}}
		contBuf, err := json.Marshal(cont)
		c.Assert(err, check.IsNil)
		w.Write(contBuf)
	}
	server, err := testing.NewTLSServer("127.0.0.1:0", nil, nil, tlsConfig)
	c.Assert(err, check.IsNil)
	defer server.Stop()
	server.CustomHandler("/containers/.*/json", http.HandlerFunc(containerHandler))
	defer server.CustomHandler("/containers/.*/json", server.DefaultHandler())
	mockMachine := &Machine{Address: server.URL(), IP: "127.0.0.1", CAPath: s.TLSCertsPath.RootDir}
	_, err = containerStatus("mongo", mockMachine)
	c.Assert(err, check.IsNil)
}
