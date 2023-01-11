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
package util

import "github.com/google/uuid"

func GenerateUniqueName(names ...string) string {
	prefix := ""
	id, err := uuid.NewRandom()
	if err != nil {
		id = uuid.UUID([16]byte{1, 2, 3, 4, 5, 6, 7, 8})
	}
	for _, name := range names {
		if prefix == "" {
			prefix = name
		} else {
			prefix = prefix + "-" + name
		}
	}

	if prefix == "" {
		return id.String()
	}

	return prefix + "-" + id.String()
}
