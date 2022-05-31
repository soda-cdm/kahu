// Copyright 2022 The SODA Authors.
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

package utils

import (
	"fmt"
	"strings"

	"github.com/gobwas/glob"
	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/sets"
)

type globStringSet struct {
	sets.String
}

func newGlobStringSet() globStringSet {
	return globStringSet{sets.NewString()}
}

func (gss globStringSet) match(match string) bool {
	for _, item := range gss.List() {
		g, err := glob.Compile(item)
		if err != nil {
			return false
		}
		if g.Match(match) {
			return true
		}
	}
	return false
}

type IncludesExcludes struct {
	includes globStringSet
	excludes globStringSet
}

func NewIncludesExcludes() *IncludesExcludes {
	return &IncludesExcludes{
		includes: newGlobStringSet(),
		excludes: newGlobStringSet(),
	}
}

func (ie *IncludesExcludes) Includes(includes ...string) *IncludesExcludes {
	ie.includes.Insert(includes...)
	return ie
}

// GetIncludes returns the items in the includes list
func (ie *IncludesExcludes) GetIncludes() []string {
	return ie.includes.List()
}

// Excludes adds items to the excludes list
func (ie *IncludesExcludes) Excludes(excludes ...string) *IncludesExcludes {
	ie.excludes.Insert(excludes...)
	return ie
}

// GetExcludes returns the items in the excludes list
func (ie *IncludesExcludes) GetExcludes() []string {
	return ie.excludes.List()
}

func (ie *IncludesExcludes) ShouldInclude(s string) bool {
	if ie.excludes.match(s) {
		return false
	}

	// len=0 means include everything
	return ie.includes.Len() == 0 || ie.includes.Has("*") || ie.includes.match(s)
}

func (ie *IncludesExcludes) IncludesString() string {
	return asString(ie.GetIncludes(), "*")
}

func (ie *IncludesExcludes) ExcludesString() string {
	return asString(ie.GetExcludes(), "<none>")
}

func asString(in []string, empty string) string {
	if len(in) == 0 {
		return empty
	}
	return strings.Join(in, ", ")
}

func (ie *IncludesExcludes) IncludeEverything() bool {
	return ie.excludes.Len() == 0 && (ie.includes.Len() == 0 || (ie.includes.Len() == 1 && ie.includes.Has("*")))
}

func ValidateIncludesExcludes(includesList, excludesList []string) []error {
	var errs []error

	includes := sets.NewString(includesList...)
	excludes := sets.NewString(excludesList...)

	if includes.Len() > 1 && includes.Has("*") {
		errs = append(errs, errors.New("includes list must either contain '*' only, or a non-empty list of items"))
	}

	if excludes.Has("*") {
		errs = append(errs, errors.New("excludes list cannot contain '*'"))
	}

	for _, itm := range excludes.List() {
		if includes.Has(itm) {
			errs = append(errs, errors.Errorf("excludes list cannot contain an item in the includes list: %v", itm))
		}
	}

	return errs
}

func ValidateNamespaceIncludesExcludes(includesList, excludesList []string) []error {
	errs := ValidateIncludesExcludes(includesList, excludesList)

	includes := sets.NewString(includesList...)
	excludes := sets.NewString(excludesList...)

	for _, itm := range includes.List() {
		if nsErrs := validateNamespaceName(itm); nsErrs != nil {
			errs = append(errs, nsErrs...)
		}
	}
	for _, itm := range excludes.List() {
		if nsErrs := validateNamespaceName(itm); nsErrs != nil {
			errs = append(errs, nsErrs...)
		}
	}

	fmt.Println("includeList namespace  errs:", errs)
	return errs
}

func validateNamespaceName(ns string) []error {
	var errs []error

	if ns == "" {
		return nil
	}

	tmpNamespace := strings.ReplaceAll(ns, "*", "x")
	if errMsgs := validation.ValidateNamespaceName(tmpNamespace, false); errMsgs != nil {
		for _, msg := range errMsgs {
			errs = append(errs, errors.Errorf("invalid namespace %q: %s", ns, msg))
		}
	}

	return errs
}

func GenerateIncludesExcludes(includes, excludes []string, mapFunc func(string) string) *IncludesExcludes {
	res := NewIncludesExcludes()

	for _, item := range includes {
		if item == "*" {
			res.Includes(item)
			continue
		}

		key := mapFunc(item)
		if key == "" {
			continue
		}
		res.Includes(key)
	}

	for _, item := range excludes {
		// wildcards are invalid for excludes,
		// so ignore them.
		if item == "*" {
			continue
		}

		key := mapFunc(item)
		if key == "" {
			continue
		}
		res.Excludes(key)
	}

	return res
}
