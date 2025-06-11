package compositions

import (
	types "resource-tree-handler/apis"
	"testing"
)

func TestShouldItSkip(t *testing.T) {
	// Match ApiVersion
	exclude := types.Exclude{
		ApiVersion: "^widgets\\.templates\\.krateo\\.io.+",
		Resource:   "",
		Name:       "",
	}
	managedResource := types.Reference{
		ApiVersion: "widgets.templates.krateo.io/v1beta1",
		Name:       "test-composition-values-panel",
		Namespace:  "fireworksapp-system",
		Resource:   "fireworksapps",
		Kind:       "FireworksApp",
	}
	if !ShouldItSkip(exclude, managedResource) {
		t.Fail()
	}

	// Match Name
	exclude = types.Exclude{
		ApiVersion: "",
		Resource:   "",
		Name:       "test-composition-values-panel",
	}
	managedResource = types.Reference{
		ApiVersion: "widgets.templates.krateo.io/v1beta1",
		Name:       "test-composition-values-panel",
		Namespace:  "fireworksapp-system",
		Resource:   "fireworksapps",
		Kind:       "FireworksApp",
	}
	if !ShouldItSkip(exclude, managedResource) {
		t.Fail()
	}

	// Name match with resource mismatch
	exclude = types.Exclude{
		ApiVersion: "",
		Resource:   "fireworksapps-fail",
		Name:       "test-composition-values-panel",
	}
	managedResource = types.Reference{
		ApiVersion: "widgets.templates.krateo.io/v1beta1",
		Name:       "test-composition-values-panel",
		Namespace:  "fireworksapp-system",
		Resource:   "fireworksapps",
		Kind:       "FireworksApp",
	}
	if ShouldItSkip(exclude, managedResource) {
		t.Fail()
	}

	// Partial Regex Match
	exclude = types.Exclude{
		ApiVersion: "^widgets\\.templates\\.krateo\\.io",
		Resource:   "",
		Name:       "",
	}
	managedResource = types.Reference{
		ApiVersion: "widgets.templates.krateo.io/v1beta1",
		Name:       "test-composition-values-panel",
		Namespace:  "fireworksapp-system",
		Resource:   "fireworksapps",
		Kind:       "FireworksApp",
	}
	if ShouldItSkip(exclude, managedResource) {
		t.Fail()
	}

	// Full Regex Match with name mismatch
	exclude = types.Exclude{
		ApiVersion: "^widgets\\.templates\\.krateo\\.io",
		Resource:   "",
		Name:       "test-composition-values-panel-fail",
	}
	managedResource = types.Reference{
		ApiVersion: "widgets.templates.krateo.io/v1beta1",
		Name:       "test-composition-values-panel",
		Namespace:  "fireworksapp-system",
		Resource:   "fireworksapps",
		Kind:       "FireworksApp",
	}
	if ShouldItSkip(exclude, managedResource) {
		t.Fail()
	}
}
