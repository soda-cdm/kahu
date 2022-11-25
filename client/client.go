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

package client

import (
	"fmt"
	"runtime"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
)

const (
	defaultClientQPS   = 0.0
	defaultClientBurst = 0
)

type Factory interface {
	AddFlags(flags *pflag.FlagSet)

	KahuClient() (clientset.Interface, error)

	KubeClient() (kubernetes.Interface, error)

	DynamicClient() (dynamic.Interface, error)

	ClientConfig() (*rest.Config, error)
}


type Config struct {
	KubeConfig    string `json:"kubeConfig"`
	ClientQPS     float32 `json:"clientQPS"`
	ClientBurst   int `json:"clientBurst"`
}


type factory struct {
	agentBaseName string
	*Config
}

// NewFactoryConfig returns factory configuration.
func NewFactoryConfig() *Config {
	cfg := &Config{
		ClientQPS:     defaultClientQPS,
		ClientBurst:   defaultClientBurst,
	}

	return cfg
}

// NewFactory returns a Factory.
func NewFactory(agentBaseName string, cfg *Config) Factory {
	f := &factory{
		agentBaseName: agentBaseName,
		Config: cfg,
	}

	return f
}

func (cfg *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVarP(&cfg.KubeConfig, "kube-config", "k", cfg.KubeConfig,
		"Path to the kubeconfig file to use to talk to the Kubernetes apiserver. "+
			"If unset, try the environment variable KUBECONFIG, as well as in-cluster configuration")
	fs.Float32VarP(&cfg.ClientQPS, "clientqps", "q", cfg.ClientQPS,
		"Indicates the maximum QPS in kubernetes client")
	fs.IntVarP(&cfg.ClientBurst, "clientburst", "b", cfg.ClientBurst,
		"Maximum burst for throttle in Kubernetes client")
}

func (cfg *Config) Validate() error {
	if cfg.ClientBurst < 0 {
		return fmt.Errorf("invalid client burst value %d", cfg.ClientBurst)
	}

	if cfg.ClientQPS < 0 {
		return fmt.Errorf("invalid client QPS value %f", cfg.ClientQPS)
	}

	return nil
}

func (f *factory) ClientConfig() (*rest.Config, error) {
	return f.config(f.KubeConfig, f.agentBaseName, f.ClientQPS, f.ClientBurst)
}

func (f *factory) KahuClient() (clientset.Interface, error) {
	clientConfig, err := f.ClientConfig()
	if err != nil {
		return nil, err
	}

	client, err := clientset.NewForConfig(clientConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

func (f *factory) KubeClient() (kubernetes.Interface, error) {
	clientConfig, err := f.ClientConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

func (f *factory) DynamicClient() (dynamic.Interface, error) {
	clientConfig, err := f.ClientConfig()
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

// Config returns a *rest.Config, using either the kubeconfig (if specified) or an in-cluster
// configuration.
func (f *factory) config(kubeConfig, baseName string, qps float32, burst int) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = kubeConfig
	configOverrides := &clientcmd.ConfigOverrides{}
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	clientConfig, err := config.ClientConfig()
	if err != nil {
		return nil, errors.Wrap(err, "error finding Kubernetes API server config in "+
			"--kubeconfig, $KUBECONFIG, or in-cluster configuration")
	}

	if qps > 0.0 {
		clientConfig.QPS = qps
	}
	if burst > 0 {
		clientConfig.Burst = burst
	}

	clientConfig.UserAgent = buildUserAgent(
		baseName,
		runtime.GOOS,
		runtime.GOARCH,
	)

	return clientConfig, nil
}

// buildUserAgent builds a User-Agent string from given args.
func buildUserAgent(agentBaseName, os, arch string) string {
	return fmt.Sprintf(
		"%s (%s/%s)", agentBaseName, os, arch)
}
