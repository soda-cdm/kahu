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

package identity

import (
	"context"

	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuClient "github.com/soda-cdm/kahu/client"
)

const (
	// MetaService component name
	agentBaseName = "kahu-provider-identity"
)

// registerProvider creates CRD entry on behalf of the provider getting added.
func registerProvider(ctx context.Context, conn *grpc.ClientConnInterface, providerType apiv1.ProviderType) (*apiv1.Provider, error) {
	providerInfo, err := GetProviderInfo(ctx, conn)
	if err != nil {
		return nil, err
	}

	providerCapabilities, err := GetProviderCapabilities(ctx, conn)
	if err != nil {
		return nil, err
	}

	provider, err := createProviderCR(providerInfo, providerType, providerCapabilities)
	return provider, err

}

// createProviderCR creates CRD entry on behalf of the provider getting added.
func createProviderCR(
	providerInfo ProviderInfo,
	providerType apiv1.ProviderType,
	providerCapabilities map[string]bool) (*apiv1.Provider, error) {
	cfg := kahuClient.NewFactoryConfig()
	clientFactory := kahuClient.NewFactory(agentBaseName, cfg)
	client, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	// Create provider CRD as it is not found and update the status
	provider, err := client.KahuV1beta1().Providers().Get(context.TODO(), providerInfo.provider, metav1.GetOptions{})
	if err == nil {
		return provider, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	provider = &apiv1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerInfo.provider,
		},
		Spec: apiv1.ProviderSpec{
			Version:      providerInfo.version,
			Type:         providerType,
			Manifest:     providerInfo.manifest,
			Capabilities: providerCapabilities,
		},
	}

	provider, err = client.KahuV1beta1().Providers().Create(context.TODO(), provider, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	provider.Status.State = apiv1.ProviderStateAvailable

	provider, err = client.KahuV1beta1().Providers().UpdateStatus(context.TODO(), provider, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return provider, nil
}
