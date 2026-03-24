package messaging

import (
	"fmt"
	"time"
)

// PostprocessEnhancer adds a second layer of metadata after DefaultEnhancer:
//   - char_count:             character count of Text
//   - postprocessed_at:       UTC RFC3339 timestamp of when postprocessing ran
//   - postprocessor_version:  semver tag for the postprocessing logic
func PostprocessEnhancer(in Envelope) (Envelope, error) {
	out := in
	if out.Metadata == nil {
		out.Metadata = make(map[string]string)
	}
	out.Metadata["char_count"] = fmt.Sprintf("%d", len(in.Text))
	out.Metadata["postprocessed_at"] = time.Now().UTC().Format(time.RFC3339)
	out.Metadata["postprocessor_version"] = "1.0.0"
	return out, nil
}
