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
	"fmt"

	"google.golang.org/grpc"

	providerSvc "github.com/soda-cdm/kahu/providers/lib/go"
)

type ProviderInfo struct {
	provider string
	version  string
	manifest map[string]string
}

// GetProviderInfo returns information of the provider being registered.
func GetProviderInfo(ctx context.Context, conn *grpc.ClientConnInterface) (ProviderInfo, error) {
	providerInfo := ProviderInfo{}

	client := providerSvc.NewIdentityClient(*conn)

	req := providerSvc.GetProviderInfoRequest{}
	rsp, err := client.GetProviderInfo(ctx, &req)
	if err != nil {
		return providerInfo, err
	}

	provider := rsp.GetProvider()
	if "" == provider {
		return providerInfo, fmt.Errorf("provider name is empty")
	}
	providerInfo.provider = provider

	version := rsp.GetVersion()
	if "" == version {
		return providerInfo, fmt.Errorf("version is empty")
	}
	providerInfo.version = version
	providerInfo.manifest = rsp.GetManifest()

	return providerInfo, nil
}

// ProviderCapabilitySet is set of provider capabilities. Only supported capabilities are in the map.
type ProviderCapabilitySet []string

// GetProviderCapabilities returns set of supported capabilities of provider.
func GetProviderCapabilities(ctx context.Context, conn *grpc.ClientConnInterface) (ProviderCapabilitySet, error) {
	client := providerSvc.NewIdentityClient(*conn)
	caps := ProviderCapabilitySet{}
	req := providerSvc.GetProviderCapabilitiesRequest{}
	rsp, err := client.GetProviderCapabilities(ctx, &req)
	if err != nil {
		return caps, err
	}
	for _, capability := range rsp.GetCapabilities() {
		if capability == nil {
			continue
		}
		srv := capability.GetService()
		if srv == nil {
			continue
		}
		capabilityType := int32(srv.GetType())
		capabilityName := providerSvc.ProviderCapability_Service_Type_name[capabilityType]
		caps = append(caps, capabilityName)
	}
	return caps, nil
}
