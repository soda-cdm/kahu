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

type factory struct {
	agentBaseName string
	kubeConfig    string
	clientQPS     float32
	clientBurst   int
}

// NewFactory returns a Factory.
func NewFactory(agentBaseName string) Factory {
	f := &factory{
		agentBaseName: agentBaseName,
		clientQPS:     defaultClientQPS,
		clientBurst:   defaultClientBurst,
	}

	return f
}

func (f *factory) AddFlags(fs *pflag.FlagSet) {
	fs.StringVarP(&f.kubeConfig, "kubeconfig", "k", f.kubeConfig,
		"Path to the kubeconfig file to use to talk to the Kubernetes apiserver. "+
			"If unset, try the environment variable KUBECONFIG, as well as in-cluster configuration")
	fs.Float32VarP(&f.clientQPS, "clientqps", "q", f.clientQPS,
		"Indicates the maximum QPS in kubernetes client")
	fs.IntVarP(&f.clientBurst, "clientburst", "b", f.clientBurst,
		"Maximum burst for throttle in Kubernetes client")
}

func (f *factory) ClientConfig() (*rest.Config, error) {
	return f.config(f.kubeConfig, f.agentBaseName, f.clientQPS, f.clientBurst)
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
