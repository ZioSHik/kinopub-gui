package kinopub

import (
	"fmt"
	"reflect"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// validateDependencies checks that no required field in the Dependencies struct is nil.
// Returns ErrMissingDependency wrapping the field name if any required dependency is absent.
// HLSDownloader and PageScraper are optional (nil is valid).
func validateDependencies(deps Dependencies) error {
	v := reflect.ValueOf(deps)
	t := v.Type()

	for i := range t.NumField() {
		field := v.Field(i)
		fieldName := t.Field(i).Name

		// Skip optional fields.
		if fieldName == "HLSDownloader" || fieldName == "PageScraper" || fieldName == "AudioChooser" {
			continue
		}

		if field.Kind() == reflect.Interface && field.IsNil() {
			return fmt.Errorf("%w: %s", domain.ErrMissingDependency, fieldName)
		}
	}
	return nil
}
