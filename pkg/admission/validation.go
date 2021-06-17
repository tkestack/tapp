/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package admission

import (
	"strconv"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/intstr"
	validation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func validate(tapp *tappv1.TApp) error {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateTAppSpec(&tapp.Spec, field.NewPath("spec"))...)
	if len(allErrs) > 0 {
		return allErrs.ToAggregate()
	}
	return nil
}

func validateTAppSpec(spec *tappv1.TAppSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateTAppUpdateStrategy(&spec.UpdateStrategy, fldPath.Child("updateStrategy"))...)

	return allErrs
}

func validateTAppUpdateStrategy(strategy *tappv1.TAppUpdateStrategy, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if strategy.MaxUnavailable != nil {
		allErrs = append(allErrs, validateIntOrPercent(*strategy.MaxUnavailable, fldPath.Child("maxUnavailable"))...)
	}
	if strategy.ForceUpdate.MaxUnavailable != nil {
		allErrs = append(allErrs, validateIntOrPercent(*strategy.ForceUpdate.MaxUnavailable, fldPath.Child("forceUpdate").Child("maxUnavailable"))...)
	}

	return allErrs
}

func validateIntOrPercent(intOrPercent intstr.IntOrString, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, validatePositiveIntOrPercent(intOrPercent, fldPath)...)
	allErrs = append(allErrs, isNotMoreThan100Percent(intOrPercent, fldPath)...)
	allErrs = append(allErrs, isNotZero(intOrPercent, fldPath)...)
	return allErrs
}

// validatePositiveIntOrPercent tests if a given value is a valid int or percentage.
func validatePositiveIntOrPercent(intOrPercent intstr.IntOrString, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	switch intOrPercent.Type {
	case intstr.String:
		for _, msg := range validation.IsValidPercent(intOrPercent.StrVal) {
			allErrs = append(allErrs, field.Invalid(fldPath, intOrPercent, msg))
		}
	case intstr.Int:
		allErrs = append(allErrs, apimachineryvalidation.ValidateNonnegativeField(int64(intOrPercent.IntValue()), fldPath)...)
	default:
		allErrs = append(allErrs, field.Invalid(fldPath, intOrPercent, "must be an integer or percentage (e.g '5%')"))
	}
	return allErrs
}

// isNotMoreThan100Percent tests is a value can be represented as a percentage and if this value is not more than 100%.
func isNotMoreThan100Percent(intOrStringValue intstr.IntOrString, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	value, isPercent := getPercentValue(intOrStringValue)
	if !isPercent || value <= 100 {
		return nil
	}
	allErrs = append(allErrs, field.Invalid(fldPath, intOrStringValue, "must not be greater than 100%"))
	return allErrs
}

// isNotZero tests if a given value is not zero
func isNotZero(intOrStringValue intstr.IntOrString, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	value, isPercent := getPercentValue(intOrStringValue)
	if (!isPercent && intOrStringValue.IntVal == 0) || (isPercent && value == 0) {
		allErrs = append(allErrs, field.Invalid(fldPath, intOrStringValue, "must not be zero"))
	}
	return allErrs
}

func getPercentValue(intOrStringValue intstr.IntOrString) (int, bool) {
	if intOrStringValue.Type != intstr.String {
		return 0, false
	}
	if len(validation.IsValidPercent(intOrStringValue.StrVal)) != 0 {
		return 0, false
	}
	value, _ := strconv.Atoi(intOrStringValue.StrVal[:len(intOrStringValue.StrVal)-1])
	return value, true
}
