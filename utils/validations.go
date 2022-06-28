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

package utils

import (
	"strings"

	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/sets"
)

func ValidateIncludesExcludes(includesList, excludesList []string) []error {
	var errs []error

	includeRes := sets.NewString(includesList...)
	excludeRes := sets.NewString(excludesList...)

	if includeRes.Len() == 1 && includeRes.Has("*") {
		errs = append(errs, errors.New("IncludeList must be either empty or only '*'"))
	}

	if excludeRes.Has("*") {
		errs = append(errs, errors.New("excludeList cannot contain '*'"))
	}

	for _, resource := range excludeRes.List() {
		if includeRes.Has(resource) {
			errs = append(errs, errors.Errorf("same resource can not be in iclude and exlude list: %v", resource))
		}
	}

	return errs
}

func ValidateNamespace(includesList, excludesList []string) []error {
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
